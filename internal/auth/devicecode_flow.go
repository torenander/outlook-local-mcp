// Package auth — this file holds the device code sign-in flow.
//
// Split out of middleware.go to satisfy CR-0067 NFR-27, alongside
// browser_flow.go. The presentation half of this flow — the sign-in link, the
// code, and the acknowledgement wait — already lives in devicecode_present.go
// and devicecode_prompt.go; what remains here is the flow control that drives
// them.
package auth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// handleDeviceCodeAuth performs re-authentication using the device code flow.
// It starts the flow in a background goroutine and waits for the device code
// prompt from Entra ID via the deviceCodeCh channel. Once received, it
// attempts to present the device code and URL via form mode elicitation
// (RequestElicitation), falling back to returning the prompt as a tool result
// when elicitation is not supported.
//
// Parameters:
//   - ctx: the tool handler context containing the MCPServer.
//   - next: the inner tool handler to retry after successful re-authentication.
//   - request: the original tool call request for retry.
//   - origErr: the original Go error from the failed handler call.
//   - cred: the Authenticator to use for re-authentication (per-account or closure).
//   - authRecordPath: the filesystem path for persisting the AuthenticationRecord.
//
// Returns the device code prompt as a text result, the retried handler result
// on silent auth success, or an error result with troubleshooting guidance.
func (s *authMiddlewareState) handleDeviceCodeAuth(
	ctx context.Context,
	next mcpserver.ToolHandlerFunc,
	request mcp.CallToolRequest,
	origErr error,
	cred Authenticator,
	authRecordPath string,
) (*mcp.CallToolResult, error) {
	// Send notification to MCP client that authentication is required.
	sendClientNotification(ctx, mcp.LoggingLevelWarning,
		"Authentication required. Initiating device code login flow...")

	// Use a background context for the device code flow, since the tool call
	// context may have a short deadline. The device code flow can take up to
	// ~15 minutes while the user completes login, but it must not run
	// unbounded: before CR-0067 this context had no deadline at all, so an
	// abandoned login left pendingAuth set for the lifetime of the process and
	// froze every subsequent tool call.
	authCtx, cancelAuth := context.WithTimeout(context.Background(), backgroundAuthTimeout)

	// Inject the tool handler's MCPServer context for UserPrompt notifications.
	authCtx = injectMCPServer(ctx, authCtx)

	// Inject channel so deviceCodeUserPrompt can forward the device code
	// challenge back for inclusion in the tool result.
	deviceCodeCh := make(chan DeviceCodePrompt, 1)
	authCtx = context.WithValue(authCtx, DeviceCodeMsgKey, deviceCodeCh)

	// Start the device code flow in the background so the tool call can
	// return the device code prompt to the agent/user immediately.
	attempt := s.begin()

	// Signal that the credential's internal lock is about to be held for the
	// duration of the flow, so best-effort Graph work skips rather than blocks
	// (CR-0067 A4; see inflight.go).
	BeginInteractiveAuth()

	go func() {
		var err error
		defer func() {
			EndInteractiveAuth()
			cancelAuth()
			attempt.finish(err)
		}()
		_, err = s.authenticate(authCtx, cred, authRecordPath, s.scopes)
		if err != nil {
			slog.Error("background re-authentication failed", "error", err)
		} else if s.authenticated.CompareAndSwap(false, true) {
			slog.Info("server authenticated and ready to serve")
		}
	}()

	// Wait for the device code message or early auth completion.
	select {
	case prompt := <-deviceCodeCh:
		// Present the challenge as a direct sign-in link when the client
		// supports URL elicitation.
		slog.Info("device code prompt captured, presenting to client")
		return s.presentDeviceCode(ctx, next, request, prompt, attempt), nil

	case <-attempt.done:
		// Auth completed before the device code prompt was needed
		// (e.g. a cached refresh token was still valid).
		s.settle()
		if attempt.err != nil {
			errToFormat := attempt.err
			if origErr != nil {
				errToFormat = origErr
			}
			return mcp.NewToolResultError(FormatAuthErrorFor(errToFormat, "device_code")), nil
		}
		// Retry the original tool call.
		return next(ctx, request)

	case <-time.After(15 * time.Second):
		// Timeout waiting for the device code prompt from Entra ID.
		return mcp.NewToolResultError(FormatAuthErrorFor(
			fmt.Errorf("authentication required: device code prompt was not received from Entra ID"), "device_code")), nil
	}
}
