---
id: CR-0066-validation-report
cr: CR-0066
title: Validation Report — Folder Management and Message Move Operations
date: 2026-09-02
branch: feat/cr-0066-folder-management
head: e21574c
validator: implementation agent, with live-mailbox verification by the requestor
status: PASS
---

# CR-0066 Validation Report: Folder Management and Message Move Operations

This report validates the amended CR-0066 implementation against its
Functional Requirements, Non-Functional Requirements, Acceptance Criteria, and
Test Strategy, and records which behaviour was confirmed against a real
Microsoft 365 mailbox versus which rests on unit tests alone.

CR-0066 was submitted once, reviewed, and amended in six respects. Two of its
original requirements — FR-9 (three genuinely distinct output tiers) and FR-11
(annotation tests) — were **not met by the first submission** despite being
marked complete in the CR's own quality checklist. Both are now met and are
called out explicitly below rather than folded silently into a green column.

## Summary

Requirements: 20/20 PASS (16 FR + 4 NFR) | Acceptance Criteria: 13/13 PASS |
Amendment items: 6/6 PASS | Tests: 57 added or rewritten, all PASS |
CRUD: folder steps 37-44 PASS live, plus 30b-30f | Gaps: 0

Two requirements regressed-then-fixed relative to the first submission:

| Req | First submission | Now |
|---|---|---|
| FR-9 | **UNMET.** `summary` and `raw` were byte-identical for all three folder-list verbs; the tier parameter was accepted and ignored. | PASS. `summary` uses tool vocabulary, `raw` uses Graph vocabulary, and `TestListFolders_SummaryAndRawDiffer` fails if they ever converge again. |
| FR-11 | **UNMET.** No annotation assertions were ever written; `delete_folder`'s `destructiveHint=true` (AC-6) was unverified. | PASS. Presence and gating in `tool_annotations_test.go`; the per-verb matrix in `internal/server/mail_verbs_test.go`. |

## Amendment Verification

| # | Amendment | Status | Evidence |
|---|---|---|---|
| 1 | Folder browsing collapses to one verb | PASS | `list_child_folders` and `list_folder_tree` removed; `TestMailVerbs_MergedFolderBrowsing`, `TestMailAnnotations_FolderVerbsPresent`; enum 19 → 17 |
| 2 | Natural-language addressing replaces API-shaped ids | PASS | `internal/tools/folder_ref.go`; params renamed `folder`/`parent`/`destination`; `TestResolveFolderPath_*`; verified live |
| 3 | Tree view kept, made token-cheap | PASS | Markdown tree is the default tier, no Graph ids; `TestListFolders_DefaultIsSingleLevelText` asserts no id leak; verified live |
| 4 | One tool, `recursive`/`max_depth` instead of a second verb | PASS | `TestListFolders_MaxDepthOneIsExactlyOneLevel` pins the off-by-one fix; verified live at `max_depth=2` |
| 5 | Scope added after evidence: `folder` on `list_messages`/`search_messages` | PASS | `internal/tools/folder_param.go`; `TestMessageVerbs_*`; verified live via CRUD Step 30f |
| 6 | Unexplored vs withheld subfolders distinguished | PASS | `FolderNode.Expanded`; `TestListFolders_HiddenSubfolderNotMislabelled`; **found live, see below** |

## Requirement Verification

| Req # | Description | Status | Evidence |
|---|---|---|---|
| FR-1 | Four write verbs registered under `MailManageEnabled`; `list_folders` not | PASS | `internal/server/mail_verbs.go` `buildMailVerbs`; `TestMailVerbs_FolderBrowsingNotManageGated` |
| FR-2 | Folder browsing is exactly one verb | PASS | `TestMailVerbs_MergedFolderBrowsing`; `TestMailAnnotations_FolderVerbsPresent` asserts both old names absent |
| FR-3 | `list_folders` params; `max_depth=1` means exactly one level | PASS | `internal/tools/list_folders.go`; `TestListFolders_MaxDepthOneIsExactlyOneLevel` (asserts exactly one Graph call), `_MaxDepthTwo`, `_RecursiveDefaultDepth` |
| FR-4 | `create_folder` takes `display_name` + optional `parent` | PASS | `internal/tools/create_folder.go`; CRUD Steps 37, 38 live |
| FR-5 | `delete_folder` takes `folder`; `destructiveHint=true` | PASS | `internal/tools/delete_folder.go`; `TestMailVerbAnnotations_FolderAndMove/delete_folder`; CRUD Steps 43, 44 live |
| FR-6 | `move_message` takes `message_id` + `destination` | PASS | `internal/tools/move_message.go`; CRUD Step 41 live |
| FR-7 | `move_messages` batch, max 50, per-message reporting | PASS | `internal/tools/move_messages.go`; `TestMoveMessages_BatchLimitExceeded`; CRUD Step 42 live |
| FR-8 | All five verbs accept `account` | PASS | Schema declares `account` on each; `TestMailVerbs_MergedFolderBrowsing` checks the merged set |
| FR-9 | **Three genuinely distinct output tiers** | PASS (was UNMET) | `internal/tools/folder_serialize.go`; `TestListFolders_SummaryAndRawDiffer`, `TestSerializeFolders_TiersDiffer` |
| FR-10 | Markdown tree, paths not ids, failed subtrees surfaced | PASS | `FormatFolderTreeText` in `internal/tools/text_format.go`; `TestFormatFolderTreeText_Nested`, `_ErrorSubtreeVisible`; verified live |
| FR-11 | **Annotation tests for every folder and move verb** | PASS (was UNMET) | `internal/tools/tool_annotations_test.go` (presence/gating); `internal/server/mail_verbs_test.go` (per-verb matrix) |
| FR-12 | Reference forms accepted; actionable failure; aliases retained | PASS | `internal/tools/folder_ref.go`; `TestResolveFolderRef_*`; `TestMailVerbs_AliasDeclarationRule`; error path verified live |
| FR-13 | `list_folders` at `MAIL_ENABLED`; writes at `MAIL_MANAGE_ENABLED` | PASS | `TestMailVerbs_FolderBrowsingNotManageGated`; `TestMailAnnotations_FolderWriteVerbsAbsent` |
| FR-14 | No silent truncation | PASS (unit only) | `fetchTopLevelFolders`/`fetchChildFolders` return an `@odata.nextLink` flag; `TestFormatFolderTreeText_TruncationVisible`, `TestSerializeSummaryFolders`. Not reachable live — see Limitations |
| FR-15 | `folder` on `list_messages` and `search_messages` | PASS | `internal/tools/folder_param.go`; `TestMessageVerbs_FolderByPath`, `_ByWellKnownName`, `_UnresolvableFolderErrors`; verified live via Step 30f |
| FR-16 | Subfolder-count gap attributed to a specific cause | PASS | `FolderNode.Expanded`, `UnexploredChildren()`, `UnreturnedChildren()`; `TestListFolders_HiddenSubfolderNotMislabelled`, `_UnexploredHintTracksRecursion`, `_HiddenSubfolderInJSONTiers`; verified live |
| NFR-1 | `RetryGraphCall` on every level and every path-resolution request | PASS | `internal/tools/folder_fetch.go` wraps all three calls; `buildFolderTree` recurses through them |
| NFR-2 | One handler per file | PASS | Eight new single-purpose files in `internal/tools/`; three removed |
| NFR-3 | Go doc comments on all exported symbols | PASS | `go vet` clean; every exported symbol in the new files carries intent/params/returns/errors/side-effects |
| NFR-4 | Merged verb must not grow enum or description | PASS | Enum 19 → 17; description 1745 → 1581 chars. Schema bytes do grow; see Measurements |

## Acceptance Criteria Verification

| AC # | Statement | Status | Evidence |
|---|---|---|---|
| AC-1 | One level by default | PASS | `TestListFolders_DefaultIsSingleLevelText`; live flat listing, 8 top-level folders |
| AC-2 | Nested hierarchy on request | PASS | `TestListFolders_MaxDepthTwo`, `_RecursiveDefaultDepth`; live `recursive=true max_depth=2`, 22 folders |
| AC-2a | Folders addressable by path | PASS | `TestListFolders_ByPath`; live `folder="Inbox/Clients"` returned the 5 client subfolders |
| AC-2b | Three tiers distinct | PASS | `TestListFolders_SummaryAndRawDiffer`; `TestSerializeRawFolders` vs `TestSerializeSummaryFolders` |
| AC-3 | `create_folder` creates top-level | PASS | CRUD Step 37 live |
| AC-4 | `create_folder` creates nested | PASS | CRUD Step 38 live, addressed by parent **name** |
| AC-5 | `delete_folder` removes folder | PASS | CRUD Steps 43, 44 live |
| AC-6 | `delete_folder` has `destructiveHint=true` | PASS (was unverified) | `TestMailVerbAnnotations_FolderAndMove/delete_folder` |
| AC-7 | `move_message` relocates a message | PASS | CRUD Step 41 live |
| AC-8 | `move_messages` reports per message | PASS | CRUD Step 42 live |
| AC-9 | Writes gated; browsing not | PASS | `TestMailAnnotations_FolderWriteVerbsAbsent` |
| AC-10 | Folder browsing is one verb | PASS | `TestMailVerbs_MergedFolderBrowsing` |
| AC-11 | No listing truncates silently | PASS (unit only) | See FR-14 |

## Live Verification vs Unit-Tested Only

This split is the point of the section. Everything in the first table was
exercised against a real Microsoft 365 mailbox; everything in the second rests
on the mock.

### Verified against a live Microsoft 365 mailbox

CRUD run `2026-08-31T19-27-32`, executed by the requestor against an
integrated build of this branch. The run predates this report, so its row is
not yet in `docs/bench/crud-runs.csv`; the benchmark run that appends there is
being held until this report lands, so that it measures final code.

| Behaviour | Result |
|---|---|
| Folder steps 37-44 | All PASS |
| Step 30f — folder scoping does not silently widen | `folder_id: "Inbox"` returned **22** against a **22** baseline from `folder: "Inbox"` |
| Flat `list_folders` | 8 top-level folders, markdown tree, **zero Graph ids**, `[+N subfolders]` hints present |
| `recursive=true, max_depth=2` | Correct indented tree, 22 folders; trailer suggested a real path (`folder="Deleted Items/Event"`) |
| Path addressing | `folder="Inbox/Clients"` resolved and returned the 5 client subfolders |
| `folder` on `list_messages` | Scoped correctly; `folder="Inbox/Clients/Swedfund"` returned a different set |
| Error path | `no folder named "NoSuchFolder" under "Inbox"; available: "Clients", "Internal", "Notifications", "Personal"` |
| Hidden-subfolder fix | `[1 subfolder hidden]` on Conversation History vs `[+1 subfolders — increase max_depth]` on Training, rendered correctly |
| Graph folder id shape | 120 characters of URL-safe base64; alphabet a strict subset of what `looksLikeGraphFolderID` accepts |

### Unit-tested only

| Behaviour | Why not live | Risk |
|---|---|---|
| Truncation reporting (FR-14, AC-11) | Requires a mailbox with more than `max_results` folders at one level; the test mailbox has 8 top-level and at most 5 per level | Low. The flag is a nil-check on `GetOdataNextLink()`; the rendering is unit-tested in all three tiers |
| Failed-subtree rendering (`_error`) | Requires a Graph error mid-walk (throttling or a permissions edge) | Low. Pure formatting over a field set by the error branch |
| `move_messages` at the 50-id batch limit | Live run moved one message | Low. Limit is validated before any Graph call |
| Path resolution deeper than three segments | Test mailbox is three deep | Low. The walk is a loop over segments with no depth-specific logic |
| Non-`Inbox` well-known names (`sentitems`, `deleteditems`, …) | Live run used `Inbox` and `Archive` | Low. Single map lookup, pass-through, no per-name logic |

## Measurements

The reviewer's question was context-window cost. The honest answer is that it
moved in two directions.

| Metric | Before | After | Δ |
|---|---|---|---|
| `operation` enum | 19 verbs | 17 verbs | **−2** |
| Aggregate `mail` description | 1745 chars | 1581 chars | **−164 (−9.4%)** |
| Serialized `mail` tool JSON | 5710 B | 6019 B | **+309 (+5.4%)** |
| All four tools JSON | 15002 B | 15311 B | +309 |

**The description shrank and the parameter union grew.** Two verbs left the
enum, taking their description clauses with them, but natural-language
addressing added `folder`, `recursive`, `max_depth`, `parent`, and
`destination` to a flat schema union that dedupes by name. The +309 bytes are
real and are not offset by the −164 characters.

The alias-declaration rule (see the CR) held that growth down from +453 by
declaring only the alias whose loss would be silent:

| Variant | Bytes | vs. baseline |
|---|---|---|
| All aliases declared, verbose descriptions | 6163 | +453 |
| All aliases dropped | 5954 | +244 |
| Rule applied: `parent_folder_id` declared, `destination_folder_id` and `folder_id` not | 6027 | +317 |
| Final, after adding `folder` to the two message verbs | **6019** | **+309** |

Adding `folder` to `list_messages` and `search_messages` cost **zero** schema
bytes, because `list_folders` already contributes the name and
`aggregateSchemaOptions` dedupes; the 8-byte drop came from tightening two
`folder_id` descriptions at the same time.

**The real win is per-call output, not schema.** Folder ids in this mailbox are
120 characters of URL-safe base64:

```
AQMkADhkN2JlZQBhYi1lMjNhLTRjNDctYTVmMi0wMjhiNTliMWYyNzIALgAAAyxSSpj_kzdFju-9dZN_ICwBAIbY0YMDKXBOtEoMTonV13QAAAIBDAAAAA==
```

Base64 tokenizes poorly — roughly **30 to 40 tokens per id**, so a 40-folder
listing spends on the order of **1,500 tokens on identifiers alone** before a
single folder name is transmitted. The default text tier now emits none of
them. The schema is paid once per session; the listing is paid on every call.

## Two Bugs Live Testing Caught That the Mock Could Not

### 1. Folder scoping silently ignored (pre-existing, since 0.4.0)

Seven rows of `docs/prompts/mcp-tool-crud-test.md` passed `folder: "Inbox"` to
`list_messages`, which reads only `folder_id`
(`internal/tools/list_messages.go:170`, `search_messages.go:165`). Two of those
rows also passed `top: 1`, where the handler reads `max_results`
(`list_messages.go:254`, `search_messages.go:172`). Those steps queried the
**entire mailbox** instead of Inbox, ignored their row limits, and still
appeared to pass.

Confirmed present on release 0.4.0 (`5b22fb5`) and on `main`; it predates this
branch. Fixed in isolation by commit `a1aede4` so it can be reviewed and
cherry-picked on its own.

This is what justified amendment item 5. A developer who knew this codebase
well wrote the natural thing seven times and was silently wrong every time.
After the folder-verb merge, `folder` meant "folder reference" on five verbs
while `list_messages` still demanded a differently-named 120-character id — a
half-abstracted API that actively invites the mistake it had just
demonstrated. `TestMessageVerbs_*` now pin the behaviour, and CRUD Step 30f
turns a silent widening into a visible count mismatch.

### 2. Misleading hint when Graph counts a child it will not return

Live output at `recursive=true, max_depth=2`:

```
- Conversation History — 0 / 0 [+1 subfolders — use recursive=true]
```

`recursive=true` had already been passed and the node sat at depth 1, well
inside `max_depth=2`. Probing directly:

```
list_folders folder="Conversation History" recursive=true max_depth=3 output=summary
-> {"count":0,"folders":[],"parent":"Conversation History"}
```

Graph reports `childFolderCount: 1` — the Teams "Team Chat" folder — and
returns nothing, because hidden folders are counted and then withheld unless
`includeHiddenFolders=true`. The hint was false, and an agent that trusts it
can retry indefinitely.

### Why the mock could not have caught either

**The mock was written from the Graph documentation.** There,
`childFolderCount` is described as the number of child folders, so every
fixture it produced was self-consistent: the count always matched the children
returned. A mock built from a specification can only encode the assumptions of
that specification, and the bug *is* the specification being wrong about real
mailboxes. The same holds for the prompt bug: a fixture generated from the
handler's own parameter list can never disagree with that parameter list, which
is precisely the disagreement that was wrong.

Both are now in the mock — `mockConvHistID` carries `childFolderCount: 1` with
an empty child listing — and both are pinned by tests. **But the fixtures came
from production.** That is the argument for `make crud-test`: a unit suite can
only falsify assumptions someone already thought to question.

Both fixes were mutation-checked. Removing `node.Expanded = true` reproduces
the reported symptom verbatim; reverting the `folder` alias makes the message
verbs issue `/messages` instead of `/mailFolders/{id}/messages`.

## Test Strategy Verification

57 test functions added or rewritten across the commit range. Selected mapping:

| Test File | Test Name | Requirement | Status |
|---|---|---|---|
| `internal/tools/list_folders_test.go` | `TestListFolders_MaxDepthOneIsExactlyOneLevel` | FR-3 off-by-one | PASS |
| | `TestListFolders_ByPath`, `_ByWellKnownName`, `_LegacyFolderIDAlias` | FR-12 | PASS |
| | `TestListFolders_SummaryAndRawDiffer` | FR-9 | PASS |
| | `TestListFolders_HiddenSubfolderNotMislabelled` | FR-16, amendment 6 | PASS |
| | `TestListFolders_UnexploredHintTracksRecursion` | FR-16 | PASS |
| | `TestListFolders_HiddenSubfolderInJSONTiers` | FR-16 in summary and raw | PASS |
| `internal/tools/folder_ref_test.go` | `TestResolveFolderPath_NestedPath`, `_DeepPath` | FR-12 | PASS |
| | `TestResolveFolderRef_UnknownSegment` | FR-12 actionable error | PASS |
| | `TestLooksLikeGraphFolderID_LiveMailboxID` | Id heuristic vs a real 120-char id | PASS |
| `internal/tools/folder_param_test.go` | `TestMessageVerbs_FolderByPath` + 4 more, table-driven over both verbs | FR-15 | PASS |
| `internal/tools/folder_serialize_test.go` | `TestSerializeSummaryFolders`, `TestSerializeRawFolders`, `_TiersDiffer` | FR-9 | PASS |
| `internal/tools/text_format_test.go` | `TestFormatFolderTreeText_*` (9 tests) | FR-10, FR-14, FR-16 | PASS |
| `internal/tools/tool_annotations_test.go` | `TestMailAnnotations_FolderVerbsPresent`, `_FolderWriteVerbsAbsent` | FR-11, FR-13, AC-9 | PASS |
| `internal/server/mail_verbs_test.go` | `TestMailVerbAnnotations_FolderAndMove` | FR-11, AC-6 | PASS |
| | `TestMailVerbs_FolderBrowsingNotManageGated` | FR-13 | PASS |
| | `TestMailVerbs_MergedFolderBrowsing` | FR-2, AC-10 | PASS |
| | `TestMailVerbs_AliasDeclarationRule` | FR-12 declaration policy | PASS |
| `internal/tools/folder_node_test.go` | `TestCountFolders` | Recursive count | PASS |

Tests removed with the code they covered: `list_mail_folders_test.go`,
`list_child_folders_test.go`, `list_folder_tree_test.go`, and
`TestFormatMailFoldersText*`.

## Diff Coverage

Range `89c570d..e21574c` (5 commits), 39 files, +3686 / −1368.

| File | +/− | Mapped Requirements |
|---|---|---|
| `internal/tools/list_folders.go` | +184 | FR-2, FR-3, FR-9, FR-14 |
| `internal/tools/folder_ref.go` | +322 | FR-12 |
| `internal/tools/folder_wellknown.go` | +58 | FR-12 |
| `internal/tools/folder_fetch.go` | +139 | FR-14, NFR-1 |
| `internal/tools/folder_tree.go` | +107 | FR-3, FR-16 |
| `internal/tools/folder_node.go` | +145 | FR-16 |
| `internal/tools/folder_serialize.go` | +142 | FR-9, FR-16 |
| `internal/tools/folder_param.go` | +54 | FR-15 |
| `internal/tools/param_alias.go` | +37 | FR-12 |
| `internal/tools/text_format.go` | +123 / −57 | FR-10, FR-16 |
| `internal/tools/list_messages.go`, `search_messages.go` | +12 / −13 | FR-15 |
| `internal/tools/create_folder.go`, `delete_folder.go`, `move_message.go`, `move_messages.go` | +96 / −34 | FR-4 – FR-7, FR-12 |
| `internal/server/mail_verbs.go` | +95 / −112 | FR-1, FR-2, FR-13, FR-15 |
| `internal/tools/list_{mail_folders,child_folders,folder_tree}.go` (+ their tests) | −932 | FR-2 |
| Test files (11 changed or added) | +1728 / −448 | FR-9, FR-11, FR-12, FR-15, FR-16 |
| `docs/cr/CR-0066-*.md` | +443 | Amendment record |
| `docs/prompts/mcp-tool-crud-test.md` | +98 | Harness maintenance; pre-existing bug fix |
| `docs/concepts.md` | +8 | FR-13, read-only-mode verb list |
| `extension/manifest.json` | ±2 | Manifest alignment |

Unmapped changed files: none.

## Gate Results

Run at `e21574c`.

```
make docs-bundle build vet fmt-check tidy test   COMBINED_EXIT=0
  ==> docs-bundle OK
  go build -o ./outlook-local-mcp ./cmd/outlook-local-mcp/
  go vet ./...
  go mod tidy                        (no go.mod/go.sum drift)
  CGO_ENABLED=0 go test -coverprofile=coverage.out ./...
    ok  cmd/outlook-local-mcp        coverage: 27.1%
    ok  internal/audit               coverage: 91.3%
    ok  internal/auth                coverage: 75.2%
    ok  internal/config              coverage: 90.7%
    ok  internal/docs                coverage: 83.3%
    ok  internal/graph               coverage: 78.6%
    ok  internal/logging             coverage: 91.1%
    ok  internal/observability       coverage: 86.4%
    ok  internal/server              coverage: 91.8%
    ok  internal/tools               coverage: 71.9%
    ok  internal/tools/help          coverage: 73.6%
    ok  internal/validate            coverage: 100.0%

go test -race ./...                              RACE_EXIT=0  (14/14 packages ok)
jq -e . extension/manifest.json                  JQ_EXIT=0    (+ extension/manifest_test.go PASS)
```

`golangci-lint run --timeout 15m` reports exactly two findings, both
pre-existing `QF1012` (staticcheck) in files this branch does not touch:

```
internal/tools/refresh_account.go:126:3   QF1012: Use fmt.Fprintf(...) instead of WriteString(fmt.Sprintf(...))
internal/tools/search_events_test.go:330:3 QF1012: Use fmt.Fprintf(...) instead of WriteString(fmt.Sprintf(...))
2 issues: * staticcheck: 2
```

Both are fixed on the CR-0067 branch (PR #3), not here, so `make ci` is clean
once the two branches are integrated. `goreleaser check` and `mcpb validate`
were not run — neither binary is installed in this environment; the manifest
was validated with `jq` and `extension/manifest_test.go` instead.

## Known Limitations

1. **No `@odata.nextLink` following.** A folder level with more than
   `max_results` entries reports truncation rather than paging automatically
   (FR-14). Auto-pagination would hide an unbounded number of Graph calls
   behind one tool call; raising `max_results` is the escape hatch. Recorded as
   rejected alternative 5 in the CR. **Not reachable in live testing** — the
   test mailbox has 8 top-level folders and at most 5 per level.

2. **`includeHiddenFolders` deliberately out of scope.** Graph can surface the
   hidden folders that amendment item 6 detects — the Teams "Team Chat" folder
   and its siblings. Doing so is a product decision about what the tool
   exposes, not a formatting fix, and belongs in its own CR. This change only
   stops the tool from lying about them.

3. **`looksLikeGraphFolderID` validated against one tenant only.** Observed ids
   were 120 characters of URL-safe base64 — three times the 40-character
   threshold — using only alphanumerics plus `_`, `-`, `=`. `+` and `/` are
   also accepted for standard-base64 emitters, but no mailbox producing those
   has been seen. A tenant whose folder ids are shorter than 40 characters, or
   use characters outside that alphabet, would have an id fall through to
   display-name resolution and fail with a not-found error. Loud, not silent,
   and pinned by `TestLooksLikeGraphFolderID_LiveMailboxID`.

4. **Path resolution costs one Graph request per path segment.** Well-known
   names and id-shaped references resolve with no request at all; only
   display-name paths walk. Deep paths in a latency-sensitive loop will notice.

5. **Aggregate schema union is first-verb-wins.** `folder`'s description comes
   from `list_folders` and must serve all five folder-taking verbs; it was
   reworded to "Omit for the default scope" to stay true for each. This is a
   pre-existing property of `dispatch_aggregate_schema.go`, not introduced
   here.

## Gaps

None.
