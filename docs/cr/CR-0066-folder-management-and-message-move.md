---
name: folder-management-and-message-move
description: Add six new mail domain operations for folder hierarchy browsing, folder creation/deletion, and message relocation, all gated behind MAIL_MANAGE_ENABLED.
id: "CR-0066"
status: "proposed"
date: 2026-04-28
requestor: tor
stakeholders:
  - "maintainers"
  - "LLM end-users with nested folder structures"
priority: "medium"
target-version: "0.8.0"
source-branch: main
source-commit: 5b22fb5
---

# Folder Management and Message Move Operations

## Change Summary

The mail domain currently exposes `list_folders` which returns only top-level folders. Users with nested folder structures (e.g., Inbox > 01 Projects > Swedfund) cannot browse their hierarchy, and there is no way to create, delete, or move messages between folders — a core Outlook workflow. This CR adds six new verbs to the mail domain tool, all gated behind `MAIL_MANAGE_ENABLED` (which requests the `Mail.ReadWrite` scope): `list_child_folders`, `list_folder_tree`, `create_folder`, `delete_folder`, `move_message`, and `move_messages`.

## Motivation and Background

The existing `list_folders` verb only returns the flat list of top-level mail folders (Inbox, Sent Items, Drafts, etc.). Many Outlook users organize their mail into deep nested folder hierarchies. Without the ability to browse child folders, create new folders, or move messages between them, the LLM cannot help users with folder-based mail organization — one of the most common Outlook workflows.

These operations complement the existing mail read tier (list_folders, list_messages, get_message, search_messages) and the draft management tier (create_draft, update_draft, delete_draft), completing the folder management and message organization surface.

## Change Drivers

* Users with nested folder hierarchies cannot browse beyond top-level folders.
* No way to programmatically organize messages into folders via the MCP surface.
* Folder creation and deletion are basic mailbox management operations missing from the tool surface.
* Batch message move is a common workflow (e.g., "move all messages from sender X to folder Y").

## Current State

The mail domain registers up to 13 verbs depending on feature flags:

* **Always-on (5):** `help`, `list_folders`, `list_messages`, `get_message`, `search_messages`.
* **MailEnabled-gated (3):** `get_conversation`, `list_attachments`, `get_attachment`.
* **MailManageEnabled-gated (5):** `create_draft`, `create_reply_draft`, `create_forward_draft`, `update_draft`, `delete_draft`.

`list_folders` calls `GET /me/mailFolders` and returns only top-level folders. There is no verb for child folder access, folder creation, folder deletion, or message move.

## Proposed Change

Add six new verbs to the `MailManageEnabled` gate in `buildMailVerbs()`:

| Verb | Type | Graph Endpoint | Annotations |
|------|------|---------------|-------------|
| `list_child_folders` | read | `GET /me/mailFolders/{id}/childFolders` | readOnly, idempotent |
| `list_folder_tree` | read | Recursive `GET .../childFolders` at each level | readOnly, idempotent |
| `create_folder` | write | `POST /me/mailFolders` or `POST /me/mailFolders/{id}/childFolders` | non-idempotent |
| `delete_folder` | write | `DELETE /me/mailFolders/{id}` | **destructive**, idempotent |
| `move_message` | write | `POST /me/messages/{id}/move` | non-idempotent |
| `move_messages` | write | Iterates `POST .../move` per message | non-idempotent |

All six verbs require `Mail.ReadWrite` scope (already requested when `MailManageEnabled=true`). Read verbs support the three-tier output model (text/summary/raw). Write verbs return text confirmations.

## Requirements

### Functional Requirements

* **FR-1:** Six new verbs **MUST** be registered in the `MailManageEnabled` block of `buildMailVerbs()`.
* **FR-2:** `list_child_folders` **MUST** accept a required `folder_id` and optional `max_results` (default 25), `output`, and `account` parameters.
* **FR-3:** `list_folder_tree` **MUST** accept an optional `folder_id`, `max_depth` (default 3, max 10), `output`, and `account` parameters. It **MUST** recurse via `ChildFolders().Get()` at each level.
* **FR-4:** `create_folder` **MUST** accept a required `display_name` and optional `parent_folder_id` and `account` parameters. When `parent_folder_id` is omitted, the folder **MUST** be created at the top level.
* **FR-5:** `delete_folder` **MUST** accept a required `folder_id` and optional `account` parameter. The verb **MUST** carry `destructiveHint=true`.
* **FR-6:** `move_message` **MUST** accept required `message_id` and `destination_folder_id` parameters, and an optional `account` parameter.
* **FR-7:** `move_messages` **MUST** accept a required comma-separated `message_ids` (max 50) and `destination_folder_id`, and an optional `account` parameter. It **MUST** report per-message success/failure without aborting on the first failure.
* **FR-8:** All six verbs **MUST** support the `account` parameter for multi-account resolution.
* **FR-9:** Read verbs (`list_child_folders`, `list_folder_tree`) **MUST** implement all three output tiers (text, summary, raw).
* **FR-10:** `list_folder_tree` text output **MUST** show a tree structure with indentation.
* **FR-11:** All six verbs **MUST** have corresponding annotation test assertions in `tool_annotations_test.go`.

### Non-Functional Requirements

* **NFR-1:** `list_folder_tree` **MUST** use `RetryGraphCall` with 429 backoff at each level to respect Graph API rate limits.
* **NFR-2:** Each handler **MUST** live in its own file under `internal/tools/` per the project's file isolation convention.
* **NFR-3:** All exported functions **MUST** have Go doc comments per CLAUDE.md documentation standards.

## Affected Components

* `internal/server/mail_verbs.go` — verb registration, 6 new builder functions.
* `internal/tools/list_child_folders.go` — new file.
* `internal/tools/list_folder_tree.go` — new file.
* `internal/tools/create_folder.go` — new file.
* `internal/tools/delete_folder.go` — new file.
* `internal/tools/move_message.go` — new file.
* `internal/tools/move_messages.go` — new file.
* `internal/tools/text_format.go` — new `FormatFolderTreeText` function.
* `extension/manifest.json` — updated mail tool description.
* `docs/prompts/mcp-tool-crud-test.md` — new test steps.

## Scope Boundaries

### In Scope

* Six new verbs as described above.
* Text formatter for folder tree output.
* Verb registration and annotation tests.
* CRUD test prompt updates.
* Extension manifest update.

### Out of Scope

* Renaming or moving existing folders (Graph supports PATCH but this is deferred).
* Copying messages (Graph has a `/copy` action; deferred to a future CR).
* Folder-level permissions or sharing.
* Changes to the `list_folders` verb (it remains always-on and top-level only).
* Modifying the `Mail.ReadWrite` scope request logic (already handled by `MailManageEnabled`).

## Alternative Approaches Considered

1. **Gate folder read verbs under MailEnabled instead of MailManageEnabled.** Rejected because `list_child_folders` and `list_folder_tree` are logically part of folder management, and keeping all six verbs under one gate simplifies the mental model.

2. **Use Graph batch API ($batch) for move_messages.** Rejected because the Graph batch endpoint has a 20-request limit per batch, requires constructing raw HTTP requests, and the existing per-message retry logic handles transient failures more gracefully.

3. **Add folder operations as a separate domain tool.** Rejected because folders are part of the mail domain and the project convention is to add verbs to existing domains, not create new tools.

## Impact Assessment

### User Impact

* Users with nested folder structures can now browse, create, and manage folders.
* Users can organize messages into folders via move operations.
* No breaking changes to existing verbs.

### Technical Impact

* Six new files in `internal/tools/`, one new function in `text_format.go`.
* Six new verb builders in `mail_verbs.go`.
* Aggregate annotations unchanged (already most-conservative).

### Business Impact

* Completes the mail management surface, making the tool useful for folder-based workflows.

## Implementation Approach

1. Create six handler files following the existing single-file-per-handler pattern.
2. Add `FormatFolderTreeText` to `text_format.go` for tree-structured output.
3. Add six `buildXxxVerb()` functions to `mail_verbs.go` and register in the `MailManageEnabled` block.
4. Update annotation tests, CRUD test prompt, and extension manifest.

## Test Strategy

### Tests to Add

| Test File | Test Name | Description | Inputs | Expected Output |
|-----------|-----------|-------------|--------|-----------------|
| `tool_annotations_test.go` | `TestMailAnnotations/new_verbs_present` | Verify 6 new verbs in operation enum when MailManageEnabled=true | Config with MailManageEnabled=true | All 6 verb names in enum |
| `tool_annotations_test.go` | `TestMailAnnotations/new_verbs_absent` | Verify 6 new verbs absent when MailManageEnabled=false | Config with MailManageEnabled=false | None of 6 verb names in enum |
| `text_format_test.go` | `TestFormatFolderTreeText` | Verify tree formatter with nested structure | Nested folder maps | Indented text output |
| `text_format_test.go` | `TestFormatFolderTreeText_Empty` | Verify empty tree output | Empty slice | "No folders found." |

### Tests to Modify

| Test File | Test Name | Current Behavior | New Behavior | Reason for Change |
|-----------|-----------|-----------------|--------------|-------------------|
| None | — | — | — | — |

### Tests to Remove

| Test File | Test Name | Reason for Removal |
|-----------|-----------|-------------------|
| None | — | — |

## Acceptance Criteria

### AC-1: list_child_folders returns child folders

Given MAIL_MANAGE_ENABLED=true and a folder with child folders
When the user calls `mail` with `operation=list_child_folders` and a valid `folder_id`
Then the response contains the immediate child folders with id, displayName, unreadItemCount, and totalItemCount

### AC-2: list_folder_tree returns nested hierarchy

Given MAIL_MANAGE_ENABLED=true and a folder hierarchy with depth > 1
When the user calls `mail` with `operation=list_folder_tree`
Then the response contains all folders in a nested tree structure up to max_depth levels
  And the text output shows indented folder names

### AC-3: create_folder creates top-level folder

Given MAIL_MANAGE_ENABLED=true
When the user calls `mail` with `operation=create_folder` and `display_name=TestFolder`
Then a new top-level folder is created with the given name
  And the response contains the new folder's ID and display name

### AC-4: create_folder creates nested folder

Given MAIL_MANAGE_ENABLED=true and an existing parent folder
When the user calls `mail` with `operation=create_folder`, `display_name=SubFolder`, and a valid `parent_folder_id`
Then a new child folder is created under the parent
  And the response contains the new folder's ID, display name, and parent folder ID

### AC-5: delete_folder permanently removes a folder

Given MAIL_MANAGE_ENABLED=true and an existing user-created folder
When the user calls `mail` with `operation=delete_folder` and a valid `folder_id`
Then the folder and all its contents are permanently removed
  And the response confirms the deletion

### AC-6: delete_folder has destructiveHint annotation

Given the mail domain tool with MailManageEnabled=true
When the delete_folder verb annotations are inspected
Then destructiveHint is true

### AC-7: move_message moves a message to destination folder

Given MAIL_MANAGE_ENABLED=true, a message, and a destination folder
When the user calls `mail` with `operation=move_message`, `message_id`, and `destination_folder_id`
Then the message is moved to the destination folder
  And the response contains the new message ID

### AC-8: move_messages reports per-message results

Given MAIL_MANAGE_ENABLED=true and multiple message IDs
When the user calls `mail` with `operation=move_messages`, comma-separated `message_ids`, and `destination_folder_id`
Then each message is processed independently
  And the response reports success or failure for each message

### AC-9: New verbs gated by MailManageEnabled

Given MailManageEnabled=false
When the mail tool's operation enum is inspected
Then none of the six new verbs appear in the enum

## Quality Standards Compliance

- [x] All code compiles (`make build`)
- [x] All tests pass (`make test`)
- [x] Code is formatted (`make fmt-check`)
- [x] go.mod is tidy (`make tidy`)
- [x] New verbs have Go doc comments on all exported symbols
- [x] New verbs follow single-file-per-handler convention
- [x] Extension manifest updated with new verb descriptions

## Risks and Mitigation

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| list_folder_tree generates excessive Graph API calls for deep hierarchies | Medium | Medium | max_depth capped at 10, default 3; RetryGraphCall handles 429 backoff |
| move_messages partial failure confuses the LLM | Low | Low | Per-message success/failure reporting with clear summary counts |
| delete_folder accidentally removes important folders | Low | High | Graph API rejects deletion of well-known folders (Inbox, Sent, Drafts); destructiveHint=true signals the LLM to confirm |

## Dependencies

* CR-0060 (domain-aggregated tools) — completed; provides the verb dispatch infrastructure.
* CR-0052 (MCP annotations) — completed; provides annotation conventions.

## Estimated Effort

* Handler files (6): ~2 hours
* Text formatter: ~15 minutes
* Verb registration: ~30 minutes
* Tests and manifest: ~30 minutes
* CR document: ~30 minutes

## Decision Outcome

All six operations are added as verbs under the `MailManageEnabled` gate of the existing `mail` domain tool, following the project's verb-based dispatch architecture (CR-0060). This approach maintains the existing four-tool surface while extending the mail domain's capabilities.

## Related Items

* CR-0060 — Domain-aggregated tools with verb-based operations (foundation for verb registration).
* CR-0058 — Mail domain feature flags (MailEnabled, MailManageEnabled gating).
* CR-0052 — MCP tool annotations (annotation conventions for new verbs).
