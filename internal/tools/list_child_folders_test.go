// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for the list_child_folders handler, including
// handler construction, no-client error path, and parameter validation.
package tools

import (
	"context"
	"testing"

	"github.com/desek/outlook-local-mcp/internal/auth"
	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestNewHandleListChildFolders_ReturnsHandler validates that
// NewHandleListChildFolders returns a non-nil handler function.
func TestNewHandleListChildFolders_ReturnsHandler(t *testing.T) {
	handler := NewHandleListChildFolders(graph.RetryConfig{}, 0)
	if handler == nil {
		t.Fatal("expected non-nil handler function")
	}
}

// TestListChildFolders_NoClient validates that the handler returns a tool error
// when no Graph client is present in the context.
func TestListChildFolders_NoClient(t *testing.T) {
	handler := NewHandleListChildFolders(graph.RetryConfig{}, 0)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"folder_id": "test-folder"}

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

// TestListChildFolders_MissingFolderID validates that the handler returns a
// tool error when the required folder_id parameter is missing.
func TestListChildFolders_MissingFolderID(t *testing.T) {
	handler := NewHandleListChildFolders(graph.RetryConfig{}, 0)

	client, srv := newTestGraphClient(t, nil)
	defer srv.Close()
	ctx := auth.WithGraphClient(context.Background(), client)

	req := mcp.CallToolRequest{}
	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when folder_id is missing")
	}
	text := result.Content[0].(mcp.TextContent).Text
	if text != "missing required parameter: folder_id" {
		t.Errorf("error text = %q, want %q", text, "missing required parameter: folder_id")
	}
}
