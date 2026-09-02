package tools

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/desek/outlook-local-mcp/internal/auth"
	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// TestEventBodyType covers the contract for choosing an event body's content
// type. Before body_type existed, the only rule was strings.Contains(body, "<"),
// which silently misclassified two ordinary cases in opposite directions:
// escaped HTML with no literal "<" was sent as plain text, and prose containing
// a comparison such as "a < b" was sent as HTML and rendered as broken markup.
//
// An explicit body_type must win over the heuristic, and an unrecognised value
// must be an error rather than a silent guess.
func TestEventBodyType(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		explicit string
		want     models.BodyType
		wantErr  bool
	}{
		// The two silent misclassifications the parameter exists to fix.
		{"prose with comparison, explicit text", "throughput when a < b holds", "text", models.TEXT_BODYTYPE, false},
		{"escaped html, explicit html", "&lt;p&gt;Agenda&lt;/p&gt;", "html", models.HTML_BODYTYPE, false},

		// Explicit values win generally.
		{"explicit text on markup", "<p>Agenda</p>", "text", models.TEXT_BODYTYPE, false},
		{"explicit html on plain", "Agenda", "html", models.HTML_BODYTYPE, false},

		// Tolerant parsing, so a caller's casing or stray space is not an error.
		{"uppercase", "Agenda", "HTML", models.HTML_BODYTYPE, false},
		{"padded", "Agenda", "  text  ", models.TEXT_BODYTYPE, false},

		// Omitted: the legacy heuristic is preserved for back-compatibility.
		{"omitted, markup", "<p>Agenda</p>", "", models.HTML_BODYTYPE, false},
		{"omitted, plain", "Agenda", "", models.TEXT_BODYTYPE, false},
		{"omitted, comparison still guesses html", "a < b", "", models.HTML_BODYTYPE, false},

		// A typo must not be silently swallowed.
		{"unrecognised value", "Agenda", "plaintext", models.TEXT_BODYTYPE, true},
		{"markdown is not a Graph body type", "Agenda", "markdown", models.TEXT_BODYTYPE, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := eventBodyType(tt.content, tt.explicit)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("eventBodyType(%q, %q) = %v, want an error", tt.content, tt.explicit, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("eventBodyType(%q, %q) returned unexpected error: %v", tt.content, tt.explicit, err)
			}
			if got != tt.want {
				t.Errorf("eventBodyType(%q, %q) = %v, want %v", tt.content, tt.explicit, got, tt.want)
			}
		})
	}
}

// createEventPOSTBody runs HandleCreateEvent against a capturing test server
// and returns the serialized POST body, the tool result, and how many HTTP
// requests the handler issued.
func createEventPOSTBody(t *testing.T, args map[string]any) (string, *mcp.CallToolResult, int) {
	t.Helper()

	var captured []byte
	requests := 0
	client, srv := newTestGraphClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, _ := io.ReadAll(r.Body) //nolint:errcheck // test helper
		captured = body
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockEventJSON)) //nolint:errcheck // test helper
	}))
	defer srv.Close()

	handler := HandleCreateEvent(graph.RetryConfig{}, 30*time.Second, "America/New_York", "")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args

	result, err := handler(auth.WithGraphClient(context.Background(), client), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return string(captured), result, requests
}

// TestCreateEvent_BodyTypeReachesGraph is the end-to-end half of the body_type
// contract: the chosen content type must appear in the JSON sent to Graph.
//
// It replaces TestBodyContentTypeDetection, which re-implemented
// strings.Contains(body, "<") inside the test and asserted on its own copy, so
// it passed no matter what the handler did -- and recorded the "x < y" case as
// HTML, the very misclassification body_type exists to let callers avoid.
func TestCreateEvent_BodyTypeReachesGraph(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		bodyType string
		want     string
	}{
		{"explicit text on prose with comparison", "throughput when a < b holds", "text", `"contentType":"text"`},
		{"explicit html on escaped markup", "&lt;p&gt;Agenda&lt;/p&gt;", "html", `"contentType":"html"`},
		{"omitted keeps heuristic for markup", "<p>Agenda</p>", "", `"contentType":"html"`},
		{"omitted keeps heuristic for plain text", "Agenda", "", `"contentType":"text"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := createEventBaseArgs()
			args["body"] = tt.body
			if tt.bodyType != "" {
				args["body_type"] = tt.bodyType
			}

			posted, result, requests := createEventPOSTBody(t, args)
			if result.IsError {
				t.Fatalf("expected success, got error: %v", result.Content[0].(mcp.TextContent).Text)
			}
			if requests != 1 {
				t.Fatalf("issued %d Graph requests, want 1", requests)
			}
			if !strings.Contains(posted, tt.want) {
				t.Errorf("POST body missing %s\ngot: %s", tt.want, posted)
			}
		})
	}
}

// TestCreateEvent_InvalidBodyTypeIsRejected checks that a body_type the Graph
// API has no notion of fails the call instead of falling back to a guess, and
// that nothing is sent to Graph.
func TestCreateEvent_InvalidBodyTypeIsRejected(t *testing.T) {
	args := createEventBaseArgs()
	args["body"] = "Agenda"
	args["body_type"] = "markdown"

	_, result, requests := createEventPOSTBody(t, args)

	if !result.IsError {
		t.Fatal("expected an error result for body_type=markdown")
	}
	if requests != 0 {
		t.Errorf("issued %d Graph requests, want 0: the event must not be created", requests)
	}
	msg := result.Content[0].(mcp.TextContent).Text
	for _, want := range []string{"body_type", "markdown", `"text"`, `"html"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %s: %q", want, msg)
		}
	}
}

// TestUpdateEvent_BodyTypeReachesGraph is the same contract on the update path,
// which shares eventBodyType through newEventBody.
func TestUpdateEvent_BodyTypeReachesGraph(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		bodyType string
		want     string
	}{
		{"explicit text on prose with comparison", "latency when p < q", "text", `"contentType":"text"`},
		{"explicit html on escaped markup", "&lt;b&gt;Updated&lt;/b&gt;", "html", `"contentType":"html"`},
		{"omitted keeps heuristic", "<p>Updated</p>", "", `"contentType":"html"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured []byte
			client, srv := newTestGraphClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body) //nolint:errcheck // test helper
				captured = body
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(mockEventJSON)) //nolint:errcheck // test helper
			}))
			defer srv.Close()

			handler := HandleUpdateEvent(graph.RetryConfig{}, 30*time.Second, "America/New_York")
			args := map[string]any{"event_id": "AAMkAGTest123", "body": tt.body}
			if tt.bodyType != "" {
				args["body_type"] = tt.bodyType
			}
			req := mcp.CallToolRequest{}
			req.Params.Arguments = args

			result, err := handler(auth.WithGraphClient(context.Background(), client), req)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if result.IsError {
				t.Fatalf("expected success, got error: %v", result.Content[0].(mcp.TextContent).Text)
			}
			if posted := string(captured); !strings.Contains(posted, tt.want) {
				t.Errorf("PATCH body missing %s\ngot: %s", tt.want, posted)
			}
		})
	}
}
