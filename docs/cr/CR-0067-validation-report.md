---
cr: CR-0067
title: Validation Report — Authentication Resilience and Recovery
date: 2026-09-02
branch: feat/cr-0067-auth-resilience
finalized-commit: 504d87a
validator: implementation agent (self-validation); live verification by requestor
status: PASS (A2 WITHDRAWN)
---

# CR-0067 Validation Report

## Summary

CR-0067 set out to remove the authentication friction that repeatedly interrupts LLM sessions. Six of its seven work items landed. The seventh, A2, was implemented, tested against a real Microsoft 365 account, disproved, and reverted.

The honest headline of this CR is not what was built but what was disproved. Three assumptions that looked correct on paper — two of them load-bearing for the original design — failed against a live account. One of them concealed a functional defect that unit tests could not have caught. A reader picking this CR up later will get more value from the "Disproved assumptions" section below than from the requirement tables.

Counts:

- Work items: 6/6 PASS, 1 WITHDRAWN (A2)
- Functional Requirements: 30/30 PASS (FR-5/6/7 restated after the A2 revert)
- Non-Functional Requirements: 4/4 PASS
- Acceptance Criteria: 7/7 PASS
- Tests added: 28 PASS; tests modified: 6 files
- Gate: `make docs-bundle build vet fmt-check tidy test` PASS, `go test -race ./...` PASS (14/14), `golangci-lint` 0 issues
- Gaps: 3 — three unit-tested-only items. NFR-27 was PARTIAL and is now met; 2 follow-ups recorded.

## Verification tier

This is the most important table in the report. "Live" means exercised against a real Microsoft 365 account by the requestor. "Unit" means covered by automated tests only, with no live confirmation. Nothing below is marked live on the strength of reasoning.

| Item | Tier | Evidence |
|---|---|---|
| A1 silent refresh + retry | **LIVE** | Cold `device_code` credential: silent refresh succeeded and the original call was retried in **143ms with no device code emitted**, on the path that previously always prompted |
| A3 `complete_auth` always registered | **LIVE** | Verb present in the `system` operation enum; with elicitation unsupported the fallback returned the auth URL in **0.5s** |
| A4 recovery verbs reachable | **LIVE** | `account.list` answered immediately during pending auth (previously hung past 60s); `account.refresh` declined with an explanation (previously hung) |
| A7 sign-in link + code | **LIVE (negative)** | Live testing disproved the pre-fill premise and exposed the missing-code defect; the corrected behaviour is unit-tested only |
| A5 guidance wording | **UNIT ONLY** | `TestRecoverySteps_MethodSpecific`, `TestClassifyAuthError_*`. No live confirmation that an LLM follows the new guidance successfully |
| A6 per-account re-auth | **UNIT ONLY** | `TestAccountResolver_ReportsAccountToAuthMiddleware`. Requires a **second** Microsoft account to verify live; not available |
| URL-mode elicitation `Accept` path | **UNIT ONLY** | No known client has been observed answering URL-mode elicitation with `Accept`. The ack→wait→retry path has never run against a real client |
| A2 `auth_code` default | **LIVE (disproved)** | See below |

## Requirement Verification

| ID | Requirement | Evidence (file) | Test Evidence | Status |
|----|-------------|-----------------|---------------|--------|
| FR-1 | Silent acquisition attempted before any interactive flow, on both paths | `internal/auth/middleware.go` (entry fast path; `handleAuthError`) | `TestMiddleware_FreshCredential_SilentRefreshSkipsPrompt`, `TestHandleAuthError_SilentRefreshRetries` | PASS |
| FR-2 | Attempt bounded by a short timeout (5s) | `internal/auth/silent.go` (`silentTokenTimeout`) | Covered by the above | PASS |
| FR-3 | azidentity credentials constructed with `DisableAutomaticAuthentication` | `internal/auth/auth.go` (`setupBrowserCredential`, `setupDeviceCodeCredential`) | `TestSetupCredential_GetTokenIsSilentOnly` | PASS |
| FR-3a | Eligibility is an explicit fail-safe allowlist, not an auth-method string | `internal/auth/silent.go` (`silentOnlyAcquirer`) | `TestSilentOnlyAcquirer_Eligibility`, `TestTrySilentToken_SkipsEscalatingCredential` | PASS |
| FR-3b | Interactive flows still work; `Authenticate()` bypasses the option | `azidentity public_client.go:110-133` (read); existing browser/device-code middleware tests | `TestAuthMiddleware_DeviceCodeAuth_*`, `TestAuthMiddleware_BrowserAuth_*` | PASS |
| FR-3c | `IsAuthError` classifies `AuthenticationRequiredError`; no SDK advice leaks | `internal/auth/errors.go` | `TestClassifyAuthError_AuthenticationRequiredError` | PASS |
| FR-3d | Startup probe runs for every method | `cmd/outlook-local-mcp/main.go` (`probeStartupToken`) | `TestStartupTokenProbe_CompletesWithin5Seconds/device_code_*` | PASS |
| FR-4 | Silent success marks authenticated and retries | `internal/auth/middleware.go` | `TestHandleAuthError_SilentRefreshRetries` | PASS |
| FR-5 | `InferAuthMethod` returns `("device_code","inferred")` for well-known IDs; rationale recorded in the doc comment | `internal/config/config.go` | `TestInferAuthMethod_DefaultDeviceCode`, `TestInferAuthMethod_WellKnownDeviceCode` | PASS (restated after A2 revert) |
| FR-6 | Explicit `OUTLOOK_MCP_AUTH_METHOD` wins, including `auth_code` | `internal/config/config.go` | `TestInferAuthMethod_ExplicitOverride`, `TestInferAuthMethod_ReturnsSource` | PASS |
| FR-7 | Custom client IDs return `("browser","default")` | `internal/config/config.go` | `TestInferAuthMethod_CustomClientBrowser` | PASS |
| FR-8 | `system.complete_auth` registered regardless of method | `internal/server/system_verbs.go` | Live: present in the operation enum | PASS |
| FR-9 | Non-`auth_code` credential gets method-specific guidance, not a type error | `internal/tools/complete_auth_guidance.go` | `TestCompleteAuth_NonAuthCodeCredential` (3 sub-cases) | PASS |
| FR-10 | `account` calls pass through during pending auth | `internal/auth/middleware.go` | `TestMiddleware_PendingAuth_AllowsAccountVerb` | PASS |
| FR-11 | `account` calls pass through on the fresh-credential fast path | `internal/auth/middleware.go` | `TestMiddleware_FreshCredential_AllowsAccountVerb` | PASS |
| FR-11a | Exempted verbs must also **return promptly**, not merely run | `internal/auth/inflight.go`, `email_resolver.go`, `refresh_account.go` | `TestRecoveryVerbsStayResponsiveDuringAuth` (2s bound), `TestRefreshAccount_DeclinesDuringInteractiveAuth` | PASS |
| FR-12 | Device code background context bounded at 300s | `internal/auth/pending.go` (`backgroundAuthTimeout`) | Code inspection; browser flow bounded identically | PASS |
| FR-13 | Pending completion channel/error published race-free | `internal/auth/pending.go` (`atomic.Pointer`) | `TestPendingOutcome`; `go test -race` clean | PASS |
| FR-14 | Guidance names `account.login`, not `account.add` | `internal/auth/errors.go` (`recoverySteps`) | `TestRecoverySteps_MethodSpecific` asserts `operation="add"` absent | PASS |
| FR-15 | Method-aware variant names `system.complete_auth` under `auth_code` | `internal/auth/errors.go` (`FormatAuthErrorFor`) | `TestRecoverySteps_MethodSpecific` | PASS |
| FR-16 | Detail after `authentication required` preserved | `internal/auth/errors.go` (`authRequiredDetail`) | `TestClassifyAuthError_PreservesDeviceCodeDetail`, `TestClassifyAuthError_BareAuthRequired` | PASS |
| FR-17 | Middleware installs a mutable slot; resolver fills it | `internal/auth/account_slot.go`, `account_resolver.go` | `TestAccountResolver_ReportsAccountToAuthMiddleware` | PASS (unit only) |
| FR-18 | `handleAuthError` prefers the slot over the closure credential | `internal/auth/middleware.go` (`resolvedAccountAuth`) | Same test asserts the closure credential is NOT used | PASS (unit only) |
| FR-19 | `inferAuthMethod` honours persisted method; recognises `DeviceCodeCredential` | `internal/auth/account_resolver.go` | `TestInferAuthMethod_DeviceCodeEntry` (5 cases) | PASS |
| FR-20 | URL-mode elicitation with `otc=<UserCode>` | `internal/auth/devicecode_present.go` | `TestAuthMiddleware_DeviceCodeAuth_URLElicitation` | PASS |
| FR-20a | Elicitation message MUST quote the code | `internal/auth/devicecode_present.go` (`deviceCodeElicitMessage`) | `TestDeviceCodeElicitMessage_QuotesTheCode` | PASS (defect found live, fixed) |
| FR-20b | No string may claim the code is pre-filled | codebase-wide | `TestDeviceCodeElicitMessage_QuotesTheCode` asserts absence | PASS |
| FR-21 | Structured prompt forwarded through `DeviceCodeMsgKey` | `internal/auth/auth.go`, `devicecode_prompt.go` | `TestNewDeviceCodePrompt` | PASS |
| FR-22 | Fallback is the Entra message verbatim, unaugmented | `internal/auth/devicecode_present.go` | `TestAuthMiddleware_DeviceCodeAuth_ElicitationFallback` | PASS |
| FR-23 | After acknowledgement, wait bounded and retry | `internal/auth/devicecode_present.go` | `TestAuthMiddleware_DeviceCodeAuth_URLElicitation` (asserts 2 handler calls) | PASS (unit only) |
| NFR-24 | No new top-level MCP tool | `internal/server/*_verbs.go` | Live: 4 aggregate tools | PASS |
| NFR-25 | `system` aggregate annotations stay conservative | `internal/server/system_verbs.go` | `TestToolAnnotations_System` — unchanged, already assumed `complete_auth` | PASS |
| NFR-26 | `go test -race ./...` passes | — | 14/14, no races | PASS |
| NFR-27 | Small single-purpose files; `middleware.go` must not grow | 9 new files in `internal/auth` | **`middleware.go` 804 → 637 lines (−167)** after `handleBrowserAuth` and `handleDeviceCodeAuth` moved to `browser_flow.go` and `devicecode_flow.go` | PASS |

## Acceptance Criteria Verification

| AC | Statement | Evidence | Status |
|----|-----------|----------|--------|
| AC-1 | Silent refresh precedes interactive auth and never escalates | `TestSetupCredential_GetTokenIsSilentOnly`, `TestTrySilentToken_SkipsEscalatingCredential`; **live: 143ms retry, no device code** | PASS |
| AC-2 | Default inference unchanged; rationale recorded; no forced re-auth | `TestInferAuthMethod_*`; `InferAuthMethod` doc comment names both rejected alternatives | PASS |
| AC-3 | `complete_auth` always exists and always answers usefully | `TestCompleteAuth_NonAuthCodeCredential`; **live: present in enum, fallback in 0.5s** | PASS |
| AC-4 | Recovery stays reachable, responsive, pending self-clears | `TestRecoveryVerbsStayResponsiveDuringAuth`; **live: `account.list` immediate, `account.refresh` declines** | PASS |
| AC-5 | Guidance correct and method-aware | `TestRecoverySteps_MethodSpecific`, `TestClassifyAuthError_PreservesDeviceCodeDetail` | PASS (unit only) |
| AC-6 | Re-auth targets the resolved account | `TestAccountResolver_ReportsAccountToAuthMiddleware` | PASS (unit only) |
| AC-7 | Sign-in link and the code both reach the user | `TestDeviceCodeElicitMessage_QuotesTheCode`, `TestAuthMiddleware_DeviceCodeAuth_URLElicitation` | PASS |

## Disproved assumptions

All three were disproved on 2026-09-02 against a live Microsoft 365 account. Each had already been implemented when it was disproved.

### 1. `auth_code` as the inferred default — WITHDRAWN

The original A2. `auth_code` uses the `nativeclient` redirect URI, which the Microsoft Office app registration does register (CR-0030), and it returns a value the user can paste back inside the conversation — so an assistant could in principle drive it end to end.

Microsoft has closed that pattern. Driving a real sign-in, the redirect page renders:

> "This page is not normally shown and could be a sign of a phishing attempt. The URL contains your password. Close this page immediately and do not copy or share the URL with anyone."

then:

> "This is not the right page. You have reached the wrong page. Please close this app or window and try again."

The flow does not complete. This is not a transient bug: copying an authorization code out of the address bar is behaviourally identical to the phishing technique the interstitial exists to prevent. A default cannot instruct users to do what the platform is warning them not to do.

**Disposition: WITHDRAWN, not failed.** `auth_code` remains fully implemented and selectable via `OUTLOOK_MCP_AUTH_METHOD=auth_code`. Only the inference reverted. CR-0030's design was sound when written; the platform moved.

### 2. `browser` as an alternative default — still dead, now first-hand

Reopened rather than taken on trust from CR-0030. A real sign-in gives:

```
AADSTS50011: The redirect URI 'http://localhost:65053' specified in the request does not
match the redirect URIs configured for the application 'd3590ed6-52b3-4102-aeff-aad2292ab01c'.
```

`InteractiveBrowserCredential` binds a random localhost port; every one of them is unregistered. `browser` remains correct for custom app registrations, unchanged.

**Net: `device_code` is the only flow that completes against the shipped client ID.**

### 3. `?otc=` pre-fills the device code — false, and it hid a defect

Verified: opening `https://login.microsoft.com/device?otc=GFXCFLG2A` redirects to `https://login.microsoftonline.com/common/oauth2/deviceauth?otc=GFXCFLG2A`. The parameter **is preserved** through the redirect chain, not stripped. The page nonetheless renders "Enter code to allow access" with the **Code field empty**. There is no pre-fill. The link removes the navigation step only.

**The second-order defect is the instructive part.** Because pre-fill was assumed, the code was treated as redundant and omitted from the URL-mode elicitation message. URL-mode elicitation shows the user a link and a message and nothing else — so on clients that *do* support URL elicitation, the code would never have reached the user at all. They would have landed on the correct page with an empty field and no code anywhere on screen.

No unit test could have caught this: every assertion was consistent with the false premise. It was only visible by looking at what a human would actually see. Fixed by making the message quote the code, with `TestDeviceCodeElicitMessage_QuotesTheCode` guarding both the presence of the code and the absence of any pre-fill claim.

## A4 root cause: azidentity's shared, non-context-aware credential mutex

Recorded in full because it is non-obvious, it defeated the first implementation of A4, and it will bite anyone who adds work to a recovery path.

`azidentity`'s `publicClient.client()` returns a shared `*sync.Mutex` (`caeMu` or `noCAEMu`), and **both `Authenticate` and `GetToken` take it** (`public_client.go`). An interactive `Authenticate` therefore holds it for the entire sign-in — up to `backgroundAuthTimeout` (300s). It is a plain `sync.Mutex`, so **it is not context-aware**: passing a short context to a Graph call does not bound the wait. Request timeouts are simply ignored.

Consequence: exempting the `account` verbs from the middleware gate let them reach their handlers, where they then blocked on the credential. The live symptom was `account.list` timing out past 60s with no response at all — worse than the pre-A4 behaviour, which at least returned a message.

Per-verb, reproduced against the real `AuthMiddleware`:

| Verb | Behaviour while a sign-in is pending | Mechanism |
|---|---|---|
| `account.list` | hung | `EnsureEmail` → Graph `/me` → `GetToken` on the shared credential |
| `account.refresh` | hung | `entry.Credential.GetToken` on the shared credential |
| `account.login` | fine | `setupCredential` builds a *new* credential with its own mutex |

**Verified by instrumentation:** that the exemption matches (`Params.Name == "account"`, `isRecoveryOperation() == true`), that the inner handler runs (`innerRan=true`), that the hang is downstream, and all three per-verb outcomes.

**Read from source, not executed:** that the *real* azidentity mutex behaves as the test double does. `p.client()` returning a shared `*sync.Mutex` taken by both methods is unambiguous in `public_client.go`, and it predicts the live symptom exactly, but it has not been executed against a real credential mid-`Authenticate`.

**Fix:** bounding is unavailable — abandoning a goroutine blocked on a non-context-aware mutex leaves it to write `entry.Email` later, racing every reader. So the call is not made while the lock is held. `internal/auth/inflight.go` counts running interactive flows; `EnsureEmail` skips (cosmetic, resolves next call) and `account.refresh` declines with an explanation (a refresh is redundant while the flow about to mint a token is still running).

**Not done:** removing `authMW` from the account verbs entirely. Arguably the cleaner long-term shape, but it would **not** have fixed this — the hang is past the middleware — and would drop auth-error detection for no benefit. Recorded as a follow-up.

## Methodology cautions

Two process failures worth carrying forward, because both produced confident wrong answers that shaped design.

### An authorize-endpoint page render does not validate `redirect_uri`

While reopening the `browser` option, a `curl` probe against `/authorize` with `redirect_uri=http://localhost` and `redirect_uri=http://localhost:12345` returned a rendered Microsoft login page in both cases and no error. This was read as "the redirect URIs are accepted". **It was wrong.** Entra ID defers `redirect_uri` validation until after authentication; the initial page render says nothing about registration. Only a completed sign-in surfaces `AADSTS50011`.

**Do not use an authorize-endpoint page render to test redirect URI acceptance.** The only reliable test is end-to-end.

### A truncated grep produced a false SDK claim that shaped the design

An early investigation ran `grep -rn "DisableAutomaticAuthentication" <azidentity>/ | head`. The default `head` cap of 10 lines silently truncated the results, hiding the hits in `device_code_credential.go`. The conclusion drawn — that the option exists only on `InteractiveBrowserCredentialOptions` — was false, and it was written into code comments, the CR, and three documentation files as established fact.

The option is declared on **both** `InteractiveBrowserCredentialOptions:44-47` (wired at `:93`) and `DeviceCodeCredentialOptions:45-48` (wired at `:113`). Had the error stood, A1's benefit would have been limited to `auth_code` users only — which, after the A2 revert, would have meant no benefit to anyone by default. The correction is what makes A1 the headline result of this CR.

**Do not pipe an exhaustiveness check through `head`.** A grep whose purpose is "does X exist anywhere" must not be truncated.

## Test Strategy Verification

| Test File | Test Name | Status |
|---|---|---|
| internal/auth/silent_test.go | TestTrySilentToken_NilCredential | PASS |
| internal/auth/silent_test.go | TestTrySilentToken_Success | PASS |
| internal/auth/silent_test.go | TestTrySilentToken_Failure | PASS |
| internal/auth/silent_test.go | TestTrySilentToken_SkipsEscalatingCredential | PASS |
| internal/auth/silent_test.go | TestAuthCodeCredential_ImplementsSilentTokenCredential | PASS |
| internal/auth/silent_test.go | TestSetupCredential_GetTokenIsSilentOnly | PASS |
| internal/auth/silent_test.go | TestSilentOnlyAcquirer_Eligibility | PASS |
| internal/auth/silent_test.go | TestClassifyAuthError_AuthenticationRequiredError | PASS |
| internal/auth/devicecode_prompt_test.go | TestNewDeviceCodePrompt | PASS |
| internal/auth/devicecode_prompt_test.go | TestDeviceCodePrompt_SignInURL | PASS |
| internal/auth/devicecode_prompt_test.go | TestDeviceCodeElicitMessage_QuotesTheCode | PASS |
| internal/auth/recovery_ops_test.go | TestIsRecoveryOperation | PASS |
| internal/auth/recovery_reachable_test.go | TestRecoveryVerbsStayResponsiveDuringAuth | PASS (verified to FAIL against unfixed code) |
| internal/auth/recovery_reachable_test.go | TestEnsureEmail_SkipsDuringInteractiveAuth | PASS |
| internal/auth/recovery_reachable_test.go | TestInteractiveAuthInFlight_Nests | PASS |
| internal/auth/middleware_cr0067_test.go | TestMiddleware_PendingAuth_BlocksOrdinaryVerb | PASS |
| internal/auth/middleware_cr0067_test.go | TestMiddleware_PendingAuth_AllowsAccountVerb | PASS |
| internal/auth/middleware_cr0067_test.go | TestMiddleware_FreshCredential_AllowsAccountVerb | PASS |
| internal/auth/middleware_cr0067_test.go | TestMiddleware_FreshCredential_SilentRefreshSkipsPrompt | PASS |
| internal/auth/middleware_cr0067_test.go | TestHandleAuthError_SilentRefreshRetries | PASS |
| internal/auth/middleware_cr0067_test.go | TestAccountResolver_ReportsAccountToAuthMiddleware | PASS |
| internal/auth/middleware_cr0067_test.go | TestInferAuthMethod_DeviceCodeEntry | PASS |
| internal/auth/middleware_cr0067_test.go | TestPendingOutcome | PASS |
| internal/auth/middleware_cr0067_test.go | TestClassifyAuthError_PreservesDeviceCodeDetail | PASS |
| internal/auth/middleware_cr0067_test.go | TestClassifyAuthError_BareAuthRequired | PASS |
| internal/auth/middleware_cr0067_test.go | TestRecoverySteps_MethodSpecific | PASS |
| internal/tools/refresh_account_test.go | TestRefreshAccount_DeclinesDuringInteractiveAuth | PASS |
| internal/tools/complete_auth_test.go | TestCompleteAuth_NonAuthCodeCredential (3 sub-cases) | PASS |

Modified test files: `internal/auth/middleware_test.go`, `internal/auth/errors_test.go`, `internal/config/config_test.go`, `internal/tools/add_account_test.go`, `internal/tools/complete_auth_test.go`, `cmd/outlook-local-mcp/main_test.go`.

Only one test in this set was verified to fail against the unfixed code (`TestRecoveryVerbsStayResponsiveDuringAuth`). The others were written alongside their implementations and have not been mutation-checked.

## Gate Results

```
make docs-bundle build vet fmt-check tidy test
  ==> docs-bundle OK          (slugs resolve, bundle 45132 bytes / 2 MiB cap, secret lint clean, llms.txt matches)
  go build -o ./outlook-local-mcp ./cmd/outlook-local-mcp/
  go vet ./...                (no output)
  go mod tidy                 (no diff)
  CGO_ENABLED=0 go test -coverprofile=coverage.out ./...
  ok  cmd/outlook-local-mcp          coverage: 21.3%
  ok  docs                           coverage: [no statements]
  ok  extension                      coverage: [no statements]
  ok  internal/audit                 coverage: 91.3%
  ok  internal/auth                  coverage: 79.1%
  ok  internal/config                coverage: 90.7%
  ok  internal/docs                  coverage: 83.3%
  ok  internal/graph                 coverage: 78.6%
  ok  internal/logging               coverage: 91.1%
  ok  internal/observability         coverage: 86.4%
  ok  internal/server                coverage: 91.5%
  ok  internal/tools                 coverage: 71.7%
  ok  internal/tools/help            coverage: 73.6%
  ok  internal/validate              coverage: 100.0%

go test -race ./...
  race packages ok: 14
  no failures, no races

golangci-lint run --timeout 15m
  0 issues.
```

`make ci` was **not** run in full: `lint`, `goreleaser-check` and `mcpb-validate` require `golangci-lint`, `goreleaser` and `mcpb`. Only `golangci-lint` was available, and it was run separately as shown above. `goreleaser-check` and `mcpb-validate` are **unverified** in this environment.

`internal/auth` coverage is 79.1%. (It measured 76.2% at the first CR-0067 commit; the pre-CR baseline on `main` was not captured, so no delta against `main` is claimed.)

## Diff Coverage

Base `5b22fb5` (main), head `504d87a`, 5 commits:

```
1927bdd style: use fmt.Fprintf instead of WriteString(fmt.Sprintf(...)) (QF1012)
ecd6707 feat(auth): silent refresh, auth_code default, and in-band recovery (CR-0067)
b13ca5f revert(auth): keep device_code as the inferred default (CR-0067 A2)
21a9dab fix(auth): device code link does not pre-fill the code (CR-0067 A7)
504d87a fix(auth): stop recovery verbs hanging during a sign-in (CR-0067 A4)
```

39 files, +3196/-522. The A2 trial and its reversal are deliberately preserved as separate commits rather than squashed, so a future reader can see that `auth_code` was attempted and why it was abandoned.

New files (8 production, 4 test):

| File | Purpose |
|---|---|
| `internal/auth/silent.go` | `SilentTokenCredential`, `silentOnlyAcquirer`, `TrySilentToken` |
| `internal/auth/pending.go` | `pendingAuthAttempt`, race-free state, `backgroundAuthTimeout` |
| `internal/auth/account_slot.go` | Mutable per-request account slot (A6) |
| `internal/auth/recovery_ops.go` | `isRecoveryOperation` |
| `internal/auth/inflight.go` | Interactive-auth in-flight counter (A4 fix) |
| `internal/auth/devicecode_prompt.go` | `DeviceCodePrompt`, `SignInURL` |
| `internal/auth/devicecode_present.go` | URL-mode presentation, ack→wait→retry, verbatim fallback |
| `internal/tools/complete_auth_guidance.go` | `completeAuthUnavailable` |

`CHANGELOG.md` not edited, per CLAUDE.md. All changed files map to a CR-0067 Affected Component, the Test Strategy, or the documentation set. One out-of-scope one-line fix is included and called out below.

## Gaps

1. ~~**NFR-27 is only half met — `middleware.go` grew.**~~ **Resolved.** At validation time seven cohesive pieces had been split out and `presentDeviceCode` (~40 lines) moved, but the entry-point restructure, the silent-refresh call sites, the in-flight bracketing and their doc comments added more than came out: 804 → 833 lines, +29 net. Reporting that as PASS would have been false, so the mechanical remedy was deferred. It has since been applied: `handleBrowserAuth` and `handleDeviceCodeAuth` now live in `browser_flow.go` and `devicecode_flow.go`, leaving `middleware.go` at **637 lines, 167 below its original size**. The move was verified to be pure — all 178 lines that left `middleware.go` reappear in the new files and none were added — with the full gate, `go test -race ./...` and `golangci-lint` (0 issues) re-run after it.
2. **A5 guidance is unit-tested only.** The wording is asserted, but no live session has confirmed that an LLM handed the new guidance actually recovers. Cheap to check during the next real re-auth.
3. **A6 is unit-tested only.** Verifying that re-auth targets the correct account requires a **second** Microsoft account. The unit test asserts the account credential is used and the closure credential is not, which is the whole mechanism, but the wiring has never run live.
4. **URL-mode elicitation `Accept` path is unit-tested only.** No client is known to answer URL-mode elicitation with `Accept`. If none does in practice, A7's ack→wait→retry branch is dead code in the field and the plain-text fallback is the entire user experience — which is why FR-22 specifies that fallback as the primary surface rather than a degraded one.

## Notes

- **One out-of-scope fix folded in:** `docs/deploy/configmap.yaml:18` carried the comment `# auth_code (default) ...`, which was wrong before this CR (main's default was already `device_code`) and wrong after. Corrected in its own commit. Verified pre-existing via an empty `git diff 5b22fb5 -- docs/deploy/configmap.yaml`.
- **Follow-up recorded, not done:** remove `authMW` from the account verbs entirely so they behave like `system.status`. See the A4 section for why it is not this CR's fix.
- **Pre-existing issue, unrelated to this CR:** `handleBrowserAuth` previously ran on an unbounded `context.Background()`. Bounded here at 300s alongside the device-code path, since the fix was one line and the failure mode identical.
- **Behaviour change for existing `browser` and `device_code` users, no re-auth required:** with `DisableAutomaticAuthentication` set, an expired token now yields a coordinated middleware prompt instead of the Graph SDK spontaneously opening a browser or emitting a device code mid-request. This is the CR-0022 architecture applied consistently.
- **Migration cost: none.** The inferred default is unchanged, so no installation is prompted to re-authenticate on upgrade. This is a strict improvement on the withdrawn A2 revision, which would have forced one interactive sign-in per account.
