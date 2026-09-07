package graph

import (
	"testing"

	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// TestSerializeMessageBody_Nil verifies the two absent cases: a nil message and
// a message with no body. Both must return nil so a handler can attach the
// result conditionally without emitting an empty body object.
func TestSerializeMessageBody_Nil(t *testing.T) {
	if got := SerializeMessageBody(nil); got != nil {
		t.Errorf("SerializeMessageBody(nil) = %v, want nil", got)
	}
	if got := SerializeMessageBody(models.NewMessage()); got != nil {
		t.Errorf("SerializeMessageBody(bodyless message) = %v, want nil", got)
	}
}

// TestSerializeMessageBody_Text verifies that a plain-text body — what Graph
// returns under Prefer: outlook.body-content-type="text" — round-trips with
// both its content type and its content intact.
func TestSerializeMessageBody_Text(t *testing.T) {
	body := models.NewItemBody()
	contentType := models.TEXT_BODYTYPE
	body.SetContentType(&contentType)
	content := "Line one.\nLine two."
	body.SetContent(&content)

	msg := models.NewMessage()
	msg.SetBody(body)

	got := SerializeMessageBody(msg)
	if got == nil {
		t.Fatal("SerializeMessageBody returned nil for a message with a body")
	}
	if got["contentType"] != "text" {
		t.Errorf("contentType = %q, want %q", got["contentType"], "text")
	}
	if got["content"] != content {
		t.Errorf("content = %q, want %q", got["content"], content)
	}
}

// TestSerializeMessageBody_MatchesSerializeMessage verifies that the exported
// helper produces exactly what SerializeMessage embeds under "body", so the
// summary-plus-body payload and the raw payload never disagree about the shape
// of a body.
func TestSerializeMessageBody_MatchesSerializeMessage(t *testing.T) {
	msg := buildFullMessage()

	standalone := SerializeMessageBody(msg)
	embedded, ok := SerializeMessage(msg)["body"].(map[string]string)
	if !ok {
		t.Fatalf("SerializeMessage body is %T, want map[string]string", SerializeMessage(msg)["body"])
	}
	if len(standalone) != len(embedded) {
		t.Fatalf("standalone body = %v, embedded body = %v", standalone, embedded)
	}
	for k, v := range embedded {
		if standalone[k] != v {
			t.Errorf("body[%q] = %q standalone, %q embedded", k, standalone[k], v)
		}
	}
}
