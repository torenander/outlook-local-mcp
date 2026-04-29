// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file provides the move_message handler, which moves a single mail
// message to a different folder via POST /me/messages/{id}/move on the
// Microsoft Graph API. The move operation creates a new copy of the message
// in the destination folder and returns the new message ID.
package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/desek/outlook-local-mcp/internal/logging"
	"github.com/desek/outlook-local-mcp/internal/validate"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/microsoftgraph/msgraph-sdk-go/users"
)

// NewHandleMoveMessage creates a tool handler that moves a single mail message
// to a destination folder by calling POST /me/messages/{id}/move on the
// Graph API.
//
// Parameters:
//   - retryCfg: retry configuration for transient Graph API errors.
//   - timeout: the maximum duration for the Graph API call.
//
// Returns a handler function compatible with the MCP server's verb dispatch.
//
// The handler:
//   - Retrieves the Graph client from context via GraphClient.
//   - Validates both message_id and destination_folder_id parameters.
//   - Constructs the move request body with the destination folder ID.
//   - Calls POST /me/messages/{id}/move.
//   - Returns a text confirmation with the new message ID.
//
// Side effects: calls POST /me/messages/{id}/move on the Microsoft Graph API,
// relocating the message to a new folder.
func NewHandleMoveMessage(retryCfg graph.RetryConfig, timeout time.Duration) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		logger := logging.Logger(ctx)
		start := time.Now()

		client, err := GraphClient(ctx)
		if err != nil {
			return mcp.NewToolResultError("no account selected"), nil
		}

		messageID, err := request.RequireString("message_id")
		if err != nil {
			return mcp.NewToolResultError("missing required parameter: message_id"), nil
		}
		if err := validate.ValidateResourceID(messageID, "message_id"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		destFolderID, err := request.RequireString("destination_folder_id")
		if err != nil {
			return mcp.NewToolResultError("missing required parameter: destination_folder_id"), nil
		}
		if err := validate.ValidateResourceID(destFolderID, "destination_folder_id"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body := users.NewItemMessagesItemMovePostRequestBody()
		body.SetDestinationId(&destFolderID)

		timeoutCtx, cancel := graph.WithTimeout(ctx, timeout)
		defer cancel()

		logger.DebugContext(ctx, "graph API request",
			"endpoint", fmt.Sprintf("POST /me/messages/%s/move", messageID),
			"destination_folder_id", destFolderID)

		var newMessageID string
		err = graph.RetryGraphCall(ctx, retryCfg, func() error {
			moved, gErr := client.Me().Messages().ByMessageId(messageID).Move().Post(timeoutCtx, body, nil)
			if gErr != nil {
				return gErr
			}
			newMessageID = graph.SafeStr(moved.GetId())
			return nil
		})
		if err != nil {
			if graph.IsTimeoutError(err) {
				logger.ErrorContext(ctx, "request timed out",
					"timeout_seconds", int(timeout.Seconds()),
					"error", err.Error())
				return mcp.NewToolResultError(graph.TimeoutErrorMessage(int(timeout.Seconds()))), nil
			}
			logger.ErrorContext(ctx, "move message failed", "error", graph.FormatGraphError(err))
			return mcp.NewToolResultError(graph.RedactGraphError(err)), nil
		}

		logger.InfoContext(ctx, "message moved",
			"old_id", messageID,
			"new_id", newMessageID,
			"destination_folder_id", destFolderID,
			"duration", time.Since(start))

		response := fmt.Sprintf("Message moved successfully.\nOriginal ID: %s\nNew ID: %s\nDestination folder: %s", messageID, newMessageID, destFolderID)
		if line := AccountInfoLine(ctx); line != "" {
			response += "\n" + line
		}
		return mcp.NewToolResultText(response), nil
	}
}
