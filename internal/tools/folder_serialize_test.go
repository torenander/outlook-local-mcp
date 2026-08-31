// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for the summary and raw folder projections,
// pinning the contract that the two JSON tiers are genuinely different views
// of the same tree (CR-0066 FR-9) rather than the same bytes twice.
package tools

import (
	"encoding/json"
	"testing"
)

// sampleFolderListing is a two-level listing with one truncated node and one
// failed subtree, exercising every optional key in both projections.
func sampleFolderListing() FolderListing {
	return FolderListing{
		Root:      "Inbox",
		Truncated: true,
		Nodes: []FolderNode{
			{
				ID: "id-projects", Name: "01 Projects", Path: "Inbox/01 Projects",
				Unread: 2, Total: 58, SubfolderCount: 1,
				Children: []FolderNode{
					{ID: "id-swedfund", Name: "Swedfund", Path: "Inbox/01 Projects/Swedfund", Total: 8},
				},
			},
			{
				ID: "id-legal", Name: "Legal", Path: "Inbox/Legal",
				SubfolderCount: 3, Err: "could not load subfolders: throttled",
			},
			{
				ID: "id-big", Name: "Big", Path: "Inbox/Big",
				SubfolderCount: 900, Truncated: true,
			},
		},
	}
}

// TestSerializeSummaryFolders verifies the tool-native key set, the recursive
// count, and that optional keys appear only when they carry information.
func TestSerializeSummaryFolders(t *testing.T) {
	out := SerializeSummaryFolders(sampleFolderListing())

	if got := out["count"]; got != 4 {
		t.Errorf("count = %v, want 4 (recursive)", got)
	}
	if out["truncated"] != true {
		t.Error("root-level truncation must be reported in summary output")
	}
	if out["parent"] != "Inbox" {
		t.Errorf("parent = %v, want %q", out["parent"], "Inbox")
	}

	folders := out["folders"].([]map[string]any)
	if len(folders) != 3 {
		t.Fatalf("folders length = %d, want 3", len(folders))
	}

	projects := folders[0]
	for key, want := range map[string]any{
		"name": "01 Projects", "path": "Inbox/01 Projects",
		"unread": int32(2), "total": int32(58), "subfolder_count": int32(1),
		"id": "id-projects",
	} {
		if projects[key] != want {
			t.Errorf("summary[%q] = %v, want %v", key, projects[key], want)
		}
	}
	if _, ok := projects["children"]; !ok {
		t.Error("summary node with fetched children must carry a children key")
	}
	if _, ok := projects["error"]; ok {
		t.Error("summary must omit the error key when the subtree loaded")
	}

	if folders[1]["error"] != "could not load subfolders: throttled" {
		t.Error("a failed subtree must surface its error in summary output")
	}
	if _, ok := folders[1]["children"]; ok {
		t.Error("summary must omit the children key when nothing was fetched")
	}
	if folders[2]["truncated"] != true {
		t.Error("a truncated child listing must be flagged in summary output")
	}
}

// TestSerializeRawFolders verifies the Graph vocabulary, the collection shape,
// and that nothing is elided in the debugging tier.
func TestSerializeRawFolders(t *testing.T) {
	out := SerializeRawFolders(sampleFolderListing())

	if out["_truncated"] != true {
		t.Error("root-level truncation must be reported in raw output")
	}

	value := out["value"].([]map[string]any)
	if len(value) != 3 {
		t.Fatalf("value length = %d, want 3", len(value))
	}

	projects := value[0]
	for key, want := range map[string]any{
		"id": "id-projects", "displayName": "01 Projects",
		"unreadItemCount": int32(2), "totalItemCount": int32(58), "childFolderCount": int32(1),
	} {
		if projects[key] != want {
			t.Errorf("raw[%q] = %v, want %v", key, projects[key], want)
		}
	}
	if _, ok := projects["childFolders"]; !ok {
		t.Error("raw nesting must use the Graph navigation-property name childFolders")
	}
	if _, ok := projects["path"]; ok {
		t.Error("raw output must not invent fields Graph does not have")
	}
	if value[1]["_error"] != "could not load subfolders: throttled" {
		t.Error("a failed subtree must surface its error in raw output")
	}
}

// TestSerializeFolders_TiersDiffer asserts directly that the two projections
// of one listing serialize to different bytes.
func TestSerializeFolders_TiersDiffer(t *testing.T) {
	listing := sampleFolderListing()

	summary, err := json.Marshal(SerializeSummaryFolders(listing))
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	raw, err := json.Marshal(SerializeRawFolders(listing))
	if err != nil {
		t.Fatalf("marshal raw: %v", err)
	}
	if string(summary) == string(raw) {
		t.Fatal("summary and raw projections are byte-identical")
	}
}

// TestSerializeFolders_EmptyListing verifies both tiers emit empty collections
// rather than JSON null for an empty listing.
func TestSerializeFolders_EmptyListing(t *testing.T) {
	summary, err := json.Marshal(SerializeSummaryFolders(FolderListing{}))
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if string(summary) != `{"count":0,"folders":[]}` {
		t.Errorf("summary = %s, want an empty folders array", summary)
	}

	raw, err := json.Marshal(SerializeRawFolders(FolderListing{}))
	if err != nil {
		t.Fatalf("marshal raw: %v", err)
	}
	if string(raw) != `{"value":[]}` {
		t.Errorf("raw = %s, want an empty Graph collection", raw)
	}
}
