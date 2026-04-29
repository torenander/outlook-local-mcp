// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for the create_folder handler, including handler
// construction, no-client error path, and parameter validation.
package tools

import (
	"context"
	"testing"

	"github.com/desek/outlook-local-mcp/internal/auth"
	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestNewHandleCreateFolder_ReturnsHandler validates that NewHandleCreateFolder
// returns a non-nil handler function.
func TestNewHandleCreateFolder_ReturnsHandler(t *testing.T) {
	handler := NewHandleCreateFolder(graph.RetryConfig{}, 0)
	if handler == nil {
		t.Fatal("expected non-nil handler function")
	}
}

// TestCreateFolder_NoClient validates that the handler returns a tool error
// when no Graph client is present in the context.
func TestCreateFolder_NoClient(t *testing.T) {
	handler := NewHandleCreateFolder(graph.RetryConfig{}, 0)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"display_name": "Test"}

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

// TestCreateFolder_MissingDisplayName validates that the handler returns a tool
// error when the required display_name parameter is missing.
func TestCreateFolder_MissingDisplayName(t *testing.T) {
	handler := NewHandleCreateFolder(graph.RetryConfig{}, 0)

	client, srv := newTestGraphClient(t, nil)
	defer srv.Close()
	ctx := auth.WithGraphClient(context.Background(), client)

	req := mcp.CallToolRequest{}
	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when display_name is missing")
	}
}

// TestCreateFolder_EmptyDisplayName validates that the handler rejects
// whitespace-only display names.
func TestCreateFolder_EmptyDisplayName(t *testing.T) {
	handler := NewHandleCreateFolder(graph.RetryConfig{}, 0)

	client, srv := newTestGraphClient(t, nil)
	defer srv.Close()
	ctx := auth.WithGraphClient(context.Background(), client)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"display_name": "   "}
	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for whitespace-only display_name")
	}
	text := result.Content[0].(mcp.TextContent).Text
	if text != "display_name must not be empty or whitespace-only" {
		t.Errorf("error text = %q, want whitespace error", text)
	}
}
