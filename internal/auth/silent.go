// Package auth silent token acquisition.
//
// This file isolates the "try the cache before bothering the user" step that
// the auth middleware performs before it starts any interactive flow
// (CR-0067 A1). It is deliberately small and separate from middleware.go so
// the safety rule below stays visible instead of being buried in a 500-line
// state machine.
package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// silentTokenTimeout bounds a silent token acquisition attempt. It mirrors
// silentLoginTimeout in internal/tools/login_account.go: long enough for a
// refresh-token round trip to Entra ID, short enough that a tool call does not
// visibly stall when the network is unavailable.
const silentTokenTimeout = 5 * time.Second

// SilentTokenCredential is the narrow capability the auth middleware needs in
// order to refresh an access token without any possibility of interrupting the
// user.
//
// It exists because azcore.TokenCredential alone is NOT safe for this purpose.
// azidentity's publicClient.GetToken tries AcquireTokenSilent first and then,
// unless DisableAutomaticAuthentication is set, falls through to reqToken — an
// interactive browser window for InteractiveBrowserCredential, or a fresh
// device code challenge for DeviceCodeCredential (azidentity@v1.13.1
// public_client.go:135-156). Probing such a credential speculatively would pop
// a browser window or emit an unusable device code on every tool call, which
// is the exact friction this CR removes.
//
// Credentials therefore opt in explicitly by implementing SilentOnly.
// *AuthCodeCredential does so natively: its GetToken calls AcquireTokenSilent
// and returns an "authentication required" error on miss.
type SilentTokenCredential interface {
	// GetToken acquires an access token for the requested scopes without any
	// user interaction, returning an error when the cache cannot satisfy the
	// request.
	GetToken(ctx context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error)

	// SilentOnly is a marker asserting that GetToken never escalates to an
	// interactive authentication flow. It has no behaviour; implementing it is
	// a promise about GetToken.
	SilentOnly()
}

// silentTokenAcquirer is the token-acquisition half of SilentTokenCredential,
// used for credentials whose silent-only guarantee comes from how this package
// constructs them rather than from a method they declare.
type silentTokenAcquirer interface {
	GetToken(ctx context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error)
}

// silentOnlyAcquirer returns cred's token-acquisition interface when cred is
// known not to escalate to an interactive flow, or nil when it is not.
//
// Two things make a credential eligible:
//
//   - It implements SilentTokenCredential, promising the guarantee itself.
//     *AuthCodeCredential does this.
//   - It is one of the azidentity credentials that this package constructs.
//     setupBrowserCredential and setupDeviceCodeCredential both set
//     DisableAutomaticAuthentication: true, which makes GetToken return
//     azidentity.AuthenticationRequiredError on a cache miss instead of
//     prompting. Because the guarantee lives at the construction site and
//     azidentity's types are foreign (no method can be attached to them
//     without wrapping every credential in the system), it is asserted by type
//     here and pinned behaviourally by TestSetupCredential_GetTokenIsSilentOnly.
//
// Anything else returns nil: an unrecognised credential is assumed unsafe to
// probe, so adding a new credential type fails safe rather than surprising the
// user with a prompt.
//
// Parameters:
//   - cred: the credential to classify. May be nil.
//
// Returns the acquirer interface, or nil when cred is ineligible. No side
// effects.
func silentOnlyAcquirer(cred Authenticator) silentTokenAcquirer {
	switch c := cred.(type) {
	case SilentTokenCredential:
		return c
	case *azidentity.DeviceCodeCredential:
		return c
	case *azidentity.InteractiveBrowserCredential:
		return c
	default:
		return nil
	}
}

// TrySilentToken attempts a non-interactive token acquisition for cred.
//
// The attempt is skipped entirely — returning false without any network call —
// when cred is not known to be silent-only, because an unrecognised
// credential's GetToken may escalate to an interactive flow. See
// silentOnlyAcquirer for what qualifies.
//
// Parameters:
//   - ctx: the caller's context; the attempt is additionally bounded by
//     silentTokenTimeout so a hung network call cannot stall a tool call.
//   - cred: the credential to refresh. May be nil.
//   - scopes: the OAuth scopes to request, normally Scopes(cfg).
//
// Returns true when a token was acquired from the cache or by refresh-token
// exchange, false when the credential is ineligible, nil, or the acquisition
// failed.
//
// Side effects: may perform a token refresh round trip to Entra ID and update
// the credential's token cache. Never prompts the user.
func TrySilentToken(ctx context.Context, cred Authenticator, scopes []string) bool {
	if cred == nil {
		return false
	}
	silent := silentOnlyAcquirer(cred)
	if silent == nil {
		return false
	}

	silentCtx, cancel := context.WithTimeout(ctx, silentTokenTimeout)
	defer cancel()

	if _, err := silent.GetToken(silentCtx, policy.TokenRequestOptions{
		Scopes:    scopes,
		EnableCAE: true,
	}); err != nil {
		slog.Debug("silent token acquisition failed, interactive authentication required",
			"error", err)
		return false
	}

	slog.Debug("silent token acquisition succeeded, skipping interactive authentication")
	return true
}
