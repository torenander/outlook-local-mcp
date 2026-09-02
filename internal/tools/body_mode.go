// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file provides the shared helper that validates the body delivery mode
// on the mail read verbs that can return a message body (get_message and
// get_conversation). CR-0068 introduces this parameter as a dimension
// orthogonal to the three output tiers: `output` decides the *shape* of the
// response (text, summary JSON, raw Graph JSON) while `body_mode` decides how
// much of the message body it carries and in what form. Neither collapses into
// the other, which is why no fourth output tier was added.
package tools

import (
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// Body delivery modes accepted by the body-mode-aware mail read verbs.
const (
	// BodyModePreview is the default and preserves the CR-0033/CR-0043/CR-0051
	// token posture exactly: the response carries only Graph's bodyPreview, a
	// whitespace-normalised snippet capped at 255 characters. The `body`
	// property is never added to $select, so the server does not pay for a body
	// it was not asked for.
	BodyModePreview = "preview"

	// BodyModeText returns the complete message body as plain text. The
	// conversion is performed by Microsoft Graph in response to the
	// Prefer: outlook.body-content-type="text" request header; this codebase
	// never parses or sanitizes HTML itself (see CR-0019 on HTML handling
	// responsibility).
	BodyModeText = "text"

	// BodyModeFull returns the complete message body in its stored form, which
	// for most modern mail is HTML. It is the body half of what output=raw
	// delivers today, without the internet headers, conversationIndex, replyTo
	// and bcc fields that raw drags along with it.
	BodyModeFull = "full"
)

// bodyModeParam is the declared parameter name for the body delivery mode.
//
// It is deliberately not spelled `body`: the mail domain is one aggregate MCP
// tool whose input schema is the union of its verbs' parameters, and
// create_draft/update_draft already own `body` as a free-text draft content
// string. Because aggregateSchemaOptions resolves duplicate names first-verb-
// wins and get_message is registered before the draft verbs, declaring `body`
// here would replace the draft parameter's description and constrain it to the
// preview/text/full enum for every caller of the mail tool.
const bodyModeParam = "body_mode"

// bodyModeAliasParam is accepted at call time as a convenience spelling of
// bodyModeParam, because `body` is the name a caller reaches for first. It is
// never declared in a schema: the draft verbs already contribute `body` to the
// aggregate union, so a client that forwards declared arguments will pass it
// through untouched whenever mail management is enabled. Dropping the alias
// degrades to the documented default (a preview), never to a wrong body, and
// the declared bodyModeParam spelling always works regardless of gating.
const bodyModeAliasParam = "body"

// ValidateBodyMode extracts and validates the body delivery mode from an MCP
// tool request. It reads the declared `body_mode` parameter first and falls
// back to the `body` alias, returning BodyModePreview when neither is present.
//
// Parameters:
//   - request: the MCP tool call request containing optional arguments.
//
// Returns the validated body mode string, or an error naming the accepted
// values when the caller supplied something else.
//
// Side effects: none.
func ValidateBodyMode(request mcp.CallToolRequest) (string, error) {
	mode := request.GetString(bodyModeParam, "")
	if mode == "" {
		mode = request.GetString(bodyModeAliasParam, "")
	}
	switch mode {
	case "":
		return BodyModePreview, nil
	case BodyModePreview, BodyModeText, BodyModeFull:
		return mode, nil
	default:
		return "", fmt.Errorf("%s must be 'preview', 'text', or 'full'", bodyModeParam)
	}
}
