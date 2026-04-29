// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file provides the create_folder handler, which creates a new mail folder
// via POST /me/mailFolders (top-level) or POST /me/mailFolders/{id}/childFolders
// (nested) on the Microsoft Graph API.
package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/desek/outlook-local-mcp/internal/logging"
	"github.com/desek/outlook-local-mcp/internal/validate"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// NewHandleCreateFolder creates a tool handler that creates a new mail folder
// via the Microsoft Graph API. When parent_folder_id is provided, the folder
// is created as a child of that parent; otherwise it is created at the
// top level under msgFolderRoot.
//
// Parameters:
//   - retryCfg: retry configuration for transient Graph API errors.
//   - timeout: the maximum duration for the Graph API call.
//
// Returns a handler function compatible with the MCP server's verb dispatch.
//
// The handler:
//   - Retrieves the Graph client from context via GraphClient.
//   - Validates the required display_name parameter.
//   - Optionally validates parent_folder_id if provided.
//   - Constructs a MailFolder model with the display name.
//   - POSTs to the appropriate endpoint based on parent_folder_id presence.
//   - Returns a text confirmation with the new folder's ID and display name.
//
// Side effects: calls POST /me/mailFolders or
// POST /me/mailFolders/{id}/childFolders on the Microsoft Graph API.
func NewHandleCreateFolder(retryCfg graph.RetryConfig, timeout time.Duration) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		logger := logging.Logger(ctx)
		start := time.Now()

		logger.Debug("tool called")

		client, err := GraphClient(ctx)
		if err != nil {
			return mcp.NewToolResultError("no account selected"), nil
		}

		displayName, err := request.RequireString("display_name")
		if err != nil {
			return mcp.NewToolResultError("missing required parameter: display_name"), nil
		}
		if strings.TrimSpace(displayName) == "" {
			return mcp.NewToolResultError("display_name must not be empty or whitespace-only"), nil
		}
		if err := validate.ValidateStringLength(displayName, "display_name", 255); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		parentFolderID := request.GetString("parent_folder_id", "")
		if parentFolderID != "" {
			if err := validate.ValidateResourceID(parentFolderID, "parent_folder_id"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
		}

		folder := models.NewMailFolder()
		folder.SetDisplayName(&displayName)

		timeoutCtx, cancel := graph.WithTimeout(ctx, timeout)
		defer cancel()

		var created models.MailFolderable
		if parentFolderID != "" {
			logger.DebugContext(ctx, "graph API request",
				"endpoint", fmt.Sprintf("POST /me/mailFolders/%s/childFolders", parentFolderID))

			err = graph.RetryGraphCall(ctx, retryCfg, func() error {
				var gErr error
				created, gErr = client.Me().MailFolders().ByMailFolderId(parentFolderID).ChildFolders().Post(timeoutCtx, folder, nil)
				return gErr
			})
		} else {
			logger.DebugContext(ctx, "graph API request",
				"endpoint", "POST /me/mailFolders")

			err = graph.RetryGraphCall(ctx, retryCfg, func() error {
				var gErr error
				created, gErr = client.Me().MailFolders().Post(timeoutCtx, folder, nil)
				return gErr
			})
		}

		if err != nil {
			if graph.IsTimeoutError(err) {
				logger.ErrorContext(ctx, "request timed out",
					"timeout_seconds", int(timeout.Seconds()),
					"error", err.Error())
				return mcp.NewToolResultError(graph.TimeoutErrorMessage(int(timeout.Seconds()))), nil
			}
			logger.ErrorContext(ctx, "create folder failed", "error", graph.FormatGraphError(err))
			return mcp.NewToolResultError(graph.RedactGraphError(err)), nil
		}

		folderID := graph.SafeStr(created.GetId())
		folderName := graph.SafeStr(created.GetDisplayName())
		logger.InfoContext(ctx, "folder created",
			"folder_id", folderID,
			"display_name", folderName,
			"duration", time.Since(start))

		response := fmt.Sprintf("Folder created: %s\nID: %s", folderName, folderID)
		if parentFolderID != "" {
			response += fmt.Sprintf("\nParent folder: %s", parentFolderID)
		}
		if line := AccountInfoLine(ctx); line != "" {
			response += "\n" + line
		}
		return mcp.NewToolResultText(response), nil
	}
}
