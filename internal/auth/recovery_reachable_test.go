package auth

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/mark3labs/mcp-go/mcp"
)

// lockingCred models the one property of azidentity that makes CR-0067 A4
// hard: Authenticate and GetToken contend on a single plain sync.Mutex
// (publicClient.caeMu), held for the whole interactive flow and deaf to
// context deadlines. Any test that omits this models a world where A4 was
// never broken.
type lockingCred struct {
	mu       sync.Mutex
	release  chan struct{}
	prompted chan struct{}
	promptOK sync.Once
	getToken atomic.Int32
}

func newLockingCred() *lockingCred {
	return &lockingCred{release: make(chan struct{}), prompted: make(chan struct{})}
}

func (c *lockingCred) Authenticate(ctx context.Context, _ *policy.TokenRequestOptions) (azidentity.AuthenticationRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ch, ok := ctx.Value(DeviceCodeMsgKey).(chan DeviceCodePrompt); ok {
		select {
		case ch <- DeviceCodePrompt{Message: "enter code ABC123", UserCode: "ABC123"}:
		default:
		}
	}
	c.promptOK.Do(func() { close(c.prompted) })
	<-c.release // the user has not finished signing in
	return azidentity.AuthenticationRecord{}, nil
}

func (c *lockingCred) GetToken(_ context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	c.getToken.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	return azcore.AccessToken{}, fmt.Errorf("DeviceCodeCredential can't acquire a token without user interaction")
}

func (c *lockingCred) SilentOnly() {}

// toolRequest builds a call to the named aggregate tool.
func toolRequest(tool, operation string) mcp.CallToolRequest {
	var r mcp.CallToolRequest
	r.Params.Name = tool
	r.Params.Arguments = map[string]any{"operation": operation}
	return r
}

// TestRecoveryVerbsStayResponsiveDuringAuth is the regression test for the
// defect that live testing found in CR-0067 A4: with a device code sign-in
// outstanding, account.list did not merely stay blocked — it HUNG, returning
// nothing at all, which is worse than the pending-auth message it replaced.
//
// The middleware exemption was never the problem; it matched correctly and the
// inner handler ran. The hang was downstream, in the handler's own Graph call
// contending with the in-flight Authenticate on the credential's internal
// mutex. This test drives the real AuthMiddleware and asserts the recovery
// verb both runs AND returns promptly.
func TestRecoveryVerbsStayResponsiveDuringAuth(t *testing.T) {
	cred := newLockingCred()
	defer close(cred.release)

	mw, _ := AuthMiddleware(cred, t.TempDir()+"/rec.json", "device_code", []string{"Calendars.ReadWrite"})

	// Step 1: an ordinary verb on a cold credential starts the device code
	// flow and leaves it pending.
	mail := mw(func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, fmt.Errorf("DeviceCodeCredential: expired token")
	})
	step1 := make(chan struct{})
	go func() {
		defer close(step1)
		_, _ = mail(context.Background(), toolRequest("mail", "list_folders"))
	}()
	select {
	case <-step1:
	case <-time.After(15 * time.Second):
		t.Fatal("the triggering mail call itself hung")
	}
	<-cred.prompted // the sign-in is now holding the credential lock

	// Step 2: a recovery verb whose handler enriches its response with a Graph
	// lookup, exactly as account.list does via EnsureEmail.
	// The Graph client is backed by the SAME credential the sign-in is holding,
	// so EnsureEmail's /me call reaches GetToken and contends on that mutex.
	// A nil Client here would short-circuit EnsureEmail and make this test
	// vacuous.
	graphClient, err := NewDefaultGraphClientFactory([]string{"Calendars.ReadWrite"})(cred)
	if err != nil {
		t.Fatalf("graph client: %v", err)
	}

	var innerRan bool
	account := mw(func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		innerRan = true
		entry := &AccountEntry{Label: "default", Client: graphClient}
		EnsureEmail(ctx, entry) // must skip, not block
		return mcp.NewToolResultText("accounts: default"), nil
	})

	type outcome struct {
		text string
		err  error
	}
	got := make(chan outcome, 1)
	start := time.Now()
	go func() {
		res, err := account(context.Background(), toolRequest("account", "list"))
		got <- outcome{extractResultText(res), err}
	}()

	select {
	case o := <-got:
		elapsed := time.Since(start)
		if o.err != nil {
			t.Fatalf("account verb returned a Go error: %v", o.err)
		}
		if o.text == "" {
			t.Error("account verb returned an empty result; recovery must produce something actionable")
		}
		if elapsed > 2*time.Second {
			t.Errorf("account verb took %v; recovery verbs must return promptly", elapsed)
		}
		if !innerRan {
			t.Error("account verb did not reach its handler; the recovery exemption did not apply")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("account verb HUNG while a sign-in was pending (innerRan=%v, getTokenCalls=%d)",
			innerRan, cred.getToken.Load())
	}
}

// TestEnsureEmail_SkipsDuringInteractiveAuth pins the guard directly: the
// best-effort address lookup must not touch the credential while a sign-in
// holds its lock.
func TestEnsureEmail_SkipsDuringInteractiveAuth(t *testing.T) {
	BeginInteractiveAuth()
	defer EndInteractiveAuth()

	if !InteractiveAuthInFlight() {
		t.Fatal("InteractiveAuthInFlight() = false after BeginInteractiveAuth()")
	}

	// A non-nil Client would otherwise trigger a Graph call.
	entry := &AccountEntry{Label: "default"}
	done := make(chan struct{})
	go func() { defer close(done); EnsureEmail(context.Background(), entry) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("EnsureEmail blocked while an interactive sign-in was in flight")
	}
}

// TestInteractiveAuthInFlight_Nests verifies the counter handles concurrent
// flows on different accounts.
//
// The assertions are relative to whatever the counter already holds: other
// tests in this package start background authentication goroutines that are
// still in flight, so an absolute zero baseline is not available.
func TestInteractiveAuthInFlight_Nests(t *testing.T) {
	baseline := interactiveAuthCount.Load()

	BeginInteractiveAuth()
	BeginInteractiveAuth()
	if got := interactiveAuthCount.Load(); got != baseline+2 {
		t.Errorf("counter = %d, want %d after two Begins", got, baseline+2)
	}

	EndInteractiveAuth()
	if !InteractiveAuthInFlight() {
		t.Error("InteractiveAuthInFlight() = false while one flow is still outstanding")
	}

	EndInteractiveAuth()
	if got := interactiveAuthCount.Load(); got != baseline {
		t.Errorf("counter = %d, want the baseline %d restored", got, baseline)
	}
}
