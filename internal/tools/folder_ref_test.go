// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for natural-language folder addressing: well-known
// name pass-through, case-insensitive display-name path walking, Graph-id
// detection, and the actionable failure message for an unresolved segment.
package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/desek/outlook-local-mcp/internal/graph"
)

// TestWellKnownFolder verifies case-insensitive matching of Graph well-known
// folder names and the display labels used to build readable paths.
func TestWellKnownFolder(t *testing.T) {
	tests := []struct {
		ref       string
		wantName  string
		wantLabel string
		wantOK    bool
	}{
		{"inbox", "inbox", "Inbox", true},
		{"Inbox", "inbox", "Inbox", true},
		{"  SENTITEMS  ", "sentitems", "Sent Items", true},
		{"deleteditems", "deleteditems", "Deleted Items", true},
		{"archive", "archive", "Archive", true},
		{"01 Projects", "", "", false},
		{"", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			name, label, ok := wellKnownFolder(tt.ref)
			if ok != tt.wantOK || name != tt.wantName || label != tt.wantLabel {
				t.Errorf("wellKnownFolder(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.ref, name, label, ok, tt.wantName, tt.wantLabel, tt.wantOK)
			}
		})
	}
}

// TestSplitFolderPath verifies that path splitting trims segments and drops
// empty ones, so "Inbox / 01 Projects /" is the same reference as
// "Inbox/01 Projects".
func TestSplitFolderPath(t *testing.T) {
	tests := []struct {
		path string
		want []string
	}{
		{"Inbox/01 Projects", []string{"Inbox", "01 Projects"}},
		{"Inbox / 01 Projects /", []string{"Inbox", "01 Projects"}},
		{"/Inbox//Sub/", []string{"Inbox", "Sub"}},
		{"", nil},
		{"///", nil},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := splitFolderPath(tt.path)
			if len(got) != len(tt.want) {
				t.Fatalf("splitFolderPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitFolderPath(%q)[%d] = %q, want %q", tt.path, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestLooksLikeGraphFolderID verifies that the id heuristic accepts real
// base64url folder ids and rejects the display names and paths a user types.
func TestLooksLikeGraphFolderID(t *testing.T) {
	realID := "AAMkAGI2THVSAAA=AAMkAGI2THVSAAA=AAMkAGI2THVSAAA="
	tests := []struct {
		ref  string
		want bool
	}{
		{realID, true},
		{"AAMkAGI2_THVS-AAA=" + strings.Repeat("A", 30), true},
		{"Inbox", false},
		{"Inbox/01 Projects", false},
		{"01 Projects", false},
		// A long, slash-separated path does satisfy the raw shape test. That is
		// harmless because resolution attempts the path walk first and only
		// falls back to id pass-through when the walk finds nothing.
		{"Projects/Customers/Contoso/Engineering/Q1", true},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			if got := looksLikeGraphFolderID(tt.ref); got != tt.want {
				t.Errorf("looksLikeGraphFolderID(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}

// TestResolveFolderRef_WellKnown verifies that a well-known name is passed
// straight through to Graph without any lookup request.
func TestResolveFolderRef_WellKnown(t *testing.T) {
	var seen []string
	client, srv := newTestGraphClient(t, newFolderMockHandler(t, &seen))
	defer srv.Close()

	id, err := ResolveFolderRef(context.Background(), client, graph.RetryConfig{}, 0, "Inbox")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "inbox" {
		t.Errorf("id = %q, want %q", id, "inbox")
	}
	if len(seen) != 0 {
		t.Errorf("well-known name should not issue lookup requests, got %v", seen)
	}
}

// TestResolveFolderRef_TopLevelName verifies that a bare display name that is
// not a Graph well-known name is matched against the top-level folders,
// case-insensitively.
func TestResolveFolderRef_TopLevelName(t *testing.T) {
	client, srv := newTestGraphClient(t, newFolderMockHandler(t, nil))
	defer srv.Close()

	id, err := ResolveFolderRef(context.Background(), client, graph.RetryConfig{}, 0, "team reports")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != mockReportsID {
		t.Errorf("id = %q, want %q", id, mockReportsID)
	}
}

// TestResolveFolderPath_NestedPath verifies that a display-name path is walked
// segment by segment, case-insensitively, and that the canonical path returned
// uses the folders' real letter case.
func TestResolveFolderPath_NestedPath(t *testing.T) {
	client, srv := newTestGraphClient(t, newFolderMockHandler(t, nil))
	defer srv.Close()

	id, path, err := ResolveFolderPath(context.Background(), client, graph.RetryConfig{}, 0, "inbox/01 projects")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != mockProjectsID {
		t.Errorf("id = %q, want %q", id, mockProjectsID)
	}
	if path != "Inbox/01 Projects" {
		t.Errorf("path = %q, want %q", path, "Inbox/01 Projects")
	}
}

// TestResolveFolderPath_DeepPath verifies that a three-segment path resolves
// to the leaf folder.
func TestResolveFolderPath_DeepPath(t *testing.T) {
	client, srv := newTestGraphClient(t, newFolderMockHandler(t, nil))
	defer srv.Close()

	id, path, err := ResolveFolderPath(context.Background(), client, graph.RetryConfig{}, 0, "Inbox/01 Projects/Swedfund")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != mockSwedfundID {
		t.Errorf("id = %q, want %q", id, mockSwedfundID)
	}
	if path != "Inbox/01 Projects/Swedfund" {
		t.Errorf("path = %q, want %q", path, "Inbox/01 Projects/Swedfund")
	}
}

// TestResolveFolderRef_UnknownSegment verifies that a failed walk names the
// segment that did not match, says where it looked, and lists the candidates
// that were there — everything the caller needs to retry without guessing.
func TestResolveFolderRef_UnknownSegment(t *testing.T) {
	client, srv := newTestGraphClient(t, newFolderMockHandler(t, nil))
	defer srv.Close()

	_, err := ResolveFolderRef(context.Background(), client, graph.RetryConfig{}, 0, "Inbox/Nope")
	if err == nil {
		t.Fatal("expected an error for an unmatched path segment")
	}
	msg := err.Error()
	for _, want := range []string{`"Nope"`, `"Inbox"`, `"01 Projects"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should mention %s", msg, want)
		}
	}
}

// TestResolveFolderRef_UnknownTopLevel verifies the root-level failure message
// points at the top level rather than a parent folder.
func TestResolveFolderRef_UnknownTopLevel(t *testing.T) {
	client, srv := newTestGraphClient(t, newFolderMockHandler(t, nil))
	defer srv.Close()

	_, err := ResolveFolderRef(context.Background(), client, graph.RetryConfig{}, 0, "Nonexistent")
	if err == nil {
		t.Fatal("expected an error for an unmatched top-level folder")
	}
	if !strings.Contains(err.Error(), "the top level") {
		t.Errorf("error %q should mention the top level", err.Error())
	}
}

// TestResolveFolderRef_Empty verifies that a blank reference is rejected
// before any network call.
func TestResolveFolderRef_Empty(t *testing.T) {
	client, srv := newTestGraphClient(t, newFolderMockHandler(t, nil))
	defer srv.Close()

	if _, err := ResolveFolderRef(context.Background(), client, graph.RetryConfig{}, 0, "   "); err == nil {
		t.Fatal("expected an error for a blank folder reference")
	}
}

// TestResolveFolderRef_GraphIDPassthrough verifies that an id-shaped reference
// is handed to Graph verbatim without a lookup.
func TestResolveFolderRef_GraphIDPassthrough(t *testing.T) {
	var seen []string
	client, srv := newTestGraphClient(t, newFolderMockHandler(t, &seen))
	defer srv.Close()

	raw := "AAMkAGI2THVSAAA=AAMkAGI2THVSAAA=AAMkAGI2THVSAAA="
	id, err := ResolveFolderRef(context.Background(), client, graph.RetryConfig{}, 0, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != raw {
		t.Errorf("id = %q, want the reference passed through unchanged", id)
	}
	if len(seen) != 0 {
		t.Errorf("id pass-through should not issue lookup requests, got %v", seen)
	}
}

// TestResolveFolderPath_GraphIDResolvesDisplayName verifies that addressing a
// folder by opaque id still yields a human-readable path, so text output
// remains labelled even when the caller supplied an id.
func TestResolveFolderPath_GraphIDResolvesDisplayName(t *testing.T) {
	client, srv := newTestGraphClient(t, newFolderMockHandler(t, nil))
	defer srv.Close()

	id, path, err := ResolveFolderPath(context.Background(), client, graph.RetryConfig{}, 0, mockProjectsID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != mockProjectsID {
		t.Errorf("id = %q, want %q", id, mockProjectsID)
	}
	if path != "01 Projects" {
		t.Errorf("path = %q, want %q", path, "01 Projects")
	}
}

// TestLooksLikeGraphFolderID_LiveMailboxID pins the heuristic against a folder
// id captured from a live Microsoft 365 mailbox. The id is 120 characters of
// URL-safe base64 — three times minGraphFolderIDLen — and uses only
// alphanumerics plus '_', '-' and '='.
//
// This is the evidence behind the threshold. If a future mailbox type emits
// ids that fail this test, the constant and its doc comment must be revisited
// together.
func TestLooksLikeGraphFolderID_LiveMailboxID(t *testing.T) {
	const liveID = "AQMkADhkN2JlZQBhYi1lMjNhLTRjNDctYTVmMi0wMjhiNTliMWYyNzIALgAAAyxSSpj_kzdFju-9dZN_ICwBAIbY0YMDKXBOtEoMTonV13QAAAIBDAAAAA=="

	if got := len(liveID); got != 120 {
		t.Fatalf("sample live id length = %d, want 120 (sample was edited?)", got)
	}
	if !looksLikeGraphFolderID(liveID) {
		t.Error("a real Microsoft 365 folder id must be recognised as an id")
	}

	// Real display names from the same mailbox must never be mistaken for ids.
	for _, name := range []string{"Inbox", "Archive", "Conversation History", "Deleted Items", "Sent Items"} {
		if looksLikeGraphFolderID(name) {
			t.Errorf("display name %q must not be classified as a Graph id", name)
		}
	}
}
