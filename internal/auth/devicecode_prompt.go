// Package auth device code prompt plumbing.
//
// This file defines the structured device code prompt that travels from the
// azidentity UserPrompt callback back to whichever caller started the flow
// (the auth middleware or the add_account tool). Carrying the structured
// fields — rather than only the pre-rendered English sentence — lets callers
// compose a one-click sign-in URL that pre-fills the user code, which is the
// difference between "read this code and retype it" and "click here"
// (CR-0067 A7).
package auth

import (
	"net/url"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// defaultDeviceLoginURL is the Entra ID device login page used when the
// credential does not supply a verification URL of its own. Entra ID accepts
// an "otc" (one-time code) query parameter on this page that pre-fills the
// user code field, removing the manual transcription step.
const defaultDeviceLoginURL = "https://microsoft.com/devicelogin"

// DeviceCodePrompt is the structured device code challenge forwarded from the
// azidentity UserPrompt callback to the caller that initiated authentication.
//
// azidentity.DeviceCodeMessage exposes only these three fields, so this type
// mirrors them exactly rather than inventing a richer shape. Callers that only
// need the human-readable sentence use Message; callers that want to present a
// clickable link use OneClickURL.
type DeviceCodePrompt struct {
	// Message is the full English instruction produced by Entra ID, for
	// example "To sign in, use a web browser to open the page
	// https://microsoft.com/devicelogin and enter the code ABCD1234 to
	// authenticate." It is the verbatim fallback text shown to clients that
	// cannot render elicitations (CR-0031).
	Message string

	// UserCode is the one-time code the user must supply on the verification
	// page, for example "ABCD1234". Used to build OneClickURL.
	UserCode string

	// VerificationURL is the page the user must visit, normally
	// https://microsoft.com/devicelogin. Empty when Entra ID omits it, in
	// which case OneClickURL falls back to defaultDeviceLoginURL.
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

// FallbackText returns the tool result text shown to clients that cannot
// render an elicitation — which, per CR-0031, is the only channel that reaches
// the user at all for clients such as Claude Code. Since device_code is the
// inferred default, this is the text most users will actually see, so it is
// worth more than the bare SDK sentence.
//
// The Entra ID message is reproduced verbatim and first, satisfying CR-0031
// FR-2. A one-click link with the code pre-filled is appended below it, which
// is strictly additive: a reader who ignores the extra line still has complete
// instructions. The link is omitted when there is no user code to embed, since
// a bare device login page adds nothing the message did not already say.
//
// Returns the tool result text. No side effects.
func (p DeviceCodePrompt) FallbackText() string {
	if p.UserCode == "" {
		return p.Message
	}
	return p.Message + "\n\nOr open this link to sign in with the code already filled in:\n" + p.OneClickURL()
}

// OneClickURL returns the device login URL with the user code pre-filled via
// the "otc" query parameter, so the user only has to approve the sign-in
// rather than transcribe a code.
//
// The base is VerificationURL when Entra ID supplied one, otherwise
// defaultDeviceLoginURL. When UserCode is empty the base URL is returned
// unchanged: a URL with an empty otc parameter would render a confusing empty
// code box.
//
// Returns an absolute https URL suitable for MCP URL-mode elicitation. No
// side effects.
func (p DeviceCodePrompt) OneClickURL() string {
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
