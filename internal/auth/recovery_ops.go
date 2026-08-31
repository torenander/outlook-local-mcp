// Package auth recovery verb classification.
//
// This file answers one question for the auth middleware: is the incoming
// tool call the user's way out of a broken authentication state? Before
// CR-0067 the answer was never consulted, so while a background flow was
// pending every wrapped verb — including account.login, account.list and
// account.refresh — was answered with "Authentication is still in progress".
// The verbs that exist to repair authentication were locked behind the very
// condition they repair.
package auth

import "github.com/mark3labs/mcp-go/mcp"

// recoveryDomain is the aggregate MCP tool name whose verbs manage
// authentication itself: add, remove, list, login, logout and refresh. Every
// verb in this domain either drives its own authentication flow or only reads
// registry state, so none of them needs — or benefits from — the middleware
// stepping in front of it.
const recoveryDomain = "account"

// isRecoveryOperation reports whether request targets a verb the user needs in
// order to inspect or repair authentication.
//
// Classification is by aggregate tool name rather than by verb, because the
// whole account domain is recovery surface and enumerating its verbs here
// would create a second registry to keep in sync with
// internal/server/account_verbs.go.
//
// Parameters:
//   - request: the incoming tool call.
//
// Returns true for calls to the account domain tool, false otherwise. No side
// effects.
func isRecoveryOperation(request mcp.CallToolRequest) bool {
	return request.Params.Name == recoveryDomain
}
