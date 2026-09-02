// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for the plain-text formatters used by the "text"
// output mode on read tools.
package tools

import (
	"strings"
	"testing"
)

// TestFormatEventsText_MultipleEvents verifies that multiple events are
// formatted as a numbered list with displayTime, location, showAs, organizer,
// and a total count (FR-18).
func TestFormatEventsText_MultipleEvents(t *testing.T) {
	events := []map[string]any{
		{
			"subject":     "Team Sync",
			"displayTime": "Wed Mar 19, 2:00 PM - 3:00 PM",
			"location":    "Conference Room A",
			"showAs":      "busy",
			"organizer":   "Alice Smith",
		},
		{
			"subject":     "1:1 with Bob",
			"displayTime": "Wed Mar 19, 4:00 PM - 4:30 PM",
			"location":    "Microsoft Teams",
			"showAs":      "busy",
			"organizer":   "Bob Jones",
		},
		{
			"subject":     "All Hands",
			"displayTime": "Wed Mar 19, 5:00 PM - 6:00 PM",
			"location":    "",
			"showAs":      "tentative",
			"organizer":   "Carol Lee",
		},
	}

	result := FormatEventsText(events)

	// Check numbered listing.
	if !strings.Contains(result, "1. Team Sync") {
		t.Error("expected '1. Team Sync' in output")
	}
	if !strings.Contains(result, "2. 1:1 with Bob") {
		t.Error("expected '2. 1:1 with Bob' in output")
	}
	if !strings.Contains(result, "3. All Hands") {
		t.Error("expected '3. All Hands' in output")
	}

	// Check time/location/status detail line.
	if !strings.Contains(result, "Wed Mar 19, 2:00 PM - 3:00 PM | Conference Room A | Busy") {
		t.Error("expected formatted detail line for Team Sync")
	}

	// Check organizer.
	if !strings.Contains(result, "Organizer: Alice Smith") {
		t.Error("expected 'Organizer: Alice Smith' in output")
	}

	// Check total.
	if !strings.Contains(result, "3 event(s) total.") {
		t.Error("expected '3 event(s) total.' in output")
	}

	// Event with empty location should not include extra pipe.
	if strings.Contains(result, "| |") {
		t.Error("empty location should not produce '| |'")
	}
}

// TestFormatEventsText_Empty verifies that an empty event list returns
// "No events found." (FR-18).
func TestFormatEventsText_Empty(t *testing.T) {
	result := FormatEventsText(nil)
	if result != "No events found." {
		t.Errorf("result = %q, want %q", result, "No events found.")
	}

	result = FormatEventsText([]map[string]any{})
	if result != "No events found." {
		t.Errorf("result = %q, want %q", result, "No events found.")
	}
}

// TestFormatFreeBusyText verifies that busy periods are formatted as a
// numbered list with time ranges, status, and a total count (FR-18).
func TestFormatFreeBusyText(t *testing.T) {
	data := FreeBusyResponse{
		TimeRange: FreeBusyTimeRange{
			Start: "2026-03-19T00:00:00",
			End:   "2026-03-19T23:59:59",
		},
		BusyPeriods: []BusyPeriod{
			{
				Start:   "2026-03-19T14:00:00",
				End:     "2026-03-19T15:00:00",
				Status:  "busy",
				Subject: "Team Sync",
			},
			{
				Start:   "2026-03-19T16:00:00",
				End:     "2026-03-19T16:30:00",
				Status:  "tentative",
				Subject: "1:1 with Bob",
			},
		},
	}

	result := FormatFreeBusyText(data)

	if !strings.Contains(result, "1. Team Sync") {
		t.Error("expected '1. Team Sync' in output")
	}
	if !strings.Contains(result, "2. 1:1 with Bob") {
		t.Error("expected '2. 1:1 with Bob' in output")
	}
	if !strings.Contains(result, "2 busy period(s) total.") {
		t.Error("expected '2 busy period(s) total.' in output")
	}
	if !strings.Contains(result, "Busy") {
		t.Error("expected title-cased 'Busy' in output")
	}
}

// TestFormatFreeBusyText_Empty verifies that empty busy periods returns the
// expected message.
func TestFormatFreeBusyText_Empty(t *testing.T) {
	data := FreeBusyResponse{
		TimeRange:   FreeBusyTimeRange{Start: "2026-03-19T00:00:00", End: "2026-03-19T23:59:59"},
		BusyPeriods: []BusyPeriod{},
	}

	result := FormatFreeBusyText(data)
	if result != "No busy periods found." {
		t.Errorf("result = %q, want %q", result, "No busy periods found.")
	}
}

// TestFormatCalendarsText verifies that calendars are formatted as a numbered
// list with name, default/read-only tags, owner, and a total count.
func TestFormatCalendarsText(t *testing.T) {
	calendars := []map[string]any{
		{
			"name":              "Calendar",
			"isDefaultCalendar": true,
			"canEdit":           true,
			"owner":             map[string]string{"name": "Alice Smith", "address": "alice@example.com"},
		},
		{
			"name":              "Shared Calendar",
			"isDefaultCalendar": false,
			"canEdit":           false,
			"owner":             map[string]string{"name": "Bob Jones", "address": "bob@example.com"},
		},
	}

	result := FormatCalendarsText(calendars)

	if !strings.Contains(result, "1. Calendar (default)") {
		t.Error("expected '1. Calendar (default)' in output")
	}
	if !strings.Contains(result, "2. Shared Calendar (read-only)") {
		t.Error("expected '2. Shared Calendar (read-only)' in output")
	}
	if !strings.Contains(result, "Owner: Alice Smith") {
		t.Error("expected 'Owner: Alice Smith' in output")
	}
	if !strings.Contains(result, "2 calendar(s) total.") {
		t.Error("expected '2 calendar(s) total.' in output")
	}
}

// TestFormatCalendarsText_Empty verifies that an empty calendar list returns
// the expected message.
func TestFormatCalendarsText_Empty(t *testing.T) {
	result := FormatCalendarsText(nil)
	if result != "No calendars found." {
		t.Errorf("result = %q, want %q", result, "No calendars found.")
	}
}

// TestFormatWriteConfirmation verifies that write tool confirmations include
// action, subject, event ID, time, and location.
func TestFormatWriteConfirmation(t *testing.T) {
	result := FormatWriteConfirmation(
		"created",
		"Weekly Sync",
		"AAMkAGQ3...",
		"Wed Mar 25, 2:00 PM - 3:00 PM",
		"Conference Room A",
	)

	if !strings.Contains(result, `Event created: "Weekly Sync"`) {
		t.Error("expected action and subject line")
	}
	if !strings.Contains(result, "ID: AAMkAGQ3...") {
		t.Error("expected ID line")
	}
	if !strings.Contains(result, "Time: Wed Mar 25, 2:00 PM - 3:00 PM") {
		t.Error("expected time line")
	}
	if !strings.Contains(result, "Location: Conference Room A") {
		t.Error("expected location line")
	}

	// Must not exceed 5 lines.
	lines := strings.Split(result, "\n")
	if len(lines) > 5 {
		t.Errorf("response has %d lines, want at most 5", len(lines))
	}
}

// TestFormatWriteConfirmation_NoLocation verifies that the Location line is
// omitted when location is empty.
func TestFormatWriteConfirmation_NoLocation(t *testing.T) {
	result := FormatWriteConfirmation(
		"updated",
		"Sprint Planning",
		"AAMkABC1...",
		"Thu Mar 26, 10:00 AM - 11:00 AM",
		"",
	)

	if !strings.Contains(result, `Event updated: "Sprint Planning"`) {
		t.Error("expected action and subject line")
	}
	if !strings.Contains(result, "ID: AAMkABC1...") {
		t.Error("expected ID line")
	}
	if !strings.Contains(result, "Time: Thu Mar 26, 10:00 AM - 11:00 AM") {
		t.Error("expected time line")
	}
	if strings.Contains(result, "Location:") {
		t.Error("location line should be omitted when location is empty")
	}

	// Must not exceed 5 lines.
	lines := strings.Split(result, "\n")
	if len(lines) > 5 {
		t.Errorf("response has %d lines, want at most 5", len(lines))
	}
}

// TestFormatEventDetailText verifies that a single event detail is formatted
// with subject, time, location, organizer, status, attendees, and body preview.
func TestFormatEventDetailText(t *testing.T) {
	event := map[string]any{
		"subject":     "Team Sync",
		"displayTime": "Wed Mar 19, 2:00 PM - 3:00 PM",
		"location":    "Conference Room A",
		"organizer":   "Alice Smith",
		"showAs":      "busy",
		"attendees": []map[string]string{
			{"name": "Bob Jones", "response": "accepted"},
			{"name": "Carol Lee", "response": "tentativelyAccepted"},
		},
		"bodyPreview": "Weekly sync to discuss project updates.",
	}

	result := FormatEventDetailText(event)

	if !strings.Contains(result, "Team Sync") {
		t.Error("expected 'Team Sync' in output")
	}
	if !strings.Contains(result, "Time: Wed Mar 19, 2:00 PM - 3:00 PM") {
		t.Error("expected formatted time line")
	}
	if !strings.Contains(result, "Location: Conference Room A") {
		t.Error("expected location line")
	}
	if !strings.Contains(result, "Organizer: Alice Smith") {
		t.Error("expected organizer line")
	}
	if !strings.Contains(result, "Status: Busy") {
		t.Error("expected status line")
	}
	if !strings.Contains(result, "- Bob Jones (accepted)") {
		t.Error("expected attendee Bob Jones with response")
	}
	if !strings.Contains(result, "- Carol Lee (tentativelyAccepted)") {
		t.Error("expected attendee Carol Lee with response")
	}
	if !strings.Contains(result, "Weekly sync to discuss project updates.") {
		t.Error("expected body preview")
	}
}

// TestFormatMessagesText verifies that multiple messages are formatted as a
// numbered list with sender, date, status flags, body preview, and total count.
func TestFormatMessagesText(t *testing.T) {
	messages := []map[string]any{
		{
			"subject":          "Weekly Design Review",
			"from":             map[string]string{"name": "Alice", "address": "alice@contoso.com"},
			"receivedDateTime": "2026-03-16T15:45:00Z",
			"isRead":           true,
			"hasAttachments":   false,
			"bodyPreview":      "Hi team, please review the attached mockups before...",
		},
		{
			"subject":          "Sprint Planning Notes",
			"from":             map[string]string{"name": "Bob", "address": "bob@contoso.com"},
			"receivedDateTime": "2026-03-16T14:12:00Z",
			"isRead":           false,
			"hasAttachments":   true,
			"bodyPreview":      "Here are the notes from today's sprint planning...",
		},
	}

	result := FormatMessagesText(messages)

	// Check numbered listing.
	if !strings.Contains(result, "1. Weekly Design Review") {
		t.Error("expected '1. Weekly Design Review' in output")
	}
	if !strings.Contains(result, "2. Sprint Planning Notes") {
		t.Error("expected '2. Sprint Planning Notes' in output")
	}

	// Check sender.
	if !strings.Contains(result, "From: alice@contoso.com") {
		t.Error("expected 'From: alice@contoso.com' in output")
	}
	if !strings.Contains(result, "From: bob@contoso.com") {
		t.Error("expected 'From: bob@contoso.com' in output")
	}

	// Check date formatting.
	if !strings.Contains(result, "Mon Mar 16, 2026") {
		t.Error("expected formatted date in output")
	}

	// Check flags: first message is read (no flag), second is unread with attachments.
	if strings.Contains(result, "[Unread]\n   Preview: Hi team") {
		t.Error("first message should not have [Unread] flag")
	}
	if !strings.Contains(result, "[Unread] [Has attachments]") {
		t.Error("expected '[Unread] [Has attachments]' for second message")
	}

	// Check preview.
	if !strings.Contains(result, "Preview: Hi team, please review") {
		t.Error("expected body preview for first message")
	}

	// Check total.
	if !strings.Contains(result, "2 message(s) total.") {
		t.Error("expected '2 message(s) total.' in output")
	}
}

// TestFormatMessagesText_Empty verifies that a nil message list returns
// "No messages found."
func TestFormatMessagesText_Empty(t *testing.T) {
	result := FormatMessagesText(nil)
	if result != "No messages found." {
		t.Errorf("result = %q, want %q", result, "No messages found.")
	}

	result = FormatMessagesText([]map[string]any{})
	if result != "No messages found." {
		t.Errorf("result = %q, want %q", result, "No messages found.")
	}
}

// TestFormatMessageDetailText verifies that a single message detail is formatted
// with subject, from, to, date, importance, attachment indicator, and body preview.
func TestFormatMessageDetailText(t *testing.T) {
	message := map[string]any{
		"subject":          "Weekly Design Review",
		"from":             map[string]string{"name": "Alice", "address": "alice@contoso.com"},
		"toRecipients":     []map[string]string{{"name": "Team", "address": "team@contoso.com"}},
		"receivedDateTime": "2026-03-16T15:45:00Z",
		"importance":       "high",
		"hasAttachments":   true,
		"bodyPreview":      "Hi team, please review the attached mockups before Wednesday...",
	}

	result := FormatMessageDetailText(message)

	if !strings.Contains(result, "Weekly Design Review\n") {
		t.Error("expected subject as first line")
	}
	if !strings.Contains(result, "From: alice@contoso.com") {
		t.Error("expected From line")
	}
	if !strings.Contains(result, "To: team@contoso.com") {
		t.Error("expected To line")
	}
	if !strings.Contains(result, "Date: Mon Mar 16, 2026 3:45 PM") {
		t.Error("expected formatted date line")
	}
	if !strings.Contains(result, "Importance: high") {
		t.Error("expected importance line for high importance")
	}
	if !strings.Contains(result, "[Has attachments]") {
		t.Error("expected attachment indicator")
	}
	if !strings.Contains(result, "Hi team, please review the attached mockups before Wednesday...") {
		t.Error("expected body preview")
	}
}

// TestFormatMessageDetailText_NormalImportance verifies that normal importance
// is not displayed in the detail view.
func TestFormatMessageDetailText_NormalImportance(t *testing.T) {
	message := map[string]any{
		"subject":          "Test",
		"from":             map[string]string{"name": "Alice", "address": "alice@contoso.com"},
		"receivedDateTime": "2026-03-16T15:45:00Z",
		"importance":       "normal",
		"hasAttachments":   false,
	}

	result := FormatMessageDetailText(message)

	if strings.Contains(result, "Importance:") {
		t.Error("normal importance should not be displayed")
	}
	if strings.Contains(result, "[Has attachments]") {
		t.Error("attachment indicator should not appear when hasAttachments is false")
	}
}

// TestFormatAccountsText verifies that accounts with no UPN or auth_method
// fall back to the label-and-state rendering and the total count is correct.
func TestFormatAccountsText(t *testing.T) {
	accounts := []map[string]any{
		{"label": "work", "authenticated": true},
		{"label": "personal", "authenticated": false},
	}

	result := FormatAccountsText(accounts)

	if !strings.Contains(result, "1. work (authenticated)") {
		t.Errorf("expected '1. work (authenticated)' in output, got:\n%s", result)
	}
	if !strings.Contains(result, "2. personal (disconnected)") {
		t.Errorf("expected '2. personal (disconnected)' in output, got:\n%s", result)
	}
	if !strings.Contains(result, "2 account(s) total.") {
		t.Error("expected '2 account(s) total.' in output")
	}
}

// TestFormatAccountsText_WithUPNAndMethod verifies the CR-0056 format
// "N. label — upn (state, auth_method)" for both authenticated and
// disconnected accounts.
func TestFormatAccountsText_WithUPNAndMethod(t *testing.T) {
	accounts := []map[string]any{
		{"label": "default", "authenticated": true, "email": "alice@contoso.com", "auth_method": "browser"},
		{"label": "work", "authenticated": false, "email": "bob@contoso.com", "auth_method": "device_code"},
	}

	result := FormatAccountsText(accounts)

	if !strings.Contains(result, "1. default — alice@contoso.com (authenticated, browser)") {
		t.Errorf("expected CR-0056 format line for default, got:\n%s", result)
	}
	if !strings.Contains(result, "2. work — bob@contoso.com (disconnected, device_code)") {
		t.Errorf("expected CR-0056 format line for work, got:\n%s", result)
	}
}

// TestFormatAccountsText_Empty verifies that a nil account list returns
// "No accounts registered."
func TestFormatAccountsText_Empty(t *testing.T) {
	result := FormatAccountsText(nil)
	if result != "No accounts registered." {
		t.Errorf("result = %q, want %q", result, "No accounts registered.")
	}

	result = FormatAccountsText([]map[string]any{})
	if result != "No accounts registered." {
		t.Errorf("result = %q, want %q", result, "No accounts registered.")
	}
}

// TestFormatStatusText verifies that the status tool text output includes
// version, timezone, uptime, account list, and feature flags.
func TestFormatStatusText(t *testing.T) {
	status := statusResponse{
		Version:             "1.2.0",
		Timezone:            "Europe/Stockholm",
		ServerUptimeSeconds: 13320, // 3h 42m
		Accounts: []statusAccount{
			{Label: "work", Authenticated: true},
			{Label: "personal", Authenticated: false},
		},
		Config: statusConfig{
			Features: statusConfigFeatures{
				ReadOnly:      false,
				MailEnabled:   true,
				ProvenanceTag: "mcp_created",
			},
		},
	}

	result := FormatStatusText(status)

	if !strings.Contains(result, "Server: outlook-local-mcp v1.2.0") {
		t.Error("expected server version line")
	}
	if !strings.Contains(result, "Timezone: Europe/Stockholm") {
		t.Error("expected timezone line")
	}
	if !strings.Contains(result, "Uptime: 3h 42m") {
		t.Error("expected uptime line")
	}
	if !strings.Contains(result, "work: authenticated") {
		t.Error("expected work account with authenticated state")
	}
	if !strings.Contains(result, "personal: disconnected") {
		t.Error("expected personal account with disconnected state")
	}
	if !strings.Contains(result, "Features: read-only=off, mail=on, mail-manage=off, provenance=mcp_created") {
		t.Error("expected features line")
	}
}

// TestFormatAccountsText_WithEmail verifies that accounts with an email field
// display the UPN alongside the label per CR-0056. Accounts with an empty
// email fall back to the label-only format, and disconnected accounts use the
// "disconnected" state wording.
func TestFormatAccountsText_WithEmail(t *testing.T) {
	accounts := []map[string]any{
		{"label": "work", "authenticated": true, "email": "work@example.com"},
		{"label": "personal", "authenticated": false, "email": ""},
	}

	result := FormatAccountsText(accounts)

	if !strings.Contains(result, "1. work — work@example.com (authenticated)") {
		t.Errorf("expected formatted account line with email, got:\n%s", result)
	}
	// Personal has no email — should still render label and disconnected state.
	if !strings.Contains(result, "2. personal (disconnected)") {
		t.Errorf("expected disconnected account line without email, got:\n%s", result)
	}
}

// TestFormatStatusText_WithUPN verifies the status text output renders each
// account's persisted UPN and auth_method when available (CR-0056 FR-31).
func TestFormatStatusText_WithUPN(t *testing.T) {
	status := statusResponse{
		Version:             "1.2.0",
		Timezone:            "UTC",
		ServerUptimeSeconds: 60,
		Accounts: []statusAccount{
			{Label: "default", Authenticated: true, UPN: "alice@contoso.com", AuthMethod: "browser"},
			{Label: "work", Authenticated: false, UPN: "bob@contoso.com", AuthMethod: "device_code"},
		},
	}

	result := FormatStatusText(status)

	if !strings.Contains(result, "default: authenticated — alice@contoso.com (browser)") {
		t.Errorf("expected default line with UPN and auth_method, got:\n%s", result)
	}
	if !strings.Contains(result, "work: disconnected — bob@contoso.com (device_code)") {
		t.Errorf("expected work line with UPN and auth_method, got:\n%s", result)
	}
}

// TestFormatAccountLine_IncludesDisconnectedAdvisory verifies that a non-empty
// advisory is appended on a new line, surfacing the wider account landscape
// (CR-0056 FR-52).
func TestFormatAccountLine_IncludesDisconnectedAdvisory(t *testing.T) {
	advisory := "Note: 'work' is disconnected; run account_login to reconnect."
	result := FormatAccountLine("default", "alice@contoso.com", advisory)
	want := "Account: default (alice@contoso.com)\n" + advisory
	if result != want {
		t.Errorf("FormatAccountLine = %q, want %q", result, want)
	}
}

// TestFormatAccountLine_WithEmail verifies the full label+email format.
func TestFormatAccountLine_WithEmail(t *testing.T) {
	result := FormatAccountLine("default", "user@example.com")
	want := "Account: default (user@example.com)"
	if result != want {
		t.Errorf("FormatAccountLine = %q, want %q", result, want)
	}
}

// TestFormatAccountLine_EmailOmitted verifies that email is omitted when empty.
func TestFormatAccountLine_EmailOmitted(t *testing.T) {
	result := FormatAccountLine("default", "")
	want := "Account: default"
	if result != want {
		t.Errorf("FormatAccountLine = %q, want %q", result, want)
	}
}

// TestFormatAccountLine_EmptyLabel verifies that an empty label returns "".
func TestFormatAccountLine_EmptyLabel(t *testing.T) {
	result := FormatAccountLine("", "user@example.com")
	if result != "" {
		t.Errorf("FormatAccountLine with empty label = %q, want empty string", result)
	}
}

// TestFormatFolderTreeText_Empty verifies that an empty listing reports no
// folders, and names the parent folder when one was requested.
func TestFormatFolderTreeText_Empty(t *testing.T) {
	if got := FormatFolderTreeText(FolderListing{}); got != "No folders found." {
		t.Errorf("FormatFolderTreeText(empty) = %q, want %q", got, "No folders found.")
	}

	got := FormatFolderTreeText(FolderListing{Nodes: []FolderNode{}, Root: "Inbox/01 Projects"})
	want := `No subfolders found under "Inbox/01 Projects".`
	if got != want {
		t.Errorf("FormatFolderTreeText(empty under root) = %q, want %q", got, want)
	}
}

// TestFormatFolderTreeText_Flat verifies the markdown rendering of a single
// level: one list item per folder, the word "unread" only when the unread
// count is non-zero, and a footer offering a worked `folder` value.
func TestFormatFolderTreeText_Flat(t *testing.T) {
	listing := FolderListing{Nodes: []FolderNode{
		{Name: "Inbox", Path: "Inbox", Unread: 3, Total: 42},
		{Name: "Sent Items", Path: "Sent Items", Unread: 0, Total: 100},
	}}

	result := FormatFolderTreeText(listing)

	if !strings.Contains(result, "- Inbox — 3 unread / 42\n") {
		t.Errorf("expected Inbox list item, got:\n%s", result)
	}
	if !strings.Contains(result, "- Sent Items — 0 / 100\n") {
		t.Errorf("expected Sent Items list item without the word 'unread', got:\n%s", result)
	}
	if strings.Contains(result, "Sent Items — 0 unread") {
		t.Errorf("the word 'unread' must be omitted for a zero count, got:\n%s", result)
	}
	if !strings.Contains(result, "2 folders. Target one with folder=") {
		t.Errorf("expected footer with folder hint, got:\n%s", result)
	}
}

// TestFormatFolderTreeText_Nested verifies two-space-per-level indentation,
// that paths (not Graph IDs) are what the footer hands back to the caller, and
// that the recursive folder count includes descendants.
func TestFormatFolderTreeText_Nested(t *testing.T) {
	listing := FolderListing{Nodes: []FolderNode{
		{
			Name: "Inbox", Path: "Inbox", Unread: 12, Total: 340, SubfolderCount: 1, Expanded: true,
			Children: []FolderNode{
				{
					Name: "01 Projects", Path: "Inbox/01 Projects", Unread: 0, Total: 58, SubfolderCount: 1, Expanded: true,
					Children: []FolderNode{
						{Name: "Swedfund", Path: "Inbox/01 Projects/Swedfund", Unread: 0, Total: 8},
					},
				},
			},
		},
		{Name: "Archive", Path: "Archive", Unread: 0, Total: 1204},
	}}

	result := FormatFolderTreeText(listing)

	for _, want := range []string{
		"- Inbox — 12 unread / 340\n",
		"  - 01 Projects — 0 / 58\n",
		"    - Swedfund — 0 / 8\n",
		"- Archive — 0 / 1204\n",
		`4 folders. Target one with folder="Inbox/01 Projects/Swedfund".`,
	} {
		if !strings.Contains(result, want) {
			t.Errorf("expected %q in output, got:\n%s", want, result)
		}
	}

	// The default tier must not leak Graph IDs — that is what makes it cheap.
	if strings.Contains(result, "AAMkAG") {
		t.Errorf("text output must not contain Graph IDs, got:\n%s", result)
	}
}

// TestFormatFolderTreeText_UnnamedFolder verifies that folders with an empty
// display name fall back to "(Unnamed)" rather than rendering a blank item.
func TestFormatFolderTreeText_UnnamedFolder(t *testing.T) {
	result := FormatFolderTreeText(FolderListing{Nodes: []FolderNode{{Name: "", Path: ""}}})

	if !strings.Contains(result, "(Unnamed)") {
		t.Errorf("expected (Unnamed) fallback, got: %q", result)
	}
}

// TestFormatFolderTreeText_SubfolderHint verifies that a non-recursive listing
// tells the caller which folders have subfolders it did not fetch, and that
// the suggested fix matches how the listing was requested.
func TestFormatFolderTreeText_SubfolderHint(t *testing.T) {
	node := FolderNode{Name: "Inbox", Path: "Inbox", Total: 10, SubfolderCount: 4}

	flat := FormatFolderTreeText(FolderListing{Nodes: []FolderNode{node}})
	if !strings.Contains(flat, "[+4 subfolders — use recursive=true]") {
		t.Errorf("a non-recursive listing should suggest recursive=true, got:\n%s", flat)
	}

	// Same unexpanded node, but the caller already asked to recurse — so the
	// only remaining lever is depth, and suggesting recursive=true would be
	// advice they have already followed.
	deep := FormatFolderTreeText(FolderListing{Nodes: []FolderNode{node}, Recursive: true})
	if !strings.Contains(deep, "[+4 subfolders — increase max_depth]") {
		t.Errorf("a recursive listing should suggest increase max_depth, got:\n%s", deep)
	}
	if strings.Contains(deep, "recursive=true") {
		t.Errorf("must not tell a recursive caller to pass recursive=true, got:\n%s", deep)
	}
}

// TestFormatFolderTreeText_HiddenSubfoldersNotActionable is the regression test
// for the state conflation found by live testing against a real Microsoft 365
// mailbox.
//
// "Conversation History" reports childFolderCount=1 (the Teams "Team Chat"
// folder) but returns nothing from GET /childFolders. The node was expanded,
// so the caller has already done everything they can; the old formatter told
// them to "use recursive=true" — advice that produces an empty result and
// invites an LLM to retry forever.
func TestFormatFolderTreeText_HiddenSubfoldersNotActionable(t *testing.T) {
	result := FormatFolderTreeText(FolderListing{
		Recursive: true,
		Nodes: []FolderNode{{
			Name: "Conversation History", Path: "Conversation History",
			SubfolderCount: 1, Expanded: true,
		}},
	})

	if !strings.Contains(result, "[1 subfolder hidden]") {
		t.Errorf("expected a hidden-subfolder annotation, got:\n%s", result)
	}
	for _, forbidden := range []string{"recursive=true", "max_depth", "max_results"} {
		if strings.Contains(result, forbidden) {
			t.Errorf("must not suggest %q for a subfolder Graph withheld, got:\n%s", forbidden, result)
		}
	}
}

// TestFormatFolderTreeText_HiddenSubfoldersPartial verifies the same
// disambiguation when Graph returns some but not all of the children it
// counted — the milder form of the same trap.
func TestFormatFolderTreeText_HiddenSubfoldersPartial(t *testing.T) {
	result := FormatFolderTreeText(FolderListing{
		Recursive: true,
		Nodes: []FolderNode{{
			Name: "Inbox", Path: "Inbox", SubfolderCount: 3, Expanded: true,
			Children: []FolderNode{{Name: "Clients", Path: "Inbox/Clients"}},
		}},
	})

	if !strings.Contains(result, "[2 subfolders hidden]") {
		t.Errorf("expected the two withheld subfolders to be reported, got:\n%s", result)
	}
}

// TestFormatFolderTreeText_ExpandedNoHint verifies that a fully expanded node
// carries no annotation at all, so the common case stays cheap.
func TestFormatFolderTreeText_ExpandedNoHint(t *testing.T) {
	result := FormatFolderTreeText(FolderListing{
		Recursive: true,
		Nodes: []FolderNode{{
			Name: "Inbox", Path: "Inbox", SubfolderCount: 1, Expanded: true,
			Children: []FolderNode{{Name: "Clients", Path: "Inbox/Clients"}},
		}},
	})

	if strings.Contains(result, "[") {
		t.Errorf("a fully expanded node needs no annotation, got:\n%s", result)
	}
}

// TestFormatFolderTreeText_ErrorSubtreeVisible verifies that a subtree which
// failed to load is surfaced in text output. Under the previous formatter the
// "_error" marker was rendered nowhere, so partial results looked complete.
func TestFormatFolderTreeText_ErrorSubtreeVisible(t *testing.T) {
	result := FormatFolderTreeText(FolderListing{Nodes: []FolderNode{
		{Name: "Inbox", Path: "Inbox", Total: 10, SubfolderCount: 2, Err: "could not load subfolders: throttled"},
	}})

	if !strings.Contains(result, "[subfolders unavailable: could not load subfolders: throttled]") {
		t.Errorf("expected failed subtree to be visible, got:\n%s", result)
	}
}

// TestFormatFolderTreeText_TruncationVisible verifies that both root-level and
// per-folder truncation are reported, so a capped listing is never mistaken
// for a complete one.
func TestFormatFolderTreeText_TruncationVisible(t *testing.T) {
	result := FormatFolderTreeText(FolderListing{
		Nodes: []FolderNode{
			{Name: "Inbox", Path: "Inbox", Total: 10, SubfolderCount: 500, Truncated: true},
		},
		Truncated: true,
	})

	if !strings.Contains(result, "[more subfolders not shown — raise max_results]") {
		t.Errorf("expected per-folder truncation marker, got:\n%s", result)
	}
	if !strings.Contains(result, "More folders exist than were returned") {
		t.Errorf("expected root-level truncation notice, got:\n%s", result)
	}
}

// TestFormatFolderTreeText_Singular verifies the footer uses the singular noun
// for a one-folder listing.
func TestFormatFolderTreeText_Singular(t *testing.T) {
	result := FormatFolderTreeText(FolderListing{Nodes: []FolderNode{
		{Name: "Archive", Path: "Archive", Total: 3},
	}})

	if !strings.Contains(result, `1 folder. Target one with folder="Archive".`) {
		t.Errorf("expected singular footer, got:\n%s", result)
	}
}

// TestFormatMessageDetailText_FullBodyReplacesPreview verifies that when a
// body_mode escalation attached a body, the detail view prints it instead of
// the 255-character preview, and adds no truncation notice.
func TestFormatMessageDetailText_FullBodyReplacesPreview(t *testing.T) {
	message := map[string]any{
		"subject":     "Long one",
		"bodyPreview": strings.Repeat("a", 255),
		"body":        map[string]string{"contentType": "text", "content": "The whole message."},
	}

	result := FormatMessageDetailText(message)

	if !strings.Contains(result, "The whole message.") {
		t.Errorf("full body missing from output:\n%s", result)
	}
	if strings.Contains(result, strings.Repeat("a", 255)) {
		t.Errorf("preview printed alongside the full body:\n%s", result)
	}
	if strings.Contains(result, bodyPreviewTruncatedNotice) {
		t.Errorf("truncation notice printed for an escalated body:\n%s", result)
	}
}

// TestFormatMessageDetailText_FullBodyFromJSONRoundTrip verifies that a body
// map that has been through JSON (map[string]any rather than map[string]string)
// is read the same way, matching how recipient slices are already handled.
func TestFormatMessageDetailText_FullBodyFromJSONRoundTrip(t *testing.T) {
	message := map[string]any{
		"subject": "Round trip",
		"body":    map[string]any{"contentType": "html", "content": "<p>Marked up.</p>"},
	}

	if result := FormatMessageDetailText(message); !strings.Contains(result, "<p>Marked up.</p>") {
		t.Errorf("body from a JSON round-trip was not printed:\n%s", result)
	}
}

// TestFormatMessageDetailText_TruncatedPreviewIsMarked verifies the in-band
// truncation marker: a preview at the cap is announced, a short one is not, and
// an empty body map does not suppress the fallback.
func TestFormatMessageDetailText_TruncatedPreviewIsMarked(t *testing.T) {
	cases := []struct {
		name        string
		message     map[string]any
		wantNotice  bool
		wantPreview string
	}{
		{
			name:        "preview at the cap is marked",
			message:     map[string]any{"subject": "S", "bodyPreview": strings.Repeat("b", 255)},
			wantNotice:  true,
			wantPreview: strings.Repeat("b", 255),
		},
		{
			name:        "short preview is not marked",
			message:     map[string]any{"subject": "S", "bodyPreview": "all of it"},
			wantNotice:  false,
			wantPreview: "all of it",
		},
		{
			name:        "empty body map falls back to the preview",
			message:     map[string]any{"subject": "S", "bodyPreview": "all of it", "body": map[string]string{"contentType": "text", "content": ""}},
			wantNotice:  false,
			wantPreview: "all of it",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := FormatMessageDetailText(tc.message)
			if !strings.Contains(result, tc.wantPreview) {
				t.Errorf("preview missing from output:\n%s", result)
			}
			if got := strings.Contains(result, bodyPreviewTruncatedNotice); got != tc.wantNotice {
				t.Errorf("truncation notice present = %v, want %v:\n%s", got, tc.wantNotice, result)
			}
		})
	}
}

// TestFormatConversationText_FullBodies verifies that escalated thread entries
// render a Body block rather than a Preview line, and that the footer hint is
// absent when nothing was truncated.
func TestFormatConversationText_FullBodies(t *testing.T) {
	thread := map[string]any{
		"conversationId": "c1",
		"messages": []map[string]any{
			{"subject": "First", "bodyPreview": "hi", "body": map[string]string{"content": "First whole body."}},
			{"subject": "Second", "bodyPreview": "hello", "body": map[string]string{"content": "Second whole body."}},
		},
	}

	result := FormatConversationText(thread)

	for _, want := range []string{"First whole body.", "Second whole body."} {
		if !strings.Contains(result, want) {
			t.Errorf("thread output missing %q:\n%s", want, result)
		}
	}
	if strings.Contains(result, "Preview:") {
		t.Errorf("thread rendered a preview line despite full bodies:\n%s", result)
	}
	if strings.Contains(result, conversationPreviewTruncatedHint) {
		t.Errorf("truncation hint printed when nothing was truncated:\n%s", result)
	}
}

// TestFormatConversationText_MixedTruncation verifies that only truncated
// entries get the compact marker while the actionable hint is printed once for
// the whole thread.
func TestFormatConversationText_MixedTruncation(t *testing.T) {
	thread := map[string]any{
		"conversationId": "c1",
		"messages": []map[string]any{
			{"subject": "Short", "bodyPreview": "brief"},
			{"subject": "Long", "bodyPreview": strings.Repeat("c", 255)},
		},
	}

	result := FormatConversationText(thread)

	if got := strings.Count(result, conversationPreviewEllipsis+"\n"); got != 1 {
		t.Errorf("ellipsis marker ends %d preview lines, want 1", got)
	}
	if got := strings.Count(result, conversationPreviewTruncatedHint); got != 1 {
		t.Errorf("escalation hint appears %d times, want exactly 1", got)
	}
	if !strings.Contains(result, "Preview: brief\n") {
		t.Errorf("short preview was altered:\n%s", result)
	}
}
