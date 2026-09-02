// Package tools — this file decides the Graph content type for an event body,
// for the create_event, update_event, create_meeting and update_meeting verbs,
// which all route through HandleCreateEvent or HandleUpdateEvent.
//
// Before the body_type parameter existed, the content type was guessed from
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

// eventBodyTypeParam is the tool argument callers use to state the body type.
const eventBodyTypeParam = "body_type"

// eventBodyType decides the Graph content type for an event body.
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
//   - explicit: the caller's body_type argument, or "" when not supplied.
//
// Returns the content type to send to Graph, or an error naming the accepted
// values. No side effects.
func eventBodyType(content, explicit string) (models.BodyType, error) {
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
			eventBodyTypeParam, explicit)
	}
}

// newEventBody builds the Graph ItemBody for an event, choosing the content
// type with eventBodyType.
//
// Parameters:
//   - content: the body text as supplied by the caller.
//   - explicit: the caller's body_type argument, or "" when not supplied.
//
// Returns the populated ItemBody, or an error when explicit is unrecognised.
func newEventBody(content, explicit string) (models.ItemBodyable, error) {
	contentType, err := eventBodyType(content, explicit)
	if err != nil {
		return nil, err
	}

	body := models.NewItemBody()
	body.SetContentType(&contentType)
	body.SetContent(&content)
	return body, nil
}
