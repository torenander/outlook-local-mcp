package auth

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/desek/outlook-local-mcp/internal/config"
)

// silentCred is a credential that opts in to SilentTokenCredential and
// records whether GetToken was called.
type silentCred struct {
	called bool
	err    error
}

func (c *silentCred) GetToken(_ context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	c.called = true
	if c.err != nil {
		return azcore.AccessToken{}, c.err
	}
	return azcore.AccessToken{Token: "token"}, nil
}

func (c *silentCred) SilentOnly() {}

func (c *silentCred) Authenticate(_ context.Context, _ *policy.TokenRequestOptions) (azidentity.AuthenticationRecord, error) {
	return azidentity.AuthenticationRecord{}, nil
}

// escalatingCred has a GetToken method but does NOT implement SilentOnly,
// standing in for azidentity credentials whose GetToken falls through to an
// interactive flow.
type escalatingCred struct {
	called bool
}

func (c *escalatingCred) GetToken(_ context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	c.called = true
	return azcore.AccessToken{Token: "token"}, nil
}

func (c *escalatingCred) Authenticate(_ context.Context, _ *policy.TokenRequestOptions) (azidentity.AuthenticationRecord, error) {
	return azidentity.AuthenticationRecord{}, nil
}

// TestTrySilentToken_NilCredential verifies that a nil credential is handled
// without panicking.
func TestTrySilentToken_NilCredential(t *testing.T) {
	if TrySilentToken(context.Background(), nil, []string{"Calendars.ReadWrite"}) {
		t.Error("TrySilentToken(nil) = true, want false")
	}
}

// TestTrySilentToken_Success verifies that a credential opted in to
// SilentTokenCredential is asked for a token and reported as authenticated.
func TestTrySilentToken_Success(t *testing.T) {
	cred := &silentCred{}
	if !TrySilentToken(context.Background(), cred, []string{"Calendars.ReadWrite"}) {
		t.Error("TrySilentToken() = false, want true for a credential with a cached token")
	}
	if !cred.called {
		t.Error("GetToken should have been called")
	}
}

// TestTrySilentToken_Failure verifies that a cache miss is reported as a
// failure rather than surfacing an error.
func TestTrySilentToken_Failure(t *testing.T) {
	cred := &silentCred{err: fmt.Errorf("authentication required")}
	if TrySilentToken(context.Background(), cred, []string{"Calendars.ReadWrite"}) {
		t.Error("TrySilentToken() = true, want false when the cache misses")
	}
}

// TestTrySilentToken_SkipsEscalatingCredential is the safety guarantee of
// CR-0067 A1: credentials whose GetToken can escalate to an interactive flow
// must never be probed speculatively, because doing so opens a browser window
// or emits an unusable device code on every tool call.
func TestTrySilentToken_SkipsEscalatingCredential(t *testing.T) {
	cred := &escalatingCred{}
	if TrySilentToken(context.Background(), cred, []string{"Calendars.ReadWrite"}) {
		t.Error("TrySilentToken() = true, want false for a credential that may escalate")
	}
	if cred.called {
		t.Error("GetToken must NOT be called on a credential that can escalate to interactive auth")
	}
}

// TestAuthCodeCredential_ImplementsSilentTokenCredential pins the promise that
// the auth_code credential is safe to probe silently.
func TestAuthCodeCredential_ImplementsSilentTokenCredential(t *testing.T) {
	var _ SilentTokenCredential = (*AuthCodeCredential)(nil)
}

// TestSetupCredential_GetTokenIsSilentOnly pins the invariant that
// silentOnlyAcquirer relies on: every azidentity credential this package
// constructs has DisableAutomaticAuthentication set, so GetToken returns
// azidentity.AuthenticationRequiredError on a cache miss instead of escalating
// to an interactive flow (CR-0067 A1).
//
// AuthenticationRequiredError is returned only on the
// DisableAutomaticAuthentication branch of azidentity's publicClient.GetToken,
// so asserting on it is equivalent to asserting the option is set — which is
// otherwise unreadable, the SDK keeping it in an unexported field. The
// assertion is offline and immediate: the silent attempt fails locally because
// no account is cached, before any network call is made.
func TestSetupCredential_GetTokenIsSilentOnly(t *testing.T) {
	for _, method := range []string{"browser", "device_code"} {
		t.Run(method, func(t *testing.T) {
			cfg := config.Config{
				ClientID:       "d3590ed6-52b3-4102-aeff-aad2292ab01c",
				TenantID:       "common",
				AuthRecordPath: filepath.Join(t.TempDir(), "auth_record.json"),
				CacheName:      "test-cache-silent-" + method,
				AuthMethod:     method,
			}

			cred, authenticator, err := SetupCredential(cfg)
			if err != nil {
				t.Fatalf("SetupCredential() error: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			_, tokenErr := cred.GetToken(ctx, policy.TokenRequestOptions{
				Scopes: []string{"Calendars.ReadWrite"},
			})
			if tokenErr == nil {
				t.Fatal("GetToken() unexpectedly succeeded with an empty cache")
			}

			var authRequired *azidentity.AuthenticationRequiredError
			if !errors.As(tokenErr, &authRequired) {
				t.Fatalf("GetToken() error = %v (%T), want *azidentity.AuthenticationRequiredError; "+
					"DisableAutomaticAuthentication is probably not set, which means GetToken can "+
					"open a browser or emit a device code on a cache miss", tokenErr, tokenErr)
			}

			// The middleware must recognise this error, or the tool call would
			// fail instead of routing into the authentication flow.
			if !IsAuthError(tokenErr) {
				t.Errorf("IsAuthError(%v) = false, want true", tokenErr)
			}

			// And the credential must be eligible for speculative probing.
			if silentOnlyAcquirer(authenticator) == nil {
				t.Errorf("silentOnlyAcquirer() = nil for %s, want the credential to be probe-eligible", method)
			}
		})
	}
}

// TestSilentOnlyAcquirer_Eligibility documents which credentials may be probed
// speculatively. Unknown credentials must fail safe by being skipped.
func TestSilentOnlyAcquirer_Eligibility(t *testing.T) {
	browserCred, err := azidentity.NewInteractiveBrowserCredential(
		&azidentity.InteractiveBrowserCredentialOptions{
			ClientID: "d3590ed6-52b3-4102-aeff-aad2292ab01c", TenantID: "common",
			DisableAutomaticAuthentication: true,
		})
	if err != nil {
		t.Fatalf("NewInteractiveBrowserCredential() error: %v", err)
	}
	deviceCred, err := azidentity.NewDeviceCodeCredential(
		&azidentity.DeviceCodeCredentialOptions{
			ClientID: "d3590ed6-52b3-4102-aeff-aad2292ab01c", TenantID: "common",
			DisableAutomaticAuthentication: true,
		})
	if err != nil {
		t.Fatalf("NewDeviceCodeCredential() error: %v", err)
	}

	tests := []struct {
		name     string
		cred     Authenticator
		eligible bool
	}{
		{"auth code credential (native SilentOnly)", &silentCred{}, true},
		{"interactive browser credential", browserCred, true},
		{"device code credential", deviceCred, true},
		{"unknown credential", &escalatingCred{}, false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := silentOnlyAcquirer(tt.cred) != nil
			if got != tt.eligible {
				t.Errorf("silentOnlyAcquirer() eligible = %v, want %v", got, tt.eligible)
			}
		})
	}
}

// TestClassifyAuthError_AuthenticationRequiredError verifies that azidentity's
// AuthenticationRequiredError does not leak "Call Authenticate to authenticate
// a user interactively" — SDK-level advice the LLM cannot act on.
func TestClassifyAuthError_AuthenticationRequiredError(t *testing.T) {
	cred, err := azidentity.NewDeviceCodeCredential(
		&azidentity.DeviceCodeCredentialOptions{
			ClientID: "d3590ed6-52b3-4102-aeff-aad2292ab01c", TenantID: "common",
			DisableAutomaticAuthentication: true,
		})
	if err != nil {
		t.Fatalf("NewDeviceCodeCredential() error: %v", err)
	}

	_, tokenErr := cred.GetToken(context.Background(), policy.TokenRequestOptions{
		Scopes: []string{"Calendars.ReadWrite"},
	})
	if tokenErr == nil {
		t.Fatal("GetToken() unexpectedly succeeded")
	}

	got := classifyAuthError(tokenErr)
	if strings.Contains(got, "Call Authenticate") {
		t.Errorf("classifyAuthError() = %q, must not leak SDK-level advice", got)
	}
	if got != "Authentication is required for this account." {
		t.Errorf("classifyAuthError() = %q, want the plain authentication-required message", got)
	}
}
