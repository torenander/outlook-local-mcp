package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/pkg/browser"
)

// authenticateFunc is the function signature for performing authentication.
// The default implementation calls the package-level Authenticate function.
// Tests can replace this to avoid real Entra ID calls.
type authenticateFunc func(ctx context.Context, auth Authenticator, authRecordPath string, scopes []string) (azidentity.AuthenticationRecord, error)

// urlElicitFunc is the function signature for requesting URL mode elicitation.
// The default implementation uses ServerFromContext and ClientSessionFromContext
// to call RequestURLElicitation. Tests replace this to avoid requiring a real
// MCP server and session.
type urlElicitFunc func(ctx context.Context, elicitationID, url, message string) (*mcp.ElicitationResult, error)

// defaultURLElicit retrieves the MCPServer and ClientSession from context and
// calls RequestURLElicitation. This is the production implementation of
// urlElicitFunc.
//
// Parameters:
//   - ctx: the context containing the MCPServer and ClientSession.
//   - elicitationID: unique identifier for the elicitation request.
//   - url: the URL for the user to open.
//   - message: human-readable message explaining the request.
//
// Returns the elicitation result, or an error if the server/session is not
// in context or the client does not support elicitation.
func defaultURLElicit(ctx context.Context, elicitationID, url, message string) (*mcp.ElicitationResult, error) {
	srv := mcpserver.ServerFromContext(ctx)
	if srv == nil {
		return nil, mcpserver.ErrElicitationNotSupported
	}
	session := mcpserver.ClientSessionFromContext(ctx)
	if session == nil {
		return nil, mcpserver.ErrElicitationNotSupported
	}
	return srv.RequestURLElicitation(ctx, session, elicitationID, url, message)
}

// authMiddlewareState holds the shared state for the AuthMiddleware closure.
// It coordinates concurrent re-authentication attempts and tracks whether the
// "server authenticated and ready to serve" log message has been emitted.
type authMiddlewareState struct {
	// cred is the credential used for re-authentication. It implements the
	// Authenticator interface, which both DeviceCodeCredential and
	// InteractiveBrowserCredential satisfy.
	cred Authenticator

	// authRecordPath is the filesystem path for persisting the AuthenticationRecord.
	authRecordPath string

	// authMethod controls which re-authentication flow is used in handleAuthError.
	// "browser" triggers direct browser-based authentication without a device code
	// channel. "device_code" preserves the existing channel-based device code prompt
	// forwarding mechanism.
	authMethod string

	// authenticate is the function called to perform re-authentication.
	// Defaults to the package-level Authenticate function.
	authenticate authenticateFunc

	// elicit is the function called to request form mode elicitation from the
	// client. Used by handleDeviceCodeAuth to display the device code and URL.
	// Defaults to defaultElicit.
	elicit elicitFunc

	// urlElicit is the function called to request URL mode elicitation from the
	// client. Used by handleBrowserAuth to present the login URL.
	// Defaults to defaultURLElicit.
	urlElicit urlElicitFunc

	// mu serializes re-authentication attempts so that only one authentication
	// flow runs at a time. Other goroutines encountering auth errors wait on
	// this lock.
	mu sync.Mutex

	// preAuthenticated is set to true by MarkPreAuthenticated when the startup
	// token probe confirms that the default credential has a valid cached token.
	// When false and authenticated is also false, the middleware assumes a fresh
	// credential and skips the inner handler (which would block for the Graph
	// API timeout) to present the auth prompt immediately.
	preAuthenticated atomic.Bool

	// authenticated tracks whether a successful authentication has occurred.
	// The "server authenticated and ready to serve" log is emitted only on the
	// first transition from false to true.
	authenticated atomic.Bool

	// pendingAuth is true while a background authentication flow is running.
	// Checked at middleware entry to avoid starting duplicate flows.
	pendingAuth atomic.Bool

	// pending points at the in-flight (or most recently finished) background
	// authentication attempt. It is an atomic pointer, and the attempt's own
	// fields are published through its done channel, so middleware entry can
	// inspect the outcome without taking mu — which the completing goroutine
	// could not take anyway, since handleAuthError holds mu while waiting on
	// it (CR-0067 A4).
	pending atomic.Pointer[pendingAuthAttempt]

	// openBrowser opens a URL in the system browser. Defaults to
	// browser.OpenURL in production; tests inject a no-op to prevent
	// real browser opens.
	openBrowser func(url string) error

	// browserTimeout is the maximum time to wait for the user to complete
	// browser authentication. Defaults to 120 seconds in production; tests
	// can set a shorter duration.
	browserTimeout time.Duration

	// scopes holds the OAuth scopes to request during authentication and
	// token exchange. Built from Scopes(cfg) at middleware construction time.
	scopes []string
}

// AuthMiddleware returns a middleware factory that wraps tool handlers with
// authentication error detection and automatic re-authentication. When the
// inner handler returns an authentication error (detected via IsAuthError on
// the Go error or auth error patterns in CallToolResult content), the
// middleware:
//
//  1. Sends a LoggingMessageNotification to the MCP client indicating
//     authentication is required.
//  2. Calls Authenticate to initiate the authentication flow. For device code
//     auth, the credential's UserPrompt callback fires during this call. For
//     browser auth, the system browser is opened directly.
//  3. On success, logs "server authenticated and ready to serve" (once), and
//     retries the original tool call exactly once.
//  4. On failure, returns an mcp.NewToolResultError with troubleshooting
//     guidance from FormatAuthError.
//
// A sync.Mutex ensures that only one re-authentication attempt runs at a time.
// Concurrent tool calls encountering auth errors wait for the single
// re-authentication to complete before retrying.
//
// Fresh-credential detection: when neither preAuthenticated nor authenticated
// is true, the middleware assumes the credential has never been authenticated.
// In this case, it skips calling the inner handler (which would block for the
// Graph API timeout, typically 30 seconds) and immediately initiates the
// re-authentication flow. The startup token probe in main.go calls
// MarkPreAuthenticated to signal that the cached token is valid, preventing
// this fast-path from triggering for credentials with valid cached tokens.
//
// Parameters:
//   - cred: the Authenticator constructed by SetupCredential.
//   - authRecordPath: absolute path for persisting the AuthenticationRecord.
//   - authMethod: the authentication method ("browser" or "device_code") that
//     controls the re-authentication flow in handleAuthError. For "browser",
//     the system browser is opened directly. For "device_code", the device
//     code prompt is forwarded via the DeviceCodeMsgKey channel mechanism.
//   - scopes: OAuth scopes to request during authentication (from Scopes(cfg)).
//
// Returns the middleware function compatible with the tool handler wrapping
// pattern used in RegisterTools, and a MarkPreAuthenticated callback that
// the startup token probe calls to signal that the credential has a valid
// cached token.
func AuthMiddleware(cred Authenticator, authRecordPath string, authMethod string, scopes []string) (func(mcpserver.ToolHandlerFunc) mcpserver.ToolHandlerFunc, func()) {
	state := &authMiddlewareState{
		cred:           cred,
		authRecordPath: authRecordPath,
		authMethod:     authMethod,
		authenticate:   Authenticate,
		elicit:         defaultElicit,
		urlElicit:      defaultURLElicit,
		openBrowser:    browser.OpenURL,
		browserTimeout: 120 * time.Second,
		scopes:         scopes,
	}

	middleware := func(next mcpserver.ToolHandlerFunc) mcpserver.ToolHandlerFunc {
		return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Install the per-request account slot so AccountResolver, which
			// runs inside this middleware, can report which account it picked
			// (CR-0067 A6). See account_slot.go for why a mutable slot is
			// needed instead of a plain context value.
			ctx, _ = withAccountAuthSlot(ctx)

			// Verbs that exist to repair authentication must never be gated on
			// authentication being healthy (CR-0067 A4).
			recovery := isRecoveryOperation(request)

			// Check if a background authentication flow completed.
			if state.pendingAuth.Load() {
				running, pendingErr := state.pendingOutcome()
				switch {
				case running && recovery:
					// Let the user reach account.login / account.list /
					// account.refresh while a flow is still outstanding.
					return next(ctx, request)
				case running:
					return mcp.NewToolResultError(pendingAuthMessage(state.authMethod)), nil
				default:
					state.settle()
					if pendingErr != nil {
						return mcp.NewToolResultError(FormatAuthErrorFor(pendingErr, state.authMethod)), nil
					}
					if state.authenticated.CompareAndSwap(false, true) {
						slog.Info("server authenticated and ready to serve")
					}
					// Auth succeeded, fall through to execute the tool call.
				}
			}

			// Fresh-credential fast-path: if neither the startup token probe
			// nor a previous tool call has confirmed a valid credential, skip
			// the inner handler (which would block for the Graph API timeout)
			// and go directly to the re-authentication flow.
			if !state.preAuthenticated.Load() && !state.authenticated.Load() {
				// Recovery verbs read and repair the account registry; they do
				// not need a working default credential, so never divert them
				// into an authentication prompt (CR-0067 A4).
				if recovery {
					return next(ctx, request)
				}
				// Try the token cache before assuming the credential is cold.
				// The startup probe deliberately skips some credential types,
				// so "not pre-authenticated" does not mean "no cached token"
				// (CR-0067 A1).
				if TrySilentToken(ctx, state.cred, state.scopes) {
					if state.authenticated.CompareAndSwap(false, true) {
						slog.Info("server authenticated and ready to serve")
					}
					return next(ctx, request)
				}
				slog.Info("fresh credential detected, skipping Graph API call to present auth prompt immediately")
				freshErr := fmt.Errorf("authentication required: credential not yet authenticated")
				return state.handleAuthError(ctx, next, request, freshErr)
			}

			// Call the inner handler.
			result, err := next(ctx, request)

			// If successful, pass through.
			if err == nil && (result == nil || !result.IsError) {
				return result, nil
			}

			// Check if the error is authentication-related.
			if !isAuthRelated(err, result) {
				return result, err
			}

			// Auth error detected; attempt re-authentication.
			return state.handleAuthError(ctx, next, request, err)
		}
	}

	markPreAuthenticated := func() {
		state.preAuthenticated.Store(true)
	}

	return middleware, markPreAuthenticated
}

// handleAuthError coordinates re-authentication when a tool call encounters
// an authentication error. It acquires the re-auth mutex, resolves the
// credential for the account the call actually targeted, attempts a silent
// token refresh, and only then delegates to an interactive flow based on the
// resolved authMethod:
//
// For "browser" auth (handleBrowserAuth):
//
//  1. Sends an MCP notification that a browser window will open for login.
//  2. Calls Authenticate in a background goroutine (which opens the browser).
//  3. Waits for completion or timeout, then retries the tool call on success.
//
// For "device_code" auth (handleDeviceCodeAuth):
//
//  1. Sends an MCP notification that device code login is starting.
//  2. Calls Authenticate in a background goroutine with a deviceCodeCh channel.
//  3. Waits for the device code prompt, auth completion, or timeout.
//  4. Returns the device code prompt as a tool result for the user to act on.
//
// Before any of those flows runs, a silent token acquisition is attempted via
// TrySilentToken. When it succeeds the original tool call is retried
// immediately and the user is never prompted (CR-0067 A1).
//
// Subsequent tool calls while a background flow is in progress receive a
// "still in progress" message until the user completes login — except calls
// to the account domain, which stay reachable so the user can recover
// (CR-0067 A4).
//
// Parameters:
//   - ctx: the tool handler context containing the MCPServer.
//   - next: the inner tool handler to retry after successful re-authentication.
//   - request: the original tool call request for retry.
//   - origErr: the original Go error from the failed handler call.
//
// Returns the appropriate tool result depending on auth method and outcome.
func (s *authMiddlewareState) handleAuthError(
	ctx context.Context,
	next mcpserver.ToolHandlerFunc,
	request mcp.CallToolRequest,
	origErr error,
) (*mcp.CallToolResult, error) {
	// Serialize re-authentication attempts.
	s.mu.Lock()
	defer s.mu.Unlock()

	// If auth is already in progress, tell the user to wait.
	if s.pendingAuth.Load() {
		return mcp.NewToolResultError(pendingAuthMessage(s.authMethod)), nil
	}

	// Resolve per-account auth details for this request. resolvedAccountAuth
	// reads the slot that AccountResolver filled in during the handler call
	// (CR-0067 A6) and falls back to a directly injected context value. When
	// neither is present — for example on the fresh-credential fast path,
	// which never reaches AccountResolver — the closure credential is used.
	cred := s.cred
	authRecordPath := s.authRecordPath
	authMethod := s.authMethod
	if aa, ok := resolvedAccountAuth(ctx); ok {
		cred = aa.Authenticator
		authRecordPath = aa.AuthRecordPath
		authMethod = aa.AuthMethod
	}

	// Before showing the user anything, ask the token cache. A revoked-looking
	// 401 from Graph is often just an expired access token that the cached
	// refresh token can renew (CR-0067 A1).
	if TrySilentToken(ctx, cred, s.scopes) {
		if s.authenticated.CompareAndSwap(false, true) {
			slog.Info("server authenticated and ready to serve")
		}
		return next(ctx, request)
	}

	if authMethod == "auth_code" {
		return s.handleAuthCodeAuth(ctx, next, request, origErr, cred, authRecordPath)
	}
	if authMethod == "browser" {
		return s.handleBrowserAuth(ctx, next, request, origErr, cred, authRecordPath)
	}
	return s.handleDeviceCodeAuth(ctx, next, request, origErr, cred, authRecordPath)
}

// handleAuthCodeAuth performs re-authentication using the manual authorization
// code flow with PKCE. Unlike the browser and device_code flows (which use
// background goroutines), this flow is synchronous: it blocks on elicitation
// until the user pastes the redirect URL.
//
// The method:
//  1. Type-asserts the credential to AuthCodeFlow.
//  2. Calls AuthCodeURL to obtain the authorization URL.
//  3. Opens the URL in the system browser.
//  4. Attempts MCP elicitation with a redirect_url text field.
//  5. If elicitation succeeds, calls ExchangeCode and retries the tool call.
//  6. If elicitation is not supported, returns the auth URL with instructions
//     to use the complete_auth tool.
//
// Parameters:
//   - ctx: the tool handler context containing the MCPServer.
//   - next: the inner tool handler to retry after successful re-authentication.
//   - request: the original tool call request for retry.
//   - origErr: the original Go error from the failed handler call.
//   - cred: the Authenticator to use, which must also implement AuthCodeFlow.
//   - authRecordPath: the filesystem path for persisting the account metadata.
//
// Returns the retried handler result on success, or an error result with
// instructions on failure.
func (s *authMiddlewareState) handleAuthCodeAuth(
	ctx context.Context,
	next mcpserver.ToolHandlerFunc,
	request mcp.CallToolRequest,
	origErr error,
	cred Authenticator,
	authRecordPath string,
) (*mcp.CallToolResult, error) {
	// Type-assert to AuthCodeFlow interface.
	acf, ok := cred.(AuthCodeFlow)
	if !ok {
		return mcp.NewToolResultError(
			"Internal error: credential does not support the auth_code flow. " +
				"Check that OUTLOOK_MCP_AUTH_METHOD is set correctly."), nil
	}

	// Obtain the authorization URL.
	authURL, err := acf.AuthCodeURL(ctx, s.scopes)
	if err != nil {
		return mcp.NewToolResultError(FormatAuthErrorFor(err, "auth_code")), nil
	}

	// Open the authorization URL in the system browser.
	if browserErr := s.openBrowser(authURL); browserErr != nil {
		slog.Warn("failed to open browser for auth code flow", "error", browserErr)
	}

	// Attempt MCP elicitation with a redirect_url text field.
	elicitationRequest := mcp.ElicitationRequest{
		Params: mcp.ElicitationParams{
			Message: "Authentication required. A browser window has opened for Microsoft login. " +
				"After you sign in, the browser will show a blank page. " +
				"Copy the full URL from the browser's address bar and paste it below.",
			RequestedSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"redirect_url": map[string]any{
						"type":        "string",
						"description": "Paste the full URL from the browser's address bar after signing in.",
					},
				},
				"required": []string{"redirect_url"},
			},
		},
	}

	result, elicitErr := s.elicit(ctx, elicitationRequest)
	if elicitErr != nil {
		slog.Info("elicitation failed, returning auth URL for complete_auth tool", "error", elicitErr)
		return mcp.NewToolResultText(fmt.Sprintf(
			"Authentication required. A browser window has opened for Microsoft login.\n\n"+
				"After signing in, copy the full URL from the browser's address bar and use the "+
				"complete_auth tool with the redirect_url parameter to finish authentication.\n\n"+
				"Auth URL: %s",
			authURL)), nil
	}

	// Handle elicitation response action.
	switch result.Action {
	case mcp.ElicitationResponseActionAccept:
		// Extract redirect_url from the response content.
		content, contentOK := result.Content.(map[string]any)
		if !contentOK {
			return mcp.NewToolResultError(
				"Invalid elicitation response: expected form content."), nil
		}
		redirectURL, urlOK := content["redirect_url"].(string)
		if !urlOK || redirectURL == "" {
			return mcp.NewToolResultError(
				"No redirect URL provided. Please try again and paste the full URL " +
					"from the browser's address bar after signing in."), nil
		}

		// Exchange the authorization code.
		if exchangeErr := acf.ExchangeCode(ctx, redirectURL, s.scopes); exchangeErr != nil {
			return mcp.NewToolResultError(FormatAuthErrorFor(exchangeErr, "auth_code")), nil
		}

		// Persist the account metadata.
		if acc, isACC := cred.(*AuthCodeCredential); isACC {
			if persistErr := acc.PersistAccount(authRecordPath); persistErr != nil {
				slog.Warn("failed to persist auth code account", "error", persistErr)
			}
		}

		// Log successful authentication (once).
		if s.authenticated.CompareAndSwap(false, true) {
			slog.Info("server authenticated and ready to serve")
		}

		// Retry the original tool call.
		return next(ctx, request)

	case mcp.ElicitationResponseActionDecline:
		return mcp.NewToolResultError(
			"Authentication declined. Please retry when you are ready to sign in."), nil

	case mcp.ElicitationResponseActionCancel:
		return mcp.NewToolResultError(
			"Authentication cancelled."), nil

	default:
		return mcp.NewToolResultError(fmt.Sprintf(
			"Unexpected elicitation response: %s", result.Action)), nil
	}
}

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

	go func() {
		var err error
		defer func() {
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

	go func() {
		var err error
		defer func() {
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

// pendingAuthMessage returns the appropriate "authentication in progress"
// message based on the configured auth method. For browser auth, the user
// is told to complete login in their browser. For device code auth, the
// user is told to complete the device code login.
//
// Parameters:
//   - authMethod: the configured authentication method ("browser" or "device_code").
//
// Returns the user-facing message string.
func pendingAuthMessage(authMethod string) string {
	switch authMethod {
	case "auth_code":
		return "Authentication is still in progress. " +
			"Please complete the login in your browser, copy the redirect URL, " +
			"and paste it when prompted, then retry this request."
	case "browser":
		return "Authentication is still in progress. " +
			"Please complete the login in your browser, then retry this request."
	default:
		return "Authentication is still in progress. " +
			"Please complete the device code login in your browser, then retry this request."
	}
}

// injectMCPServer copies the MCPServer from src context into dst context.
// The mcp-go library stores the server in context via a private key, so
// we use ServerFromContext to extract it and WithValue with the same key
// type to propagate it. Since the key type is private to mcp-go, we
// re-embed the entire src context's value chain by creating a derived context.
//
// Parameters:
//   - src: context containing the MCPServer (from the tool handler).
//   - dst: context to receive the MCPServer (background context for auth).
//
// Returns dst enriched with the MCPServer if available, or dst unchanged.
func injectMCPServer(src, dst context.Context) context.Context {
	srv := mcpserver.ServerFromContext(src)
	if srv == nil {
		return dst
	}
	// Since the mcp-go serverKey type is unexported, we cannot directly copy
	// the value. Instead, we use a wrapping context that delegates Value
	// lookups to src for unknown keys while preserving dst's cancellation.
	return &mergedContext{
		Context: dst,
		values:  src,
	}
}

// mergedContext is a context that uses one context for cancellation/deadline
// and another for Value lookups. This allows the background context's
// non-cancellable behavior to be combined with the tool handler context's
// stored MCPServer value.
type mergedContext struct {
	context.Context
	values context.Context
}

// Value returns the value associated with key, checking the values context
// first (which contains the MCPServer), then falling back to the embedded
// context.
func (c *mergedContext) Value(key any) any {
	if v := c.values.Value(key); v != nil {
		return v
	}
	return c.Context.Value(key)
}

// isAuthRelated checks whether a handler response indicates an authentication
// error. It inspects both the Go error (via IsAuthError) and the
// CallToolResult content text for known auth error patterns.
//
// Parameters:
//   - err: the Go error returned by the handler. May be nil.
//   - result: the CallToolResult returned by the handler. May be nil.
//
// Returns true if either the error or result content indicates an
// authentication failure.
func isAuthRelated(err error, result *mcp.CallToolResult) bool {
	if IsAuthError(err) {
		return true
	}

	// Check CallToolResult content for auth error text.
	if result != nil && result.IsError {
		text := extractResultText(result)
		if containsAuthPattern(text) {
			return true
		}
	}

	return false
}

// extractResultText extracts the text content from the first TextContent
// element in a CallToolResult. Tool handlers in this project consistently
// use a single TextContent element for error messages.
//
// Parameters:
//   - result: the CallToolResult to extract text from.
//
// Returns the text string, or empty string if no TextContent is found.
func extractResultText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	if tc, ok := result.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// containsAuthPattern reports whether text contains any of the known
// authentication error substrings. This mirrors the pattern matching in
// IsAuthError but operates on string content rather than error values.
//
// Parameters:
//   - text: the string to search for auth error patterns.
//
// Returns true if any pattern is found.
func containsAuthPattern(text string) bool {
	for _, pattern := range authErrorPatterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

// sendClientNotification sends a LoggingMessageNotification to the MCP client
// via the MCPServer found in ctx. If no MCPServer is available (e.g., in tests
// or outside a tool handler context), the message is written to stderr as a
// fallback.
//
// Parameters:
//   - ctx: the context potentially containing an MCPServer.
//   - level: the logging level for the notification.
//   - message: the message text to send.
//
// Side effects: sends a notification to the MCP client or writes to stderr.
func sendClientNotification(ctx context.Context, level mcp.LoggingLevel, message string) {
	srv := mcpserver.ServerFromContext(ctx)
	if srv != nil {
		notification := mcp.NewLoggingMessageNotification(level, "auth", message)
		if err := srv.SendLogMessageToClient(ctx, notification); err != nil {
			slog.Warn("failed to send log notification to client, falling back to stderr",
				"error", err)
			fmt.Fprintln(os.Stderr, message) //nolint:errcheck // best-effort stderr fallback
		}
		return
	}

	// Fallback: no MCPServer available in context.
	fmt.Fprintln(os.Stderr, message) //nolint:errcheck // best-effort stderr fallback
}
