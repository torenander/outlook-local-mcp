package graph

import "github.com/microsoftgraph/msgraph-sdk-go/models"

// SerializeMessageBody exposes the message-body serialization that
// SerializeMessage performs internally, so that a caller can attach a body to a
// summary payload without also inheriting the full-only field set.
//
// It exists because the summary field set is a deliberate contract
// (AGENTS.md, "summary field sets are intentional"): SerializeSummaryMessage
// must keep excluding the body unconditionally. CR-0068 lets a caller ask for
// the body explicitly via body_mode, and the handler then attaches it to the
// summary map the same way it attaches the provenance flag — an opt-in
// addition, not a change to the curated field set.
//
// Parameters:
//   - msg: a models.Messageable from the Microsoft Graph API. May be nil.
//
// Returns a map with "contentType" and "content" keys, or nil when msg is nil
// or carries no body (which is the case whenever `body` was not requested in
// $select).
//
// Side effects: none.
func SerializeMessageBody(msg models.Messageable) map[string]string {
	if msg == nil {
		return nil
	}
	return serializeBody(msg.GetBody())
}
