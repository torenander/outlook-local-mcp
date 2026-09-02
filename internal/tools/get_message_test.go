// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for the get_message tool, including tool
// registration, handler construction, parameter validation, required
// message_id enforcement, error handling for missing Graph client, and the
// body_mode escalation added by CR-0068.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/desek/outlook-local-mcp/internal/auth"
	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TestGetMessageTool_Registration validates that NewGetMessageTool is properly
// defined with the expected name and read-only annotation.
func TestGetMessageTool_Registration(t *testing.T) {
	tool := NewGetMessageTool()
	if tool.Name != "mail_get_message" {
		t.Errorf("tool name = %q, want %q", tool.Name, "mail_get_message")
	}

	annotations := tool.Annotations
	if annotations.ReadOnlyHint == nil || !*annotations.ReadOnlyHint {
		t.Error("expected ReadOnlyHint to be true")
	}
}

// TestGetMessageTool_HasParameters validates that NewGetMessageTool defines
// all expected parameters, with message_id being required.
func TestGetMessageTool_HasParameters(t *testing.T) {
	tool := NewGetMessageTool()
	schema := tool.InputSchema

	if len(schema.Required) != 1 || schema.Required[0] != "message_id" {
		t.Errorf("expected required = [message_id], got %v", schema.Required)
	}

	expectedParams := []string{
		"message_id", "account", "output", "body_mode",
	}
	for _, param := range expectedParams {
		if _, ok := schema.Properties[param]; !ok {
			t.Errorf("expected %q property to be defined", param)
		}
	}
}

// TestNewHandleGetMessage_ReturnsHandler validates that NewHandleGetMessage
// returns a non-nil handler function.
func TestNewHandleGetMessage_ReturnsHandler(t *testing.T) {
	handler := NewHandleGetMessage(graph.RetryConfig{}, 0, "")
	if handler == nil {
		t.Fatal("expected non-nil handler function")
	}
}

// TestGetMessageToolCanBeAddedToServer validates that NewGetMessageTool and
// its handler can be registered on an MCP server without error or panic.
func TestGetMessageToolCanBeAddedToServer(t *testing.T) {
	s := server.NewMCPServer("test-server", "0.0.1",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)
	s.AddTool(NewGetMessageTool(), NewHandleGetMessage(graph.RetryConfig{}, 0, ""))
}

// TestGetMessage_Success validates that the handler proceeds past the client
// lookup and message_id validation when a Graph client is in the context and a
// message_id is provided. The handler will fail at the Graph API call (no mock
// response), but should not return "no account selected" or the missing
// message_id error.
func TestGetMessage_Success(t *testing.T) {
	handler := NewHandleGetMessage(graph.RetryConfig{}, 0, "")
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{
		"message_id": "AAMkAGI2TGULAAA=",
	}

	client, srv := newTestGraphClient(t, nil)
	defer srv.Close()
	ctx := auth.WithGraphClient(context.Background(), client)

	result, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		text := result.Content[0].(mcp.TextContent).Text
		if text == "no account selected" {
			t.Error("expected error other than 'no account selected' when client is in context")
		}
		if text == "missing required parameter: message_id. Tip: Use mail_list_messages or mail_search_messages to find the message ID." {
			t.Error("expected error other than missing message_id when message_id is provided")
		}
	}
}

// TestGetMessage_NoMessageId validates that the handler returns a tool error
// when the required message_id parameter is empty or missing.
func TestGetMessage_NoMessageId(t *testing.T) {
	handler := NewHandleGetMessage(graph.RetryConfig{}, 0, "")

	client, srv := newTestGraphClient(t, nil)
	defer srv.Close()
	ctx := auth.WithGraphClient(context.Background(), client)

	// Test with no arguments at all.
	request := mcp.CallToolRequest{}
	result, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when message_id is missing")
	}
	text := result.Content[0].(mcp.TextContent).Text
	expected := "missing required parameter: message_id. Tip: Use mail_list_messages or mail_search_messages to find the message ID."
	if text != expected {
		t.Errorf("error text = %q, want %q", text, expected)
	}

	// Test with explicit empty string.
	request.Params.Arguments = map[string]any{
		"message_id": "",
	}
	result, err = handler(ctx, request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when message_id is empty string")
	}
}

// TestGetMessage_NotFound validates that the handler returns a tool error when
// no Graph client is present in the context (simulating a scenario where the
// account is not configured or the message cannot be found).
func TestGetMessage_NotFound(t *testing.T) {
	handler := NewHandleGetMessage(graph.RetryConfig{}, 0, "")
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{
		"message_id": "AAMkAGI2TGULAAA=",
	}

	// No Graph client in context simulates "no account selected".
	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when no client in context")
	}
	text := result.Content[0].(mcp.TextContent).Text
	if text != "no account selected" {
		t.Errorf("error text = %q, want %q", text, "no account selected")
	}
}

// TestGetMessage_ProvenanceDetection validates that when provenance tagging is
// configured and the Graph API returns a message with a matching
// singleValueExtendedProperties entry, the serialized response includes
// "provenance": true. When the property is absent the field is false.
func TestGetMessage_ProvenanceDetection(t *testing.T) {
	propID := graph.BuildProvenancePropertyID("com.github.desek.outlook-local-mcp.created")

	tests := []struct {
		name     string
		body     string
		expected bool
	}{
		{
			name:     "tagged",
			body:     fmt.Sprintf(`{"id":"msg-1","subject":"Tagged","singleValueExtendedProperties":[{"id":"%s","value":"true"}]}`, propID),
			expected: true,
		},
		{
			name:     "untagged",
			body:     `{"id":"msg-2","subject":"Plain"}`,
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, srv := newTestGraphClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				//nolint:errcheck // test helper
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()

			handler := NewHandleGetMessage(graph.RetryConfig{}, 30*time.Second, propID)
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]any{
				"message_id": "AAMkAGI2TGULAAA=",
				"output":     "summary",
			}
			ctx := auth.WithGraphClient(context.Background(), client)
			result, err := handler(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.IsError {
				t.Fatalf("unexpected tool error: %s", result.Content[0].(mcp.TextContent).Text)
			}

			var out map[string]any
			if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &out); err != nil {
				t.Fatalf("failed to parse response: %v", err)
			}
			got, ok := out["provenance"]
			if !ok {
				t.Fatal("provenance key missing from response")
			}
			if got != tc.expected {
				t.Errorf("provenance = %v, want %v", got, tc.expected)
			}
		})
	}
}

// bodyModeCapture records the query string and Prefer header of the single
// Graph request a get_message call makes, and serves the supplied JSON body.
type bodyModeCapture struct {
	rawQuery string
	prefer   []string
}

// newBodyModeStub returns a Graph client whose responses are the given JSON
// document and whose request is recorded into the returned capture.
func newBodyModeStub(t *testing.T, json string) (*bodyModeCapture, context.Context, func()) {
	t.Helper()
	capture := &bodyModeCapture{}
	client, srv := newTestGraphClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.rawQuery = r.URL.RawQuery
		capture.prefer = r.Header.Values("Prefer")
		w.Header().Set("Content-Type", "application/json")
		//nolint:errcheck // test helper
		fmt.Fprint(w, json)
	}))
	ctx := auth.WithGraphClient(context.Background(), client)
	return capture, ctx, srv.Close
}

// callGetMessage invokes the get_message handler with the given arguments and
// fails the test on any error result.
func callGetMessage(t *testing.T, ctx context.Context, args map[string]any) string {
	t.Helper()
	handler := NewHandleGetMessage(graph.RetryConfig{}, 30*time.Second, "")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].(mcp.TextContent).Text)
	}
	return result.Content[0].(mcp.TextContent).Text
}

// longPreview is a bodyPreview at Graph's 255-character cap, i.e. one that was
// truncated.
var longPreview = strings.Repeat("a", 255)

// selectRequestsBody reports whether the $select of a captured request query
// contains the `body` field itself. A substring match would be wrong:
// `bodyPreview` is always present and contains "body".
func selectRequestsBody(t *testing.T, rawQuery string) bool {
	t.Helper()
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("parse captured query %q: %v", rawQuery, err)
	}
	for _, field := range strings.Split(values.Get("$select"), ",") {
		if strings.TrimSpace(field) == "body" {
			return true
		}
	}
	return false
}

// TestGetMessage_PreviewModeAsksGraphForNoBody verifies that the default path
// is unchanged by CR-0068: `body` never enters $select and no Prefer header is
// sent, so the CR-0051 token posture is preserved byte for byte.
func TestGetMessage_PreviewModeAsksGraphForNoBody(t *testing.T) {
	capture, ctx, closeSrv := newBodyModeStub(t, `{"id":"m1","subject":"S","bodyPreview":"short"}`)
	defer closeSrv()

	callGetMessage(t, ctx, map[string]any{"message_id": "AAMkAGI2TGULAAA="})

	if selectRequestsBody(t, capture.rawQuery) {
		t.Errorf("default $select requested a body: %q", capture.rawQuery)
	}
	if len(capture.prefer) != 0 {
		t.Errorf("default call sent Prefer %v, want none", capture.prefer)
	}
}

// TestGetMessage_TextModeSelectsBodyAndPrefersText verifies that body_mode=text
// adds `body` to $select and asks Graph — not this process — to convert the
// body to plain text.
func TestGetMessage_TextModeSelectsBodyAndPrefersText(t *testing.T) {
	capture, ctx, closeSrv := newBodyModeStub(t,
		`{"id":"m1","subject":"S","bodyPreview":"short","body":{"contentType":"text","content":"Full plain body."}}`)
	defer closeSrv()

	out := callGetMessage(t, ctx, map[string]any{
		"message_id": "AAMkAGI2TGULAAA=",
		"body_mode":  "text",
	})

	if !selectRequestsBody(t, capture.rawQuery) {
		t.Errorf("$select did not include body: %q", capture.rawQuery)
	}
	if len(capture.prefer) != 1 {
		t.Fatalf("Prefer headers = %v, want exactly one", capture.prefer)
	}
	if capture.prefer[0] != `outlook.body-content-type="text"` {
		t.Errorf("Prefer = %q, want outlook.body-content-type=\"text\"", capture.prefer[0])
	}
	if !strings.Contains(out, "Full plain body.") {
		t.Errorf("text output does not contain the full body:\n%s", out)
	}
}

// TestGetMessage_FullModeSelectsBodyWithoutPrefer verifies that body_mode=full
// fetches the body as Graph stores it, with no conversion preference.
func TestGetMessage_FullModeSelectsBodyWithoutPrefer(t *testing.T) {
	capture, ctx, closeSrv := newBodyModeStub(t,
		`{"id":"m1","subject":"S","bodyPreview":"short","body":{"contentType":"html","content":"<p>Full HTML body.</p>"}}`)
	defer closeSrv()

	out := callGetMessage(t, ctx, map[string]any{
		"message_id": "AAMkAGI2TGULAAA=",
		"body_mode":  "full",
	})

	if !selectRequestsBody(t, capture.rawQuery) {
		t.Errorf("$select did not include body: %q", capture.rawQuery)
	}
	if len(capture.prefer) != 0 {
		t.Errorf("body_mode=full sent Prefer %v, want none", capture.prefer)
	}
	if !strings.Contains(out, "<p>Full HTML body.</p>") {
		t.Errorf("text output does not contain the stored body:\n%s", out)
	}
	// The point of `full`: a whole body without the raw tier's header block.
	if strings.Contains(out, "internetMessageHeaders") {
		t.Errorf("body_mode=full leaked internet headers:\n%s", out)
	}
}

// TestGetMessage_BodyAliasAccepted verifies that the undeclared `body` alias
// reaches the handler, since `body` is the spelling a caller reaches for first.
func TestGetMessage_BodyAliasAccepted(t *testing.T) {
	capture, ctx, closeSrv := newBodyModeStub(t,
		`{"id":"m1","subject":"S","body":{"contentType":"text","content":"Aliased body."}}`)
	defer closeSrv()

	out := callGetMessage(t, ctx, map[string]any{
		"message_id": "AAMkAGI2TGULAAA=",
		"body":       "text",
	})

	if len(capture.prefer) != 1 || capture.prefer[0] != `outlook.body-content-type="text"` {
		t.Errorf("Prefer = %v, want the text body-content-type preference", capture.prefer)
	}
	if !strings.Contains(out, "Aliased body.") {
		t.Errorf("alias did not escalate the body:\n%s", out)
	}
}

// TestGetMessage_BodyModeComposesWithSummaryOutput verifies that body_mode and
// output are orthogonal: summary JSON gains a body object without gaining any
// of the raw tier's full-only fields.
func TestGetMessage_BodyModeComposesWithSummaryOutput(t *testing.T) {
	_, ctx, closeSrv := newBodyModeStub(t,
		`{"id":"m1","subject":"S","bodyPreview":"short","body":{"contentType":"text","content":"Structured body."}}`)
	defer closeSrv()

	out := callGetMessage(t, ctx, map[string]any{
		"message_id": "AAMkAGI2TGULAAA=",
		"body_mode":  "text",
		"output":     "summary",
	})

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("summary output is not JSON: %v", err)
	}
	body, ok := parsed["body"].(map[string]any)
	if !ok {
		t.Fatalf("summary body is %T, want an object", parsed["body"])
	}
	if body["content"] != "Structured body." {
		t.Errorf("body.content = %v, want %q", body["content"], "Structured body.")
	}
	if body["contentType"] != "text" {
		t.Errorf("body.contentType = %v, want %q", body["contentType"], "text")
	}
	for _, key := range []string{"internetMessageHeaders", "conversationIndex", "replyTo", "bccRecipients"} {
		if _, exists := parsed[key]; exists {
			t.Errorf("summary + body_mode leaked full-only field %q", key)
		}
	}
}

// TestGetMessage_PreviewTruncationIsAnnounced verifies that a preview sitting on
// Graph's 255-character cap is marked as cut, and that a short one is not.
func TestGetMessage_PreviewTruncationIsAnnounced(t *testing.T) {
	_, ctx, closeSrv := newBodyModeStub(t,
		fmt.Sprintf(`{"id":"m1","subject":"S","bodyPreview":%q}`, longPreview))
	defer closeSrv()

	out := callGetMessage(t, ctx, map[string]any{"message_id": "AAMkAGI2TGULAAA="})
	if !strings.Contains(out, bodyPreviewTruncatedNotice) {
		t.Errorf("truncated preview was not announced:\n%s", out)
	}

	_, shortCtx, closeShort := newBodyModeStub(t, `{"id":"m1","subject":"S","bodyPreview":"short"}`)
	defer closeShort()

	shortOut := callGetMessage(t, shortCtx, map[string]any{"message_id": "AAMkAGI2TGULAAA="})
	if strings.Contains(shortOut, bodyPreviewTruncatedNotice) {
		t.Errorf("short preview was wrongly announced as truncated:\n%s", shortOut)
	}
}

// TestGetMessage_InvalidBodyMode verifies that a bogus value is rejected by
// name instead of silently degrading to a preview.
func TestGetMessage_InvalidBodyMode(t *testing.T) {
	client, srv := newTestGraphClient(t, nil)
	defer srv.Close()
	ctx := auth.WithGraphClient(context.Background(), client)

	handler := NewHandleGetMessage(graph.RetryConfig{}, 30*time.Second, "")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"message_id": "AAMkAGI2TGULAAA=",
		"body_mode":  "html",
	}
	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected a tool error for body_mode=html")
	}
	if got := result.Content[0].(mcp.TextContent).Text; got != "body_mode must be 'preview', 'text', or 'full'" {
		t.Errorf("error = %q, want the body_mode validation message", got)
	}
}
