// Package auth device code presentation.
//
// This file renders a device code challenge to the user and, when the client
// can talk back, waits for the sign-in to finish and retries the original tool
// call. It is separate from middleware.go so the two presentation modes —
// URL elicitation (link plus code) and the verbatim plain-text fallback — stay
// readable side by side.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// deviceCodeElicitMessage builds the instruction shown alongside the sign-in
// link in a URL-mode elicitation.
//
// It MUST quote the user code. A URL elicitation shows the user a link and
// this message and nothing else, and the sign-in page does not pre-fill the
// code (see DeviceCodePrompt.SignInURL) — so a message that omitted the code
// would drop the user on the right page with nothing to type. An earlier
// revision of CR-0067 did exactly that, on the false premise that the otc
// parameter pre-filled the field.
//
// Parameters:
//   - prompt: the device code challenge being presented.
//
// Returns the message text. No side effects.
func deviceCodeElicitMessage(prompt DeviceCodePrompt) string {
	if prompt.UserCode == "" {
		return "Authentication required. Open this link to finish signing in to Microsoft."
	}
	return fmt.Sprintf(
		"Authentication required. Open this link and enter the code %s to finish signing in to Microsoft.",
		prompt.UserCode)
}

// presentDeviceCode shows the device code challenge to the user and, when the
// client acknowledges it, waits for authentication to complete and retries the
// original tool call.
//
// Presentation is attempted as a URL mode elicitation: a direct link to the
// device sign-in page, plus a message quoting the code to type there. The link
// is a navigation shortcut only — Microsoft does not pre-fill the code field
// (see DeviceCodePrompt.SignInURL) — which is why the message must carry the
// code (CR-0067 A7).
//
// When the client does not support elicitation, the Entra ID message is
// returned verbatim as plain text. It already names both the page and the
// code, so nothing is appended to it: a second near-identical URL would add
// noise, not help. That fallback carries most of the real traffic — per
// CR-0031, clients such as Claude Code answer elicitation requests with
// "Method not found", and the tool result text is then the only channel that
// reaches the user at all — so the Entra sentence must never be reworded or
// dropped.
//
// Parameters:
//   - ctx: the tool handler context, used for elicitation and for the retry.
//   - next: the inner tool handler to retry once authentication completes.
//   - request: the original tool call to retry.
//   - prompt: the structured device code challenge from Entra ID.
//   - attempt: the in-flight background authentication attempt to wait on.
//
// Returns the retried handler result when the user completes sign-in in time,
// or a text result carrying the device code instructions otherwise. Errors
// from the retried handler are swallowed into the result by the caller's
// signature; the function itself never returns a Go error.
//
// Side effects: may send an elicitation request to the MCP client, and blocks
// for up to browserTimeout waiting for the background flow to finish.
func (s *authMiddlewareState) presentDeviceCode(
	ctx context.Context,
	next mcpserver.ToolHandlerFunc,
	request mcp.CallToolRequest,
	prompt DeviceCodePrompt,
	attempt *pendingAuthAttempt,
) *mcp.CallToolResult {
	result, err := s.urlElicit(ctx, uuid.New().String(), prompt.SignInURL(), deviceCodeElicitMessage(prompt))
	if err != nil {
		if errors.Is(err, mcpserver.ErrElicitationNotSupported) {
			slog.Info("URL elicitation not supported, returning device code as text")
		} else {
			slog.Warn("device code elicitation failed, returning as text", "error", err)
		}
		return mcp.NewToolResultText(prompt.Message)
	}

	if result == nil || result.Action != mcp.ElicitationResponseActionAccept {
		// Declined or cancelled: still hand back the instructions so the user
		// can complete the sign-in later without starting over.
		return mcp.NewToolResultText(prompt.Message)
	}

	// The user says they have opened the link. Wait for the background flow to
	// finish rather than returning instructions the agent cannot act on.
	return s.awaitDeviceCodeCompletion(ctx, next, request, prompt, attempt)
}

// awaitDeviceCodeCompletion blocks until the background authentication attempt
// finishes or browserTimeout elapses, then retries the original tool call.
//
// Parameters:
//   - ctx: the tool handler context, used for the retry.
//   - next: the inner tool handler to retry.
//   - request: the original tool call to retry.
//   - prompt: the device code challenge, returned as text if the wait expires.
//   - attempt: the background authentication attempt to wait on.
//
// Returns the retried handler result on success, an error result when
// authentication failed, or the verbatim device code text when the wait timed
// out and the user may still be signing in.
//
// Side effects: clears the pending authentication flag once the attempt is
// observed to have finished.
func (s *authMiddlewareState) awaitDeviceCodeCompletion(
	ctx context.Context,
	next mcpserver.ToolHandlerFunc,
	request mcp.CallToolRequest,
	prompt DeviceCodePrompt,
	attempt *pendingAuthAttempt,
) *mcp.CallToolResult {
	select {
	case <-attempt.done:
		s.settle()
		if attempt.err != nil {
			return mcp.NewToolResultError(FormatAuthErrorFor(attempt.err, "device_code"))
		}
		result, err := next(ctx, request)
		if err != nil {
			return mcp.NewToolResultError(err.Error())
		}
		return result

	case <-time.After(s.browserTimeout):
		// Sign-in is still outstanding. Leave the background flow running and
		// return the instructions so the user can finish; the next tool call
		// picks up the completed authentication at middleware entry.
		return mcp.NewToolResultText(prompt.Message)
	}
}
