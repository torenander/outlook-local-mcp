// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file provides the move_messages handler, which moves multiple mail
// messages to a destination folder by iterating over each message ID and
// calling POST /me/messages/{id}/move on the Microsoft Graph API. The handler
// does not abort on the first failure; instead it reports per-message outcomes.
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
	"github.com/microsoftgraph/msgraph-sdk-go/users"
)

// maxBatchMoveMessages is the maximum number of message IDs that can be
// provided in a single move_messages call to prevent abuse and excessive
// Graph API calls.
const maxBatchMoveMessages = 50

// NewHandleMoveMessages creates a tool handler that moves multiple mail
// messages to a destination folder by iterating over each message ID and
// calling POST /me/messages/{id}/move on the Graph API.
//
// The handler processes all messages regardless of individual failures,
// reporting per-message success or failure in the output.
//
// Parameters:
//   - retryCfg: retry configuration for transient Graph API errors.
//   - timeout: the maximum duration for each individual Graph API call.
//
// Returns a handler function compatible with the MCP server's verb dispatch.
//
// The handler:
//   - Validates message_ids (comma-separated, max 50) and
//     destination_folder_id.
//   - Iterates over each message ID, calling the move endpoint.
//   - Collects per-message results (success with new ID, or failure reason).
//   - Returns a text summary of all outcomes.
//
// Side effects: calls POST /me/messages/{id}/move on the Microsoft Graph API
// for each message ID. Failed moves do not prevent subsequent messages from
// being processed.
func NewHandleMoveMessages(retryCfg graph.RetryConfig, timeout time.Duration) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		logger := logging.Logger(ctx)
		start := time.Now()

		logger.Debug("tool called")

		client, err := GraphClient(ctx)
		if err != nil {
			return mcp.NewToolResultError("no account selected"), nil
		}

		messageIDsRaw, err := request.RequireString("message_ids")
		if err != nil {
			return mcp.NewToolResultError("missing required parameter: message_ids"), nil
		}

		destFolderID, err := request.RequireString("destination_folder_id")
		if err != nil {
			return mcp.NewToolResultError("missing required parameter: destination_folder_id"), nil
		}
		if err := validate.ValidateResourceID(destFolderID, "destination_folder_id"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Parse comma-separated message IDs.
		messageIDs := parseMessageIDs(messageIDsRaw)
		if len(messageIDs) == 0 {
			return mcp.NewToolResultError("message_ids must contain at least one message ID"), nil
		}
		if len(messageIDs) > maxBatchMoveMessages {
			return mcp.NewToolResultError(fmt.Sprintf("message_ids must contain at most %d message IDs", maxBatchMoveMessages)), nil
		}

		// Validate each message ID.
		for _, id := range messageIDs {
			if err := validate.ValidateResourceID(id, "message_ids"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
		}

		logger.DebugContext(ctx, "batch move started",
			"count", len(messageIDs),
			"destination_folder_id", destFolderID)

		// Process each message.
		var succeeded, failed int
		var b strings.Builder
		for i, msgID := range messageIDs {
			// Check for parent context cancellation between iterations.
			if ctx.Err() != nil {
				fmt.Fprintf(&b, "%d. SKIPPED: %s — context cancelled\n", i+1, msgID)
				failed++
				continue
			}
			body := users.NewItemMessagesItemMovePostRequestBody()
			body.SetDestinationId(&destFolderID)

			timeoutCtx, cancel := graph.WithTimeout(ctx, timeout)

			var newID string
			moveErr := graph.RetryGraphCall(ctx, retryCfg, func() error {
				moved, gErr := client.Me().Messages().ByMessageId(msgID).Move().Post(timeoutCtx, body, nil)
				if gErr != nil {
					return gErr
				}
				newID = graph.SafeStr(moved.GetId())
				return nil
			})
			cancel()

			if moveErr != nil {
				failed++
				fmt.Fprintf(&b, "%d. FAILED: %s — %s\n", i+1, msgID, graph.RedactGraphError(moveErr))
				logger.WarnContext(ctx, "move message failed in batch",
					"message_id", msgID,
					"error", graph.FormatGraphError(moveErr))
			} else {
				succeeded++
				fmt.Fprintf(&b, "%d. OK: %s -> %s\n", i+1, msgID, newID)
			}
		}

		logger.InfoContext(ctx, "batch move completed",
			"succeeded", succeeded,
			"failed", failed,
			"total", len(messageIDs),
			"duration", time.Since(start))

		summary := fmt.Sprintf("Moved %d of %d message(s) to folder %s.\n\n%s",
			succeeded, len(messageIDs), destFolderID, b.String())
		if line := AccountInfoLine(ctx); line != "" {
			summary += "\n" + line
		}

		// Signal total failure as an MCP error so LLM consumers can distinguish
		// between partial success and complete failure.
		if succeeded == 0 && failed > 0 {
			return mcp.NewToolResultError(summary), nil
		}
		return mcp.NewToolResultText(summary), nil
	}
}

// parseMessageIDs splits a comma-separated string of message IDs into a
// trimmed, non-empty slice.
//
// Parameters:
//   - raw: comma-separated message IDs (e.g., "id1,id2,id3").
//
// Returns a slice of trimmed, non-empty message ID strings.
//
// Side effects: none.
func parseMessageIDs(raw string) []string {
	parts := strings.Split(raw, ",")
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	return ids
}
