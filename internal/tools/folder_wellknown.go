// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file enumerates the Microsoft Graph well-known mail folder names. Graph
// accepts these names anywhere a folder id is expected, so a reference that
// matches one is passed straight through without a lookup round-trip.
package tools

import "strings"

// wellKnownFolderLabels maps each Graph well-known mail folder name (always
// lowercase on the wire) to the display label Outlook shows for it. The label
// is used as the root segment of a folder path so that text output reads
// "Sent Items/2024" rather than "sentitems/2024".
//
// The list mirrors the mailFolder well-known names documented for the
// /me/mailFolders/{id} addressing form.
var wellKnownFolderLabels = map[string]string{
	"archive":                   "Archive",
	"clutter":                   "Clutter",
	"conflicts":                 "Conflicts",
	"conversationhistory":       "Conversation History",
	"deleteditems":              "Deleted Items",
	"drafts":                    "Drafts",
	"inbox":                     "Inbox",
	"junkemail":                 "Junk Email",
	"localfailures":             "Local Failures",
	"msgfolderroot":             "Mailbox Root",
	"outbox":                    "Outbox",
	"recoverableitemsdeletions": "Recoverable Items",
	"scheduled":                 "Scheduled",
	"searchfolders":             "Search Folders",
	"sentitems":                 "Sent Items",
	"serverfailures":            "Server Failures",
	"syncissues":                "Sync Issues",
}

// wellKnownFolder reports whether ref is a Graph well-known mail folder name
// and returns the Outlook display label for it.
//
// Matching is case-insensitive and ignores surrounding whitespace, so "Inbox",
// "inbox", and " INBOX " all resolve.
//
// Parameters:
//   - ref: the caller-supplied folder reference.
//
// Returns the canonical lowercase Graph name, the Outlook display label, and
// true when ref is well-known; otherwise two empty strings and false.
//
// Side effects: none.
func wellKnownFolder(ref string) (name, label string, ok bool) {
	key := strings.ToLower(strings.TrimSpace(ref))
	label, ok = wellKnownFolderLabels[key]
	if !ok {
		return "", "", false
	}
	return key, label, true
}
