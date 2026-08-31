// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for the tier-agnostic FolderNode tree helpers.
package tools

import "testing"

// TestCountFolders validates that the recursive folder counter includes every
// fetched descendant, not just the top level.
func TestCountFolders(t *testing.T) {
	tests := []struct {
		name  string
		nodes []FolderNode
		want  int
	}{
		{"nil", nil, 0},
		{"empty", []FolderNode{}, 0},
		{"flat", []FolderNode{{Name: "A"}, {Name: "B"}}, 2},
		{"nested", []FolderNode{
			{Name: "A", Children: []FolderNode{
				{Name: "A1"},
				{Name: "A2", Children: []FolderNode{{Name: "A2a"}}},
			}},
			{Name: "B"},
		}, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CountFolders(tt.nodes); got != tt.want {
				t.Errorf("CountFolders() = %d, want %d", got, tt.want)
			}
		})
	}
}
