---
name: message-body-delivery-modes
description: Add a body_mode parameter to mail.get_message and mail.get_conversation so a full message body can be read as plain text, converted server-side by Graph, without escalating to the raw output tier.
id: "CR-0068"
status: "proposed"
date: 2026-09-02
requestor: desek
stakeholders:
  - "maintainers"
  - "LLM consumers reading mail longer than 255 characters"
priority: "medium"
target-version: "0.8.0"
source-branch: feat/cr-0066-folder-management
source-commit: af379d0
---

# Message body delivery modes

## Change Summary

`mail.get_message` can return a message body in exactly two forms today: a 255-character preview, or the complete body as HTML bundled with every internet header. There is nothing in between, and there is no plain-text full body anywhere in the codebase.

This CR adds a `body_mode` parameter with values `preview` (default, unchanged behaviour), `text` (the complete body as plain text) and `full` (the complete body as stored, usually HTML) to `mail.get_message` and `mail.get_conversation`. Plain text is produced by Microsoft Graph in response to a `Prefer: outlook.body-content-type="text"` request header, so no HTML parsing, stripping or sanitizing enters this repository and no dependency is added.

`body_mode` is a new axis, not a fourth output tier. It also fixes an independent defect: a truncated preview now says that it was truncated.

## Motivation and Background

### What the doctrine decided, and what it did not

CR-0033 introduced the three output tiers. CR-0043 and CR-0051 hardened them into a default: `text` first, `raw` never implicit, previews instead of bodies. AGENTS.md codified the result as the "Body escalation pattern": return a preview, document that the full content requires `output=raw`, and let the model decide from the preview whether the full content is worth the tokens.

That reasoning is correct and this CR does not touch it. The preview stays the default; `raw` stays explicit; the token posture of an unescalated read is unchanged byte for byte.

What the doctrine never decided is **what form the escalated body takes**. It answered "when do we deliver the body" and left "in what shape" to fall out of an implementation detail — `body` happened to be in the raw `$select` list and nowhere else, so the body inherited raw's shape and raw's baggage. The result is a two-point scale where both points are wrong for the common case:

| Today | What the caller gets | Why it is wrong for "read me this email" |
|---|---|---|
| `output=text` / `summary` (default) | `bodyPreview` — 255 characters, cut mid-sentence, no marker | Most real mail is longer than 255 characters |
| `output=raw` | The full body **as HTML with inline styles**, plus `internetMessageHeaders`, `conversationIndex`, `replyTo`, `bccRecipients` | Pays for markup and a header block to read prose |

So the only route to a complete body costs several times what the body itself costs. That is not a bug in the tiering; it is a gap the tiering was never asked to fill.

Measured against a live Microsoft 365 mailbox on 2026-09-02, one real HTML message read four ways:

| Mode | Response chars | ~tokens (4 chars/token) | vs `output=raw` |
|---|---|---|---|
| `preview` (default) | 536 | 134 | — |
| `body_mode=text` | 3,559 | 889 | **5.2× cheaper** |
| `body_mode=full` | 5,585 | 1,396 | 3.3× cheaper |
| `output=raw` | 18,409 | 4,602 | — |

The `full` row is the stronger half of the argument: even keeping the HTML exactly as stored, dropping the internet-header bundle alone saves 3.3×. The `text` row is what the common case — "read me this email" — actually costs once the markup goes too.

### The silent part

On the preview path, `FormatMessageDetailText` prints `bodyPreview` and stops. A message cut at 255 characters is textually indistinguishable from a message that is 200 characters long. The reader cannot tell that anything is missing, and before this CR there was no parameter to ask for the rest even if they could. Marking the truncation is cheap and worth doing on its own merits; it is included here because this CR is what gives the marker something to point at.

### Why Graph does the conversion

`Prefer: outlook.body-content-type` had **zero** occurrences in the repository. Microsoft Graph will return `body.content` already converted to plain text when asked, and this server had never asked. Doing the conversion locally would mean an HTML parser in a security-reviewed codebase whose CR-0019 explicitly declines responsibility for HTML sanitization. Asking Graph costs one request header.

Confirmed live on 2026-09-02: Graph honours the header. A real HTML email returned as server-converted plain text, with no tags and no inline styles. See "Live verification".

**Caveat: plain text is not uniformly small.** Graph's conversion preserves URLs inline and fully expanded, including SafeLinks rewrites. On the message measured above — a GitHub notification — a single link renders as roughly 500 characters of `https://eur03.safelinks.protection.outlook.com/?url=...&data=...&sdata=...`. So for link-heavy mail the saving comes from dropping markup and headers, not from shrinking URLs, and a reader should not expect `text` to be proportional to the prose they can see. 889 tokens against 4,602 is still decisive, so this does not change the design — but it is the reason the win on some messages will be nearer 3× than 5×.

## Change Drivers

* The only path to a complete mail body is `output=raw`, which is several times more expensive than the body warrants and returns markup where prose was wanted.
* No plain-text full-body path exists anywhere in the codebase, despite Graph offering one for the price of a request header.
* A truncated preview is silent: nothing in the response distinguishes a cut message from a short one.
* `get_conversation` multiplies both failures by the length of the thread — N × 255 characters, or N full HTML bodies each with its own header block, and nothing in between.

## Current State

* `internal/tools/get_message.go` declares two `$select` lists. `body` appears only in the raw list, so on the text and summary paths the server never asks Graph for a body at all.
* `internal/graph/mail_serialize.go`: `SerializeMessage` emits `body`; `SerializeSummaryMessage` deliberately excludes it, and three tests assert that exclusion.
* `internal/tools/text_format.go`: `FormatMessageDetailText` prints `bodyPreview` and never `body.content`. `FormatConversationText` prints one `Preview:` line per message.
* `get_message.go` and `get_conversation.go` set no request `Headers` at all. The `Prefer` header idiom exists in the calendar verbs and in `list_messages`, always for `outlook.timezone`.
* The mail domain is one aggregate MCP tool whose input schema is the union of its verbs' parameters, resolved first-verb-wins by `aggregateSchemaOptions`.

## Proposed Change

### One new parameter on two verbs

`body_mode`, accepted by `mail.get_message` and `mail.get_conversation`:

| Value | Behaviour | Graph request |
|---|---|---|
| `preview` (default) | Exactly today's behaviour | No `body` in `$select`, no `Prefer` |
| `text` | Complete body as plain text | `body` added to `$select`; `Prefer: outlook.body-content-type="text"` |
| `full` | Complete body as stored, usually HTML | `body` added to `$select`; no `Prefer` |

On the non-raw path the body is added to the **summary** field set, not fetched via the raw field set. That is the point: a full body no longer drags in `internetMessageHeaders`, `conversationIndex`, `replyTo` and `bccRecipients`.

`output=raw` is unchanged and still returns the whole body under every `body_mode`, because raw is by definition the complete Graph serialisation. On that tier `body_mode` selects only the body's content type: `text` sends the conversion preference, `preview` and `full` do not.

### Why not a fourth output tier

`internal/tools/output.go` and AGENTS.md both assert exactly three output tiers, and every read verb in all four domains implements them. A `output=body` tier would be meaningless on `list_calendars`, `account.list` and `system.status`, and adding it would churn every verb's enum and description to serve two mail verbs.

More fundamentally the two are different questions. `output` asks *what shape is the response*; `body_mode` asks *how much of the body does that shape carry*. They are independent and every combination is meaningful — `body_mode=text, output=summary` is a legitimate request for the full plain-text body as structured JSON, and collapsing the axes would make it unexpressible.

### Why the parameter is not called `body`

The obvious name is taken, and taking it back would break drafts.

The `mail` tool's input schema is the union of its verbs' parameters. `create_draft` and `update_draft` already declare `body` as a free-text string for draft content. `aggregateSchemaOptions` resolves duplicate names **first-verb-wins**, and `get_message` is registered before the draft verbs.

The ordering was **confirmed against the registry, not inferred**. In `internal/server/mail_verbs.go`: `buildGetMessageVerb` is appended at line 122, `create_draft` declares `body` at line 541 and `update_draft` at line 653. `get_message` wins. Declaring `body` on it with an enum would therefore publish, for the whole `mail` tool:

```json
"body": {"type":"string",
         "description":"Body delivery: 'preview' (default) …",
         "enum":["preview","text","full"]}
```

Every caller of `create_draft` would then see the draft body described as a body-delivery mode and constrained to three values that are not draft content. A client that validates enums would reject a real draft body outright; one that does not would still be reading a description for a different parameter. Neither failure is visible from `get_message`, which is the verb that caused it.

Following CR-0066's alias declaration rule ("declare an alias only when losing it fails silently") the canonical name is `body_mode`, declared on both verbs so it is always forwarded. Plain `body` is accepted at call time and never declared: `create_draft` already contributes that name to the union, and if it is stripped — the only case being a `MAIL_MANAGE_ENABLED=false` server plus a stripping client — the result is the documented default, never a wrong body.

### Truncation is stated in band

`FormatMessageDetailText` prints the full body when one is attached, and otherwise the preview followed by:

```
[preview truncated at 255 characters — pass body_mode="text" for the full plain-text body]
```

A thread can hold 100 messages, so `FormatConversationText` marks each cut preview with a compact ` [...]` and prints the actionable sentence once in the footer. Repeating an 89-character hint per message would spend on the hint the tokens the preview tier exists to save.

Graph returns no truncation flag, so the signal is the length itself: a preview at or beyond the documented 255-character cap is treated as cut. A body whose plain-text rendering is exactly 255 characters is therefore marked truncated when it is not — one wrong line, against the alternative of silently losing the tail of every long message.

### Prefer headers are composed, never stacked

The brief for this work warned that adding a body preference to a path that already sends `outlook.timezone` could overwrite it. Verified against the SDK, the real behaviour is different and worth recording:

* `RequestHeaders.Add` **does not** overwrite. It stores values in a `map[string]struct{}` per header name, so a second `Add("Prefer", …)` accumulates.
* The Kiota HTTP adapter then emits **one header line per stored value** (`nethttp_request_adapter.go`: `for _, v := range values { request.Header.Add(key, v) }`), in Go map iteration order.

So two `Add` calls produce two `Prefer:` lines in non-deterministic order rather than one combined value. RFC 7240 defines `Prefer` as a list-based field so that is arguably legal, but it depends on Graph's tolerance for a construction the codebase has never sent, for no benefit.

`tools.newPreferHeaders` therefore joins every preference into a single comma-separated value and is the only place a `Prefer` header is constructed in the new code. Neither target verb currently sends any other preference — contrary to the brief's premise, `get_conversation` sends no `Prefer` header at all today; only `list_messages` and the calendar verbs do — so nothing is at risk right now. The helper exists so nothing is at risk later, and `TestNewPreferHeaders_CombinesIntoOneValue` pins it.

### Scope boundary: not on the collection verbs

`list_messages` and `search_messages` do **not** get `body_mode`. Returning 25 whole bodies from a browse is precisely the outcome the tiering exists to prevent, and every result from those verbs already carries the message ID needed to escalate one specific message. The boundary is recorded in AGENTS.md so a future CR has to argue against it rather than drift across it.

`get_conversation` does get it, because a thread is the worst case the parameter exists to fix and escalating it message by message would cost N tool calls to avoid N bodies the caller has already decided they want.

## Requirements

### Functional Requirements

* **FR-1:** `mail.get_message` **MUST** accept `body_mode` with values `preview`, `text` and `full`, defaulting to `preview`.
* **FR-2:** `mail.get_conversation` **MUST** accept the same parameter with the same semantics, applied to every message in the thread.
* **FR-3:** `body_mode=preview` **MUST** produce byte-identical Graph requests to the pre-CR default: no `body` in `$select`, no `Prefer` header.
* **FR-4:** `body_mode=text` **MUST** be implemented by sending `Prefer: outlook.body-content-type="text"`. No HTML parsing, stripping or sanitizing may be performed locally, and no dependency for it may be added.
* **FR-5:** `body_mode=full` **MUST** return the body as Graph stores it, without adding `internetMessageHeaders`, `conversationIndex`, `replyTo` or `bccRecipients` to the response.
* **FR-6:** `body_mode` **MUST** be orthogonal to `output`. All nine combinations must be valid, and no fourth output tier may be added.
* **FR-7:** An unrecognised `body_mode` value **MUST** return `body_mode must be 'preview', 'text', or 'full'` rather than silently degrading to the default.
* **FR-8:** Text output **MUST** state in band when a preview was truncated, and **MUST NOT** state it when the preview is shorter than the cap.
* **FR-9:** Conversation text output **MUST** mark each truncated preview compactly and print the escalation hint at most once per response.
* **FR-10:** `SerializeSummaryMessage` **MUST** continue to exclude `body` unconditionally. An escalated body is attached by the handler, in the same manner as the provenance flag.
* **FR-11:** Every `Prefer` header the new code sends **MUST** be built by `tools.newPreferHeaders`, which emits one combined header value.
* **FR-12:** `list_messages` and `search_messages` **MUST NOT** accept `body_mode`.
* **FR-13:** Plain `body` **MUST** be accepted at call time as an alias for `body_mode` on the two verbs, and **MUST NOT** be declared in either verb's schema.

### Non-Functional Requirements

* **NFR-1:** New logic **MUST** live in small single-purpose files under `internal/tools/` and `internal/graph/` per the project's file isolation convention.
* **NFR-2:** All exported symbols **MUST** carry Go doc comments per AGENTS.md documentation standards.
* **NFR-3:** No new module dependency. `go.mod` is unchanged.
* **NFR-4:** The mail operation enum **MUST NOT** grow. Measured: 17 operations before and after. The serialized `mail` tool grows from **6019 to 6292 bytes (+273)**, and the four-tool cold-start schema from **15311 to 15584 bytes (+273)** — one string property with a three-value enum, declared on two verbs and deduped to one by the aggregate union. The top-level description moves 1581 → 1586 characters.

## Affected Components

* `internal/tools/body_mode.go` — **new.** `ValidateBodyMode`, the three mode constants, and the reasoning for the parameter name.
* `internal/tools/prefer_header.go` — **new.** `newPreferHeaders` (single combined header value) and `bodyContentTypePreference`.
* `internal/tools/body_text.go` — **new.** `messageBodyContent`, `isTruncatedPreview`, and the marker strings.
* `internal/graph/mail_body.go` — **new.** `SerializeMessageBody`, the exported wrapper that lets a handler attach a body to a summary payload without changing the curated summary field set.
* `internal/tools/get_message.go` — body-mode validation, conditional `body` in `$select`, conditional `Prefer`, conditional body attachment; legacy tool definition and description updated.
* `internal/tools/get_conversation.go` — the same, plus `pageIterator.SetHeaders` so a threaded body preference survives pagination.
* `internal/tools/text_format.go` — `FormatMessageDetailText` and `FormatConversationText` print an attached body, and announce a truncated preview otherwise.
* `internal/server/mail_verbs.go` — `body_mode` schema, revised `Summary` and `Description`, and `Examples` on both verbs.
* `AGENTS.md` — "Body escalation pattern" amended from one paragraph to five rules.
* `docs/concepts.md` — new `## Message body modes` section; the "Output tiers" section no longer claims raw is where the body lives.
* `extension/manifest.json` — mail tool description mentions `body_mode`.
* `docs/prompts/mcp-tool-crud-test.md` — new Steps 30g and 35b, plus their summary-table rows.
* `internal/graph/mail_serialize.go` — **unchanged.** See "Tests to Modify".
* `scripts/crud-test.sh`, `docs/bench/crud-runs.csv` — **no change required.** No domain or top-level tool was added or removed; the CSV buckets key on the four tool names.

## Scope Boundaries

### In Scope

* `body_mode` on `mail.get_message` and `mail.get_conversation`, with the `body` call-time alias.
* Server-side plain-text conversion via `Prefer: outlook.body-content-type`.
* Single-combined-value `Prefer` header construction.
* In-band truncation marking on both mail text formatters.
* Verb registry, concepts, manifest, AGENTS.md and CRUD-prompt updates.

### Out of Scope ("Here, But Not Further")

* `body_mode` on `list_messages` / `search_messages`. Prohibited, not deferred — see FR-12 and the AGENTS.md rule.
* A fourth `output` tier. Prohibited; see "Why not a fourth output tier".
* Local HTML-to-text conversion or sanitization, and any dependency that would enable it. Prohibited by FR-4 and CR-0019.
* Changing any default. `preview` is the default and `raw` remains explicit.
* Calendar event bodies. `get_event` already selects `body` and has a different shape of problem; if it needs the same treatment it is a separate CR against the same doctrine.
* Attachment content. `get_attachment` already has its own size-limited escalation.
* Renaming `create_draft` / `update_draft`'s `body` parameter to free the name. That is a breaking change to a shipped write surface in exchange for a nicer read parameter name.

## Alternative Approaches Considered

1. **Add `output=body` as a fourth tier.** Rejected. It would churn every read verb in four domains for two mail verbs, and it conflates two independent questions — `body_mode=text, output=summary` becomes unexpressible.
2. **Strip HTML locally on the existing raw body.** Rejected. It needs an HTML parser in a codebase whose CR-0019 explicitly declines responsibility for HTML sanitization, and Graph already does the conversion correctly for the cost of one request header.
3. **Raise the preview cap instead.** Not possible. 255 characters is Graph's `bodyPreview` property, not a local truncation.
4. **Name the parameter `body`.** Rejected on evidence: the aggregate schema union is first-verb-wins and the draft verbs already own `body`. See "Why the parameter is not called `body`". `body` survives as an undeclared call-time alias.
5. **Add `body` to `SerializeSummaryMessage` when present.** Rejected. AGENTS.md makes summary field sets a deliberate contract, and three tests assert the exclusion. The handler attaches the body instead — the pattern already used for the provenance flag — so the contract holds and the tests keep their meaning.
6. **Escalate a thread message by message.** Rejected. It costs N tool calls and N round trips to avoid N bodies the caller has already decided they want, and the per-message IDs are only available after the first thread read anyway.

## Impact Assessment

### User Impact

* Reading a whole email costs roughly the length of the email, instead of the length of its HTML plus a header block. Measured live at 5.2× cheaper than `output=raw` for `text` and 3.3× for `full`.
* A truncated preview is now visibly truncated and names the parameter that completes it.
* A whole thread can be read in one call at prose cost.
* No existing call changes behaviour. Every response to a request that omits `body_mode` is unchanged except for the truncation marker.

### Technical Impact

* Four new files, all small and single-purpose. No dependency added; `go.mod` untouched.
* +273 bytes of tool schema, paid once per session.
* `Prefer` header construction is now centralised, which removes a latent multi-preference hazard from future work.
* `SerializeSummaryMessage`'s contract is unchanged, so no existing serialization test is invalidated.

### Business Impact

* Reading mail is the mail domain's primary use case, and until now it either truncated or overpaid. This makes the common case correct at the price it should cost.

## Implementation Approach

1. `ValidateBodyMode` and the mode constants (`internal/tools/body_mode.go`).
2. `newPreferHeaders` and `bodyContentTypePreference` (`internal/tools/prefer_header.go`).
3. `SerializeMessageBody` (`internal/graph/mail_body.go`).
4. Handler wiring in `get_message.go` and `get_conversation.go`: validate, conditional `$select`, conditional `Prefer` (including `SetHeaders` on the page iterator), conditional attachment.
5. Formatter changes and the truncation helpers (`internal/tools/body_text.go`, `text_format.go`).
6. Verb registry: schema, `Summary`, `Description`, `Examples`, `SeeDocs`.
7. Documentation: AGENTS.md, `docs/concepts.md`, `extension/manifest.json`, CRUD prompt.
8. Tests.

## Test Strategy

### Tests to Add

* `internal/tools/body_mode_test.go` — default, all three values, the `body` alias, `body_mode` winning over the alias, and rejection by name for four bad values.
* `internal/tools/prefer_header_test.go` — mode-to-preference mapping; nil when there is nothing to send; and `TestNewPreferHeaders_CombinesIntoOneValue`, which asserts `Get("Prefer")` returns **exactly one** value containing both preferences in order.
* `internal/graph/mail_body_test.go` — nil message and bodyless message both return nil; a text body round-trips; and the standalone helper produces exactly what `SerializeMessage` embeds.
* `internal/tools/get_message_test.go` — the default requests no body and sends no `Prefer` (parsing `$select` properly, since `bodyPreview` contains the substring `body`); `text` selects the body and sends exactly one correct `Prefer`; `full` selects the body and sends none; the `body` alias works; `body_mode` composes with `output=summary` and leaks no full-only field; a capped preview is announced and a short one is not; a bogus value errors by name.
* `internal/tools/get_conversation_test.go` — the default path is unchanged; `text` escalates every message with one `Prefer`; a thread of truncated previews gets one marker per entry and exactly one footer hint.
* `internal/tools/text_format_test.go` — an attached body replaces the preview and suppresses the notice; a JSON-round-tripped body map is read the same as a direct one; an empty body map falls back to the preview; conversation full bodies and mixed truncation.

### Tests to Modify

* `internal/tools/get_message_test.go::TestGetMessageTool_HasParameters` and `internal/tools/get_conversation_test.go::TestGetConversationTool_HasParameters` — add `body_mode` to the expected property lists.
* `internal/graph/mail_serialize_test.go` **needs no change.** The brief for this work predicted that its three exact-key-set assertions (lines 251, 331, 379) would fail. They do not, because the body is attached by the handler rather than added to `SerializeSummaryMessage`. The prediction was correct for the design it assumed; the design was changed specifically so that the "summary field sets are intentional" contract keeps its teeth.

### Tests to Remove

None.

## Acceptance Criteria

### AC-1: the default path is unchanged

Given a message whose body exceeds 255 characters
When `mail.get_message` is called without `body_mode`
Then the Graph request contains no `body` in `$select` and no `Prefer` header
And the response contains the preview and nothing more of the body

### AC-2: text mode returns plain text converted by Graph

Given a message with an HTML body
When `mail.get_message` is called with `body_mode="text"`
Then the Graph request sends exactly one `Prefer` header equal to `outlook.body-content-type="text"`
And `body` is present in `$select`
And the response contains the full body content

### AC-3: full mode returns the stored body without the raw baggage

Given a message with an HTML body
When `mail.get_message` is called with `body_mode="full"`
Then the response contains the HTML body
And no `Prefer` header is sent
And the response contains no `internetMessageHeaders`, `conversationIndex`, `replyTo` or `bccRecipients`

### AC-4: the two axes compose

When `mail.get_message` is called with `body_mode="text"` and `output="summary"`
Then the JSON response contains a `body` object with `contentType` `text` and the full content
And it contains none of the raw tier's full-only fields

### AC-5: a truncated preview says so

Given a message whose `bodyPreview` is at the 255-character cap
When `mail.get_message` is called with default arguments
Then the text response ends with the truncation notice naming `body_mode="text"`
And given a message with a short preview, the notice is absent

### AC-6: a thread escalates as a unit and hints once

When `mail.get_conversation` is called with `body_mode="text"`
Then every message renders its full body and none renders a `Preview:` line
And when called without `body_mode` against a thread of truncated previews, each entry carries the compact marker and the escalation hint appears exactly once

### AC-7: a bogus value is rejected by name

When either verb is called with `body_mode="html"`
Then the result is the error `body_mode must be 'preview', 'text', or 'full'`

### AC-8: Prefer preferences combine into one header

When `newPreferHeaders` is given a timezone preference and a body-content-type preference
Then `Get("Prefer")` returns exactly one value containing both, comma-separated

### AC-9: the collection verbs are untouched

Then neither `list_messages` nor `search_messages` declares or honours `body_mode`
And the prohibition is recorded in AGENTS.md

### AC-10: no dependency was added

Then `go.mod` and `go.sum` are unchanged and `make tidy` is clean

## Live verification

Performed 2026-09-02 by the requestor against a live Microsoft 365 mailbox. The automated suite is entirely mocked, so these are the claims that only a real account could settle.

### Verified

1. **Graph honours `Prefer: outlook.body-content-type="text"`.** `body_mode=text` against a real HTML email returned the complete body as server-converted plain text — no tags, no inline styles. This was the single assumption the whole CR rested on; it is now evidence rather than a premise. (AC-2.)
2. **The truncation marker fires correctly**, emitting `[preview truncated at 255 characters — pass body_mode="text" for the full plain-text body]` on a real over-cap message. (AC-5.)
3. **The token delta is measured**, not argued — see the table under "What the doctrine decided, and what it did not". `text` is 5.2× cheaper than `output=raw` on the same message and `full` is 3.3× cheaper, the latter purely from dropping the internet-header bundle.
4. **The `body` parameter-name collision is real**, not hypothetical. Registration order confirmed at `mail_verbs.go:122` (`get_message`), `:541` (`create_draft` `body`) and `:653` (`update_draft` `body`). First-verb-wins would have published a body-mode enum as the draft `body` parameter for every caller of the `mail` tool. See "Why the parameter is not called `body`".
5. **Gates reproduce independently:** clean build, `go test -race ./...` 14/14, and lint reporting exactly the two pre-existing `QF1012` findings — neither introduced by this CR, both already fixed on the CR-0067 branch and inherited on merge.

### Still outstanding

These were not tested and are not softened by the above:

* Whether a `bodyPreview` that is exactly 255 characters **and complete** exists in practice, which is the one case the length heuristic marks wrongly.
* Whether Graph accepts `$select`-plus-`Prefer` on the conversation query, which already carries one Graph quirk (`$orderby` rejected as `InefficientFilter` on a `conversationId` filter).
* Whether the undeclared `body` alias survives a real MCP client's argument forwarding end to end. CRUD Step 30g call iv covers it.

## Quality Standards Compliance

- [x] All code compiles (`make build`)
- [x] Static analysis passes (`make vet`)
- [x] Code is formatted (`make fmt-check`)
- [x] `go.mod` is tidy (`make tidy`)
- [x] Docs bundle gates pass (`make docs-bundle`)
- [x] All tests pass (`make test`, and `go test -race ./...`)
- [x] Lint is clean (`golangci-lint run`)
- [x] New exported symbols have Go doc comments
- [x] New logic follows the single-purpose-file convention
- [x] Extension manifest updated
- [x] Live verification against a Microsoft 365 mailbox (2026-09-02) — Graph's plain-text conversion, the truncation marker and the token delta all confirmed; three narrower items remain untested, see "Live verification"

## Risks and Mitigation

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Graph ignores `Prefer: outlook.body-content-type="text"` for some message types and returns HTML anyway | Low | Medium | **Largely closed** by the 2026-09-02 live check: a real HTML email came back as server-converted plain text. Residual risk is unusual message types only. The response carries `body.contentType`, so the caller can always see what it got |
| `bodyPreview` is not exactly 255 characters in practice, so the truncation heuristic mis-fires | Low | Low | The threshold is the named constant `bodyPreviewCapRunes` with the reasoning at its definition; a false marker costs one line, a missed one costs the previous silence. The marker was confirmed firing on real over-cap mail; the exactly-255-and-complete case remains untested. CRUD Step 30g checks both directions |
| `body_mode=text` is larger than a reader expects on link-heavy mail | Medium | Low | Real behaviour, not a defect: Graph's conversion keeps URLs inline and expanded, so a single SafeLinks-rewritten link costs ~500 characters. Documented in "Why Graph does the conversion" so the reader meets it in the CR rather than in a response |
| A very long thread with `body_mode=text` returns a large response | Medium | Medium | Bounded by `max_results` (default 50, max 100), which the caller can lower before escalating; the third registry example shows exactly that |
| The undeclared `body` alias is stripped by a client on a `MAIL_MANAGE_ENABLED=false` server | Low | Low | Degrades to the documented default, never to a wrong body. `body_mode` is declared on both verbs and always forwarded |
| A future CR adds a second `Prefer` preference to one of these verbs and stacks it | Low | Medium | `newPreferHeaders` is the only constructor in the new code, AGENTS.md makes it mandatory, and `TestNewPreferHeaders_CombinesIntoOneValue` fails if it stops combining |
| A future CR reads "body escalation" and adds `body_mode` to the collection verbs | Medium | Medium | FR-12, the AGENTS.md rule and the `buildGetConversationVerb` comment all state the prohibition and its reason |

## Dependencies

* CR-0033 / CR-0043 / CR-0051 — the output-tier doctrine this CR amends. Amendment is narrow: form, not timing. The preview stays the default and `raw` stays explicit.
* CR-0060 — domain-aggregated tools; provides the verb registry and the aggregate schema union that constrains the parameter name.
* CR-0065 — registry-driven tool reference; `Description`, `Examples` and `SeeDocs` are where this parameter is documented.
* CR-0066 — the alias declaration rule applied to `body` vs `body_mode`.
* CR-0019 — declines responsibility for HTML sanitization; the reason plain text comes from Graph.

## Estimated Effort

* Helpers and serializer wrapper (4 new files): ~45 minutes
* Handler wiring (2 files): ~45 minutes
* Formatters: ~30 minutes
* Verb registry, manifest, concepts, AGENTS.md, CRUD prompt: ~1 hour
* Tests: ~1 hour
* CR document: ~1 hour

## Decision Outcome

A full mail body is available as plain text through a `body_mode` parameter on `mail.get_message` and `mail.get_conversation`, converted by Microsoft Graph rather than by this server. `preview` remains the default so no existing call gets more expensive, there are still exactly three output tiers, and the two collection verbs are deliberately excluded. The "Body escalation pattern" in AGENTS.md is amended to say that escalation targets a `body_mode` parameter rather than the raw tier, that plain text comes from Graph, that the pattern stops at these two verbs, and that a truncated preview must say so.

## Related Items

* CR-0033 — MCP response filtering (introduced the output tiers).
* CR-0043 — response optimisation follow-up.
* CR-0051 — token-efficient response defaults (made `text` the default; the doctrine amended here).
* CR-0060 — domain-aggregated tools with verb-based operations.
* CR-0065 — user documentation architecture and registry-driven tool reference.
* CR-0066 — folder management and message move (source of the alias declaration rule).
* CR-0019 — security hardening (HTML sanitization responsibility).
