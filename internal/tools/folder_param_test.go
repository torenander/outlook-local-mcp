// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for folder scoping on list_messages and
// search_messages. Both verbs accept the same natural-language folder
// reference as the folder verbs, so an LLM never has to switch vocabulary
// halfway through a mail workflow.
package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/desek/outlook-local-mcp/internal/auth"
	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/mark3labs/mcp-go/mcp"
)

// messageVerbCase names a folder-scoped read verb and the handler under test.
type messageVerbCase struct {
	name    string
	handler func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)
	extra   map[string]any
}

// folderScopedVerbs returns the two verbs that accept a folder scope, with any
// extra required arguments they need to reach folder resolution.
func folderScopedVerbs() []messageVerbCase {
	return []messageVerbCase{
		{name: "list_messages", handler: NewHandleListMessages(graph.RetryConfig{}, 0, ""), extra: map[string]any{}},
		{name: "search_messages", handler: NewHandleSearchMessages(graph.RetryConfig{}, 0), extra: map[string]any{"query": "budget"}},
	}
}

// callFolderScoped invokes a folder-scoped verb against the mock hierarchy and
// returns the result plus every URL path the mock was asked for.
func callFolderScoped(t *testing.T, c messageVerbCase, args map[string]any) (*mcp.CallToolResult, []string) {
	t.Helper()

	var seen []string
	client, srv := newTestGraphClient(t, newFolderMockHandler(t, &seen))
	defer srv.Close()

	merged := map[string]any{}
	for k, v := range c.extra {
		merged[k] = v
	}
	for k, v := range args {
		merged[k] = v
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = merged
	ctx := auth.WithGraphClient(context.Background(), client)

	result, err := c.handler(ctx, req)
	if err != nil {
		t.Fatalf("%s: unexpected handler error: %v", c.name, err)
	}
	return result, seen
}

// TestMessageVerbs_FolderByPath verifies that a display-name path is resolved
// to a folder id and that the message query is then scoped to that id.
//
// This is the fix for the class of bug that CRUD prompt Steps 30b-30e, 33, 35
// and 36 demonstrated: passing a folder by name used to be silently ignored,
// widening the query to the whole mailbox while still looking correct.
func TestMessageVerbs_FolderByPath(t *testing.T) {
	for _, c := range folderScopedVerbs() {
		t.Run(c.name, func(t *testing.T) {
			_, seen := callFolderScoped(t, c, map[string]any{"folder": "Inbox/01 Projects"})

			want := "/" + mockProjectsID + "/messages"
			for _, path := range seen {
				if strings.HasSuffix(path, want) {
					return
				}
			}
			t.Errorf("no request scoped to the resolved folder (%s); saw %v", want, seen)
		})
	}
}

// TestMessageVerbs_FolderByWellKnownName verifies that a well-known name
// scopes the query without any lookup round-trip.
func TestMessageVerbs_FolderByWellKnownName(t *testing.T) {
	for _, c := range folderScopedVerbs() {
		t.Run(c.name, func(t *testing.T) {
			_, seen := callFolderScoped(t, c, map[string]any{"folder": "Inbox"})

			for _, path := range seen {
				if strings.HasSuffix(path, "/inbox/messages") {
					return
				}
			}
			t.Errorf("no request scoped to the inbox well-known name; saw %v", seen)
		})
	}
}

// TestMessageVerbs_FolderIDAliasStillWorks verifies the pre-rename spelling
// continues to scope the query, including for values that are folder names
// rather than ids.
func TestMessageVerbs_FolderIDAliasStillWorks(t *testing.T) {
	for _, c := range folderScopedVerbs() {
		t.Run(c.name, func(t *testing.T) {
			_, seen := callFolderScoped(t, c, map[string]any{"folder_id": "Inbox"})

			for _, path := range seen {
				if strings.HasSuffix(path, "/inbox/messages") {
					return
				}
			}
			t.Errorf("folder_id alias did not scope the query; saw %v", seen)
		})
	}
}

// TestMessageVerbs_UnresolvableFolderErrors verifies that an unmatched folder
// reference fails loudly with an actionable message instead of silently
// widening the query to every folder.
func TestMessageVerbs_UnresolvableFolderErrors(t *testing.T) {
	for _, c := range folderScopedVerbs() {
		t.Run(c.name, func(t *testing.T) {
			result, seen := callFolderScoped(t, c, map[string]any{"folder": "Inbox/Nope"})

			if !result.IsError {
				t.Fatal("expected an error for an unresolvable folder reference")
			}
			text := result.Content[0].(mcp.TextContent).Text
			if !strings.Contains(text, `"Nope"`) || !strings.Contains(text, `"01 Projects"`) {
				t.Errorf("error should name the failing segment and the candidates, got: %s", text)
			}
			for _, path := range seen {
				if strings.HasSuffix(path, "/messages") {
					t.Errorf("an unresolvable folder must not fall back to querying messages; saw %v", seen)
				}
			}
		})
	}
}

// TestMessageVerbs_NoFolderQueriesAllFolders verifies that omitting the folder
// scope still queries across all folders, i.e. the new resolution step did not
// turn an optional parameter into a required one.
func TestMessageVerbs_NoFolderQueriesAllFolders(t *testing.T) {
	for _, c := range folderScopedVerbs() {
		t.Run(c.name, func(t *testing.T) {
			result, seen := callFolderScoped(t, c, map[string]any{})

			if result.IsError {
				if text := result.Content[0].(mcp.TextContent).Text; strings.Contains(text, "folder") {
					t.Errorf("omitting the folder scope must not produce a folder error, got: %s", text)
				}
			}
			for _, path := range seen {
				if strings.Contains(path, "/mailFolders/") {
					t.Errorf("no folder lookup should occur when no folder was given; saw %v", seen)
				}
			}
		})
	}
}
