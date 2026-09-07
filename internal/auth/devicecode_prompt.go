// Package auth device code prompt plumbing.
//
// This file defines the structured device code prompt that travels from the
// azidentity UserPrompt callback back to whichever caller started the flow
// (the auth middleware or the add_account tool). Carrying the structured
// fields — rather than only the pre-rendered English sentence — lets callers
// build a direct link to the device sign-in page and quote the code
// separately, instead of relying on the user to parse both out of one
// sentence (CR-0067 A7).
package auth

import (
	"net/url"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// defaultDeviceLoginURL is the Entra ID device login page used when the
// credential does not supply a verification URL of its own. It accepts an
// "otc" (one-time code) query parameter, which is preserved through the
// redirect to the deviceauth page but does not pre-fill the code field; see
// SignInURL.
const defaultDeviceLoginURL = "https://microsoft.com/devicelogin"

// DeviceCodePrompt is the structured device code challenge forwarded from the
// azidentity UserPrompt callback to the caller that initiated authentication.
//
// azidentity.DeviceCodeMessage exposes only these three fields, so this type
// mirrors them exactly rather than inventing a richer shape. Callers that only
// need the human-readable sentence use Message; callers that want to present a
// clickable link use SignInURL, and must also display UserCode.
type DeviceCodePrompt struct {
	// Message is the full English instruction produced by Entra ID, for
	// example "To sign in, use a web browser to open the page
	// https://microsoft.com/devicelogin and enter the code ABCD1234 to
	// authenticate." It is the verbatim fallback text shown to clients that
	// cannot render elicitations (CR-0031).
	Message string

	// UserCode is the one-time code the user must supply on the verification
	// page, for example "ABCD1234". It must be shown to the user verbatim:
	// the sign-in page does not pre-fill it (see SignInURL).
	UserCode string

	// VerificationURL is the page the user must visit, normally
	// https://microsoft.com/devicelogin. Empty when Entra ID omits it, in
	// which case SignInURL falls back to defaultDeviceLoginURL.
	VerificationURL string
}

// NewDeviceCodePrompt converts an azidentity.DeviceCodeMessage into the
// structured prompt forwarded over the DeviceCodeMsgKey channel.
//
// Parameters:
//   - msg: the device code message supplied by azidentity's UserPrompt callback.
//
// Returns the equivalent DeviceCodePrompt. No side effects.
func NewDeviceCodePrompt(msg azidentity.DeviceCodeMessage) DeviceCodePrompt {
	return DeviceCodePrompt{
		Message:         msg.Message,
		UserCode:        msg.UserCode,
		VerificationURL: msg.VerificationURL,
	}
}

// SignInURL returns the device sign-in page to send the user to, carrying the
// user code in the "otc" query parameter.
//
// What this does and does not do, verified against a live device code on
// 2026-09-02: opening https://login.microsoft.com/device?otc=<code> redirects
// to https://login.microsoftonline.com/common/oauth2/deviceauth?otc=<code>,
// so the parameter survives the redirect rather than being stripped — but the
// page still renders "Enter code to allow access" with the Code field EMPTY.
// Microsoft does not pre-fill it. The user must still type the code.
//
// The URL is therefore a navigation shortcut only. It saves finding the right
// page, which is worth something because the device sign-in page is not an
// obvious URL, but callers MUST still show the user the code itself. The otc
// parameter is retained because it is the documented deep-link form and costs
// nothing, not because it currently has an observable effect.
//
// The base is VerificationURL when Entra ID supplied one, otherwise
// defaultDeviceLoginURL. When UserCode is empty the base URL is returned
// unchanged.
//
// Returns an absolute https URL suitable for MCP URL-mode elicitation. No
// side effects.
func (p DeviceCodePrompt) SignInURL() string {
	base := strings.TrimSpace(p.VerificationURL)
	if base == "" {
		base = defaultDeviceLoginURL
	}
	if p.UserCode == "" {
		return base
	}

	parsed, err := url.Parse(base)
	if err != nil {
		// A malformed verification URL from the identity provider is not
		// worth failing authentication over; fall back to the known-good page.
		parsed, err = url.Parse(defaultDeviceLoginURL)
		if err != nil {
			return defaultDeviceLoginURL
		}
	}

	q := parsed.Query()
	q.Set("otc", p.UserCode)
	parsed.RawQuery = q.Encode()
	return parsed.String()
}
