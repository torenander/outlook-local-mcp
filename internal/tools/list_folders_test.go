// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for the merged list_folders handler: default
// single-level listing, the recursive/max_depth contract (max_depth=1 means
// exactly one level), path addressing, and the three distinct output tiers.
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/desek/outlook-local-mcp/internal/auth"
	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/mark3labs/mcp-go/mcp"
)

// callListFolders invokes the list_folders handler against the mock folder
// hierarchy and returns the response text plus every URL path requested.
func callListFolders(t *testing.T, args map[string]any) (*mcp.CallToolResult, []string) {
	t.Helper()

	var seen []string
	client, srv := newTestGraphClient(t, newFolderMockHandler(t, &seen))
	defer srv.Close()

	ctx := auth.WithGraphClient(context.Background(), client)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args

	result, err := NewHandleListFolders(graph.RetryConfig{}, 0)(ctx, req)
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	return result, seen
}

// resultText extracts the text payload of a tool result, failing the test when
// the result is an error.
func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	text := result.Content[0].(mcp.TextContent).Text
	if result.IsError {
		t.Fatalf("tool returned an error: %s", text)
	}
	return text
}

// TestNewHandleListFolders_ReturnsHandler validates that the constructor
// returns a usable handler.
func TestNewHandleListFolders_ReturnsHandler(t *testing.T) {
	if NewHandleListFolders(graph.RetryConfig{}, 0) == nil {
		t.Fatal("expected non-nil handler function")
	}
}

// TestListFolders_NoClient validates the no-account error path.
func TestListFolders_NoClient(t *testing.T) {
	result, err := NewHandleListFolders(graph.RetryConfig{}, 0)(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when no client in context")
	}
	if text := result.Content[0].(mcp.TextContent).Text; text != "no account selected" {
		t.Errorf("error text = %q, want %q", text, "no account selected")
	}
}

// TestListFolders_InvalidOutputMode validates that an unsupported output tier
// is rejected before any Graph call.
func TestListFolders_InvalidOutputMode(t *testing.T) {
	result, seen := callListFolders(t, map[string]any{"output": "yaml"})
	if !result.IsError {
		t.Fatal("expected error result for an unsupported output mode")
	}
	if len(seen) != 0 {
		t.Errorf("invalid output mode must be rejected before any Graph call, got %v", seen)
	}
}

// TestListFolders_DefaultIsSingleLevelText validates that the default call
// lists exactly one level as a markdown tree, flags folders with unfetched
// subfolders, and does not leak Graph IDs into the cheap tier.
func TestListFolders_DefaultIsSingleLevelText(t *testing.T) {
	result, seen := callListFolders(t, map[string]any{})
	text := resultText(t, result)

	for _, want := range []string{
		"- Inbox — 3 unread / 42",
		"- Archive — 0 / 1204",
		"- Team Reports — 1 unread / 7",
		"[+1 subfolders — use recursive=true]",
		"4 folders. Target one with folder=",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in default output, got:\n%s", want, text)
		}
	}
	if strings.Contains(text, mockInboxID) {
		t.Errorf("default text tier must not contain Graph IDs, got:\n%s", text)
	}
	if len(seen) != 1 {
		t.Errorf("a non-recursive listing must issue exactly one Graph call, got %v", seen)
	}
}

// TestListFolders_MaxDepthOneIsExactlyOneLevel pins the off-by-one fix:
// recursive=true with max_depth=1 returns the requested level and nothing
// below it. Before the merge, max_depth=1 silently returned two levels.
func TestListFolders_MaxDepthOneIsExactlyOneLevel(t *testing.T) {
	result, seen := callListFolders(t, map[string]any{"recursive": true, "max_depth": float64(1)})
	text := resultText(t, result)

	if strings.Contains(text, "01 Projects") {
		t.Errorf("max_depth=1 must not descend into subfolders, got:\n%s", text)
	}
	if len(seen) != 1 {
		t.Errorf("max_depth=1 must issue exactly one Graph call, got %v", seen)
	}
}

// TestListFolders_MaxDepthTwo verifies that the second level is returned and
// the third is not.
func TestListFolders_MaxDepthTwo(t *testing.T) {
	result, _ := callListFolders(t, map[string]any{"recursive": true, "max_depth": float64(2)})
	text := resultText(t, result)

	if !strings.Contains(text, "  - 01 Projects — 0 / 58") {
		t.Errorf("expected the second level indented one step, got:\n%s", text)
	}
	if strings.Contains(text, "Swedfund") {
		t.Errorf("max_depth=2 must not reach the third level, got:\n%s", text)
	}
}

// TestListFolders_RecursiveDefaultDepth verifies that recursive=true without
// max_depth reaches three levels, and that the footer offers the deepest path
// as a worked `folder` value.
func TestListFolders_RecursiveDefaultDepth(t *testing.T) {
	result, _ := callListFolders(t, map[string]any{"recursive": true})
	text := resultText(t, result)

	if !strings.Contains(text, "    - Swedfund — 0 / 8") {
		t.Errorf("expected the third level indented two steps, got:\n%s", text)
	}
	if !strings.Contains(text, `Target one with folder="Inbox/01 Projects/Swedfund".`) {
		t.Errorf("expected the footer to offer the deepest path, got:\n%s", text)
	}
}

// TestListFolders_ByWellKnownName verifies that `folder` scopes the listing to
// one folder's children.
func TestListFolders_ByWellKnownName(t *testing.T) {
	result, _ := callListFolders(t, map[string]any{"folder": "Inbox"})
	text := resultText(t, result)

	if !strings.Contains(text, "- 01 Projects — 0 / 58") {
		t.Errorf("expected Inbox's child folder, got:\n%s", text)
	}
	if strings.Contains(text, "Archive") {
		t.Errorf("a scoped listing must not include sibling top-level folders, got:\n%s", text)
	}
}

// TestListFolders_ByPath verifies the headline capability of the amended CR:
// a folder addressed by the same natural-language path the text tier prints.
func TestListFolders_ByPath(t *testing.T) {
	result, _ := callListFolders(t, map[string]any{"folder": "Inbox/01 Projects"})
	text := resultText(t, result)

	if !strings.Contains(text, "- Swedfund — 0 / 8") {
		t.Errorf("expected the subfolder addressed by path, got:\n%s", text)
	}
	if !strings.Contains(text, `folder="Inbox/01 Projects/Swedfund"`) {
		t.Errorf("expected paths to stay absolute under a scoped listing, got:\n%s", text)
	}
}

// TestListFolders_LegacyFolderIDAlias verifies that the pre-rename folder_id
// spelling still selects the starting folder.
func TestListFolders_LegacyFolderIDAlias(t *testing.T) {
	result, _ := callListFolders(t, map[string]any{"folder_id": "Inbox"})
	text := resultText(t, result)

	if !strings.Contains(text, "- 01 Projects — 0 / 58") {
		t.Errorf("folder_id alias should scope the listing, got:\n%s", text)
	}
}

// TestListFolders_UnresolvableFolder verifies that an unmatched path segment
// produces an actionable error rather than an empty listing.
func TestListFolders_UnresolvableFolder(t *testing.T) {
	result, _ := callListFolders(t, map[string]any{"folder": "Inbox/Nope"})
	if !result.IsError {
		t.Fatal("expected an error for an unresolvable folder reference")
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, `"Nope"`) || !strings.Contains(text, `"01 Projects"`) {
		t.Errorf("error should name the failing segment and the candidates, got: %s", text)
	}
}

// TestListFolders_EmptyChildListing verifies the framing message when a folder
// has no subfolders.
func TestListFolders_EmptyChildListing(t *testing.T) {
	result, _ := callListFolders(t, map[string]any{"folder": "Archive"})
	text := resultText(t, result)

	if text != `No subfolders found under "Archive".` {
		t.Errorf("text = %q, want the empty-listing message naming the parent", text)
	}
}

// TestListFolders_SummaryAndRawDiffer is the regression test for CR-0066 FR-9:
// the two JSON tiers used to be byte-identical. summary must speak the tool's
// vocabulary, raw must speak Graph's.
func TestListFolders_SummaryAndRawDiffer(t *testing.T) {
	summaryResult, _ := callListFolders(t, map[string]any{"output": "summary"})
	rawResult, _ := callListFolders(t, map[string]any{"output": "raw"})

	summary := resultText(t, summaryResult)
	raw := resultText(t, rawResult)

	if summary == raw {
		t.Fatal("summary and raw output are identical; the tiers are not distinct")
	}

	for _, want := range []string{`"name"`, `"unread"`, `"total"`, `"subfolder_count"`, `"path"`, `"id"`} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary output missing key %s:\n%s", want, summary)
		}
	}
	for _, unwanted := range []string{`"displayName"`, `"unreadItemCount"`, `"childFolderCount"`} {
		if strings.Contains(summary, unwanted) {
			t.Errorf("summary output must not use Graph field name %s:\n%s", unwanted, summary)
		}
	}
	for _, want := range []string{`"displayName"`, `"unreadItemCount"`, `"totalItemCount"`, `"childFolderCount"`, `"value"`} {
		if !strings.Contains(raw, want) {
			t.Errorf("raw output missing Graph field %s:\n%s", want, raw)
		}
	}

	// Both JSON tiers must parse and both must carry the folder IDs the text
	// tier deliberately omits.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(summary), &parsed); err != nil {
		t.Fatalf("summary output is not valid JSON: %v", err)
	}
	if !strings.Contains(summary, mockInboxID) || !strings.Contains(raw, mockInboxID) {
		t.Error("both JSON tiers must expose folder IDs")
	}
}

// TestClampInt32 validates the numeric parameter clamp used for max_results
// and max_depth.
func TestClampInt32(t *testing.T) {
	tests := []struct {
		name                              string
		value                             float64
		minValue, maxValue, fallback, out int32
	}{
		{"in range", 50, 1, 1000, 100, 50},
		{"below minimum falls back", 0, 1, 1000, 100, 100},
		{"negative falls back", -7, 1, 1000, 100, 100},
		{"above maximum clamps", 5000, 1, 1000, 100, 1000},
		{"at maximum", 1000, 1, 1000, 100, 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampInt32(tt.value, tt.minValue, tt.maxValue, tt.fallback); got != tt.out {
				t.Errorf("clampInt32(%v) = %d, want %d", tt.value, got, tt.out)
			}
		})
	}
}

// TestListFolders_HiddenSubfolderNotMislabelled reproduces, end to end, the bug
// that live testing against a real Microsoft 365 mailbox exposed and that the
// original mock could not.
//
// "Conversation History" reports childFolderCount=1 (the Teams "Team Chat"
// folder) but returns nothing from GET /childFolders, because Graph counts
// hidden folders and then withholds them. With recursive=true and the node
// comfortably inside max_depth, the node WAS expanded — so the old
// "[+1 subfolders — use recursive=true]" hint was simply false, and following
// it returns an empty listing that invites an endless retry.
func TestListFolders_HiddenSubfolderNotMislabelled(t *testing.T) {
	result, _ := callListFolders(t, map[string]any{"recursive": true, "max_depth": float64(2)})
	text := resultText(t, result)

	var line string
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, "Conversation History") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("Conversation History missing from output:\n%s", text)
	}

	if !strings.Contains(line, "[1 subfolder hidden]") {
		t.Errorf("expected the withheld subfolder to be reported as hidden, got: %q", line)
	}
	for _, forbidden := range []string{"recursive=true", "max_depth", "max_results"} {
		if strings.Contains(line, forbidden) {
			t.Errorf("must not suggest %q for a folder Graph withheld, got: %q", forbidden, line)
		}
	}

	// The sibling that genuinely has reachable children must still expand, so
	// the fix does not suppress real subfolders.
	if !strings.Contains(text, "  - 01 Projects — 0 / 58") {
		t.Errorf("expandable siblings must still expand, got:\n%s", text)
	}
}

// TestListFolders_UnexploredHintTracksRecursion verifies the actionable hint
// names the lever the caller has not pulled yet: recursive=true when they did
// not recurse, max_depth when they did but ran out of budget.
func TestListFolders_UnexploredHintTracksRecursion(t *testing.T) {
	flat, _ := callListFolders(t, map[string]any{})
	if !strings.Contains(resultText(t, flat), "[+1 subfolders — use recursive=true]") {
		t.Errorf("non-recursive listing should suggest recursive=true, got:\n%s", resultText(t, flat))
	}

	// max_depth=2 expands Inbox but not 01 Projects, which still has a child.
	deep, _ := callListFolders(t, map[string]any{"recursive": true, "max_depth": float64(2)})
	text := resultText(t, deep)
	if !strings.Contains(text, "[+1 subfolders — increase max_depth]") {
		t.Errorf("depth-limited node should suggest increase max_depth, got:\n%s", text)
	}
}

// TestListFolders_HiddenSubfolderInJSONTiers verifies the same disambiguation
// reaches both JSON tiers. subfolder_count=1 with no children and no
// explanation is the identical trap in machine-readable form.
func TestListFolders_HiddenSubfolderInJSONTiers(t *testing.T) {
	for _, tc := range []struct{ mode, hidden, unexplored string }{
		{"summary", `"hidden_subfolders":1`, `"unexplored_subfolders":1`},
		{"raw", `"_hiddenChildFolders":1`, `"_unexploredChildFolders":1`},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			result, _ := callListFolders(t, map[string]any{
				"recursive": true, "max_depth": float64(2), "output": tc.mode,
			})
			text := resultText(t, result)

			if !strings.Contains(text, tc.hidden) {
				t.Errorf("%s output must flag the withheld subfolder, got:\n%s", tc.mode, text)
			}
			if !strings.Contains(text, tc.unexplored) {
				t.Errorf("%s output must flag the depth-limited subfolder, got:\n%s", tc.mode, text)
			}
		})
	}
}
