package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/microsoftgraph/msgraph-sdk-go/models/odataerrors"
)

// authErrorPatterns lists substrings whose presence in an error message
// indicates an authentication failure. Each pattern corresponds to a
// known error surface from the Azure Identity SDK or Entra ID:
//   - "DeviceCodeCredential" -- azidentity DeviceCodeCredential failures.
//   - "InteractiveBrowserCredential" -- azidentity InteractiveBrowserCredential failures.
//   - "authentication required" -- explicit auth-needed signals.
//   - "AADSTS" -- Entra ID Security Token Service error codes.
var authErrorPatterns = []string{
	"DeviceCodeCredential",
	"InteractiveBrowserCredential",
	"authentication required",
	"AADSTS",
}

// IsAuthError reports whether err represents an authentication failure
// that should trigger re-authentication.
//
// The function checks three categories of evidence:
//  1. The error message contains a known authentication-related substring
//     (DeviceCodeCredential, InteractiveBrowserCredential, authentication
//     required, or AADSTS).
//  2. The error is a context deadline exceeded error originating from a
//     credential operation (identified by "DeviceCodeCredential" or
//     "InteractiveBrowserCredential" in the error chain text combined
//     with context.DeadlineExceeded).
//  3. The error is an ODataError with HTTP status 401 Unauthorized.
//
// Parameters:
//   - err: the error to inspect. May be nil.
//
// Returns true when the error matches any authentication failure pattern,
// false otherwise. A nil error always returns false.
//
// IsAuthError is safe for concurrent use.
func IsAuthError(err error) bool {
	if err == nil {
		return false
	}

	// Check for HTTP 401 Unauthorized via ODataError first, before calling
	// err.Error(). The ODataError.Error() method panics when GetErrorEscaped()
	// returns nil, so status code extraction must precede string inspection.
	var odataErr *odataerrors.ODataError
	if errors.As(err, &odataErr) {
		return odataErr.ResponseStatusCode == 401
	}

	msg := err.Error()

	// Check known authentication error substrings.
	for _, pattern := range authErrorPatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}

	// Check for context deadline exceeded from a credential operation.
	if errors.Is(err, context.DeadlineExceeded) &&
		(strings.Contains(msg, "DeviceCodeCredential") || strings.Contains(msg, "InteractiveBrowserCredential")) {
		return true
	}

	return false
}

// FormatAuthError returns an LLM-actionable error message for the given
// authentication error. The output includes a plain-language description of
// the failure and the MCP verbs the LLM should call to recover. Raw Azure SDK
// class names (e.g., DeviceCodeCredential, InteractiveBrowserCredential) are
// stripped from the output so the LLM does not hallucinate SDK-level
// troubleshooting advice.
//
// The guidance is method-agnostic. Use FormatAuthErrorFor when the active
// authentication method is known, so the message can name the one recovery
// path that applies.
//
// Parameters:
//   - err: the authentication error to format. Must not be nil.
//
// Returns a multi-line string containing a plain-language description and
// recovery instructions referencing MCP verbs.
//
// FormatAuthError is safe for concurrent use.
func FormatAuthError(err error) string {
	return FormatAuthErrorFor(err, "")
}

// FormatAuthErrorFor returns an LLM-actionable error message tailored to the
// authentication method in use.
//
// Before CR-0067 the guidance told the LLM to call account_add, which creates
// a *new* account rather than re-authenticating the existing one — following
// it produced duplicate registry entries instead of a working session. The
// correct verb for an account that already exists is account.login; under the
// auth_code method the sign-in can additionally be finished in band with
// system.complete_auth.
//
// Parameters:
//   - err: the authentication error to format. Must not be nil.
//   - authMethod: the active method ("auth_code", "browser", "device_code"),
//     or "" when it is not known.
//
// Returns a multi-line string containing a plain-language description and
// numbered recovery steps.
//
// FormatAuthErrorFor is safe for concurrent use.
func FormatAuthErrorFor(err error, authMethod string) string {
	return classifyAuthError(err) + "\n\nTo recover:\n" + recoverySteps(authMethod)
}

// recoverySteps returns the numbered recovery instructions for the given
// authentication method.
//
// Every variant starts from account.list so the LLM discovers which account is
// disconnected instead of guessing a label, and ends by retrying the original
// request.
//
// Parameters:
//   - authMethod: the active method, or "" when it is not known.
//
// Returns the instruction block, without a trailing newline.
func recoverySteps(authMethod string) string {
	const listStep = "1. Call account with operation=\"list\" to see which account is disconnected\n"
	const retryStep = "\n3. Retry your original request"

	switch authMethod {
	case "auth_code":
		return listStep +
			"2. Call account with operation=\"login\" and that account's label. A browser opens; " +
			"after signing in, copy the full URL from the address bar. Paste it when prompted, or " +
			"call system with operation=\"complete_auth\" and redirect_url set to that URL" +
			retryStep
	case "device_code":
		return listStep +
			"2. Call account with operation=\"login\" and that account's label, then open the " +
			"sign-in link it returns and approve the request" +
			retryStep
	case "browser":
		return listStep +
			"2. Call account with operation=\"login\" and that account's label, then complete the " +
			"sign-in in the browser window that opens" +
			retryStep
	default:
		return listStep +
			"2. Call account with operation=\"login\" and that account's label to re-authenticate it. " +
			"Use account with operation=\"add\" only when the account is not registered at all" +
			retryStep
	}
}

// classifyAuthError returns a plain-language description of the authentication
// failure. Azure SDK class names are stripped from the output.
//
// Parameters:
//   - err: the authentication error to classify.
//
// Returns a human-readable description of the error.
func classifyAuthError(err error) string {
	msg := safeErrorString(err)

	// Check for context deadline / timeout errors.
	if strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "context canceled") {
		return "Authentication timed out or was cancelled for this account."
	}

	// Check for AADSTS error codes from Entra ID.
	if strings.Contains(msg, "AADSTS") {
		// Extract a sanitized version without SDK class names.
		sanitized := stripSDKClassNames(msg)
		return "Entra ID rejected the authentication request: " + sanitized
	}

	// Check for HTTP 401 via OData.
	var odataErr *odataerrors.ODataError
	if errors.As(err, &odataErr) && odataErr.ResponseStatusCode == 401 {
		return "The server received an unauthorized response from Microsoft Graph. The account token may have expired or been revoked."
	}

	// azidentity's AuthenticationRequiredError, produced when a silent-only
	// credential cannot satisfy GetToken from cache (CR-0067 A1). Its message
	// ends in "Call Authenticate to authenticate a user interactively", which
	// is SDK-level advice the LLM cannot act on and must not be shown.
	var authRequiredErr *azidentity.AuthenticationRequiredError
	if errors.As(err, &authRequiredErr) {
		return "Authentication is required for this account."
	}

	// Bare "authentication required" carries no detail, so it must be matched
	// last among the string checks. Before CR-0067 it was tested before the
	// generic branch and swallowed the flow-specific text the middleware
	// composes — for example "authentication required: device code prompt was
	// not received from Entra ID" degraded to "Authentication is required for
	// this account.", hiding the actual failure from the user.
	sanitized := stripSDKClassNames(msg)
	if detail, ok := authRequiredDetail(sanitized); ok {
		if detail == "" {
			return "Authentication is required for this account."
		}
		return "Authentication is required for this account: " + detail
	}

	// Generic auth failure — strip SDK class names.
	return "Authentication failed for this account: " + sanitized
}

// authRequiredPrefix is the marker the middleware and the credentials use to
// signal that interactive authentication is needed.
const authRequiredPrefix = "authentication required"

// authRequiredDetail splits an "authentication required" signal into the
// marker and whatever explanation follows it.
//
// The marker is located anywhere in the message rather than only at the start,
// because credentials prefix it with their own type name (for example
// "AuthCodeCredential: authentication required"). Text before the marker is
// discarded; text after it is the detail worth showing.
//
// Parameters:
//   - msg: the sanitized error message.
//
// Returns the detail text (empty when nothing follows the marker) and true
// when msg is an authentication-required signal, or "" and false when it is
// not.
func authRequiredDetail(msg string) (string, bool) {
	trimmed := strings.TrimSpace(msg)
	idx := strings.Index(strings.ToLower(trimmed), authRequiredPrefix)
	if idx < 0 {
		return "", false
	}
	detail := strings.TrimSpace(trimmed[idx+len(authRequiredPrefix):])
	detail = strings.TrimSpace(strings.TrimLeft(detail, ":-"))
	return strings.TrimRight(detail, "."), true
}

// sdkClassNames lists Azure SDK class names that must be stripped from
// user-facing error messages to avoid LLM confusion.
var sdkClassNames = []string{
	"DeviceCodeCredential",
	"InteractiveBrowserCredential",
	"AuthorizationCodeCredential",
	"ClientSecretCredential",
	"ManagedIdentityCredential",
}

// stripSDKClassNames removes known Azure SDK credential class names and
// surrounding punctuation from the error string to produce LLM-friendly
// output.
//
// Parameters:
//   - msg: the raw error message string.
//
// Returns the message with SDK class names removed and cleaned up.
func stripSDKClassNames(msg string) string {
	result := msg
	for _, name := range sdkClassNames {
		result = strings.ReplaceAll(result, name+": ", "")
		result = strings.ReplaceAll(result, name, "")
	}
	return strings.TrimSpace(result)
}

// safeErrorString returns the error message string without panicking.
// ODataError.Error() panics when GetErrorEscaped() returns nil, so this
// function extracts a safe string representation for ODataError instances
// by falling back to the embedded ApiError.Error() method.
//
// Parameters:
//   - err: the error to convert to a string.
//
// Returns the error message string.
func safeErrorString(err error) string {
	var odataErr *odataerrors.ODataError
	if errors.As(err, &odataErr) {
		if mainErr := odataErr.GetErrorEscaped(); mainErr != nil {
			code := ""
			if mainErr.GetCode() != nil {
				code = *mainErr.GetCode()
			}
			msg := ""
			if mainErr.GetMessage() != nil {
				msg = *mainErr.GetMessage()
			}
			return fmt.Sprintf("Graph API error [%s]: %s", code, msg)
		}
		return odataErr.ApiError.Error()
	}
	return err.Error()
}
