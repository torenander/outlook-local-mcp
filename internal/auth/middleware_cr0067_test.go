package auth

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/mark3labs/mcp-go/mcp"
)

// accountRequest builds a call to the account aggregate tool, which the
// middleware treats as authentication recovery surface.
func accountRequest() mcp.CallToolRequest {
	var req mcp.CallToolRequest
	req.Params.Name = "account"
	return req
}

// calendarRequest builds a call to an ordinary domain tool.
func calendarRequest() mcp.CallToolRequest {
	var req mcp.CallToolRequest
	req.Params.Name = "calendar"
	return req
}

// TestMiddleware_PendingAuth_BlocksOrdinaryVerb keeps the pre-existing
// behaviour for non-recovery verbs: while a background flow runs they are told
// to wait.
func TestMiddleware_PendingAuth_BlocksOrdinaryVerb(t *testing.T) {
	state := newTestStateWithMethod(func(_ context.Context, _ Authenticator, _ string, _ []string) (azidentity.AuthenticationRecord, error) {
		return testRecord(), nil
	}, "device_code")
	state.preAuthenticated.Store(true)
	state.begin() // publish an attempt that never finishes

	called := false
	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		called = true
		return successResult(), nil
	}

	wrapped := buildFullMiddleware(state, handler)
	result, err := wrapped(context.Background(), calendarRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("ordinary verb should not run while authentication is pending")
	}
	if !strings.Contains(extractResultText(result), "still in progress") {
		t.Errorf("result = %q, want the pending-auth message", extractResultText(result))
	}
}

// TestMiddleware_PendingAuth_AllowsAccountVerb is CR-0067 A4: the verbs that
// repair authentication must stay reachable while a flow is outstanding.
func TestMiddleware_PendingAuth_AllowsAccountVerb(t *testing.T) {
	state := newTestStateWithMethod(func(_ context.Context, _ Authenticator, _ string, _ []string) (azidentity.AuthenticationRecord, error) {
		return testRecord(), nil
	}, "device_code")
	state.preAuthenticated.Store(true)
	state.begin() // publish an attempt that never finishes

	called := false
	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		called = true
		return successResult(), nil
	}

	wrapped := buildFullMiddleware(state, handler)
	result, err := wrapped(context.Background(), accountRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("account verb must run while authentication is pending")
	}
	if result == nil || result.IsError {
		t.Errorf("account verb result = %+v, want the handler's own result", result)
	}
}

// TestMiddleware_FreshCredential_AllowsAccountVerb is the other half of A4:
// a cold credential must not divert account verbs into an auth prompt, or the
// user cannot even list which accounts need attention.
func TestMiddleware_FreshCredential_AllowsAccountVerb(t *testing.T) {
	authCalled := false
	state := newTestStateWithMethod(func(_ context.Context, _ Authenticator, _ string, _ []string) (azidentity.AuthenticationRecord, error) {
		authCalled = true
		return testRecord(), nil
	}, "device_code")

	called := false
	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		called = true
		return successResult(), nil
	}

	wrapped := buildFullMiddleware(state, handler)
	if _, err := wrapped(context.Background(), accountRequest()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("account verb must run against a fresh credential")
	}
	if authCalled {
		t.Error("account verb must not trigger an authentication flow")
	}
}

// TestMiddleware_FreshCredential_SilentRefreshSkipsPrompt is CR-0067 A1: when
// the token cache can still satisfy the request, no interactive flow starts.
func TestMiddleware_FreshCredential_SilentRefreshSkipsPrompt(t *testing.T) {
	authCalled := false
	state := newTestStateWithMethod(func(_ context.Context, _ Authenticator, _ string, _ []string) (azidentity.AuthenticationRecord, error) {
		authCalled = true
		return testRecord(), nil
	}, "auth_code")
	state.cred = &silentCred{}

	called := false
	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		called = true
		return successResult(), nil
	}

	wrapped := buildFullMiddleware(state, handler)
	result, err := wrapped(context.Background(), calendarRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler should run after a successful silent refresh")
	}
	if authCalled {
		t.Error("no interactive authentication should be started after a silent refresh")
	}
	if result == nil || result.IsError {
		t.Errorf("result = %+v, want the handler's own result", result)
	}
	if !state.authenticated.Load() {
		t.Error("a successful silent refresh should mark the middleware authenticated")
	}
}

// TestHandleAuthError_SilentRefreshRetries verifies that a silent refresh in
// the auth-error path retries the original call instead of prompting.
func TestHandleAuthError_SilentRefreshRetries(t *testing.T) {
	authCalled := false
	state := newTestStateWithMethod(func(_ context.Context, _ Authenticator, _ string, _ []string) (azidentity.AuthenticationRecord, error) {
		authCalled = true
		return testRecord(), nil
	}, "auth_code")
	state.preAuthenticated.Store(true)
	state.cred = &silentCred{}

	callCount := 0
	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		callCount++
		if callCount == 1 {
			return nil, fmt.Errorf("AuthCodeCredential: authentication required")
		}
		return successResult(), nil
	}

	wrapped := buildFullMiddleware(state, handler)
	result, err := wrapped(context.Background(), calendarRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if authCalled {
		t.Error("interactive authentication should not run when the cache still has a token")
	}
	if callCount != 2 {
		t.Errorf("handler call count = %d, want 2 (original + retry)", callCount)
	}
	if result == nil || result.IsError {
		t.Errorf("result = %+v, want the retried handler result", result)
	}
}

// TestAccountResolver_ReportsAccountToAuthMiddleware is CR-0067 A6: because
// AuthMiddleware wraps AccountResolver, the resolver must hand the resolved
// account back through the slot or re-authentication targets the wrong
// credential.
func TestAccountResolver_ReportsAccountToAuthMiddleware(t *testing.T) {
	closureCred := &trackingAuthenticator{mock: &mockAuthenticator{record: testRecord()}}
	accountCred := &trackingAuthenticator{mock: &mockAuthenticator{record: testRecord()}}

	registry := NewAccountRegistry()
	if err := registry.Add(&AccountEntry{
		Label:          "work",
		AuthMethod:     "browser",
		Authenticator:  accountCred,
		AuthRecordPath: "/tmp/work_auth_record.json",
		Authenticated:  true,
	}); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}

	state := newTestStateWithMethod(func(ctx context.Context, cred Authenticator, _ string, _ []string) (azidentity.AuthenticationRecord, error) {
		return cred.Authenticate(ctx, nil)
	}, "browser")
	state.cred = closureCred
	state.preAuthenticated.Store(true)

	callCount := 0
	inner := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		callCount++
		if callCount == 1 {
			return nil, fmt.Errorf("InteractiveBrowserCredential: expired token")
		}
		return successResult(), nil
	}

	resolver := AccountResolver(registry)
	wrapped := buildFullMiddleware(state, resolver(inner))

	if _, err := wrapped(context.Background(), calendarRequest()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !accountCred.called.Load() {
		t.Error("re-authentication should target the account the tool call resolved to")
	}
	if closureCred.called.Load() {
		t.Error("re-authentication must not fall back to the default closure credential")
	}
}

// TestInferAuthMethod_DeviceCodeEntry verifies that a device_code account is
// not misreported as browser, which would select the wrong recovery flow once
// the slot wiring of A6 is in place.
func TestInferAuthMethod_DeviceCodeEntry(t *testing.T) {
	tests := []struct {
		name  string
		entry *AccountEntry
		want  string
	}{
		{"persisted device_code", &AccountEntry{AuthMethod: "device_code"}, "device_code"},
		{"persisted auth_code", &AccountEntry{AuthMethod: "auth_code"}, "auth_code"},
		{"persisted browser", &AccountEntry{AuthMethod: "browser"}, "browser"},
		{"unset falls back to credential inspection", &AccountEntry{Authenticator: &mockAuthenticator{}}, "browser"},
		{"unset with device code credential", &AccountEntry{Authenticator: &azidentity.DeviceCodeCredential{}}, "device_code"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inferAuthMethod(tt.entry); got != tt.want {
				t.Errorf("inferAuthMethod() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPendingOutcome verifies the race-free reporting of a background attempt.
func TestPendingOutcome(t *testing.T) {
	state := &authMiddlewareState{}

	if running, err := state.pendingOutcome(); running || err != nil {
		t.Errorf("pendingOutcome() with no attempt = (%v, %v), want (false, nil)", running, err)
	}

	attempt := state.begin()
	if running, _ := state.pendingOutcome(); !running {
		t.Error("pendingOutcome() should report running for an unfinished attempt")
	}

	want := fmt.Errorf("boom")
	attempt.finish(want)
	running, got := state.pendingOutcome()
	if running {
		t.Error("pendingOutcome() should not report running after finish")
	}
	if got != want {
		t.Errorf("pendingOutcome() err = %v, want %v", got, want)
	}
}

// TestClassifyAuthError_PreservesDeviceCodeDetail is CR-0067 A5: the generic
// "authentication required" branch used to swallow the flow-specific
// explanation, leaving the user with no idea what actually failed.
func TestClassifyAuthError_PreservesDeviceCodeDetail(t *testing.T) {
	got := classifyAuthError(fmt.Errorf("authentication required: device code prompt was not received from Entra ID"))
	if !strings.Contains(got, "device code prompt was not received") {
		t.Errorf("classifyAuthError() = %q, want the device code detail preserved", got)
	}
}

// TestClassifyAuthError_BareAuthRequired verifies the detail-free signal still
// produces the short message.
func TestClassifyAuthError_BareAuthRequired(t *testing.T) {
	got := classifyAuthError(fmt.Errorf("AuthCodeCredential: authentication required"))
	if got != "Authentication is required for this account." {
		t.Errorf("classifyAuthError() = %q, want the bare authentication-required message", got)
	}
}

// TestRecoverySteps_MethodSpecific verifies that the guidance names the verb
// that actually applies and never tells the LLM to create a new account.
func TestRecoverySteps_MethodSpecific(t *testing.T) {
	tests := []struct {
		method     string
		wantSubstr string
	}{
		{"auth_code", `operation="complete_auth"`},
		{"device_code", "sign-in link"},
		{"browser", "browser window that opens"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got := FormatAuthErrorFor(fmt.Errorf("authentication required"), tt.method)
			if !strings.Contains(got, tt.wantSubstr) {
				t.Errorf("FormatAuthErrorFor(%q) = %q, want it to contain %q", tt.method, got, tt.wantSubstr)
			}
			if !strings.Contains(got, `operation="login"`) {
				t.Errorf("FormatAuthErrorFor(%q) = %q, want it to name the account login verb", tt.method, got)
			}
			if strings.Contains(got, `operation="add"`) {
				t.Errorf("FormatAuthErrorFor(%q) = %q, must not direct the LLM to create a new account", tt.method, got)
			}
		})
	}
}
