// Package tools guidance for the system.complete_auth verb.
//
// This file produces the message shown when complete_auth is called against a
// credential that is not running the authorization code flow. Before CR-0067
// the verb was simply not registered outside auth_code, so an LLM that had
// been told to "finish the sign-in" discovered at the worst possible moment
// that the verb did not exist. The verb now always exists and explains, per
// method, what the caller should do instead.
package tools

import "fmt"

// completeAuthUnavailable returns an actionable message for a complete_auth
// call that cannot proceed because the target credential does not use the
// authorization code flow.
//
// The message names the recovery path for the method actually in use rather
// than reporting an internal type mismatch, so the LLM's next tool call is a
// useful one.
//
// Parameters:
//   - authMethod: the authentication method the target account uses
//     ("browser", "device_code", or "" when unknown).
//   - account: the account label or UPN the caller targeted, or "" for the
//     default account.
//
// Returns the message text. No side effects.
func completeAuthUnavailable(authMethod, account string) string {
	target := "the default account"
	if account != "" {
		target = fmt.Sprintf("account %q", account)
	}

	switch authMethod {
	case "browser":
		return fmt.Sprintf(
			"complete_auth does not apply to %s: it uses the 'browser' authentication method, "+
				"which finishes sign-in in the browser window itself and needs no redirect URL. "+
				"To re-authenticate, call account with operation=\"login\" and complete the sign-in "+
				"in the browser that opens, then retry your original request.",
			target)
	case "device_code":
		return fmt.Sprintf(
			"complete_auth does not apply to %s: it uses the 'device_code' authentication method, "+
				"which finishes when you approve the sign-in on the device login page and needs no "+
				"redirect URL. To re-authenticate, call account with operation=\"login\", open the "+
				"sign-in link it returns, approve the request, then retry your original request.",
			target)
	default:
		return fmt.Sprintf(
			"complete_auth applies only to the 'auth_code' authentication method, and %s is not "+
				"using it. Call system with operation=\"status\" to see the active method, then call "+
				"account with operation=\"login\" to re-authenticate. To switch this server to the "+
				"authorization code flow, set OUTLOOK_MCP_AUTH_METHOD=auth_code and restart.",
			target)
	}
}
