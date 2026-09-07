// Package auth interactive-authentication in-flight tracking.
//
// This file exists because of a property of the Azure Identity SDK that is
// easy to miss and expensive to rediscover.
//
// azidentity's publicClient guards each MSAL client with a plain sync.Mutex
// (public_client.go: p.client() returns p.caeMu or p.noCAEMu, and both
// Authenticate and GetToken do mu.Lock()/defer mu.Unlock() on it). An
// interactive Authenticate holds that mutex for the entire flow — up to
// backgroundAuthTimeout while the user is signing in. Any concurrent GetToken
// on the same credential blocks on it.
//
// Crucially, sync.Mutex is NOT context-aware. A caller that passes a 3-second
// context to a Graph call does not get a 3-second bound: it waits for the
// mutex however long that takes. Bounding such a call therefore requires
// abandoning a goroutine, which in turn races with whatever that goroutine
// writes.
//
// The cheaper and safer answer is not to make the call at all while a sign-in
// is outstanding. Work that merely enriches a response, or that cannot
// possibly succeed while the credential is busy, consults
// InteractiveAuthInFlight and skips instead of blocking. This keeps the
// account recovery verbs responsive, which is the whole point of exempting
// them from the middleware's pending-auth gate (CR-0067 A4).
package auth

import "sync/atomic"

// interactiveAuthCount is the number of interactive authentication flows
// currently running in this process. It is a counter rather than a flag so
// that concurrent flows on different accounts nest correctly.
var interactiveAuthCount atomic.Int64

// BeginInteractiveAuth records that an interactive authentication flow has
// started. Every call must be paired with exactly one EndInteractiveAuth,
// normally via defer in the goroutine that runs the flow.
//
// Side effects: increments the process-wide in-flight counter.
func BeginInteractiveAuth() {
	interactiveAuthCount.Add(1)
}

// EndInteractiveAuth records that an interactive authentication flow has
// finished, successfully or not.
//
// Side effects: decrements the process-wide in-flight counter.
func EndInteractiveAuth() {
	interactiveAuthCount.Add(-1)
}

// InteractiveAuthInFlight reports whether any interactive authentication flow
// is currently running.
//
// Callers use this to skip credential-touching work that would otherwise block
// on the SDK's non-context-aware per-client mutex. The signal is process-wide
// rather than per-credential: during a sign-in the user is already being
// prompted, so briefly degrading best-effort enrichment for every account is
// an acceptable trade for never hanging a recovery verb.
//
// Returns true while at least one flow is outstanding. No side effects.
func InteractiveAuthInFlight() bool {
	return interactiveAuthCount.Load() > 0
}
