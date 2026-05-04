// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file provides the update_rule mail verb handler, which updates an
// existing inbox message rule via PATCH /me/mailFolders/inbox/messageRules/{id}
// on the Microsoft Graph API (CR-0066).
package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/desek/outlook-local-mcp/internal/logging"
	"github.com/desek/outlook-local-mcp/internal/validate"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// NewHandleUpdateRule creates the MCP tool handler for mail.update_rule. It
// uses PATCH semantics: only explicitly provided fields are included in the
// request body.
//
// Parameters:
//   - retryCfg: retry configuration for transient Graph API errors.
//   - timeout: the maximum duration for a single Graph API call.
//
// Returns a handler function compatible with the MCP server AddTool signature.
func NewHandleUpdateRule(retryCfg graph.RetryConfig, timeout time.Duration) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

		args := request.GetArguments()
		rule := models.NewMessageRule()
		hasUpdate := false

		if v, ok := args["display_name"].(string); ok && v != "" {
			rule.SetDisplayName(&v)
			hasUpdate = true
		}

		if v := request.GetFloat("sequence", 0); v > 0 {
			seqInt := int32(v)
			rule.SetSequence(&seqInt)
			hasUpdate = true
		}

		if v, ok := args["is_enabled"]; ok {
			if bv, bOK := v.(bool); bOK {
				rule.SetIsEnabled(&bv)
				hasUpdate = true
			}
		}

		if v, ok := args["conditions"].(string); ok && v != "" {
			conditions, parseErr := graph.ParseRulePredicates(v)
			if parseErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid conditions JSON: %s", parseErr.Error())), nil
			}
			rule.SetConditions(conditions)
			hasUpdate = true
		}

		if v, ok := args["actions"].(string); ok && v != "" {
			actions, parseErr := graph.ParseRuleActions(v)
			if parseErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid actions JSON: %s", parseErr.Error())), nil
			}
			rule.SetActions(actions)
			hasUpdate = true
		}

		if v, ok := args["exceptions"].(string); ok && v != "" {
			exceptions, parseErr := graph.ParseRulePredicates(v)
			if parseErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid exceptions JSON: %s", parseErr.Error())), nil
			}
			rule.SetExceptions(exceptions)
			hasUpdate = true
		}

		if !hasUpdate {
			return mcp.NewToolResultError("no fields to update; provide at least one of: display_name, sequence, is_enabled, conditions, actions, exceptions"), nil
		}

		timeoutCtx, cancel := graph.WithTimeout(ctx, timeout)
		defer cancel()

		logger.Debug("graph API request",
			"endpoint", "PATCH /me/mailFolders/inbox/messageRules/{id}",
			"rule_id", ruleID)

		var updated models.MessageRuleable
		err = graph.RetryGraphCall(ctx, retryCfg, func() error {
			var graphErr error
			updated, graphErr = client.Me().MailFolders().ByMailFolderId("inbox").MessageRules().ByMessageRuleId(ruleID).Patch(timeoutCtx, rule, nil)
			return graphErr
		})
		if err != nil {
			if graph.IsTimeoutError(err) {
				logger.ErrorContext(ctx, "request timed out",
					"timeout_seconds", int(timeout.Seconds()),
					"error", err.Error())
				return mcp.NewToolResultError(graph.TimeoutErrorMessage(int(timeout.Seconds()))), nil
			}
			logger.ErrorContext(ctx, "update rule failed", "error", graph.FormatGraphError(err))
			return mcp.NewToolResultError(graph.RedactGraphError(err)), nil
		}

		logger.InfoContext(ctx, "rule updated", "rule_id", ruleID)

		response := fmt.Sprintf("Rule updated: %s\nName: %s",
			graph.SafeStr(updated.GetId()),
			graph.SafeStr(updated.GetDisplayName()))
		if line := AccountInfoLine(ctx); line != "" {
			response += "\n" + line
		}
		return mcp.NewToolResultText(response), nil
	}
}
