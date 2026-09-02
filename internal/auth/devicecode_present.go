// Package auth device code presentation.
//
// This file renders a device code challenge to the user and, when the client
// can talk back, waits for the sign-in to finish and retries the original tool
// call. It is separate from middleware.go so the two presentation modes —
// one-click URL elicitation and the verbatim plain-text fallback — stay
// readable side by side.
package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// deviceCodeElicitMessage is the instruction shown alongside the one-click
// sign-in URL. It states what the link does, because a URL elicitation shows
// the user a link and little else.
const deviceCodeElicitMessage = "Authentication required. Open this link to finish signing in to Microsoft — " +
	"the sign-in code is already filled in for you."

// presentDeviceCode shows the device code challenge to the user and, when the
// client acknowledges it, waits for authentication to complete and retries the
// original tool call.
//
// Presentation is attempted as a URL mode elicitation pointing at the device
// login page with the user code pre-filled via the "otc" query parameter, so
// the user clicks a link instead of transcribing a code (CR-0067 A7).
//
// When the client does not support elicitation, DeviceCodePrompt.FallbackText
// is returned as plain text: the Entra ID message verbatim, plus the same
// one-click link below it. That fallback carries most of the real traffic —
// per CR-0031, clients such as Claude Code answer elicitation requests with
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
	result, err := s.urlElicit(ctx, uuid.New().String(), prompt.OneClickURL(), deviceCodeElicitMessage)
	if err != nil {
		if errors.Is(err, mcpserver.ErrElicitationNotSupported) {
			slog.Info("URL elicitation not supported, returning device code as text")
		} else {
			slog.Warn("device code elicitation failed, returning as text", "error", err)
		}
		return mcp.NewToolResultText(prompt.FallbackText())
	}

	if result == nil || result.Action != mcp.ElicitationResponseActionAccept {
		// Declined or cancelled: still hand back the instructions so the user
		// can complete the sign-in later without starting over.
		return mcp.NewToolResultText(prompt.FallbackText())
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
		return mcp.NewToolResultText(prompt.FallbackText())
	}
}
