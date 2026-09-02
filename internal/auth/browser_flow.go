// Package auth — this file holds the interactive browser sign-in flow.
//
// Split out of middleware.go to satisfy CR-0067 NFR-27 (small single-purpose
// files; middleware.go must not grow). The validation report recorded NFR-27 as
// PARTIAL because middleware.go went from 804 to 833 lines despite seven files
// being split out, and named this move as the remedy.
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

// handleBrowserAuth performs re-authentication using the interactive browser
// flow. It first attempts to notify the user via URL mode elicitation
// (RequestURLElicitation), falling back to LoggingMessageNotification when the
// client does not support elicitation.
//
// The browser credential's Authenticate method opens the system browser
// directly. The authentication runs in a background goroutine to allow a
// generous timeout for user interaction. The middleware waits for completion
// and either retries the original tool call on success or returns
// troubleshooting guidance on failure.
//
// Parameters:
//   - ctx: the tool handler context containing the MCPServer.
//   - next: the inner tool handler to retry after successful re-authentication.
//   - request: the original tool call request for retry.
//   - origErr: the original Go error from the failed handler call.
//   - cred: the Authenticator to use for re-authentication (per-account or closure).
//   - authRecordPath: the filesystem path for persisting the AuthenticationRecord.
//
// Returns the retried handler result on success, or an error result with
// troubleshooting guidance on failure or timeout.
func (s *authMiddlewareState) handleBrowserAuth(
	ctx context.Context,
	next mcpserver.ToolHandlerFunc,
	request mcp.CallToolRequest,
	origErr error,
	cred Authenticator,
	authRecordPath string,
) (*mcp.CallToolResult, error) {
	// Try URL mode elicitation to present the login URL to the user.
	// Fall back to LoggingMessageNotification if elicitation is not supported.
	elicitationID := uuid.New().String()
	loginURL := "https://login.microsoftonline.com"
	message := "Authentication required. A browser window will open for Microsoft login."

	_, elicitErr := s.urlElicit(ctx, elicitationID, loginURL, message)
	if elicitErr != nil {
		if errors.Is(elicitErr, mcpserver.ErrElicitationNotSupported) {
			slog.Info("URL elicitation not supported, falling back to notification")
			sendClientNotification(ctx, mcp.LoggingLevelWarning, message)
		} else {
			slog.Warn("URL elicitation failed, falling back to notification", "error", elicitErr)
			sendClientNotification(ctx, mcp.LoggingLevelWarning, message)
		}
	}

	// Use a background context since the tool call context may have a short
	// deadline. Browser auth requires user interaction, but must still be
	// bounded so an abandoned sign-in releases the pending flag (CR-0067 A4).
	authCtx, cancelAuth := context.WithTimeout(context.Background(), backgroundAuthTimeout)
	authCtx = injectMCPServer(ctx, authCtx)

	// Start the browser auth flow in the background.
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

	// Wait for auth completion or timeout.
	select {
	case <-attempt.done:
		s.settle()
		if attempt.err != nil {
			errToFormat := attempt.err
			if origErr != nil {
				errToFormat = origErr
			}
			return mcp.NewToolResultError(FormatAuthErrorFor(errToFormat, "browser")), nil
		}
		// Retry the original tool call.
		return next(ctx, request)

	case <-time.After(s.browserTimeout):
		// Timeout waiting for the user to complete browser login.
		return mcp.NewToolResultError(FormatAuthErrorFor(
			fmt.Errorf("authentication required: browser login was not completed in time"), "browser")), nil
	}
}
