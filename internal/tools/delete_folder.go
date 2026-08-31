// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file provides the delete_folder handler, which permanently deletes a
// mail folder via DELETE /me/mailFolders/{id} on the Microsoft Graph API.
// This is a destructive operation: the folder and all its contents (messages,
// child folders) are permanently removed.
package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/desek/outlook-local-mcp/internal/logging"
	"github.com/desek/outlook-local-mcp/internal/validate"
	"github.com/mark3labs/mcp-go/mcp"
)

// NewHandleDeleteFolder creates a tool handler that permanently deletes a mail
// folder by calling DELETE /me/mailFolders/{id} on the Graph API.
//
// Well-known folders (Inbox, Sent Items, Drafts, etc.) cannot be deleted; the
// Graph API returns HTTP 400 for those requests, and this handler surfaces
// that error without additional pre-validation.
//
// The folder is addressed by the natural-language `folder` reference — a
// well-known name, a display-name path such as "Inbox/01 Projects", a
// top-level folder name, or a raw Graph id — resolved by ResolveFolderRef.
// The legacy spelling folder_id is still accepted.
//
// Parameters:
//   - retryCfg: retry configuration for transient Graph API errors.
//   - timeout: the maximum duration for the Graph API call.
//
// Returns a handler function compatible with the MCP server's verb dispatch.
//
// Errors: returns a tool error when no account is selected, when `folder` is
// missing, when it cannot be resolved (the message names the failing path
// segment), or when the Graph delete call fails.
//
// Side effects: resolves `folder` (which may issue folder lookup requests) and
// calls DELETE /me/mailFolders/{id} on the Microsoft Graph API, permanently
// removing the folder and all its contents.
func NewHandleDeleteFolder(retryCfg graph.RetryConfig, timeout time.Duration) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		logger := logging.Logger(ctx)
		start := time.Now()

		client, err := GraphClient(ctx)
		if err != nil {
			return mcp.NewToolResultError("no account selected"), nil
		}

		folderRef := firstStringParam(request, "folder", "folder_id")
		if folderRef == "" {
			return mcp.NewToolResultError("missing required parameter: folder"), nil
		}
		if err := validate.ValidateResourceID(folderRef, "folder"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		folderID, err := ResolveFolderRef(ctx, client, retryCfg, timeout, folderRef)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		timeoutCtx, cancel := graph.WithTimeout(ctx, timeout)
		defer cancel()

		err = graph.RetryGraphCall(ctx, retryCfg, func() error {
			return client.Me().MailFolders().ByMailFolderId(folderID).Delete(timeoutCtx, nil)
		})
		if err != nil {
			if graph.IsTimeoutError(err) {
				logger.ErrorContext(ctx, "request timed out",
					"timeout_seconds", int(timeout.Seconds()),
					"error", err.Error())
				return mcp.NewToolResultError(graph.TimeoutErrorMessage(int(timeout.Seconds()))), nil
			}
			logger.ErrorContext(ctx, "delete folder failed", "error", graph.FormatGraphError(err))
			return mcp.NewToolResultError(graph.RedactGraphError(err)), nil
		}

		logger.InfoContext(ctx, "folder deleted", "folder_id", folderID, "duration", time.Since(start))

		response := fmt.Sprintf("Folder deleted: %s\nID: %s\nThe folder and all its contents have been permanently removed.", folderRef, folderID)
		if line := AccountInfoLine(ctx); line != "" {
			response += "\n" + line
		}
		return mcp.NewToolResultText(response), nil
	}
}
