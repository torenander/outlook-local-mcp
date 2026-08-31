// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file provides a small in-memory Microsoft Graph mail folder hierarchy
// served over HTTP, shared by the folder reference, folder listing, and folder
// serialization tests. It lets those tests exercise real resolution and
// recursion paths without a live mailbox.
package tools

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Mock folder ids. They are deliberately the length and alphabet of real Graph
// mailFolder ids so that looksLikeGraphFolderID classifies them the way it
// classifies production ids.
// mockGraphPrefix is the URL path prefix the msgraph SDK produces for /me
// requests when the request adapter is built without a real account context.
const mockGraphPrefix = "/v1.0/users/me-token-to-replace/mailFolders"

const (
	mockInboxID    = "AAMkAGI2THVSInboxAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	mockProjectsID = "AAMkAGI2THVSProjectsAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	mockSwedfundID = "AAMkAGI2THVSSwedfundAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	mockArchiveID  = "AAMkAGI2THVSArchiveAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	mockReportsID  = "AAMkAGI2THVSReportsAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
)

// newFolderMockHandler returns an http.Handler serving this hierarchy:
//
//	Inbox           3 unread / 42
//	  01 Projects   0 / 58
//	    Swedfund    0 / 8
//	Archive         0 / 1204
//	Team Reports    1 unread / 7
//
// Inbox and Archive are also Graph well-known names, so they can be addressed
// either way; "Team Reports" exists so that display-name matching at the top
// level is exercised without the well-known shortcut.
//
// Parameters:
//   - t: the test, used only to mark this a helper.
//   - seen: optional slice pointer that receives each requested URL path, so a
//     test can assert how many levels were fetched.
//
// Returns the handler for newTestGraphClient.
func newFolderMockHandler(t *testing.T, seen *[]string) http.Handler {
	t.Helper()

	inboxChildren := folderCollection(folderJSON(mockProjectsID, "01 Projects", 0, 58, 1))

	bodies := map[string]string{
		mockGraphPrefix: folderCollection(
			folderJSON(mockInboxID, "Inbox", 3, 42, 1),
			folderJSON(mockArchiveID, "Archive", 0, 1204, 0),
			folderJSON(mockReportsID, "Team Reports", 1, 7, 0),
		),
		mockGraphPrefix + "/" + mockInboxID + "/childFolders": inboxChildren,
		mockGraphPrefix + "/inbox/childFolders":               inboxChildren,
		mockGraphPrefix + "/" + mockProjectsID + "/childFolders": folderCollection(
			folderJSON(mockSwedfundID, "Swedfund", 0, 8, 0),
		),
		mockGraphPrefix + "/" + mockSwedfundID + "/childFolders": folderCollection(),
		mockGraphPrefix + "/" + mockArchiveID + "/childFolders":  folderCollection(),
		mockGraphPrefix + "/archive/childFolders":                folderCollection(),
		mockGraphPrefix + "/" + mockReportsID + "/childFolders":  folderCollection(),
		mockGraphPrefix + "/" + mockProjectsID:                   folderJSON(mockProjectsID, "01 Projects", 0, 58, 1),
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = append(*seen, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		body, ok := bodies[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"error":{"code":"ErrorItemNotFound","message":"not found"}}`)
			return
		}
		_, _ = fmt.Fprint(w, body)
	})
}

// folderJSON renders one mailFolder resource as Graph would return it.
func folderJSON(id, name string, unread, total, children int) string {
	return fmt.Sprintf(
		`{"id":%q,"displayName":%q,"unreadItemCount":%d,"totalItemCount":%d,"childFolderCount":%d}`,
		id, name, unread, total, children)
}

// folderCollection wraps folder resources in a Graph collection response.
func folderCollection(folders ...string) string {
	return `{"value":[` + strings.Join(folders, ",") + `]}`
}
