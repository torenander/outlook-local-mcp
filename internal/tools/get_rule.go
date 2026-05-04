// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file provides the get_rule mail verb handler, which retrieves a single
// inbox message rule by ID via GET /me/mailFolders/inbox/messageRules/{id}
// on the Microsoft Graph API (CR-0066).
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/desek/outlook-local-mcp/internal/logging"
	"github.com/desek/outlook-local-mcp/internal/validate"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// NewHandleGetRule creates the MCP tool handler for mail.get_rule. It calls
// GET /me/mailFolders/inbox/messageRules/{id} to retrieve a single rule.
//
// Parameters:
//   - retryCfg: retry configuration for transient Graph API errors.
//   - timeout: the maximum duration for a single Graph API call.
//
// Returns a handler function compatible with the MCP server AddTool signature.
func NewHandleGetRule(retryCfg graph.RetryConfig, timeout time.Duration) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		logger := logging.Logger(ctx)
		start := time.Now()

		logger.Debug("tool called")

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

		outputMode, err := ValidateOutputMode(request)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		timeoutCtx, cancel := graph.WithTimeout(ctx, timeout)
		defer cancel()

		logger.Debug("graph API request",
			"endpoint", "GET /me/mailFolders/inbox/messageRules/{id}",
			"rule_id", ruleID)

		var rule models.MessageRuleable
		err = graph.RetryGraphCall(ctx, retryCfg, func() error {
			var graphErr error
			rule, graphErr = client.Me().MailFolders().ByMailFolderId("inbox").MessageRules().ByMessageRuleId(ruleID).Get(timeoutCtx, nil)
			return graphErr
		})
		if err != nil {
			if graph.IsTimeoutError(err) {
				logger.ErrorContext(ctx, "request timed out",
					"timeout_seconds", int(timeout.Seconds()),
					"error", err.Error())
				return mcp.NewToolResultError(graph.TimeoutErrorMessage(int(timeout.Seconds()))), nil
			}
			logger.Error("graph API call failed",
				"error", graph.FormatGraphError(err),
				"duration", time.Since(start))
			return mcp.NewToolResultError(graph.RedactGraphError(err)), nil
		}

		if outputMode == "text" {
			logger.Info("tool completed", "duration", time.Since(start))
			return mcp.NewToolResultText(FormatRuleDetailText(rule)), nil
		}

		var result map[string]any
		if outputMode == "summary" {
			result = graph.SerializeSummaryRule(rule)
		} else {
			result = graph.SerializeRule(rule)
		}

		jsonBytes, err := json.Marshal(result)
		if err != nil {
			logger.Error("json serialization failed", "error", err.Error(), "duration", time.Since(start))
			return mcp.NewToolResultError(fmt.Sprintf("failed to serialize rule: %s", err.Error())), nil
		}

		logger.Info("tool completed", "duration", time.Since(start))
		return mcp.NewToolResultText(string(jsonBytes)), nil
	}
}
