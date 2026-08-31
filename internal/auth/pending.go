// Package auth background authentication bookkeeping.
//
// This file holds the record of an in-flight background authentication
// attempt. It exists as its own file because the synchronisation contract
// below is easy to get wrong: before CR-0067 the middleware stored the
// completion channel and the resulting error as plain struct fields, written
// under a mutex the completing goroutine could not take and read at middleware
// entry with no synchronisation at all — a data race that `go test -race`
// reports.
package auth

import "time"

// backgroundAuthTimeout bounds a background interactive authentication
// goroutine, for both the browser and device code flows. Entra ID device codes
// expire after roughly 15 minutes, but the server does not wait that long: an
// abandoned login must release the pending authentication flag so the session
// is not frozen indefinitely (CR-0067 A4). Before CR-0067 both flows ran on an
// unbounded context.Background(). The value matches the bound used by the
// add_account handler.
const backgroundAuthTimeout = 300 * time.Second

// pendingAuthAttempt records a single background authentication attempt.
//
// The attempt object is immutable once published: the goroutine running the
// attempt assigns err exactly once and then closes done. Because a channel
// close happens-before any receive that observes it, every reader that has
// seen done close is guaranteed to observe the final err without holding a
// lock. Readers must therefore never touch err before done is closed.
type pendingAuthAttempt struct {
	// done is closed when the attempt finishes, successfully or not.
	done chan struct{}

	// err is the outcome of the attempt: nil on success. Valid to read only
	// after done is closed.
	err error
}

// newPendingAuthAttempt allocates an attempt whose done channel is open.
//
// Returns the attempt. No side effects.
func newPendingAuthAttempt() *pendingAuthAttempt {
	return &pendingAuthAttempt{done: make(chan struct{})}
}

// finish records the outcome of the attempt and releases every waiter.
//
// Parameters:
//   - err: the authentication error, or nil on success.
//
// Side effects: closes a.done. Must be called exactly once per attempt;
// calling it twice panics on the double close, which is intentional — it
// signals a bug in the caller's goroutine bookkeeping.
func (a *pendingAuthAttempt) finish(err error) {
	a.err = err
	close(a.done)
}

// begin publishes a fresh attempt on the middleware state and marks the
// middleware as busy authenticating.
//
// Returns the newly published attempt, which the caller's goroutine must
// eventually finish.
//
// Side effects: replaces s.pending and sets s.pendingAuth.
func (s *authMiddlewareState) begin() *pendingAuthAttempt {
	attempt := newPendingAuthAttempt()
	s.pending.Store(attempt)
	s.pendingAuth.Store(true)
	return attempt
}

// settle clears the busy flag once a caller has observed that the attempt
// finished.
//
// Side effects: clears s.pendingAuth.
func (s *authMiddlewareState) settle() {
	s.pendingAuth.Store(false)
}

// pendingOutcome reports the state of the currently published attempt.
//
// Returns:
//   - running: true while a background attempt is still in flight.
//   - err: the attempt's error when it has finished (nil on success).
//
// When no attempt has ever been published, running is false and err is nil.
// No side effects.
func (s *authMiddlewareState) pendingOutcome() (running bool, err error) {
	attempt := s.pending.Load()
	if attempt == nil {
		return false, nil
	}
	select {
	case <-attempt.done:
		return false, attempt.err
	default:
		return true, nil
	}
}
