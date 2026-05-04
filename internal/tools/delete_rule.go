// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file provides the delete_rule mail verb handler, which permanently
// deletes an inbox message rule via DELETE
// /me/mailFolders/inbox/messageRules/{id} on the Microsoft Graph API (CR-0066).
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

// NewHandleDeleteRule creates the MCP tool handler for mail.delete_rule. It
// calls DELETE /me/mailFolders/inbox/messageRules/{id} to permanently remove
// an inbox rule.
//
// Parameters:
//   - retryCfg: retry configuration for transient Graph API errors.
//   - timeout: the maximum duration for a single Graph API call.
//
// Returns a handler function compatible with the MCP server AddTool signature.
func NewHandleDeleteRule(retryCfg graph.RetryConfig, timeout time.Duration) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		logger := logging.Logger(ctx)

		client, err := GraphClient(ctx)
		if err != nil {
			return mcp.NewToolResultError("no account selected"), nil
		}

		ruleID, err := request.RequireString("rule_id")
		if err != nil {
			return mcp.NewToolResultError("missing required parameter: rule_id"), nil
		}
		if err := validate.ValidateResourceID(ruleID, "rule_id"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		timeoutCtx, cancel := graph.WithTimeout(ctx, timeout)
		defer cancel()

		logger.Debug("graph API request",
			"endpoint", "DELETE /me/mailFolders/inbox/messageRules/{id}",
			"rule_id", ruleID)

		err = graph.RetryGraphCall(ctx, retryCfg, func() error {
			return client.Me().MailFolders().ByMailFolderId("inbox").MessageRules().ByMessageRuleId(ruleID).Delete(timeoutCtx, nil)
		})
		if err != nil {
			if graph.IsTimeoutError(err) {
				logger.ErrorContext(ctx, "request timed out",
					"timeout_seconds", int(timeout.Seconds()),
					"error", err.Error())
				return mcp.NewToolResultError(graph.TimeoutErrorMessage(int(timeout.Seconds()))), nil
			}
			logger.ErrorContext(ctx, "delete rule failed", "error", graph.FormatGraphError(err))
			return mcp.NewToolResultError(graph.RedactGraphError(err)), nil
		}

		logger.InfoContext(ctx, "rule deleted", "rule_id", ruleID)

		response := fmt.Sprintf("Rule deleted: %s\nThe inbox rule has been permanently removed.", ruleID)
		if line := AccountInfoLine(ctx); line != "" {
			response += "\n" + line
		}
		return mcp.NewToolResultText(response), nil
	}
}
