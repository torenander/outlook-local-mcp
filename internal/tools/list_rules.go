// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file provides the list_rules mail verb handler, which retrieves all
// inbox message rules via GET /me/mailFolders/inbox/messageRules on the
// Microsoft Graph API (CR-0066).
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/desek/outlook-local-mcp/internal/logging"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// NewHandleListRules creates the MCP tool handler for mail.list_rules. It
// calls GET /me/mailFolders/inbox/messageRules to retrieve all inbox rules.
//
// Parameters:
//   - retryCfg: retry configuration for transient Graph API errors.
//   - timeout: the maximum duration for a single Graph API call.
//
// Returns a handler function compatible with the MCP server AddTool signature.
func NewHandleListRules(retryCfg graph.RetryConfig, timeout time.Duration) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		logger := logging.Logger(ctx)
		start := time.Now()

		logger.Debug("tool called")

		client, err := GraphClient(ctx)
		if err != nil {
			return mcp.NewToolResultError("no account selected"), nil
		}

		outputMode, err := ValidateOutputMode(request)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		timeoutCtx, cancel := graph.WithTimeout(ctx, timeout)
		defer cancel()

		logger.Debug("graph API request", "endpoint", "GET /me/mailFolders/inbox/messageRules")

		var resp models.MessageRuleCollectionResponseable
		err = graph.RetryGraphCall(ctx, retryCfg, func() error {
			var graphErr error
			resp, graphErr = client.Me().MailFolders().ByMailFolderId("inbox").MessageRules().Get(timeoutCtx, nil)
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

		rules := resp.GetValue()

		if outputMode == "text" {
			logger.Info("tool completed", "duration", time.Since(start), "count", len(rules))
			return mcp.NewToolResultText(FormatRulesText(rules)), nil
		}

		// summary or raw
		results := make([]map[string]any, 0, len(rules))
		for _, rule := range rules {
			if outputMode == "summary" {
				results = append(results, graph.SerializeSummaryRule(rule))
			} else {
				results = append(results, graph.SerializeRule(rule))
			}
		}

		jsonBytes, err := json.Marshal(results)
		if err != nil {
			logger.Error("json serialization failed", "error", err.Error(), "duration", time.Since(start))
			return mcp.NewToolResultError(fmt.Sprintf("failed to serialize rules: %s", err.Error())), nil
		}

		logger.Info("tool completed", "duration", time.Since(start), "count", len(rules))
		return mcp.NewToolResultText(string(jsonBytes)), nil
	}
}
