---
name: folder-management-and-message-move
description: Merge folder browsing into a single natural-language list_folders verb and add folder creation/deletion and message relocation behind MAIL_MANAGE_ENABLED.
id: "CR-0066"
status: "proposed"
date: 2026-04-28
amended: 2026-08-31
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

## Amendment 2026-08-31 (review response)

Review of the first implementation produced four changes, all folded into the
requirements and acceptance criteria below rather than tracked separately:

1. **One folder-browsing verb, not three.** `list_folders`, `list_child_folders`
   and `list_folder_tree` differed only in where a listing starts and how deep
   it goes. Those are parameters, not operations. They are now one `list_folders`
   verb taking `folder`, `recursive` and `max_depth`. Two entries left the mail
   operation enum and the aggregate tool description.
2. **Natural-language addressing.** Folder parameters were 1:1 with the Graph
   API — opaque ids only, and a verb literally named after the `childFolders`
   navigation property. Folders are now addressed by name or display-name path
   ("Inbox/01 Projects"), with ids still accepted. Parameters are renamed
   `folder`, `parent`, `destination`; the `*_folder_id` spellings remain
   accepted aliases.
3. **The tree view is the point, and it must be cheap.** The recursive overview
   is kept — it is what lets a model see the whole mailbox structure in one
   call — but its default output is now a markdown tree labelled by path with
   the 150-character ids dropped. `summary` and `raw` remain for callers that
   need ids or Graph field names.
4. **Folder browsing is not folder management.** `list_folders` is a read that
   `Mail.Read` already covers, so it is not gated behind `MAIL_MANAGE_ENABLED`.
   This reverses rejected alternative 1 of the original CR; see the alternatives
   section.

Two defects found during review are also fixed: `max_depth` was off by one
against its own documentation, and every folder listing capped silently at 100
entries per level with `@odata.nextLink` ignored.

## Change Summary

The mail domain currently exposes `list_folders` which returns only top-level folders. Users with nested folder structures (e.g., Inbox > 01 Projects > Swedfund) cannot browse their hierarchy, and there is no way to create, delete, or move messages between folders — a core Outlook workflow.

This CR extends the existing `list_folders` verb into a full folder browser addressed by natural-language paths, and adds four write verbs gated behind `MAIL_MANAGE_ENABLED` (which requests the `Mail.ReadWrite` scope): `create_folder`, `delete_folder`, `move_message`, and `move_messages`. The net change to the mail operation enum is **minus two entries** (19 to 17), because the folder-browsing capability is one verb rather than three.

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

`list_folders` calls `GET /me/mailFolders` and returns only top-level folders, addressed and returned by opaque Graph id. There is no way to reach a child folder, create or delete a folder, or move a message.

## Proposed Change

Extend `list_folders` in the read tier, and add four write verbs to the `MailManageEnabled` gate in `buildMailVerbs()`:

| Verb | Gate | Type | Graph Endpoint | Annotations |
|------|------|------|---------------|-------------|
| `list_folders` | `MailEnabled` (read tier) | read | `GET /me/mailFolders`, then `GET /me/mailFolders/{id}/childFolders` per level descended | readOnly, idempotent |
| `create_folder` | `MailManageEnabled` | write | `POST /me/mailFolders` or `POST /me/mailFolders/{id}/childFolders` | non-idempotent |
| `delete_folder` | `MailManageEnabled` | write | `DELETE /me/mailFolders/{id}` | **destructive**, idempotent |
| `move_message` | `MailManageEnabled` | write | `POST /me/messages/{id}/move` | non-idempotent |
| `move_messages` | `MailManageEnabled` | write | Iterates `POST .../move` per message | non-idempotent |

The four write verbs require `Mail.ReadWrite` scope (already requested when `MailManageEnabled=true`). `list_folders` requires only `Mail.Read`. The read verb supports the three-tier output model (text/summary/raw). Write verbs return text confirmations.

### Why ids are the wrong default currency

Mail folder ids measured against a live Microsoft 365 mailbox are **120
characters** of URL-safe base64:

```
AQMkADhkN2JlZQBhYi1lMjNhLTRjNDctYTVmMi0wMjhiNTliMWYyNzIALgAAAyxSSpj_kzdFju-9dZN_ICwBAIbY0YMDKXBOtEoMTonV13QAAAIBDAAAAA==
```

Base64 tokenizes poorly — roughly **30 to 40 tokens per id** — so a 40-folder
listing spends on the order of **1,500 tokens on identifiers alone**, before a
single folder name is transmitted. That is the concrete cost behind the
review's token-efficiency point, and the reason the default text tier drops ids
entirely and labels folders by path instead. Callers that genuinely need ids
ask for `output=summary` or `output=raw`.

The same measurement bounds the id-detection heuristic: observed ids are three
times `minGraphFolderIDLen` (40) and use only alphanumerics plus `_`, `-`, `=`,
while real display names in the same mailbox ("Inbox", "Archive",
"Conversation History", "Deleted Items") are under 25 characters and contain
spaces. Nothing realistic sits near the threshold from either side.

### Folder addressing

Every folder-taking parameter is a *reference*, not an id. `ResolveFolderRef`
(`internal/tools/folder_ref.go`) resolves one in this order:

1. **Graph well-known name** (`inbox`, `archive`, `sentitems`, `drafts`,
   `deleteditems`, ...) — passed straight through, since Graph accepts those
   wherever an id is expected. No lookup request.
2. **Display-name path** containing `/` — walked segment by segment from the
   mailbox root, matching case-insensitively. `"Inbox/01 Projects/Swedfund"`
   costs one request per level. A segment that matches nothing produces an
   error naming the segment, the parent it was looked for under, and the
   folder names that were actually there.
3. **Graph-id shape** — a whitespace-free string of at least 40 base64-alphabet
   characters is passed through verbatim. The check runs *after* path
   resolution so a display name can never be shadowed by it, and it doubles as
   the fallback when a path walk fails on a reference that could be an id.
4. **Top-level display name** — matched case-insensitively against
   `GET /me/mailFolders`.

`ResolveFolderPath` is the same resolution plus the folder's canonical display
path, which `list_folders` uses as the prefix for the paths it emits. This is
what closes the loop: every path printed by the default text tier is a valid
argument to `folder`, `parent`, and `destination`.

### Alias declaration rule

The prior `*_folder_id` spellings all remain accepted at runtime, but they are
not all declared in the tool schema, and the difference is deliberate.

Two facts drive it. First, the mcp-go **server** passes undeclared arguments
through to handlers untouched (verified directly), so an alias works for any
caller that actually sends it. Second, MCP **clients** forward only the
arguments a tool schema declares, which is the whole reason
`dispatch_aggregate_schema.go` exists. So the only question per alias is what
happens when a stripping client drops it:

| Alias | Declared? | If a client strips it | Cost |
|---|---|---|---|
| `folder_id` | No — `list_messages` already declares it | Nothing; it is never stripped | 0 B |
| `parent_folder_id` | **Yes** | `create_folder` silently creates at the **top level** instead of nested — wrong result, no error | 97 B |
| `destination_folder_id` | No | `move_message` returns "missing required parameter: destination" — loud and recoverable in one retry | 0 B |

The rule is therefore: **declare an alias only when losing it fails silently.**
`list_folders` additionally must *not* re-declare `folder_id`, because the
aggregate union is first-verb-wins and `list_folders` precedes `list_messages`;
re-declaring would replace `list_messages`' better description for every verb.

Measured effect on the serialized `mail` tool schema:

| Variant | Bytes | vs. pre-CR baseline (5710) |
|---|---|---|
| Both aliases declared, verbose descriptions | 6163 | +453 |
| Both dropped | 5954 | +244 |
| **Rule applied** (`parent_folder_id` declared, terse; `destination_folder_id` dropped) | **6027** | **+317** |

The rule recovers 136 of the 453 bytes without giving up a single safety
property. The residual +317 is the honest price of natural-language addressing:
`folder`, `recursive`, `max_depth`, `parent`, and `destination` are five new
parameters in a flat union, and they buy the ability to address a folder the
way a user names it. Set against the ~1,500 tokens of identifier that a
40-folder listing no longer emits, it is a good trade — the schema is paid once
per session, the listing is paid on every call.

These verbs have never shipped in a release (0.4.0 is current; this CR targets
0.8.0), so no released schema ever advertised the `*_folder_id` spellings. The
aliases are a courtesy to callers that learned them from this branch, not a
compatibility obligation.

### Output tiers

| Tier | Shape |
|---|---|
| `text` (default) | Markdown tree, two spaces per level, `- Name — 12 unread / 340`. The word "unread" is printed only for a non-zero count. Folders are labelled by **path**; Graph ids are omitted. Footer gives the recursive folder count and a worked `folder="..."` value. |
| `summary` | `{"folders":[{"name","path","unread","total","subfolder_count","id","children"?}],"count","parent"?,"truncated"?}` — tool vocabulary, empty values omitted. |
| `raw` | `{"value":[{"id","displayName","unreadItemCount","totalItemCount","childFolderCount","childFolders"?}],"_truncated"?}` — Graph vocabulary and collection shape, nothing elided. |

Subtrees that failed to load and listings Graph truncated are annotated inline
in all three tiers.

## Requirements

### Functional Requirements

* **FR-1:** The four write verbs (`create_folder`, `delete_folder`, `move_message`, `move_messages`) **MUST** be registered in the `MailManageEnabled` block of `buildMailVerbs()`. `list_folders` **MUST NOT** be (see FR-13).
* **FR-2:** Folder browsing **MUST** be exactly one verb, `list_folders`. `list_child_folders` and `list_folder_tree` **MUST NOT** appear in the operation enum.
* **FR-3:** `list_folders` **MUST** accept optional `folder`, `recursive` (default false), `max_depth` (default 3, min 1, max 10), `max_results` (default 100 per level, max 1000), `output`, and `account`. `max_depth` **MUST** count the requested level itself, so `max_depth=1` returns exactly one level. `max_depth` is meaningful only with `recursive=true`.
* **FR-4:** `create_folder` **MUST** accept a required `display_name` and optional `parent` and `account`. When `parent` is omitted, the folder **MUST** be created at the top level.
* **FR-5:** `delete_folder` **MUST** accept a required `folder` and optional `account`. The verb **MUST** carry `destructiveHint=true`.
* **FR-6:** `move_message` **MUST** accept required `message_id` and `destination`, and an optional `account`.
* **FR-7:** `move_messages` **MUST** accept a required comma-separated `message_ids` (max 50) and `destination`, and an optional `account`. It **MUST** report per-message success/failure without aborting on the first failure.
* **FR-8:** All five verbs **MUST** support the `account` parameter for multi-account resolution.
* **FR-9:** `list_folders` **MUST** implement three *distinct* output tiers. `summary` **MUST** use tool-native key names (`name`, `unread`, `total`, `subfolder_count`, `path`, `children`, `id`) and `raw` **MUST** use Graph key names (`displayName`, `unreadItemCount`, `totalItemCount`, `childFolderCount`, `id`). The two **MUST NOT** serialize identically.
* **FR-10:** `list_folders` text output **MUST** be a markdown tree indented two spaces per level, labelling folders by display path rather than Graph id, and **MUST** surface subtrees that failed to load.
* **FR-11:** Every folder and move verb **MUST** have annotation test assertions. Presence and gating assertions live in `internal/tools/tool_annotations_test.go`; the per-verb readOnly/destructive/idempotent/openWorld matrix lives in `internal/server/mail_verbs_test.go`, because per-verb `Annotations` are consumed when the aggregate tool is built and are not observable from outside the `server` package.
* **FR-12:** `folder`, `parent`, and `destination` **MUST** accept a Graph well-known folder name, a slash-separated display-name path, a top-level folder display name, or a raw Graph folder id. A reference that resolves to nothing **MUST** produce an error naming the failing segment and the candidates available at that level. The prior spellings `folder_id`, `parent_folder_id`, and `destination_folder_id` **MUST** remain accepted at runtime. An alias **MUST** be declared in the tool schema if and only if a client that strips undeclared arguments would cause a *silent* wrong result; aliases whose loss produces a clear error **MUST NOT** be declared, and `list_folders` **MUST NOT** re-declare `folder_id`. See the alias declaration rule above.
* **FR-13:** `list_folders` **MUST** be available with `MAIL_ENABLED` alone. Folder writes **MUST** remain behind `MAIL_MANAGE_ENABLED`.
* **FR-14:** No folder listing **MUST** truncate silently. When Graph reports `@odata.nextLink`, the response **MUST** say so in whichever tier is active.

### Non-Functional Requirements

* **NFR-1:** Every level of a recursive `list_folders` call, and every request issued while resolving a folder path, **MUST** use `RetryGraphCall` with 429 backoff to respect Graph API rate limits.
* **NFR-2:** Each handler **MUST** live in its own file under `internal/tools/` per the project's file isolation convention.
* **NFR-3:** All exported functions **MUST** have Go doc comments per CLAUDE.md documentation standards.
* **NFR-4:** The merged verb **MUST NOT** grow the mail tool's operation enum or its top-level description. Measured: description 1745 to 1581 characters, enum 19 to 17 operations. The serialized schema does grow, to 6027 bytes from 5710, because natural-language addressing adds five parameters to the aggregate union; the alias declaration rule holds that growth to +317 bytes rather than +453.

## Affected Components

* `internal/server/mail_verbs.go` — merged `list_folders` builder, 4 write-verb builders; `buildListChildFoldersVerb` and `buildListFolderTreeVerb` removed.
* `internal/tools/list_folders.go` — merged handler (replaces `list_mail_folders.go`, `list_child_folders.go`, `list_folder_tree.go`).
* `internal/tools/folder_ref.go` — `ResolveFolderRef` / `ResolveFolderPath`.
* `internal/tools/folder_wellknown.go` — Graph well-known folder names and display labels.
* `internal/tools/folder_fetch.go` — the two Graph listing calls plus truncation detection.
* `internal/tools/folder_tree.go` — Graph models to `FolderNode` tree, depth-bounded.
* `internal/tools/folder_node.go` — `FolderNode`, `FolderListing`, `CountFolders`.
* `internal/tools/folder_serialize.go` — the `summary` and `raw` projections.
* `internal/tools/param_alias.go` — `firstStringParam`, the parameter-alias helper.
* `internal/tools/create_folder.go`, `delete_folder.go`, `move_message.go`, `move_messages.go` — reference resolution and renamed parameters.
* `internal/tools/text_format.go` — `FormatFolderTreeText` rewritten as a markdown tree; `FormatMailFoldersText` and `formatFolderTreeLevel` removed.
* `extension/manifest.json` — updated mail tool description.
* `docs/concepts.md` — read-only-mode verb list, mail gating tiers.
* `docs/prompts/mcp-tool-crud-test.md` — Steps 37-44 rewritten.
* `scripts/crud-test.sh` — **no change required**. Its per-run tool-count buckets key on the four top-level MCP tool names (`mcp__outlook-local-mcp__{calendar,mail,account,system}`), not on verbs. No domain was added or removed, so the `mcp_<domain>` CSV columns and `docs/bench/crud-runs.csv` header stand unchanged.

## Scope Boundaries

### In Scope

* One merged `list_folders` read verb and four write verbs as described above.
* Natural-language folder addressing with backward-compatible `*_folder_id` aliases.
* Markdown tree text formatter and genuinely distinct `summary` / `raw` projections.
* Truncation reporting for every folder listing.
* Verb registration and annotation tests.
* CRUD test prompt updates.
* Extension manifest and `docs/concepts.md` updates.

### Out of Scope

* Renaming or moving existing folders (Graph supports PATCH but this is deferred).
* Copying messages (Graph has a `/copy` action; deferred to a future CR).
* Folder-level permissions or sharing.
* Following `@odata.nextLink` to auto-paginate. Truncation is reported instead; raising `max_results` is the escape hatch.
* Extending path addressing to `list_messages` / `search_messages` `folder_id` (deferred; those verbs still take an id or well-known name).
* Modifying the `Mail.ReadWrite` scope request logic (already handled by `MailManageEnabled`).

## Alternative Approaches Considered

1. **Gate folder read verbs under MailEnabled instead of MailManageEnabled.** Originally rejected on the grounds that folder browsing is "logically part of folder management" and one gate is a simpler mental model. **Reversed by the 2026-08-31 amendment.** The simpler mental model was bought with a real capability loss: browsing folders is a read that the `Mail.Read` scope already grants, so gating it behind `MAIL_MANAGE_ENABLED` denied `MAIL_ENABLED` users any view of their own hierarchy while requesting no additional permission in exchange. Gating is now by *operation type*, which is the rule the rest of the surface already follows: reads at `MAIL_ENABLED`, writes at `MAIL_MANAGE_ENABLED`. `list_folders` therefore stays in the read tier and `create_folder`, `delete_folder`, `move_message`, and `move_messages` stay gated.

4. **Keep `list_child_folders` and `list_folder_tree` as separate verbs.** Rejected by the amendment. Every verb costs a permanent slot in the mail tool's operation enum and a clause in its description, which every request carries. The three browsing verbs differed only in start point and depth — two arguments — so the separation bought nothing and cost two enum entries and 164 characters of description on every call.

5. **Auto-paginate folder listings by following `@odata.nextLink`.** Rejected for now in favour of explicit truncation reporting. Auto-pagination hides an unbounded number of Graph calls behind one tool call, and the failure it would prevent (a mailbox with more than 100 folders at one level) is better handled by telling the caller and letting it raise `max_results`. Silent truncation, which is what the first implementation did, is not an option either way (FR-14).

2. **Use Graph batch API ($batch) for move_messages.** Rejected because the Graph batch endpoint has a 20-request limit per batch, requires constructing raw HTTP requests, and the existing per-message retry logic handles transient failures more gracefully.

3. **Add folder operations as a separate domain tool.** Rejected because folders are part of the mail domain and the project convention is to add verbs to existing domains, not create new tools.

## Impact Assessment

### User Impact

* Users with nested folder structures can now browse, create, and manage folders, and can name a folder the way they think of it ("Inbox/01 Projects") rather than by a 150-character id.
* Users can organize messages into folders via move operations.
* Folder browsing works with mail read access alone; no write scope is needed to see the hierarchy.
* Parameter renames (`folder`, `parent`, `destination`) are additive: the `*_folder_id` spellings still work.

### Technical Impact

* Eight new single-purpose files in `internal/tools/` (`list_folders`, `folder_ref`, `folder_wellknown`, `folder_fetch`, `folder_tree`, `folder_node`, `folder_serialize`, `param_alias`); three removed (`list_mail_folders`, `list_child_folders`, `list_folder_tree`).
* `text_format.go` loses two folder formatters and gains one markdown tree formatter.
* Five verb builders in `mail_verbs.go`; two removed.
* Aggregate annotations unchanged (already most-conservative).
* Mail tool description 1745 to 1581 characters; operation enum 19 to 17. The serialized mail tool schema grows from 5710 to **6027** bytes: path addressing adds `folder`, `recursive`, `max_depth`, `parent`, and `destination` to the aggregate union, and the enum and description savings do not fully offset them. The alias declaration rule keeps one of the three legacy aliases in the schema instead of all three, recovering 136 bytes of the 453 the naive version cost.
* Against that +317 bytes paid once per session, the default text tier stops emitting 120-character folder ids — roughly 30 to 40 tokens each, ~1,500 tokens for a 40-folder listing — on every call.

### Business Impact

* Completes the mail management surface, making the tool useful for folder-based workflows.

## Implementation Approach

1. Create the folder handler and its single-purpose collaborators (`folder_ref`, `folder_wellknown`, `folder_fetch`, `folder_tree`, `folder_node`, `folder_serialize`) following the file-isolation convention.
2. Rewrite `FormatFolderTreeText` in `text_format.go` as the one markdown tree formatter for every folder listing.
3. Add `buildListFoldersVerb()` to the read tier and the four write-verb builders to the `MailManageEnabled` block of `mail_verbs.go`.
4. Route the write verbs' folder parameters through `ResolveFolderRef` and accept the legacy aliases.
5. Update annotation and description tests, the CRUD test prompt, `docs/concepts.md`, and the extension manifest.

## Test Strategy

### Tests to Add

| Test File | Test Name | Description | Inputs | Expected Output |
|-----------|-----------|-------------|--------|-----------------|
| `tool_annotations_test.go` | `TestMailAnnotations_FolderVerbsPresent` | Folder and move verbs in the enum when MailManageEnabled=true; merged verbs absent | MailManageEnabled=true | 5 verb names present, `list_child_folders` / `list_folder_tree` absent |
| `tool_annotations_test.go` | `TestMailAnnotations_FolderWriteVerbsAbsent` | Write verbs gated, `list_folders` not | MailEnabled=true only | 4 write verbs absent, `list_folders` present |
| `internal/server/mail_verbs_test.go` | `TestMailVerbAnnotations_FolderAndMove` | Per-verb annotation matrix, incl. `delete_folder` destructiveHint=true | Verb registry | Matrix matches AC-6 |
| `internal/server/mail_verbs_test.go` | `TestMailVerbs_FolderBrowsingNotManageGated` | FR-13 gating | MailEnabled=true only | `list_folders` present, writes absent |
| `internal/server/mail_verbs_test.go` | `TestMailVerbs_MergedFolderBrowsing` | FR-2 merge, merged parameter set | Verb registry | Old names gone; `folder`/`recursive`/`max_depth`/`max_results` declared |
| `folder_ref_test.go` | `TestResolveFolderPath_NestedPath`, `_DeepPath`, `_UnknownSegment`, `_GraphIDPassthrough`, ... | FR-12 addressing, incl. the actionable failure message | Mock Graph hierarchy | Correct ids, canonical paths, segment-naming errors |
| `list_folders_test.go` | `TestListFolders_MaxDepthOneIsExactlyOneLevel` | FR-3 off-by-one fix | recursive=true, max_depth=1 | Exactly one Graph call, no second level |
| `list_folders_test.go` | `TestListFolders_SummaryAndRawDiffer` | FR-9 | output=summary vs raw | Different bytes, different vocabularies |
| `list_folders_test.go` | `TestListFolders_ByPath`, `_ByWellKnownName`, `_LegacyFolderIDAlias` | FR-12 end to end | Mock Graph hierarchy | Scoped listings, absolute paths |
| `folder_serialize_test.go` | `TestSerializeSummaryFolders`, `TestSerializeRawFolders`, `TestSerializeFolders_TiersDiffer` | FR-9 projections | Hand-built listing | Correct key sets, optional keys omitted |
| `text_format_test.go` | `TestFormatFolderTreeText_*` | FR-10 markdown tree, error and truncation visibility | `FolderListing` values | Indented markdown, no ids, inline annotations |
| `folder_node_test.go` | `TestCountFolders` | Recursive count | Nested nodes | Includes descendants |
| `folder_ref_test.go` | `TestLooksLikeGraphFolderID_LiveMailboxID` | Pins the id heuristic to a real 120-char Microsoft 365 folder id and to real display names | Live-captured id + real folder names | Id recognised, names rejected |
| `internal/server/mail_verbs_test.go` | `TestMailVerbs_AliasDeclarationRule` | Pins which legacy aliases are schema-declared and why | Verb registry | `parent_folder_id` declared; `folder_id` and `destination_folder_id` not |
| `move_message_test.go` | `TestMoveMessage_AcceptsLegacyDestinationAlias` | Undeclared alias still honoured at runtime | `destination_folder_id` only | Not a missing-parameter error |

### Tests to Modify

| Test File | Test Name | Current Behavior | New Behavior | Reason for Change |
|-----------|-----------|-----------------|--------------|-------------------|
| `text_format_test.go` | `TestFormatFolderTreeText_Nested` | Asserts `"  Projects (2 unread, 15 total)"` | Asserts markdown `"  - 01 Projects — 0 / 58"` | FR-10 output shape changed |
| `tool_description_test.go` | `TestTopLevelDescription_Mail_ManageEnabled` | Expects `list_child_folders`, `list_folder_tree` in the description | Asserts both are absent | FR-2 merge |
| `tool_description_test.go` | `TestTopLevelDescription_Mail_ManageDisabled` | Expects folder reads absent | Asserts `list_folders` present, writes absent | FR-13 gating |
| `delete_folder_test.go` | `TestDeleteFolder_MissingFolderID` | Expects `missing required parameter: folder_id` | Renamed `_MissingFolder`, expects `: folder`; new alias test | FR-12 rename |

### Tests to Remove

| Test File | Test Name | Reason for Removal |
|-----------|-----------|-------------------|
| `list_mail_folders_test.go` | all | File deleted with the handler it covered; `NewListMailFoldersTool` was a pre-CR-0060 standalone tool definition registered nowhere |
| `list_child_folders_test.go` | all | Verb merged into `list_folders` |
| `list_folder_tree_test.go` | all | Verb merged into `list_folders`; `TestCountTreeNodes` superseded by `TestCountFolders` |
| `text_format_test.go` | `TestFormatMailFoldersText`, `_Empty` | `FormatMailFoldersText` removed; one markdown formatter now serves every folder listing |

## Acceptance Criteria

### AC-1: list_folders browses one level by default

Given MAIL_ENABLED=true and a folder with child folders
When the user calls `mail` with `operation=list_folders` and `folder` set to that folder's name or path
Then the response lists its immediate child folders
  And folders that have subfolders of their own are annotated with how many
  And no descent happened, because `recursive` defaults to false

### AC-2: list_folders returns a nested hierarchy on request

Given MAIL_ENABLED=true and a folder hierarchy deeper than one level
When the user calls `mail` with `operation=list_folders` and `recursive=true`
Then the response contains the hierarchy down to `max_depth` levels
  And the text output is a markdown tree indented two spaces per level
  And `max_depth=1` returns exactly one level

### AC-2a: folders are addressable by path

Given a folder at "Inbox/01 Projects/Swedfund"
When the user passes that string as `folder`, `parent`, or `destination`
Then the operation targets that folder
  And a path segment that matches nothing produces an error naming the segment and listing the folders that were available at that level

### AC-2b: the three output tiers are distinct

Given any folder listing
When the user requests `output=text`, `output=summary`, and `output=raw`
Then text is a markdown tree labelled by path with no Graph ids
  And summary is JSON keyed `name`/`unread`/`total`/`subfolder_count`/`path`/`id`
  And raw is JSON keyed `displayName`/`unreadItemCount`/`totalItemCount`/`childFolderCount`/`id`
  And summary and raw are not byte-identical

### AC-3: create_folder creates top-level folder

Given MAIL_MANAGE_ENABLED=true
When the user calls `mail` with `operation=create_folder` and `display_name=TestFolder`
Then a new top-level folder is created with the given name
  And the response contains the new folder's ID and display name

### AC-4: create_folder creates nested folder

Given MAIL_MANAGE_ENABLED=true and an existing parent folder
When the user calls `mail` with `operation=create_folder`, `display_name=SubFolder`, and a `parent` naming an existing folder
Then a new child folder is created under the parent
  And the response contains the new folder's ID, display name, and parent folder ID

### AC-5: delete_folder permanently removes a folder

Given MAIL_MANAGE_ENABLED=true and an existing user-created folder
When the user calls `mail` with `operation=delete_folder` and a valid `folder` reference
Then the folder and all its contents are permanently removed
  And the response confirms the deletion

### AC-6: delete_folder has destructiveHint annotation

Given the mail domain tool with MailManageEnabled=true
When the delete_folder verb annotations are inspected
Then destructiveHint is true

### AC-7: move_message moves a message to destination folder

Given MAIL_MANAGE_ENABLED=true, a message, and a destination folder
When the user calls `mail` with `operation=move_message`, `message_id`, and `destination`
Then the message is moved to the destination folder
  And the response contains the new message ID

### AC-8: move_messages reports per-message results

Given MAIL_MANAGE_ENABLED=true and multiple message IDs
When the user calls `mail` with `operation=move_messages`, comma-separated `message_ids`, and `destination`
Then each message is processed independently
  And the response reports success or failure for each message

### AC-9: Write verbs gated by MailManageEnabled, browsing is not

Given MailManageEnabled=false and MailEnabled=true
When the mail tool's operation enum is inspected
Then `create_folder`, `delete_folder`, `move_message`, and `move_messages` do not appear
  And `list_folders` does appear

### AC-10: folder browsing is one verb

Given any configuration
When the mail tool's operation enum is inspected
Then `list_child_folders` and `list_folder_tree` do not appear
  And `list_folders` accepts `folder`, `recursive`, `max_depth`, and `max_results`

### AC-11: no listing truncates silently

Given a folder level with more folders than `max_results`
When the user calls `mail` with `operation=list_folders`
Then the response states that more folders exist than were returned, in whichever output tier is active

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
| Recursive `list_folders` generates excessive Graph API calls for deep hierarchies | Medium | Medium | `recursive` defaults to false; `max_depth` capped at 10, default 3; RetryGraphCall handles 429 backoff |
| Path resolution costs one Graph request per path segment | Medium | Low | Well-known names and id-shaped references resolve with no request at all; only display-name paths walk, and each level is a single `$select`-projected listing |
| A display name that looks like a Graph id is mis-resolved | Low | Medium | The id heuristic runs only after path resolution and top-level name matching have been attempted, so a real folder always wins. Validated against a live mailbox: ids are 120 chars of URL-safe base64, display names are under 25 chars with spaces — no overlap near the 40-char threshold |
| A Graph folder id fails the id heuristic and is treated as a path | Low | Medium | **Closed.** Live Microsoft 365 folder ids measured at 120 characters with zero characters outside the accepted alphabet. `TestLooksLikeGraphFolderID_LiveMailboxID` pins a real id so a future change to the constant or alphabet fails the build |
| Renaming `folder_id` / `parent_folder_id` / `destination_folder_id` breaks existing callers | Low | Low | None of the four write verbs has ever shipped in a release, so there are no released callers. All three old spellings are still accepted at runtime, and `parent_folder_id` — the only one whose loss would be silent — stays declared so MCP clients keep forwarding it |
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
* Amendment (verb merge, path addressing, tier split, gating, pagination, tests, docs): ~4 hours

## Decision Outcome

Folder browsing is one `list_folders` verb in the read tier, parameterised by `folder`, `recursive`, and `max_depth`, addressed by natural-language paths, and rendered by default as a path-labelled markdown tree. Folder creation, folder deletion, and message relocation are four write verbs under the `MailManageEnabled` gate. All five follow the project's verb-based dispatch architecture (CR-0060) and the four-tool surface is unchanged — the mail operation enum is two entries *shorter* than before this CR was first implemented.

## Related Items

* CR-0060 — Domain-aggregated tools with verb-based operations (foundation for verb registration).
* CR-0058 — Mail domain feature flags (MailEnabled, MailManageEnabled gating).
* CR-0052 — MCP tool annotations (annotation conventions for new verbs).
