---
id: "CR-0066"
status: "proposed"
date: 2026-05-04
requestor: torenander
stakeholders:
  - torenander
  - desek
priority: "medium"
target-version: "0.5.0"
---

# Inbox Message Rules Management

## Change Summary

Add five new verbs to the `mail` domain tool for managing Outlook inbox message rules via the Microsoft Graph `/me/mailFolders/inbox/messageRules` API: `list_rules`, `get_rule`, `create_rule`, `update_rule`, and `delete_rule`. Rules allow automated server-side actions on incoming mail (move, forward, categorize, mark read, delete) based on sender, subject, body, and other conditions. A new OAuth scope (`MailboxSettings.ReadWrite`) is required and gated behind the existing `MailManageEnabled` feature flag.

## Motivation and Background

CR-0058 explicitly deferred "Mail rules / inbox automation — server-side rules (`/me/mailFolders/inbox/messageRules`)" as out of scope. This CR picks up that deferred item.

Inbox rules are a core Outlook productivity feature: users create rules to automatically sort, forward, categorize, or flag incoming messages. Today, users must switch to Outlook's web or desktop UI to manage rules. With MCP rule verbs, an LLM assistant can help users create, inspect, and modify rules conversationally — e.g. "create a rule that moves all emails from finance@contoso.com to the Finance folder" or "show me my current inbox rules".

### Use Case: Conversational Rule Management

1. User asks: "Move all emails from notifications@github.com to my GitHub folder."
2. Model calls `mail.list_folders` to find the GitHub folder ID.
3. Model calls `mail.create_rule` with `displayName`, `conditions.senderContains`, and `actions.moveToFolder`.
4. Rule is created server-side; incoming messages matching the condition are automatically moved.

### Use Case: Rule Audit

1. User asks: "What inbox rules do I have?"
2. Model calls `mail.list_rules` and presents a numbered summary.
3. User asks: "Disable rule #3."
4. Model calls `mail.update_rule` with `is_enabled=false`.

## Change Drivers

* **CR-0058 deferred item**: Explicitly listed as out of scope; now requested by user.
* **Productivity**: Rule management is a common Outlook task that benefits from conversational interaction.
* **Completeness**: The mail domain covers reading, drafting, and searching — rules close the automation gap.

## Current State

The mail domain has 13 verbs across read, search, and draft pillars. No rule management capability exists.

## Proposed Change

### New OAuth Scope

| Scope | Enables | When |
|-------|---------|------|
| `MailboxSettings.ReadWrite` | CRUD operations on inbox message rules | When `MailManageEnabled=true` |

The Microsoft Graph messageRules API requires `MailboxSettings.ReadWrite` for all operations (including read). There is no read-only scope for rules. This scope is added alongside the existing `Mail.ReadWrite` when `MailManageEnabled` is true.

### New Verbs (5)

All verbs are registered in the `mail` domain and gated behind `MailManageEnabled`.

| Verb | HTTP | Graph Endpoint | Audit Op |
|------|------|---------------|----------|
| `list_rules` | GET | `/me/mailFolders/inbox/messageRules` | read |
| `get_rule` | GET | `/me/mailFolders/inbox/messageRules/{id}` | read |
| `create_rule` | POST | `/me/mailFolders/inbox/messageRules` | write |
| `update_rule` | PATCH | `/me/mailFolders/inbox/messageRules/{id}` | write |
| `delete_rule` | DELETE | `/me/mailFolders/inbox/messageRules/{id}` | delete |

### messageRule Resource Model

Rules have three top-level components:

- **conditions** (`messageRulePredicates`): when to trigger (e.g., `senderContains`, `subjectContains`, `fromAddresses`, `hasAttachments`, `importance`).
- **actions** (`messageRuleActions`): what to do (e.g., `moveToFolder`, `copyToFolder`, `forwardTo`, `markAsRead`, `assignCategories`, `delete`, `stopProcessingRules`).
- **exceptions** (`messageRulePredicates`): conditions that prevent the rule from firing.

### Verb Parameters

#### list_rules

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `account` | string | no | Account label or UPN |
| `output` | enum | no | `text` (default), `summary`, `raw` |

#### get_rule

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `rule_id` | string | yes | The rule ID |
| `account` | string | no | Account label or UPN |
| `output` | enum | no | `text` (default), `summary`, `raw` |

#### create_rule

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `display_name` | string | yes | Human-readable rule name |
| `sequence` | number | no | Execution order (default: server-assigned) |
| `is_enabled` | boolean | no | Enable the rule (default: true) |
| `conditions` | string | yes | JSON object of messageRulePredicates |
| `actions` | string | yes | JSON object of messageRuleActions |
| `exceptions` | string | no | JSON object of messageRulePredicates |
| `account` | string | no | Account label or UPN |

The `conditions`, `actions`, and `exceptions` parameters accept JSON strings that map directly to the Graph API `messageRulePredicates` and `messageRuleActions` resource shapes. The `help` verb documents the supported fields with examples.

#### update_rule

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `rule_id` | string | yes | The rule ID to update |
| `display_name` | string | no | New display name |
| `sequence` | number | no | New execution order |
| `is_enabled` | boolean | no | Enable or disable the rule |
| `conditions` | string | no | JSON object of messageRulePredicates (replaces) |
| `actions` | string | no | JSON object of messageRuleActions (replaces) |
| `exceptions` | string | no | JSON object of messageRulePredicates (replaces) |
| `account` | string | no | Account label or UPN |

PATCH semantics: only supplied fields are changed.

#### delete_rule

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `rule_id` | string | yes | The rule ID to delete |
| `account` | string | no | Account label or UPN |

## Requirements

### Functional Requirements

#### OAuth Scope

1. When `MailManageEnabled` is `true`, the OAuth scope set **MUST** include `MailboxSettings.ReadWrite` in addition to `Mail.ReadWrite`.
2. When `MailManageEnabled` is `false`, `MailboxSettings.ReadWrite` **MUST NOT** be requested.

#### list_rules

3. The system **MUST** provide a `list_rules` verb in the mail domain that returns all inbox message rules.
4. `list_rules` **MUST** call `GET /me/mailFolders/inbox/messageRules`.
5. `list_rules` **MUST** support the three-tier output model (`text`, `summary`, `raw`).
6. `list_rules` text output **MUST** show rule display name, sequence, enabled state, a conditions summary, and an actions summary per rule.
7. `list_rules` **MUST** include MCP annotations: ReadOnly=true, Destructive=false, Idempotent=true, OpenWorld=true.

#### get_rule

8. The system **MUST** provide a `get_rule` verb that returns a single rule by ID.
9. `get_rule` **MUST** call `GET /me/mailFolders/inbox/messageRules/{id}`.
10. `get_rule` **MUST** validate `rule_id` using `ValidateResourceID`.
11. `get_rule` **MUST** support the three-tier output model.
12. `get_rule` text output **MUST** show all rule fields: display name, sequence, enabled, hasError, isReadOnly, conditions, actions, and exceptions.
13. `get_rule` **MUST** include MCP annotations: ReadOnly=true, Destructive=false, Idempotent=true, OpenWorld=true.

#### create_rule

14. The system **MUST** provide a `create_rule` verb that creates a new inbox message rule.
15. `create_rule` **MUST** accept required parameters `display_name`, `conditions` (JSON string), and `actions` (JSON string).
16. `create_rule` **MUST** accept optional parameters `sequence`, `is_enabled`, and `exceptions` (JSON string).
17. `create_rule` **MUST** validate that `conditions` and `actions` are valid JSON and deserialize to the expected Graph API shapes. Invalid JSON **MUST** return a user-friendly error.
18. `create_rule` **MUST** call `POST /me/mailFolders/inbox/messageRules`.
19. `create_rule` **MUST** return the created rule ID, display name, and sequence in the confirmation text.
20. `create_rule` **MUST** include MCP annotations: ReadOnly=false, Destructive=false, Idempotent=false, OpenWorld=true.

#### update_rule

21. The system **MUST** provide an `update_rule` verb that updates an existing inbox message rule.
22. `update_rule` **MUST** accept a required `rule_id` parameter and optional `display_name`, `sequence`, `is_enabled`, `conditions`, `actions`, and `exceptions` parameters.
23. `update_rule` **MUST** use PATCH semantics: only explicitly provided fields are included in the request body.
24. `update_rule` **MUST** validate JSON parameters the same way as `create_rule`.
25. `update_rule` **MUST** call `PATCH /me/mailFolders/inbox/messageRules/{id}`.
26. `update_rule` **MUST** return the updated rule ID and display name in the confirmation text.
27. `update_rule` **MUST** include MCP annotations: ReadOnly=false, Destructive=false, Idempotent=true, OpenWorld=true.

#### delete_rule

28. The system **MUST** provide a `delete_rule` verb that deletes an inbox message rule.
29. `delete_rule` **MUST** validate `rule_id` using `ValidateResourceID`.
30. `delete_rule` **MUST** call `DELETE /me/mailFolders/inbox/messageRules/{id}`.
31. `delete_rule` **MUST** return a confirmation with the deleted rule ID.
32. `delete_rule` **MUST** include MCP annotations: ReadOnly=false, Destructive=true, Idempotent=true, OpenWorld=true.

#### Tool Descriptions

33. `create_rule` description **MUST** document the supported condition fields and action fields with at least one example for each category (text matching, recipient matching, folder operations, forwarding).
34. All rule verb descriptions **MUST** note that rules apply only to the Inbox folder.
35. `update_rule` and `delete_rule` descriptions **MUST** note that rules with `isReadOnly=true` cannot be modified or deleted via the API.

#### Aggregate Annotations

36. The existing mail domain aggregate annotations **MUST NOT** change. The new verbs fit within the existing conservative envelope: `readOnly=false`, `destructive=true`, `idempotent=false`, `openWorld=true`.

### Non-Functional Requirements

1. Each new verb handler **MUST** be defined in its own file in `internal/tools/`.
2. All new code **MUST** include Go doc comments per project documentation standards.
3. All existing tests **MUST** continue to pass.
4. The CRUD test document (`docs/prompts/mcp-tool-crud-test.md`) **MUST** be updated with rule management test steps.
5. New verb handlers **MUST** use `RetryGraphCall` for transient errors and `WithTimeout` for request timeouts.
6. All read verbs **MUST** follow the three-tier output model. Write verbs return text confirmations.
7. New verbs **MUST** add corresponding assertions in `internal/tools/tool_annotations_test.go`.

## Affected Components

| Component | Change |
|-----------|--------|
| `internal/auth/auth.go` | Add `MailboxSettings.ReadWrite` scope when `MailManageEnabled` |
| `internal/tools/list_rules.go` | **New**: `list_rules` handler |
| `internal/tools/get_rule.go` | **New**: `get_rule` handler |
| `internal/tools/create_rule.go` | **New**: `create_rule` handler |
| `internal/tools/update_rule.go` | **New**: `update_rule` handler |
| `internal/tools/delete_rule.go` | **New**: `delete_rule` handler |
| `internal/graph/rule_serialize.go` | **New**: messageRule serialization helpers (text, summary, raw) |
| `internal/tools/text_format.go` | Add `FormatRulesText`, `FormatRuleDetailText` formatters |
| `internal/server/mail_verbs.go` | Register 5 new verbs in `MailManageEnabled` block |
| `internal/tools/tool_annotations_test.go` | Add annotation tests for all 5 new verbs |
| `docs/prompts/mcp-tool-crud-test.md` | Add rule management test steps |

## Scope Boundaries

### In Scope

* 5 new verbs for inbox message rule CRUD.
* `MailboxSettings.ReadWrite` scope addition.
* Serialization and text formatting for rule resources.
* Verb registration in the mail domain under `MailManageEnabled`.
* Annotation and handler tests.

### Out of Scope

* **Rules on non-Inbox folders** — the Graph API only supports rules on the Inbox.
* **Read-only scope for rules** — `MailboxSettings.ReadWrite` is the only scope available; no read-only tier is possible.
* **Rule templates or presets** — the verbs expose the raw Graph API capabilities. Higher-level abstractions are left to the LLM.
* **Rule execution monitoring** — no telemetry on how often rules fire.
* **Bulk rule operations** — create/update/delete operate on one rule at a time.

## Impact Assessment

### User Impact

Users with `MailManageEnabled=true` gain the ability to manage inbox rules conversationally. Users who enable manage features for the first time will need to re-consent for the additional `MailboxSettings.ReadWrite` scope.

### Technical Impact

* **Scope addition**: `MailboxSettings.ReadWrite` is added to the OAuth request when `MailManageEnabled` is true. Users upgrading from a prior version will be prompted to re-authenticate.
* **Verb count**: +5 verbs to the mail domain. Does not add a new top-level MCP tool.
* **No new dependencies**: The `msgraph-sdk-go` already provides `messageRule` models.

### Security Impact

* `MailboxSettings.ReadWrite` grants read/write access to all mailbox settings (not just rules). Mitigated by: opt-in `MailManageEnabled`, ReadOnlyGuard, and audit logging.
* `delete_rule` is destructive and irreversible. Mitigated by: audit logging, and the LLM should confirm with the user before deleting.
* `create_rule` with `actions.permanentDelete=true` could cause data loss. Mitigated by: tool description warns about permanent deletion actions; the LLM should confirm destructive action parameters with the user.

## Implementation Approach

### Phase 1: OAuth Scope

**`internal/auth/auth.go`:**
- Add `mailboxSettingsScope = "MailboxSettings.ReadWrite"`.
- Update `Scopes()`: when `MailManageEnabled` is true, append `mailboxSettingsScope` alongside `mailReadWriteScope`.

### Phase 2: Serialization and Formatting

**`internal/graph/rule_serialize.go`:**
- `SerializeRule(rule)` — full raw serialization.
- `SerializeSummaryRule(rule)` — curated field set: id, displayName, sequence, isEnabled, conditions summary, actions summary.

**`internal/tools/text_format.go`:**
- `FormatRulesText(rules)` — numbered list with display name, sequence, enabled state, and brief conditions/actions summary.
- `FormatRuleDetailText(rule)` — full detail with all conditions, actions, and exceptions.

### Phase 3: Verb Handlers

Five new handler files in `internal/tools/`:

- `list_rules.go` — `NewHandleListRules(retryCfg, timeout)`
- `get_rule.go` — `NewHandleGetRule(retryCfg, timeout)`
- `create_rule.go` — `NewHandleCreateRule(retryCfg, timeout)` — parses JSON conditions/actions, builds `messageRule`, POST.
- `update_rule.go` — `NewHandleUpdateRule(retryCfg, timeout)` — PATCH semantics.
- `delete_rule.go` — `NewHandleDeleteRule(retryCfg, timeout)` — DELETE with confirmation.

### Phase 4: Verb Registration and Tests

**`internal/server/mail_verbs.go`:**
- Add 5 verb builders in the `MailManageEnabled` block after the existing draft verbs.
- `list_rules` and `get_rule` use `wrap` (read chain); `create_rule` and `update_rule` use `wrapWrite`; `delete_rule` uses `wrapWrite`.

**Tests:**
- Annotation assertions for all 5 verbs.
- Handler unit tests with mock Graph client.
- Scope test: `MailManageEnabled=true` produces `MailboxSettings.ReadWrite`.

## Test Strategy

### Tests to Add

| Test File | Test Name | Description |
|-----------|-----------|-------------|
| `auth_test.go` | `TestScopes_MailManageIncludesMailboxSettings` | `MailManageEnabled` returns `MailboxSettings.ReadWrite` |
| `list_rules_test.go` | `TestListRules_Success` | Returns all rules with text formatting |
| `list_rules_test.go` | `TestListRules_Empty` | No rules returns empty message |
| `get_rule_test.go` | `TestGetRule_Success` | Returns single rule detail |
| `get_rule_test.go` | `TestGetRule_InvalidID` | Invalid rule_id returns error |
| `create_rule_test.go` | `TestCreateRule_Success` | Rule created with conditions and actions |
| `create_rule_test.go` | `TestCreateRule_InvalidJSON` | Malformed conditions JSON returns error |
| `create_rule_test.go` | `TestCreateRule_MissingRequired` | Missing display_name or conditions returns error |
| `update_rule_test.go` | `TestUpdateRule_Success` | Rule updated with PATCH semantics |
| `update_rule_test.go` | `TestUpdateRule_DisableOnly` | Only is_enabled=false sent |
| `delete_rule_test.go` | `TestDeleteRule_Success` | Rule deleted, confirmation returned |
| `delete_rule_test.go` | `TestDeleteRule_InvalidID` | Invalid rule_id returns error |
| `tool_annotations_test.go` | 5 tests | Annotation sets for all new verbs |

## Acceptance Criteria

### AC-1: List inbox rules

```gherkin
Given MailManageEnabled is true and the inbox has message rules
When mail.list_rules is called
Then all rules are returned with display name, sequence, enabled state, and a conditions/actions summary
```

### AC-2: Get rule detail

```gherkin
Given a valid rule ID
When mail.get_rule is called with rule_id
Then the full rule is returned including all conditions, actions, and exceptions
```

### AC-3: Create a rule

```gherkin
Given MailManageEnabled is true
When mail.create_rule is called with display_name, conditions, and actions
Then a rule is created on the inbox and the rule ID is returned
  And the rule fires on subsequent matching incoming messages
```

### AC-4: Update a rule

```gherkin
Given an existing rule
When mail.update_rule is called with rule_id and is_enabled=false
Then only the isEnabled field is changed on the rule (PATCH semantics)
```

### AC-5: Delete a rule

```gherkin
Given an existing rule
When mail.delete_rule is called with rule_id
Then the rule is permanently deleted
  And subsequent mail.get_rule with the same ID returns a 404 error
```

### AC-6: Scope includes MailboxSettings.ReadWrite

```gherkin
Given MailManageEnabled is true
Then OAuth scopes include MailboxSettings.ReadWrite alongside Mail.ReadWrite

Given MailManageEnabled is false
Then OAuth scopes do NOT include MailboxSettings.ReadWrite
```

### AC-7: Read-only rules are not modified

```gherkin
Given a rule with isReadOnly=true
When mail.update_rule or mail.delete_rule is called
Then the Graph API returns an appropriate error
  And the verb surfaces the error to the caller
```

### AC-8: All quality checks pass

```gherkin
Given all code changes are applied
When make ci is executed
Then the build succeeds, all linter checks pass, and all tests pass
```

## Quality Standards Compliance

### Build & Compilation

- [ ] Code compiles/builds without errors
- [ ] No new compiler warnings introduced

### Linting & Code Style

- [ ] All linter checks pass with zero warnings/errors
- [ ] Code follows project coding conventions and style guides

### Test Execution

- [ ] All existing tests pass after implementation
- [ ] All new tests pass
- [ ] Test coverage meets project requirements for changed code

### Documentation

- [ ] Go doc comments on all new verb handlers and serialization functions
- [ ] CRUD test document updated with rule management test steps

### Code Review

- [ ] Changes submitted via pull request
- [ ] PR title follows Conventional Commits format
- [ ] Code review completed and approved
- [ ] Changes squash-merged to maintain linear history

### Verification Commands

```bash
make build
make lint
make test
make ci
```

## Risks and Mitigation

### Risk 1: MailboxSettings.ReadWrite grants broader access than just rules

**Likelihood:** high (by design)
**Impact:** medium
**Mitigation:** The scope is required by the Graph API — there is no narrower alternative. Gated behind `MailManageEnabled` (opt-in, default false). Audit logging records all rule operations. ReadOnlyGuard blocks write verbs in read-only mode.

### Risk 2: Re-consent required for existing MailManageEnabled users

**Likelihood:** high
**Impact:** low
**Mitigation:** Users upgrading will be prompted to re-authenticate with the additional scope. The auth middleware handles this transparently.

### Risk 3: Rules with permanentDelete action cause irrecoverable data loss

**Likelihood:** low
**Impact:** high
**Mitigation:** Tool description explicitly warns about `permanentDelete`. The LLM should confirm destructive rule actions with the user. Audit logging captures the full rule definition at creation time.

### Risk 4: isReadOnly rules cannot be modified via API

**Likelihood:** medium
**Impact:** low
**Mitigation:** The Graph API returns a clear error. Verb descriptions document this limitation. The `get_rule` text output includes the `isReadOnly` field so the LLM can check before attempting modification.

## Dependencies

* No new Go module dependencies. `msgraph-sdk-go` provides `messageRule` models.
* Requires Microsoft Graph API permission: `MailboxSettings.ReadWrite` (delegated).
* Users must have Exchange Online licensing.

## Alternative Approaches Considered

* **Separate feature flag (e.g., `MailRulesEnabled`)**: Rejected. Rules are a natural extension of mail management. Adding a third mail flag increases configuration complexity without clear benefit. The `MailManageEnabled` flag already gates write operations.
* **Read verbs gated under `MailEnabled` instead of `MailManageEnabled`**: Rejected. The Graph API requires `MailboxSettings.ReadWrite` even for reading rules — there is no read-only scope. Adding this scope for read-only users would be a misleading scope escalation.
* **Structured parameters instead of JSON strings for conditions/actions**: Considered but rejected for v1. The messageRulePredicates and messageRuleActions types have 30+ optional fields each. Flattening them into individual MCP parameters would create an unwieldy schema. JSON strings map directly to the Graph API shape and allow the LLM to construct conditions flexibly. The `help` verb documents the JSON structure with examples.

## Decision Outcome

Chosen approach: "Five mail domain verbs with JSON condition/action parameters gated behind MailManageEnabled", because it extends the existing mail domain naturally, reuses the established feature-flag gating, and maps cleanly to the Graph messageRules API without introducing excessive parameter complexity.

## Related Items

* CR-0058: Mail Intelligence — deferred mail rules as out of scope; this CR picks up that item.
* CR-0060: Domain Aggregated Tools — verb dispatch pattern used for registration.
* CR-0052: MCP Tool Annotations — annotation matrix compliance.
* CR-0051: Token-Efficient Response Defaults — three-tier output model for read verbs.
