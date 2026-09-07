// Package tools — this file decides the Graph content type for an event body,
// for the create_event, update_event, create_meeting and update_meeting verbs,
// which all route through HandleCreateEvent or HandleUpdateEvent.
//
// Before the content_type parameter existed, the content type was guessed from
// strings.Contains(body, "<") alone. The guess is wrong in both directions and
// fails silently either way: escaped HTML ("&lt;p&gt;") contains no literal "<"
// and was sent as plain text, so the markup showed up as visible entities; and
// ordinary prose containing a comparison ("throughput when a < b holds") was
// sent as HTML, where everything from the "<" onward is parsed as a tag and
// disappears from the rendered body.
//
// The heuristic is kept as the default so existing callers are unaffected, but
// a caller that knows the answer can now say so.
package tools

import (
	"fmt"
	"strings"

	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// eventContentTypeParam is the tool argument callers use to state the body type.
//
// Named to match the content_type parameter the mail draft verbs already ship
// (internal/server/mail_verbs.go), since it is the same concept with the same
// two values. It deliberately does not echo body_mode from CR-0068, which is a
// different thing: how much of a body to fetch, not what type a body being
// written is.
//
// One divergence to be aware of: the mail path's BuildDraftBody silently treats
// any unrecognised content_type as text, whereas this returns an error. The
// strict behaviour is the intended one; the mail path is the outlier and is
// left alone here rather than changed under a calendar commit.
const eventContentTypeParam = "content_type"

// eventContentType decides the Graph content type for an event body.
//
// An explicit value always wins. It is matched case-insensitively and after
// trimming surrounding space, so "HTML" and " text " are accepted. An
// unrecognised value is an error rather than a silent fallback: a caller that
// passed "markdown" or "plaintext" has a wrong assumption worth surfacing, and
// guessing would reintroduce exactly the silent misclassification this
// parameter exists to remove.
//
// When explicit is empty the legacy heuristic applies: a body containing "<"
// is treated as HTML, anything else as text.
//
// Parameters:
//   - content: the body text as supplied by the caller.
//   - explicit: the caller's content_type argument, or "" when not supplied.
//
// Returns the content type to send to Graph, or an error naming the accepted
// values. No side effects.
func eventContentType(content, explicit string) (models.BodyType, error) {
	switch strings.ToLower(strings.TrimSpace(explicit)) {
	case "text":
		return models.TEXT_BODYTYPE, nil
	case "html":
		return models.HTML_BODYTYPE, nil
	case "":
		if strings.Contains(content, "<") {
			return models.HTML_BODYTYPE, nil
		}
		return models.TEXT_BODYTYPE, nil
	default:
		return models.TEXT_BODYTYPE, fmt.Errorf(
			"invalid %s %q: expected \"text\" or \"html\". Omit it to infer the type from the body content",
			eventContentTypeParam, explicit)
	}
}

// newEventBody builds the Graph ItemBody for an event, choosing the content
// type with eventContentType.
//
// Parameters:
//   - content: the body text as supplied by the caller.
//   - explicit: the caller's content_type argument, or "" when not supplied.
//
// Returns the populated ItemBody, or an error when explicit is unrecognised.
func newEventBody(content, explicit string) (models.ItemBodyable, error) {
	contentType, err := eventContentType(content, explicit)
	if err != nil {
		return nil, err
	}

	body := models.NewItemBody()
	body.SetContentType(&contentType)
	body.SetContent(&content)
	return body, nil
}
