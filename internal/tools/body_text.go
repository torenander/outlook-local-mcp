// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file holds the plain-text rendering helpers for message bodies shared by
// FormatMessageDetailText and FormatConversationText: reading the optional
// `body` field a body_mode escalation attaches to a message map, and detecting
// when the fallback bodyPreview was cut off by Graph's 255-character cap so the
// truncation can be stated in band rather than left silent (CR-0068).
package tools

import "unicode/utf8"

// bodyPreviewCapRunes is the number of characters Microsoft Graph returns in
// the bodyPreview property. Graph documents the property as "the first 255
// characters of the message body" and returns it whitespace-normalised, so a
// preview that lands exactly on the cap was cut rather than complete.
const bodyPreviewCapRunes = 255

// bodyPreviewTruncatedNotice is the in-band marker appended after a truncated
// preview in single-message text output. Before CR-0068 the preview simply
// stopped mid-word with nothing to distinguish "the message ends here" from
// "there are nine more paragraphs", and no parameter existed to fetch the rest.
const bodyPreviewTruncatedNotice = `[preview truncated at 255 characters — pass body_mode="text" for the full plain-text body]`

// conversationPreviewTruncatedHint is the once-per-thread footer equivalent of
// bodyPreviewTruncatedNotice. A thread may hold up to 100 messages, so the
// per-message marker is the two-character ellipsis in
// conversationPreviewEllipsis and the actionable sentence is printed once.
const conversationPreviewTruncatedHint = `Previews marked [...] are cut at 255 characters; pass body_mode="text" for the full plain-text bodies.`

// conversationPreviewEllipsis marks an individual truncated preview inside a
// thread listing.
const conversationPreviewEllipsis = " [...]"

// messageBodyContent returns the full body content attached to a message map by
// a body_mode escalation, or "" when the map carries no body.
//
// The body field is a nested map that arrives either as map[string]string
// (attached directly by a handler from graph.SerializeMessageBody) or as
// map[string]any (after a JSON round-trip), so both shapes are accepted — the
// same dual handling formatRecipientAddresses performs for recipient slices.
//
// Parameters:
//   - message: a serialized message map.
//
// Returns the body content string, or "" when absent or empty.
//
// Side effects: none.
func messageBodyContent(message map[string]any) string {
	switch body := message["body"].(type) {
	case map[string]string:
		return body["content"]
	case map[string]any:
		content, _ := body["content"].(string)
		return content
	default:
		return ""
	}
}

// isTruncatedPreview reports whether a bodyPreview string was cut short by
// Graph's 255-character cap.
//
// The signal available to the server is the length itself: Graph does not
// return a truncation flag, so a preview at or beyond the documented cap is
// treated as truncated. A body whose plain-text rendering happens to be exactly
// 255 characters is therefore marked truncated when it is not — a false marker
// that costs one line, against the alternative of silently losing the rest of
// every long message.
//
// Parameters:
//   - preview: the bodyPreview value from a serialized message map.
//
// Returns true when the preview is at least bodyPreviewCapRunes characters.
//
// Side effects: none.
func isTruncatedPreview(preview string) bool {
	return utf8.RuneCountInString(preview) >= bodyPreviewCapRunes
}
