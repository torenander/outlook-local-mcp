// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for the account_refresh MCP tool (CR-0056), verifying
// that a successful GetToken call returns the new expiry (FR-27), that
// disconnected accounts are rejected with an actionable error, and that the
// refreshed expiry is surfaced in the tool response text.
package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/desek/outlook-local-mcp/internal/auth"
	"github.com/mark3labs/mcp-go/mcp"
)

// refreshMockCredential is a test-only azcore.TokenCredential that returns a
// preconfigured expiry (or error) from GetToken and records the last options
// passed so tests can assert EnableCAE and scopes.
type refreshMockCredential struct {
	expiry  time.Time
	err     error
	lastOpt policy.TokenRequestOptions
	calls   int
}

// GetToken returns the mock's preconfigured AccessToken or error. It records
// the received options for later assertions.
func (m *refreshMockCredential) GetToken(_ context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	m.calls++
	m.lastOpt = opts
	if m.err != nil {
		return azcore.AccessToken{}, m.err
	}
	return azcore.AccessToken{Token: "refreshed", ExpiresOn: m.expiry}, nil
}

// TestRefreshAccount_Success verifies that account_refresh issues a GetToken
// call with EnableCAE=true and the configured scopes, flips no state, and
// reports success in the plain-text response.
func TestRefreshAccount_Success(t *testing.T) {
	expiry := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	cred := &refreshMockCredential{expiry: expiry}
	registry := auth.NewAccountRegistry()
	if err := registry.Add(&auth.AccountEntry{
		Label:         "work",
		Authenticated: true,
		Credential:    cred,
	}); err != nil {
		t.Fatalf("registry.Add() error: %v", err)
	}

	cfg := addAccountTestConfig(t)
	handler := HandleRefreshAccount(registry, cfg)
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{"label": "work"}

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %v", result)
	}
	if cred.calls != 1 {
		t.Errorf("GetToken calls = %d, want 1", cred.calls)
	}
	if !cred.lastOpt.EnableCAE {
		t.Error("expected EnableCAE=true on refresh GetToken call")
	}
	if len(cred.lastOpt.Scopes) == 0 {
		t.Error("expected non-empty Scopes on refresh GetToken call")
	}

	text := extractText(t, result)
	if !strings.Contains(text, "refreshed") {
		t.Errorf("response text = %q, want to contain 'refreshed'", text)
	}
	if !strings.Contains(text, "work") {
		t.Errorf("response text = %q, want to contain label 'work'", text)
	}
}

// TestRefreshAccount_Disconnected verifies that account_refresh rejects
// disconnected accounts with an actionable error directing the user to
// account_login.
func TestRefreshAccount_Disconnected(t *testing.T) {
	registry := auth.NewAccountRegistry()
	if err := registry.Add(&auth.AccountEntry{
		Label:         "work",
		Authenticated: false,
	}); err != nil {
		t.Fatalf("registry.Add() error: %v", err)
	}

	handler := HandleRefreshAccount(registry, addAccountTestConfig(t))
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{"label": "work"}

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for disconnected account")
	}
	text := extractText(t, result)
	if !contains(text, "disconnected") {
		t.Errorf("error text = %q, want to contain 'disconnected'", text)
	}
	if !contains(text, "account_login") {
		t.Errorf("error text = %q, want to contain 'account_login'", text)
	}
}

// TestRefreshAccount_ExpiryInResponse verifies FR-27: the refreshed token's
// ExpiresOn value is surfaced in the tool response (both the expiry field and
// the human-readable message string).
func TestRefreshAccount_ExpiryInResponse(t *testing.T) {
	expiry := time.Date(2027, 1, 2, 15, 4, 5, 0, time.UTC)
	cred := &refreshMockCredential{expiry: expiry}
	registry := auth.NewAccountRegistry()
	if err := registry.Add(&auth.AccountEntry{
		Label:         "work",
		Authenticated: true,
		Credential:    cred,
	}); err != nil {
		t.Fatalf("registry.Add() error: %v", err)
	}

	handler := HandleRefreshAccount(registry, addAccountTestConfig(t))
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{"label": "work"}

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %v", result)
	}

	text := extractText(t, result)
	want := expiry.Format(time.RFC3339)
	if !strings.Contains(text, want) {
		t.Errorf("response text = %q, want to contain expiry %q", text, want)
	}
	if !strings.Contains(text, "New expiry") {
		t.Errorf("response text = %q, want to contain 'New expiry'", text)
	}
}

// TestRefreshAccount_GetTokenError verifies that a credential error is
// surfaced as an MCP tool error result rather than a handler error. Kept as a
// small sanity check alongside the primary cases.
func TestRefreshAccount_GetTokenError(t *testing.T) {
	cred := &refreshMockCredential{err: errors.New("boom")}
	registry := auth.NewAccountRegistry()
	if err := registry.Add(&auth.AccountEntry{
		Label:         "work",
		Authenticated: true,
		Credential:    cred,
	}); err != nil {
		t.Fatalf("registry.Add() error: %v", err)
	}

	handler := HandleRefreshAccount(registry, addAccountTestConfig(t))
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{"label": "work"}

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when GetToken fails")
	}
}

// TestRefreshAccount_DeclinesDuringInteractiveAuth verifies that account_refresh
// returns an actionable message instead of blocking when an interactive sign-in
// is already running (CR-0067 A4).
//
// Without this guard the handler calls GetToken on the same credential the
// sign-in is holding, and azidentity's per-client mutex is not context-aware,
// so the call hangs for the whole sign-in. account_refresh is one of the verbs
// the middleware's own recovery guidance names, so hanging it makes that
// guidance dead.
func TestRefreshAccount_DeclinesDuringInteractiveAuth(t *testing.T) {
	cred := &refreshMockCredential{expiry: time.Now().Add(time.Hour)}
	registry := auth.NewAccountRegistry()
	if err := registry.Add(&auth.AccountEntry{
		Label:         "work",
		Authenticated: true,
		Credential:    cred,
	}); err != nil {
		t.Fatalf("registry.Add() error: %v", err)
	}

	auth.BeginInteractiveAuth()
	defer auth.EndInteractiveAuth()

	handler := HandleRefreshAccount(registry, addAccountTestConfig(t))
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{"label": "work"}

	done := make(chan *mcp.CallToolResult, 1)
	go func() {
		result, err := handler(context.Background(), request)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		done <- result
	}()

	select {
	case result := <-done:
		if !result.IsError {
			t.Fatal("expected an error result while a sign-in is in progress")
		}
		text := extractText(t, result)
		if !strings.Contains(text, "sign-in is already in progress") {
			t.Errorf("result = %q, want it to explain that a sign-in is outstanding", text)
		}
		if cred.calls != 0 {
			t.Error("GetToken must not be called; it would block on the credential lock")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("account_refresh blocked while an interactive sign-in was in flight")
	}
}
