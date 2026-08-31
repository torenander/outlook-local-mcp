// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for the move_message handler, including handler
// construction, no-client error path, and parameter validation.
package tools

import (
	"context"
	"testing"

	"github.com/desek/outlook-local-mcp/internal/auth"
	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestNewHandleMoveMessage_ReturnsHandler validates that NewHandleMoveMessage
// returns a non-nil handler function.
func TestNewHandleMoveMessage_ReturnsHandler(t *testing.T) {
	handler := NewHandleMoveMessage(graph.RetryConfig{}, 0)
	if handler == nil {
		t.Fatal("expected non-nil handler function")
	}
}

// TestMoveMessage_NoClient validates that the handler returns a tool error
// when no Graph client is present in the context.
func TestMoveMessage_NoClient(t *testing.T) {
	handler := NewHandleMoveMessage(graph.RetryConfig{}, 0)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"message_id":            "msg-1",
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

// TestMoveMessage_MissingMessageID validates that the handler returns a tool
// error when the required message_id parameter is missing.
func TestMoveMessage_MissingMessageID(t *testing.T) {
	handler := NewHandleMoveMessage(graph.RetryConfig{}, 0)

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
		t.Fatal("expected error result when message_id is missing")
	}
}

// TestMoveMessage_MissingDestination validates that the handler returns a tool
// error when the required destination_folder_id parameter is missing.
func TestMoveMessage_MissingDestination(t *testing.T) {
	handler := NewHandleMoveMessage(graph.RetryConfig{}, 0)

	client, srv := newTestGraphClient(t, nil)
	defer srv.Close()
	ctx := auth.WithGraphClient(context.Background(), client)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"message_id": "msg-1"}
	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when destination_folder_id is missing")
	}
}

// TestMoveMessage_AcceptsLegacyDestinationAlias validates that the pre-rename
// destination_folder_id spelling still satisfies the required destination.
//
// The alias is intentionally NOT declared in the verb schema (see the
// alias-declaration rule in internal/server/mail_verbs_test.go), so MCP
// clients will not forward it — but the mcp-go server passes undeclared
// arguments straight through, so a direct JSON-RPC caller that sends it must
// still be understood rather than told the parameter is missing.
func TestMoveMessage_AcceptsLegacyDestinationAlias(t *testing.T) {
	handler := NewHandleMoveMessage(graph.RetryConfig{}, 0)

	client, srv := newTestGraphClient(t, nil)
	defer srv.Close()
	ctx := auth.WithGraphClient(context.Background(), client)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"message_id":            "msg-1",
		"destination_folder_id": "archive",
	}
	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The Graph call has no mock backing it, so the move fails — but it must
	// not fail with the missing-parameter error.
	if result.IsError {
		if text := result.Content[0].(mcp.TextContent).Text; text == "missing required parameter: destination" {
			t.Error("destination_folder_id alias was not accepted as a destination reference")
		}
	}
}
