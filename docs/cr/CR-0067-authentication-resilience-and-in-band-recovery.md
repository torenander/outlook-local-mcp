---
name: authentication-resilience-and-recovery
description: Remove the authentication friction that repeatedly interrupts LLM sessions by trying silent token refresh before any interactive flow, keeping the account and complete_auth recovery verbs reachable at all times, correcting the recovery guidance the middleware emits, targeting re-authentication at the account the call actually used, and presenting device codes as one-click links. device_code remains the inferred default; auth_code was trialled as the default and rejected on live evidence.
id: "CR-0067"
status: "proposed"
date: 2026-08-31
requestor: desek
stakeholders: desek, LLM consumers of the MCP server, headless and unattended deployments
priority: "high"
target-version: v0.8.0
source-branch: feat/cr-0067-auth-resilience
source-commit: 5b22fb5
---

# Authentication resilience and recovery

## Change Summary

Authentication in `outlook-local-mcp` prompts far more often than it needs to, and when it does go wrong the server hides the very verbs that would repair it. This CR fixes that chain: the middleware attempts a silent token refresh before starting any user-visible flow, and the credentials are reconfigured so that such a refresh is possible at all; `system.complete_auth` is always registered; the `account` verbs stay callable while an authentication flow is pending; the recovery guidance the server emits names verbs that actually re-authenticate an existing account; re-authentication targets the account the failing tool call resolved to instead of the server default; and the device code is presented as a one-click link with the code already filled in.

**The inferred default remains `device_code`.** An earlier revision of this CR changed it to `auth_code`. Live testing killed that premise — Microsoft now blocks the copy-the-URL-from-the-address-bar pattern with an anti-phishing interstitial — and it has been reverted. The same testing also re-confirmed, first-hand, that `browser` fails against the shipped client ID with `AADSTS50011`. `device_code` is the only flow that completes. Both findings are recorded under [Rejected alternatives](#rejected-alternatives), because the next person to look at this will have the same idea we did.

That makes the rest of the CR more important, not less. If the user must approve a device code, then every prompt we can avoid is worth avoiding, and every prompt we cannot avoid should be one click rather than a transcription exercise. That is A1 and A7 respectively.

> **Migration cost: none.** Because the inferred default is unchanged, no existing installation is asked to re-authenticate. Tokens stay where they are. This is a change in the CR's risk profile from its earlier revision, which would have forced one interactive sign-in per account. See [Migration cost](#migration-cost).

## Motivation and Background

The friction is not one bug; it is a set of defects that compound. Each was verified against `main` at commit `5b22fb5`.

**1. The shipped default needs a human at least once, and there is no way around it.** `config.InferAuthMethod` returns `device_code` for any client ID in `WellKnownClientIDs`, which includes the default `outlook-desktop` = `d3590ed6-52b3-4102-aeff-aad2292ab01c` (Microsoft Office). Device code requires a person to approve a code on a separate page, so an unattended session stalls there.

The obvious response is to change the default. Both alternatives were tested live against that client ID and neither works: `browser` is rejected with `AADSTS50011`, and `auth_code` is blocked by a Microsoft anti-phishing interstitial (see [Rejected alternatives](#rejected-alternatives)). The default therefore stands, and the only available response is to make everything around it cost less — never prompt when the cache would have served (A1), and make the unavoidable prompt one click rather than a transcription exercise (A7).

**2. The in-band alternative is unregistered exactly when it is needed.** `system.complete_auth` is only added to the system verb slice when `cfg.AuthMethod == "auth_code"` (`internal/server/system_verbs.go:212`). Under the default configuration the verb does not exist, so an assistant that has been told "finish the sign-in" discovers there is no verb to call. The per-account `auth_method` can also differ from the server default, so even a correctly configured server can be missing the verb for one of its accounts.

**3. The middleware never asks the cache before asking the user.** `handleAuthError` dispatches straight to an interactive flow. `azidentity`'s `Authenticate()` — the only method the middleware calls — goes directly to `reqToken` with no `AcquireTokenSilent` first (`azidentity@v1.13.1 public_client.go:110-133`). An expired *access* token backed by a perfectly good refresh credential therefore produces a full interactive prompt. Compounding this, the credentials are constructed without `DisableAutomaticAuthentication`, so `GetToken` — which the Graph SDK's bearer-token policy calls on every request — can itself escalate to a browser window or a device code mid-tool-call.

**4. Pending authentication freezes the recovery path.** While `pendingAuth` is set, every `authMW`-wrapped verb returns "Authentication is still in progress" — including `account.login`, `account.list` and `account.refresh`. The device code background goroutine runs on a `context.Background()` with no deadline (`middleware.go:555`), so an abandoned sign-in leaves the flag set for the lifetime of the process. The reads of `pendingDone`/`pendingErr` at middleware entry are also unsynchronised against the writes made elsewhere: a genuine data race.

**5. The recovery guidance is wrong.** `FormatAuthError` tells the LLM to "call account_list, then account_add". `account.add` registers a *new* account; following the instruction produces duplicate registry entries rather than a working session. The correct verb is `account.login`. Separately, `classifyAuthError` tests for the bare string `"authentication required"` before the generic branch, so a specific message such as `"authentication required: device code prompt was not received from Entra ID"` degrades to the contentless "Authentication is required for this account."

**6. Per-account re-authentication targets the wrong account.** `handleAuthError` reads `AccountAuthFromContext`, but `authMW` wraps *outside* `accountResolverMW` (`internal/server/mail_verbs.go:99-101`). The resolver injects the account into a context it derives inside the call, which can never travel back out to the middleware. The lookup therefore always misses and re-authentication always uses the default closure credential, whichever account the tool call targeted. `account_resolver.go:396` compounds this by reporting `"browser"` for every credential that is not an `AuthCodeFlow`, including `DeviceCodeCredential`.

Individually each is survivable. Together they mean: the default install starts a flow the agent cannot finish, does not offer the verb that would finish it, will not let the agent inspect or repair accounts while that flow is outstanding, tells it to run a verb that makes things worse, and — once it does re-authenticate — signs in the wrong account.

## Change Drivers

* Interactive sessions are interrupted by full sign-in prompts that a silent refresh would have avoided — the largest source of friction, and entirely fixable.
* The device code prompt, which cannot be eliminated, is presented as a code to transcribe rather than a link to click.
* The self-repair surface (`account.*`, `system.complete_auth`) is unavailable precisely when authentication is broken.
* Recovery guidance that names the wrong verb actively degrades the registry it is meant to repair.
* A data race in the pending-auth bookkeeping that `go test -race` can surface.
* Multi-account correctness: re-authenticating the default account when a named account failed is silently wrong.

## Current State

| Concern | Behaviour on `main` @ `5b22fb5` | Location |
|---|---|---|
| Inferred method for well-known client IDs | `device_code` (unchanged by this CR) | `internal/config/config.go:344` |
| `GetToken` on a cache miss | Escalates: opens a browser or emits a device code, outside middleware control | `internal/auth/auth.go` (credential options) |
| `system.complete_auth` registration | Only when `cfg.AuthMethod == "auth_code"` | `internal/server/system_verbs.go:212` |
| Silent refresh before interactive | None | `internal/auth/middleware.go:210`, `:268` |
| Verbs reachable during pending auth | None | `internal/auth/middleware.go:189-204` |
| Device code background deadline | None (`context.Background()`) | `internal/auth/middleware.go:555` |
| `pendingDone` / `pendingErr` synchronisation | Written under `mu`, read without it | `internal/auth/middleware.go:189-204` |
| Recovery guidance | "call account_list, then account_add" | `internal/auth/errors.go:91-97` |
| Auth-required classification | Bare marker checked before the detailed branch | `internal/auth/errors.go:128-135` |
| Per-account re-auth | Always the default closure credential | `internal/auth/middleware.go:289` |
| Resolver method inference | `"browser"` for everything but `AuthCodeFlow` | `internal/auth/account_resolver.go:396` |
| Device code presentation | Form elicitation with an `acknowledged` checkbox; returns the prompt text either way | `internal/auth/middleware.go:619` |

### Current State Diagram

```mermaid
flowchart TD
    A[tool call] --> B{pendingAuth?}
    B -->|yes| C[still in progress -- ALL verbs]
    B -->|no| D{credential warm?}
    D -->|no| E[handleAuthError]
    D -->|yes| F[run handler]
    F --> G{auth error?}
    G -->|yes| E
    G -->|no| H[result]
    E --> I[AccountAuthFromContext -- always misses]
    I --> J[interactive flow on DEFAULT credential]
    J --> K{method}
    K -->|device_code| L[device code, no deadline]
    L --> M[user must retype code elsewhere]
    E --> N[FormatAuthError: call account_add]
    N --> O[duplicate account created]
```

## Proposed Change

Seven changes, labelled A1-A7, implemented together because each removes one link of the same chain.

| ID | Change |
|---|---|
| A1 | Construct every azidentity credential with `DisableAutomaticAuthentication: true` so `GetToken` is silent-only, then attempt a bounded silent token acquisition before any interactive flow, on both the fresh-credential fast path and the auth-error path, for all three methods. |
| A2 | *(withdrawn)* Changing the inferred default to `auth_code`. Implemented, tested live, reverted — see [Rejected alternatives](#rejected-alternatives). The identifier is retained so the labels here match the branch history. |
| A3 | Register `system.complete_auth` unconditionally; when the target account is not on `auth_code`, return the recovery path that does apply. |
| A4 | Exempt the `account` domain from the pending-auth freeze and the fresh-credential fast path; bound the device code background context at 300s; make the pending-auth bookkeeping race free. |
| A5 | Rewrite the recovery guidance to be correct and method-aware; reorder `classifyAuthError` so specific detail survives. |
| A6 | Hand the resolved account back to the middleware through a mutable context slot; make the resolver's method inference honour the persisted `auth_method` and recognise `DeviceCodeCredential`. |
| A7 | Present device codes as URL-mode elicitations pointing at `https://microsoft.com/devicelogin?otc=<UserCode>`; on acknowledgement, wait for the flow and retry the original call; keep the plain-text fallback verbatim. |

### Proposed State Diagram

```mermaid
flowchart TD
    A[tool call] --> B[install account slot]
    B --> C{pendingAuth?}
    C -->|running, account verb| H[run handler]
    C -->|running, other verb| D[still in progress]
    C -->|none / finished| E{credential warm?}
    E -->|no, account verb| H
    E -->|no| F[TrySilentToken]
    F -->|success| H
    F -->|failure| I[handleAuthError]
    E -->|yes| H
    H --> J{auth error?}
    J -->|no| K[result]
    J -->|yes| I
    I --> L[read account from slot]
    L --> M[TrySilentToken on that account]
    M -->|success| N[retry original call]
    M -->|failure| O{method}
    O -->|auth_code| P[browser + elicit redirect URL, or system.complete_auth]
    O -->|browser| Q[browser, 120s]
    O -->|device_code, the default| R[one-click otc link, 300s bound]
    R --> S[on ack: wait, then retry]
```

## Requirements

### Functional Requirements

1. The middleware **MUST** attempt a non-interactive token acquisition before dispatching to any interactive authentication flow, on both the fresh-credential fast path and inside `handleAuthError`.
2. The silent attempt **MUST** be bounded by a short timeout (5 seconds, matching `silentLoginTimeout` in `internal/tools/login_account.go`).
3. `setupBrowserCredential` and `setupDeviceCodeCredential` **MUST** set `DisableAutomaticAuthentication: true` so that `GetToken` returns `azidentity.AuthenticationRequiredError` on a cache miss rather than escalating.
3a. The silent attempt **MUST NOT** be made against credentials whose `GetToken` can escalate to an interactive flow. Eligibility **MUST** be decided by an explicit allowlist that fails safe for unrecognised credential types, not inferred from an auth-method string.
3b. The deliberate interactive flows **MUST** continue to work; `Authenticate()` bypasses `DisableAutomaticAuthentication` and **MUST** be verified to do so.
3c. `IsAuthError` **MUST** classify `azidentity.AuthenticationRequiredError` as an authentication error so it routes into the middleware, and `classifyAuthError` **MUST NOT** surface its SDK-level "Call Authenticate" advice to the LLM.
3d. `probeStartupToken` **MUST** run for every auth method including `device_code`, so `preAuthenticated` reflects a real token check rather than the presence of an auth record file.
4. On a successful silent acquisition the middleware **MUST** mark itself authenticated and retry the original tool call without prompting.
5. `config.InferAuthMethod` **MUST** continue to return `("device_code", "inferred")` for client IDs present in `WellKnownClientIDs`. Its doc comment **MUST** record why the two alternatives were rejected, so the decision is not silently re-litigated.
6. An explicit `OUTLOOK_MCP_AUTH_METHOD` **MUST** continue to win with source `explicit`, including the value `auth_code`, which remains fully supported for tenants where it works.
7. Custom (non-well-known) client IDs **MUST** continue to return `("browser", "default")`.
8. `system.complete_auth` **MUST** be registered regardless of the active authentication method.
9. When `system.complete_auth` is invoked against a credential that does not implement `AuthCodeFlow`, the handler **MUST** return a message naming the recovery path for the method that account actually uses, and **MUST NOT** report an internal type error.
10. While a background authentication flow is pending, calls to the `account` aggregate tool **MUST** be passed through to their handlers rather than answered with the pending message.
11. On the fresh-credential fast path, calls to the `account` aggregate tool **MUST** be passed through to their handlers rather than diverted into an authentication flow.
12. The device code background authentication context **MUST** carry a 300-second deadline so the pending flag clears without operator intervention.
13. The pending-authentication completion channel and error **MUST** be published such that concurrent readers observe them without a data race.
14. `FormatAuthError` **MUST NOT** instruct the caller to add a new account as the primary recovery step. The guidance **MUST** name `account.login` for re-authenticating an existing account.
15. A method-aware variant **MUST** exist so that `auth_code` guidance additionally names `system.complete_auth`.
16. `classifyAuthError` **MUST** preserve explanatory detail that follows an `authentication required` marker instead of collapsing it to the bare message.
17. `AuthMiddleware` **MUST** install a mutable slot into the request context before invoking the handler chain, and `AccountResolver` **MUST** record the resolved `AccountAuth` in that slot.
18. `handleAuthError` **MUST** prefer the slot value over the closure credential when re-authenticating.
19. `auth.inferAuthMethod(entry)` **MUST** honour a persisted `AccountEntry.AuthMethod` and, when it is empty, **MUST** distinguish `*azidentity.DeviceCodeCredential` from the browser case.
20. `presentDeviceCode` **MUST** use URL-mode elicitation with a link of the form `https://microsoft.com/devicelogin?otc=<UserCode>`.
21. The structured device code message (`UserCode`, `VerificationURL`, `Message`) **MUST** be forwarded through `DeviceCodeMsgKey`, not just the rendered sentence.
22. When elicitation is unavailable or fails, the tool result text **MUST** reproduce the Entra ID device code message verbatim, preserving the CR-0031 fallback contract. It **MAY** append the one-click link below that message, and **MUST NOT** reword or omit the message itself. Because `device_code` is the inferred default and many clients cannot render elicitations, this text is the primary sign-in surface and is specified as such rather than as a degraded path.
23. After a successful elicitation acknowledgement, `presentDeviceCode` **MUST** wait (bounded) for the background flow to finish and then retry the original tool call.

### Non-Functional Requirements

24. No new top-level MCP tool is introduced; the four-domain surface from CR-0060 is unchanged.
25. The aggregate MCP annotations for the `system` tool **MUST** remain the most conservative across its verbs. (They already assume `complete_auth` is present, so they do not change.)
26. `go test -race ./...` **MUST** pass.
27. Files added to `internal/auth` **MUST** stay small and single-purpose per AGENTS.md; `middleware.go` **MUST NOT** grow.

## Migration cost

**None.** The inferred default is unchanged, so no installation is asked to re-authenticate and every cached token keeps working.

This is a deliberate improvement over the earlier revision of this CR, which changed the default to `auth_code` and therefore *would* have forced one interactive sign-in per account. That cost existed because the two flows use different token stores:

| Method | Credential | Token store |
|---|---|---|
| `device_code`, `browser` | `azidentity.DeviceCodeCredential` / `InteractiveBrowserCredential` | `azidentity/cache` entry named `cfg.CacheName` (OS keychain / libsecret / DPAPI, or the encrypted file backend) |
| `auth_code` | `auth.AuthCodeCredential` (MSAL Go `public.Client`) | its own MSAL cache blob, `{cfg.CacheName}_msal.bin`, via `InitMSALCache` |

Those blobs are separate and not interchangeable, so switching a running installation between the two groups costs one sign-in. That remains true for anyone who sets `OUTLOOK_MCP_AUTH_METHOD=auth_code` by hand, and is worth knowing — but this CR does not do it to anybody.

Behaviour that *does* change for existing users, without requiring re-authentication:

* `GetToken` no longer escalates. An expired token now produces a coordinated middleware prompt instead of a browser window or device code appearing mid-request. See Risk 3a.
* The startup probe now runs for `device_code`, so `preAuthenticated` reflects a real token check.
* Device codes are presented as a one-click link where the client supports it, and the plain-text fallback gains the same link below the unchanged Entra message.

## Affected Components

| Component | Change |
|---|---|
| `internal/config/config.go` | `InferAuthMethod` returns `auth_code` for well-known client IDs; doc comments updated. |
| `internal/auth/silent.go` *(new)* | `SilentTokenCredential` capability interface, `silentOnlyAcquirer` eligibility allowlist, and `TrySilentToken`. |
| `internal/auth/pending.go` *(new)* | `pendingAuthAttempt` and the race-free state helpers `begin`, `settle`, `pendingOutcome`. |
| `internal/auth/account_slot.go` *(new)* | Mutable per-request `accountAuthSlot` and `resolvedAccountAuth`. |
| `internal/auth/recovery_ops.go` *(new)* | `isRecoveryOperation`. |
| `internal/auth/devicecode_prompt.go` *(new)* | `DeviceCodePrompt` and `OneClickURL`. |
| `internal/auth/devicecode_present.go` *(new)* | URL-mode presentation, acknowledgement wait, verbatim fallback. |
| `internal/auth/middleware.go` | Entry-point restructure, silent attempt, slot install, pending rework, background deadlines on both interactive flows; `presentDeviceCode` moved out. |
| `cmd/outlook-local-mcp/main.go` | `probeStartupToken` no longer special-cases `device_code`; the `authRecordPath` parameter is dropped. |
| `internal/auth/errors.go` | `FormatAuthErrorFor`, `recoverySteps`, `authRequiredDetail`, an `AuthenticationRequiredError` branch, reordered classification. |
| `internal/auth/authcode.go` | `SilentOnly` marker. |
| `internal/auth/auth.go` | `DisableAutomaticAuthentication: true` on both azidentity credentials; `DeviceCodeMsgKey` channel element type becomes `DeviceCodePrompt`. |
| `internal/auth/account_resolver.go` | Slot hand-back; `inferAuthMethod` honours persisted method and recognises `DeviceCodeCredential`. |
| `internal/server/system_verbs.go` | Unconditional `complete_auth` registration; verb reference updated. |
| `internal/tools/complete_auth.go` | Accepts the default auth method; method-aware unavailability message. |
| `internal/tools/complete_auth_guidance.go` *(new)* | `completeAuthUnavailable`. |
| `internal/tools/add_account.go` | Device code channel element type. |
| `extension/manifest.json` | `system` tool description no longer claims `complete_auth` is conditional. |
| `docs/concepts.md`, `docs/troubleshooting.md`, `docs/reference/auth-flows.md` | Narrative, failure modes and contributor reference brought in line. |

## Scope Boundaries

### In Scope

* A1 and A3-A7 as listed above. A2 was implemented and then withdrawn; the revert is part of this change set.
* Test updates for every behaviour changed, plus new tests for each new behaviour.
* The three documentation files named above and this CR.

### Out of Scope ("Here, But Not Further")

* **Migrating tokens between the azidentity cache and the MSAL blob.** Rejected; see Migration cost.
* **Silent refresh on the fresh-credential fast path for a named account.** That path never invokes the handler chain, so `AccountResolver` never runs and no account is available to target. The default credential is used. Fixing it would require moving account resolution outside the auth middleware, which is a larger restructuring.
* Changing the four-domain tool surface, adding verbs, or altering read/write gating.

## Rejected alternatives

Two of these were not rejected on paper — they were implemented, driven against a real Microsoft account, and abandoned on the evidence. Both are recorded in full because both look correct until you try them.

### Making `auth_code` the inferred default — TRIED, REVERTED (2026-09-02)

This was the original A2, and it shipped on this branch before being reverted. The reasoning was sound: `auth_code` uses the `nativeclient` redirect URI, which the Microsoft Office app registration *does* register (CR-0030), and it returns a value the user can paste back inside the conversation, so an assistant could drive it end to end.

Microsoft has since closed that pattern. Driving a real sign-in through the flow this branch produced, the `nativeclient` redirect page now renders an anti-phishing interstitial:

> "This page is not normally shown and could be a sign of a phishing attempt. The URL contains your password. Close this page immediately and do not copy or share the URL with anyone."

followed by:

> "This is not the right page. You have reached the wrong page. Please close this app or window and try again."

The flow did not complete. This is not a transient bug to wait out: copying an authorization code out of the browser address bar is *behaviourally identical* to the phishing technique Microsoft is hardening against, and the interstitial exists to stop users doing it. A default that instructs the user to do precisely what the platform is warning them not to do is indefensible — it trains people to ignore a security warning, and it does not even work.

CR-0030's design was correct when written. The platform moved underneath it.

`auth_code` is **not** removed. It remains fully implemented and selectable with `OUTLOOK_MCP_AUTH_METHOD=auth_code`, for tenants or future platform states where it completes. It is simply no longer inferred.

### Making `browser` the inferred default — RE-TESTED, STILL DEAD (2026-09-02)

CR-0030 rejected `browser` because the Microsoft Office app registration has no `http://localhost` redirect URI. With `auth_code` newly unavailable, this was reopened rather than taken on trust, and the rejection is now backed by first-hand evidence rather than a citation:

```
AADSTS50011: The redirect URI 'http://localhost:65053' specified in the request does not
match the redirect URIs configured for the application 'd3590ed6-52b3-4102-aeff-aad2292ab01c'.
```

`InteractiveBrowserCredential` binds a random localhost port, so the specific port varies, but every one of them is unregistered. `browser` remains correct for **custom** app registrations, which normally do register a localhost redirect URI — that behaviour is unchanged.

#### Methodology caution: the authorize endpoint does not validate `redirect_uri` up front

Worth recording, because it produced a false negative during this investigation and will do so again.

An initial probe issued `curl` requests to the `/authorize` endpoint with `redirect_uri=http://localhost` and `redirect_uri=http://localhost:12345`. Both returned a rendered Microsoft login page and no error, which was read as "the redirect URIs are accepted". **That conclusion was wrong.** Entra ID defers `redirect_uri` validation until after authentication; the initial page render says nothing about whether the URI is registered. Only completing a real sign-in surfaces `AADSTS50011`.

**Do not use an authorize-endpoint page render to test redirect URI acceptance.** The only reliable test is an end-to-end sign-in.

### Keeping `device_code` as the default and doing nothing else

Rejected, and the reason this CR exists. Device code is the only flow that completes, but the surrounding experience was needlessly bad: a prompt on every expired access token even when the refresh credential was live, a session frozen against its own recovery verbs while a prompt was outstanding, guidance that told the LLM to create a duplicate account, and a code to be transcribed by hand. Those are all fixable without touching the flow itself, and A1 and A7 in particular are what make an unavoidable device code cheap rather than painful.

### Probing credentials with `GetToken` without disabling automatic authentication

Unsafe as written, and the reason A1 grew a construction-site change. `azidentity`'s `publicClient.GetToken` tries `AcquireTokenSilent` and then falls through to `reqToken` when the cache misses (`azidentity@v1.13.1 public_client.go:135-156`). For `InteractiveBrowserCredential` that opens a browser window; for `DeviceCodeCredential` it emits a fresh device code. With a 5-second timeout the call is then abandoned, leaving an orphaned browser tab or an unusable code — on **every** tool call. The Graph SDK's bearer-token policy calls `GetToken` on every request, so the same hazard exists outside the middleware entirely.

`DisableAutomaticAuthentication` suppresses exactly this: with it set, `GetToken` returns `AuthenticationRequiredError` instead of calling `reqToken`. The option is declared on **both** `InteractiveBrowserCredentialOptions` (`interactive_browser_credential.go:44-47`) and `DeviceCodeCredentialOptions` (`device_code_credential.go:45-48`), and both wire it into `publicClientOptions` (`:93` and `:113` respectively). This CR therefore sets it on both credentials rather than excluding them from the probe.

*(An earlier draft of this CR asserted that the option existed only on `InteractiveBrowserCredentialOptions`. That was wrong — the claim came from a truncated grep — and it would have limited A1's benefit to `auth_code` users only. The verification below is the corrected reading.)*

The probe is still gated rather than universal, because the guarantee is a property of how a credential was constructed, not of its type. `silentOnlyAcquirer` allows `*AuthCodeCredential` (which declares `SilentOnly()` natively, being silent-only by construction) plus the two azidentity types this package builds with the flag, and skips anything else. A future credential added without the flag is therefore not probed, rather than silently prompting.

### Wrapping the azidentity credentials to carry a `SilentOnly()` marker

The type-system-pure alternative to the allowlist: wrap each azidentity credential in a local type that adds the marker method. Rejected because the wrapper would become the credential every consumer sees — `AccountEntry.Credential`, `AccountAuth.Authenticator`, the Graph client constructor, and the three existing tests that assert `SetupCredential` returns the concrete azidentity types — forcing an unwrap step throughout for one bit of information. The allowlist keeps the credential transparent and pins the invariant behaviourally instead: `TestSetupCredential_GetTokenIsSilentOnly` asserts that `GetToken` returns `AuthenticationRequiredError`, which azidentity produces *only* on the `DisableAutomaticAuthentication` branch. Removing the flag fails that test.

## Impact Assessment

### User Impact

Positive and free: fewer prompts, and the prompts that remain are a link rather than a code to copy. No re-authentication, no configuration change, no behavioural surprise on upgrade. The one thing users do *not* get is unattended first sign-in, which the platform does not currently permit for this client ID.

### Technical Impact

Removes a data race. Removes two unbounded background contexts. Corrects a multi-account bug that silently re-authenticated the wrong account. Stops the Graph SDK escalating to interactive auth outside middleware control. Adds six small files to `internal/auth` and one to `internal/tools`; `middleware.go` shrinks.

### Business Impact

Reduces the interruption rate of the dominant authentication path, and records the platform constraints that bound it so the same dead ends are not explored again.

## Implementation Approach

### A1: Silent refresh before any interactive flow

Set `DisableAutomaticAuthentication: true` in `setupBrowserCredential` and `setupDeviceCodeCredential`, making `GetToken` silent-only for all three methods. Add `internal/auth/silent.go` with the `SilentTokenCredential` opt-in interface, the `silentOnlyAcquirer` eligibility allowlist, and `TrySilentToken`. Mark `*AuthCodeCredential` with `SilentOnly()`. Call `TrySilentToken` on the fresh-credential fast path and at the top of `handleAuthError` after the account is resolved. Add a `classifyAuthError` branch for `azidentity.AuthenticationRequiredError`, and drop the `device_code` special case from `probeStartupToken` in `cmd/outlook-local-mcp/main.go`.

Verified consequences of the flag:

* `Authenticate()` is unaffected — it calls `reqToken` directly and never reads the option (`public_client.go:110-133`, and the comment at `:159` that `reqToken` is "separate from GetToken() to enable Authenticate() to bypass the cache"). The deliberate interactive flows still prompt.
* The resulting `AuthenticationRequiredError` message begins with the credential name — literally `"DeviceCodeCredential"` or `"InteractiveBrowserCredential"` — both of which are already in `authErrorPatterns`, so `IsAuthError` classifies it without change. A regression test pins this.
* The Graph SDK's bearer-token policy now receives that error instead of triggering a prompt mid-request, and the error routes into `AuthMiddleware`, which is the designed prompt path.

### A2: Infer `auth_code` for well-known client IDs — WITHDRAWN

Implemented as a one-line change to `InferAuthMethod` plus test updates, then reverted after live testing (see [Rejected alternatives](#rejected-alternatives)). The net code change is that `InferAuthMethod` still returns `("device_code", "inferred")`; what it gains is a doc comment recording the two rejected alternatives and their evidence, so the next reader does not repeat the experiment blind.

### A3: Always register `system.complete_auth`

Remove the `if` in `buildSystemVerbs`; move `completeAuthVerb` into the base slice. Thread `cfg.AuthMethod` into `HandleCompleteAuth` and replace the "Internal error" branch with `completeAuthUnavailable`. Update the verb `Description`/`Examples`/`SeeDocs` and the manifest.

### A4: Stop freezing the session during pending auth

Add `pending.go` (`pendingAuthAttempt`, `begin`, `settle`, `pendingOutcome`, `backgroundAuthTimeout`) and `recovery_ops.go` (`isRecoveryOperation`). Restructure the middleware entry point. Bound both the device code and the browser background auth contexts at `backgroundAuthTimeout` (300s); both previously ran on an unbounded `context.Background()`.

### A5: Correct the recovery guidance

Add `FormatAuthErrorFor` and `recoverySteps`; keep `FormatAuthError` as the method-agnostic wrapper. Replace the substring test in `classifyAuthError` with `authRequiredDetail`. Pass the resolved method at each middleware call site.

### A6: Per-account re-auth wiring

Add `account_slot.go`. Install the slot in the middleware entry point, fill it in `AccountResolver`, read it via `resolvedAccountAuth` in `handleAuthError`. Rewrite `inferAuthMethod(entry)`.

### A7: One-click device code

Add `devicecode_prompt.go` (`DeviceCodePrompt`, `OneClickURL`) and `devicecode_present.go`. Change the `DeviceCodeMsgKey` channel element type and update every writer and reader, including `internal/tools/add_account.go`.

## Test Strategy

### Tests to Add

* `internal/auth/silent_test.go` — nil credential; successful and failing silent acquisition; **the escalating credential is never probed**; `*AuthCodeCredential` satisfies `SilentTokenCredential`.
* `internal/auth/devicecode_prompt_test.go` — field round trip; `OneClickURL` with and without a user code, with and without a verification URL, with pre-existing query parameters.
* `internal/auth/recovery_ops_test.go` — domain classification.
* `internal/auth/middleware_cr0067_test.go` — pending auth blocks an ordinary verb but allows an `account` verb; a fresh credential allows an `account` verb without starting a flow; a successful silent refresh skips the prompt on both paths; `AccountResolver` hands the account back so re-auth targets it; `inferAuthMethod` per entry shape; `pendingOutcome` transitions; classification preserves device code detail; method-specific recovery steps never mention `operation="add"`.
* `internal/tools/complete_auth_test.go` — table-driven unavailability message per method.
* `internal/auth/silent_test.go` — `TestSetupCredential_GetTokenIsSilentOnly` (both azidentity methods yield `AuthenticationRequiredError`, are recognised by `IsAuthError`, and are probe-eligible); `TestSilentOnlyAcquirer_Eligibility`; `TestClassifyAuthError_AuthenticationRequiredError`.

### Tests to Modify

* `internal/config/config_test.go` — default and well-known inference now expect `auth_code`; an explicit `device_code` case is added.
* `internal/auth/errors_test.go`, `internal/auth/middleware_test.go` — guidance assertions move from `account_list`/`account_add` to `operation="list"`/`operation="login"`.
* `internal/auth/middleware_test.go` — device code channel element type; the former form-elicitation test becomes a URL-elicitation test asserting the `otc` parameter and the retry.
* `internal/tools/add_account_test.go` — device code channel element type.
* `cmd/outlook-local-mcp/main_test.go` — the two `device_code`-skip subtests are replaced by subtests asserting the probe now calls `GetToken` for `device_code`; the `authRecordPath` argument is dropped from all call sites.

### Tests to Remove

None.

## Acceptance Criteria

### AC-1: Silent refresh precedes interactive auth and never escalates

A probe-eligible credential with a warm cache causes the original tool call to be retried with no authentication flow started, on both the fast path and the auth-error path. An unrecognised credential with a `GetToken` method is never called. `SetupCredential` returns credentials whose `GetToken` yields `azidentity.AuthenticationRequiredError` on a cache miss for both `browser` and `device_code`, `IsAuthError` classifies that error as authentication-related, and `classifyAuthError` does not surface its "Call Authenticate" text. `probeStartupToken` calls `GetToken` for `device_code` and marks pre-authenticated only on success.

### AC-2: Default inference is unchanged and its rationale is recorded

`InferAuthMethod("d3590ed6-...", "")` returns `("device_code", "inferred")`. `InferAuthMethod("d3590ed6-...", "auth_code")` returns `("auth_code", "explicit")`. `InferAuthMethod("<custom-uuid>", "")` returns `("browser", "default")`. The `InferAuthMethod` doc comment names both rejected alternatives and the errors that rejected them. No existing installation is prompted to re-authenticate on upgrade.

### AC-3: `complete_auth` always exists and always answers usefully

`system.complete_auth` appears in the registry under every auth method. Called against a `browser` account it names the browser recovery path; against `device_code`, the device login page; with no known method, `system.status` plus `account.login`. The aggregate `system` annotations are unchanged.

### AC-4: Recovery stays reachable and pending state self-clears

With a pending flow, a `calendar` call returns the pending message and an `account` call runs. With a cold credential, an `account` call runs without triggering authentication. The device code background context carries a 300s deadline. `go test -race ./...` is clean.

### AC-5: Guidance is correct and method-aware

No `FormatAuthError` output names `operation="add"` as the primary step. `auth_code` guidance names `system.complete_auth`. `classifyAuthError("authentication required: device code prompt was not received from Entra ID")` retains "device code prompt was not received".

### AC-6: Re-auth targets the resolved account

With one registered account whose authenticator differs from the closure credential, an auth error inside the resolved handler re-authenticates the account credential and not the closure credential.

### AC-7: Device code is one click, with the fallback intact

URL elicitation is called with a URL containing `otc=<UserCode>`. On acceptance the original tool call is retried. On `ErrElicitationNotSupported` the tool result text begins with the Entra ID message reproduced verbatim and carries the same one-click link below it; with no user code available it is exactly the message and nothing else.

## Quality Standards Compliance

### Verification Commands

```bash
make docs-bundle build vet fmt-check tidy test
go test -race ./...
golangci-lint run --timeout 15m
```

### Documentation

Per the AGENTS.md documentation governance rules: per-verb reference for `complete_auth` lives in the `Verb` registry (`Description`, `Examples`, `SeeDocs`), not in markdown. Narrative concepts go to `docs/concepts.md`; failure modes to `docs/troubleshooting.md` with stable anchors; internals to `docs/reference/auth-flows.md`, which is not embedded.

## Risks and Mitigation

### Risk 1: `device_code` remains unattendable

**Not mitigated, and not mitigable at this layer.** With `browser` and `auth_code` both unavailable against the first-party client ID, a fully unattended first sign-in is not possible. What this CR does is ensure it is required as rarely as possible (A1: silent refresh before every prompt) and is as cheap as possible when required (A7: one click, not a transcription). Operators who need true unattended startup must register their own application with a localhost redirect URI and set `OUTLOOK_MCP_CLIENT_ID`, which routes them to `browser` via the existing `default` inference.

### Risk 2: The platform moves again and `auth_code` becomes viable, or `device_code` stops being

**Mitigation:** The inference is one function with a doc comment naming the evidence for each rejected option, and all three flows remain fully implemented and selectable. Revisiting the decision is a one-line change plus a test, not a re-implementation. The methodology caution under Rejected alternatives records how to test it properly.

### Risk 3: The silent refresh adds latency to the auth-error path

**Mitigation:** Bounded at 5 seconds, and only attempted for credentials that cannot escalate. The silent path is a local cache read plus at most one refresh round trip; on a miss with an empty cache it returns immediately without touching the network (measured at ~150µs). The alternative it replaces is a full interactive sign-in.

### Risk 3a: `DisableAutomaticAuthentication` changes Graph SDK behaviour for existing `browser` and `device_code` users

**Mitigation:** The change is a strict improvement in this architecture. Previously the Graph SDK's bearer-token policy could open a browser window or emit a device code in the middle of an unrelated tool call, outside any middleware coordination. Now it returns an error that `IsAuthError` recognises and `AuthMiddleware` turns into a coordinated, user-visible prompt — the designed path since CR-0022. The deliberate interactive flows are untouched because `Authenticate()` bypasses the option. `TestSetupCredential_GetTokenIsSilentOnly` pins the option; the existing browser and device code middleware tests pin the interactive flows.

### Risk 4: Exempting the `account` domain weakens a safety property

**Mitigation:** The exemption is a pass-through, not a privilege escalation. Every `account` verb either reads registry state or drives its own authentication; none of them relies on the middleware having authenticated first. Auth-error detection on their results is unchanged.

### Risk 5: The device code channel type change breaks an unnoticed writer

**Mitigation:** The element type change is compile-visible at every `make(chan ...)` site; the only risk is a `ctx.Value(...).(chan string)` assertion silently returning false. All such assertions in the repository were located and updated, and the device code tests exercise both the middleware and `add_account` paths.

### Risk 6: The fresh-credential fast path still re-authenticates the default account

**Mitigation:** Documented as an explicit out-of-scope limitation rather than silently accepted. It only affects the very first tool call against a completely cold server, where there is no resolved account to prefer anyway.

## Dependencies

* CR-0022 (lazy authentication) — the middleware this CR restructures.
* CR-0024 (browser auth) — the flow whose default status is narrowed to custom client IDs.
* CR-0030 (manual auth code flow) — provides `auth_code` and documents the `AADSTS50011` constraint that rules out `browser` as the default.
* CR-0031 (elicitation fallback) — the verbatim plain-text contract A7 must preserve.
* CR-0060 (aggregate domain tools) — the `{domain}.{operation}` identity that A4 classifies on.
* CR-0065 (documentation architecture) — the placement rules the documentation changes follow.

## Estimated Effort

Medium. Seven coupled changes in one package plus mechanical test updates across two more; no new external dependencies and no schema changes.

## Decision Outcome

Proposed. A2 (inferring `auth_code`) is explicitly withdrawn on live evidence; the remainder stands.

## Implementation Status

Proposed — not yet accepted. A1 and A3-A7 are implemented on `feat/cr-0067-auth-resilience`. A2 was implemented and reverted on the same branch; the history is retained deliberately so the trial and its outcome are visible.

## Related Items

* [CR-0022](CR-0022-improved-authentication-flow.md)
* [CR-0024](CR-0024-interactive-browser-auth-default.md)
* [CR-0030](CR-0030-manual-auth-code-flow.md)
* [CR-0031](CR-0031-elicitation-fallback.md)
* [CR-0060](CR-0060-domain-aggregated-tools-with-verb-operations.md)
* [CR-0065](CR-0065-user-documentation-architecture-and-registry-driven-tool-reference.md)

## More Information

### Why the silent-refresh capability is a marker interface

`azcore.TokenCredential` is already a one-method interface, so it is tempting to depend on it directly. It is the wrong abstraction here: the middleware does not need "something that can produce a token", it needs "something that can produce a token *without interrupting the user*". Those are different contracts and only the second one is safe to call speculatively. Encoding it as an empty `SilentOnly()` marker makes the promise explicit at the type level and makes the unsafe case a compile-time non-participant rather than a runtime string comparison against `authMethod`.

### Evidence for the azidentity escalation behaviour

`azidentity@v1.13.1`, `public_client.go`:

* `Authenticate` (line 110) calls `p.reqToken` directly — no `AcquireTokenSilent`. This is the only method `AuthMiddleware` ever called before this CR.
* `GetToken` (line 135) calls `AcquireTokenSilent` first, returns `newAuthenticationRequiredError` if `DisableAutomaticAuthentication` is set, and otherwise falls through to `p.reqToken` (line 155).
* `reqToken` (line 159) dispatches on credential name: `AcquireTokenInteractive` for `credNameBrowser`, `AcquireTokenByDeviceCode` plus the `DeviceCodePrompt` callback for `credNameDeviceCode`.
* `DisableAutomaticAuthentication` is declared on **both** `InteractiveBrowserCredentialOptions` (`interactive_browser_credential.go:44-47`, wired at `:93`) and `DeviceCodeCredentialOptions` (`device_code_credential.go:45-48`, wired at `:113`). Setting it on both is what makes A1 safe and beneficial for all three authentication methods.
* Empirically, a `DeviceCodeCredential` constructed with the flag and an empty cache returns `AuthenticationRequiredError` in ~150µs with no network call and without invoking the `UserPrompt` callback.
