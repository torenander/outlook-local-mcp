// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for the move_messages handler, including handler
// construction, no-client error path, parameter validation, parseMessageIDs
// helper, and batch limit enforcement.
package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/desek/outlook-local-mcp/internal/auth"
	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestNewHandleMoveMessages_ReturnsHandler validates that NewHandleMoveMessages
// returns a non-nil handler function.
func TestNewHandleMoveMessages_ReturnsHandler(t *testing.T) {
	handler := NewHandleMoveMessages(graph.RetryConfig{}, 0)
	if handler == nil {
		t.Fatal("expected non-nil handler function")
	}
}

// TestMoveMessages_NoClient validates that the handler returns a tool error
// when no Graph client is present in the context.
func TestMoveMessages_NoClient(t *testing.T) {
	handler := NewHandleMoveMessages(graph.RetryConfig{}, 0)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"message_ids":           "msg-1,msg-2",
		"destination_folder_id": "folder-1",
	}

	result, err := handler(context.Background(), req)
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

// TestMoveMessages_MissingMessageIDs validates that the handler returns a tool
// error when the required message_ids parameter is missing.
func TestMoveMessages_MissingMessageIDs(t *testing.T) {
	handler := NewHandleMoveMessages(graph.RetryConfig{}, 0)

	client, srv := newTestGraphClient(t, nil)
	defer srv.Close()
	ctx := auth.WithGraphClient(context.Background(), client)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"destination_folder_id": "folder-1"}
	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when message_ids is missing")
	}
}

// TestMoveMessages_EmptyMessageIDs validates that the handler rejects an empty
// message_ids string.
func TestMoveMessages_EmptyMessageIDs(t *testing.T) {
	handler := NewHandleMoveMessages(graph.RetryConfig{}, 0)

	client, srv := newTestGraphClient(t, nil)
	defer srv.Close()
	ctx := auth.WithGraphClient(context.Background(), client)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"message_ids":           "",
		"destination_folder_id": "folder-1",
	}
	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for empty message_ids")
	}
}

// TestMoveMessages_BatchLimitExceeded validates that the handler rejects more
// than maxBatchMoveMessages IDs.
func TestMoveMessages_BatchLimitExceeded(t *testing.T) {
	handler := NewHandleMoveMessages(graph.RetryConfig{}, 0)

	client, srv := newTestGraphClient(t, nil)
	defer srv.Close()
	ctx := auth.WithGraphClient(context.Background(), client)

	// Build 51 IDs (exceeds limit of 50).
	ids := make([]string, maxBatchMoveMessages+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("msg-%d", i)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"message_ids":           strings.Join(ids, ","),
		"destination_folder_id": "folder-1",
	}
	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when batch limit exceeded")
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "at most") {
		t.Errorf("error text = %q, want batch limit message", text)
	}
}

// TestParseMessageIDs validates the comma-separated ID parsing helper.
func TestParseMessageIDs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"single", "msg-1", []string{"msg-1"}},
		{"multiple", "msg-1,msg-2,msg-3", []string{"msg-1", "msg-2", "msg-3"}},
		{"with spaces", " msg-1 , msg-2 ", []string{"msg-1", "msg-2"}},
		{"trailing comma", "msg-1,", []string{"msg-1"}},
		{"empty segments", "msg-1,,msg-2", []string{"msg-1", "msg-2"}},
		{"empty string", "", []string{}},
		{"only commas", ",,,", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMessageIDs(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("parseMessageIDs(%q) = %v (len %d), want %v (len %d)", tt.raw, got, len(got), tt.want, len(tt.want))
			}
			for i, id := range got {
				if id != tt.want[i] {
					t.Errorf("parseMessageIDs(%q)[%d] = %q, want %q", tt.raw, i, id, tt.want[i])
				}
			}
		})
	}
}
