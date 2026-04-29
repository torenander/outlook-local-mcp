// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for the list_folder_tree handler, including handler
// construction, no-client error path, and the countTreeNodes helper.
package tools

import (
	"context"
	"testing"

	"github.com/desek/outlook-local-mcp/internal/auth"
	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestNewHandleListFolderTree_ReturnsHandler validates that
// NewHandleListFolderTree returns a non-nil handler function.
func TestNewHandleListFolderTree_ReturnsHandler(t *testing.T) {
	handler := NewHandleListFolderTree(graph.RetryConfig{}, 0)
	if handler == nil {
		t.Fatal("expected non-nil handler function")
	}
}

// TestListFolderTree_NoClient validates that the handler returns a tool error
// when no Graph client is present in the context.
func TestListFolderTree_NoClient(t *testing.T) {
	handler := NewHandleListFolderTree(graph.RetryConfig{}, 0)
	req := mcp.CallToolRequest{}

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

// TestListFolderTree_WithClient validates that the handler proceeds past the
// client lookup when a Graph client is in the context.
func TestListFolderTree_WithClient(t *testing.T) {
	handler := NewHandleListFolderTree(graph.RetryConfig{}, 0)
	req := mcp.CallToolRequest{}

	client, srv := newTestGraphClient(t, nil)
	defer srv.Close()
	ctx := auth.WithGraphClient(context.Background(), client)

	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		text := result.Content[0].(mcp.TextContent).Text
		if text == "no account selected" {
			t.Error("expected error other than 'no account selected'")
		}
	}
}

// TestCountTreeNodes validates the recursive node counting helper.
func TestCountTreeNodes(t *testing.T) {
	tests := []struct {
		name string
		tree []map[string]any
		want int
	}{
		{"nil", nil, 0},
		{"empty", []map[string]any{}, 0},
		{"flat", []map[string]any{
			{"displayName": "A"},
			{"displayName": "B"},
		}, 2},
		{"nested", []map[string]any{
			{
				"displayName": "A",
				"children": []map[string]any{
					{"displayName": "A1"},
					{
						"displayName": "A2",
						"children": []map[string]any{
							{"displayName": "A2a"},
						},
					},
				},
			},
			{"displayName": "B"},
		}, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countTreeNodes(tt.tree)
			if got != tt.want {
				t.Errorf("countTreeNodes() = %d, want %d", got, tt.want)
			}
		})
	}
}
