// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for the delete_folder handler, including handler
// construction, no-client error path, and parameter validation.
package tools

import (
	"context"
	"testing"

	"github.com/desek/outlook-local-mcp/internal/auth"
	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestNewHandleDeleteFolder_ReturnsHandler validates that NewHandleDeleteFolder
// returns a non-nil handler function.
func TestNewHandleDeleteFolder_ReturnsHandler(t *testing.T) {
	handler := NewHandleDeleteFolder(graph.RetryConfig{}, 0)
	if handler == nil {
		t.Fatal("expected non-nil handler function")
	}
}

// TestDeleteFolder_NoClient validates that the handler returns a tool error
// when no Graph client is present in the context.
func TestDeleteFolder_NoClient(t *testing.T) {
	handler := NewHandleDeleteFolder(graph.RetryConfig{}, 0)
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

// TestDeleteFolder_MissingFolder validates that the handler returns a tool
// error naming the natural-language `folder` parameter when it is missing.
func TestDeleteFolder_MissingFolder(t *testing.T) {
	handler := NewHandleDeleteFolder(graph.RetryConfig{}, 0)

	client, srv := newTestGraphClient(t, nil)
	defer srv.Close()
	ctx := auth.WithGraphClient(context.Background(), client)

	req := mcp.CallToolRequest{}
	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when folder is missing")
	}
	text := result.Content[0].(mcp.TextContent).Text
	if text != "missing required parameter: folder" {
		t.Errorf("error text = %q, want %q", text, "missing required parameter: folder")
	}
}

// TestDeleteFolder_AcceptsLegacyFolderIDAlias validates that the pre-rename
// folder_id spelling still satisfies the required folder reference, so callers
// that learned the old schema do not break.
func TestDeleteFolder_AcceptsLegacyFolderIDAlias(t *testing.T) {
	handler := NewHandleDeleteFolder(graph.RetryConfig{}, 0)

	client, srv := newTestGraphClient(t, nil)
	defer srv.Close()
	ctx := auth.WithGraphClient(context.Background(), client)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"folder_id": "inbox"}
	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The Graph call has no mock backing it, so the call fails — but it must
	// not fail with the missing-parameter error.
	if result.IsError {
		if text := result.Content[0].(mcp.TextContent).Text; text == "missing required parameter: folder" {
			t.Error("folder_id alias was not accepted as a folder reference")
		}
	}
}
