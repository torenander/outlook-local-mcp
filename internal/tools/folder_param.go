// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file holds the one-line bridge between an MCP request and folder
// reference resolution, so that every verb taking an optional folder filter
// reads it the same way: canonical name first, legacy alias second, resolved
// through ResolveFolderRef.
package tools

import (
	"context"
	"time"

	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/desek/outlook-local-mcp/internal/validate"
	"github.com/mark3labs/mcp-go/mcp"
	msgraphsdk "github.com/microsoftgraph/msgraph-sdk-go"
)

// ResolveFolderParam reads a folder reference from the first of the given
// parameter names that carries a value, validates it, and resolves it to a
// Microsoft Graph folder id.
//
// The first name is the canonical, documented parameter; later names are
// accepted aliases. An absent reference is not an error: it means the caller
// did not scope the operation to a folder, and the empty return is the
// caller's "no folder filter" case.
//
// Parameters:
//   - ctx: the request context.
//   - client: the Graph service client used for any lookup requests.
//   - retryCfg: retry configuration applied to lookup requests.
//   - timeout: per-request timeout for lookup requests.
//   - request: the MCP tool call request.
//   - names: parameter names in preference order.
//
// Returns the resolved folder id, or "" when no name carried a value.
//
// Errors: returns an error when the supplied reference fails validation or
// cannot be resolved; the message names the failing path segment and the
// candidates available at that level.
//
// Side effects: may issue folder lookup requests to the Graph API via
// ResolveFolderRef.
func ResolveFolderParam(ctx context.Context, client *msgraphsdk.GraphServiceClient, retryCfg graph.RetryConfig, timeout time.Duration, request mcp.CallToolRequest, names ...string) (string, error) {
	ref := firstStringParam(request, names...)
	if ref == "" {
		return "", nil
	}
	if err := validate.ValidateResourceID(ref, names[0]); err != nil {
		return "", err
	}
	return ResolveFolderRef(ctx, client, retryCfg, timeout, ref)
}
