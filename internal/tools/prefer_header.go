// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file centralises construction of the Microsoft Graph `Prefer` request
// header. Graph carries several unrelated request preferences on that one
// header name (outlook.timezone, outlook.body-content-type, ...), and RFC 7240
// defines Prefer as a comma-separated list. The Kiota request-header collection
// stores repeated Add calls as a set and the HTTP adapter emits one header line
// per stored value, so calling Add twice yields two `Prefer:` lines in an
// arbitrary order rather than one combined value. Every preference this server
// sends therefore goes through newPreferHeaders, which joins them into a single
// header value (CR-0068).
package tools

import (
	"strings"

	abstractions "github.com/microsoft/kiota-abstractions-go"
)

// preferBodyContentTypeText is the Graph request preference that asks the
// service to convert a message body to plain text before returning it. Using
// it keeps HTML parsing on Microsoft's side of the wire: the server never
// takes on the responsibility of stripping or sanitizing markup, and no HTML
// dependency enters the module graph.
const preferBodyContentTypeText = `outlook.body-content-type="text"`

// bodyContentTypePreference maps a validated body mode to the Graph preference
// token it requires.
//
// Parameters:
//   - bodyMode: a mode returned by ValidateBodyMode.
//
// Returns preferBodyContentTypeText for BodyModeText and "" for every other
// mode, since preview needs no body at all and full wants the body exactly as
// Graph stores it.
//
// Side effects: none.
func bodyContentTypePreference(bodyMode string) string {
	if bodyMode == BodyModeText {
		return preferBodyContentTypeText
	}
	return ""
}

// newPreferHeaders builds a Kiota request-header collection carrying every
// supplied preference in one combined `Prefer` header value.
//
// Empty preference strings are dropped, so callers can pass the result of a
// conditional helper directly without guarding each argument.
//
// Parameters:
//   - prefs: zero or more RFC 7240 preference tokens, e.g.
//     `outlook.timezone="Europe/Stockholm"`.
//
// Returns nil when no non-empty preference was supplied, so callers can assign
// the result to a request configuration's Headers field only when there is
// something to send.
//
// Side effects: none.
func newPreferHeaders(prefs ...string) *abstractions.RequestHeaders {
	set := make([]string, 0, len(prefs))
	for _, p := range prefs {
		if p != "" {
			set = append(set, p)
		}
	}
	if len(set) == 0 {
		return nil
	}
	headers := abstractions.NewRequestHeaders()
	headers.Add("Prefer", strings.Join(set, ", "))
	return headers
}
