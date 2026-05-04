// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file provides the create_rule mail verb handler, which creates a new
// inbox message rule via POST /me/mailFolders/inbox/messageRules on the
// Microsoft Graph API (CR-0066).
package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/desek/outlook-local-mcp/internal/logging"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// NewHandleCreateRule creates the MCP tool handler for mail.create_rule. It
// parses JSON conditions and actions from the request, builds a MessageRule,
// and calls POST /me/mailFolders/inbox/messageRules.
//
// Parameters:
//   - retryCfg: retry configuration for transient Graph API errors.
//   - timeout: the maximum duration for a single Graph API call.
//
// Returns a handler function compatible with the MCP server AddTool signature.
func NewHandleCreateRule(retryCfg graph.RetryConfig, timeout time.Duration) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		logger := logging.Logger(ctx)

		client, err := GraphClient(ctx)
		if err != nil {
			return mcp.NewToolResultError("no account selected"), nil
		}

		displayName, err := request.RequireString("display_name")
		if err != nil {
			return mcp.NewToolResultError("missing required parameter: display_name"), nil
		}

		conditionsJSON, err := request.RequireString("conditions")
		if err != nil {
			return mcp.NewToolResultError("missing required parameter: conditions"), nil
		}

		actionsJSON, err := request.RequireString("actions")
		if err != nil {
			return mcp.NewToolResultError("missing required parameter: actions"), nil
		}

		conditions, err := graph.ParseRulePredicates(conditionsJSON)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid conditions JSON: %s", err.Error())), nil
		}

		actions, err := graph.ParseRuleActions(actionsJSON)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid actions JSON: %s", err.Error())), nil
		}

		rule := models.NewMessageRule()
		rule.SetDisplayName(&displayName)
		rule.SetConditions(conditions)
		rule.SetActions(actions)

		args := request.GetArguments()

		if seq := request.GetFloat("sequence", 0); seq > 0 {
			seqInt := int32(seq)
			rule.SetSequence(&seqInt)
		}

		// is_enabled defaults to true if not provided.
		if isEnabledRaw, ok := args["is_enabled"]; ok {
			if v, vOK := isEnabledRaw.(bool); vOK {
				rule.SetIsEnabled(&v)
			}
		}

		if exceptionsJSON, ok := args["exceptions"].(string); ok && exceptionsJSON != "" {
			exceptions, parseErr := graph.ParseRulePredicates(exceptionsJSON)
			if parseErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid exceptions JSON: %s", parseErr.Error())), nil
			}
			rule.SetExceptions(exceptions)
		}

		timeoutCtx, cancel := graph.WithTimeout(ctx, timeout)
		defer cancel()

		logger.Debug("graph API request", "endpoint", "POST /me/mailFolders/inbox/messageRules")

		var created models.MessageRuleable
		err = graph.RetryGraphCall(ctx, retryCfg, func() error {
			var graphErr error
			created, graphErr = client.Me().MailFolders().ByMailFolderId("inbox").MessageRules().Post(timeoutCtx, rule, nil)
			return graphErr
		})
		if err != nil {
			if graph.IsTimeoutError(err) {
				logger.ErrorContext(ctx, "request timed out",
					"timeout_seconds", int(timeout.Seconds()),
					"error", err.Error())
				return mcp.NewToolResultError(graph.TimeoutErrorMessage(int(timeout.Seconds()))), nil
			}
			logger.ErrorContext(ctx, "create rule failed", "error", graph.FormatGraphError(err))
			return mcp.NewToolResultError(graph.RedactGraphError(err)), nil
		}

		logger.InfoContext(ctx, "rule created",
			"rule_id", graph.SafeStr(created.GetId()),
			"display_name", displayName)

		response := fmt.Sprintf("Rule created: %s\nName: %s\nSequence: %d\nThe rule is now active on the Inbox folder.",
			graph.SafeStr(created.GetId()),
			graph.SafeStr(created.GetDisplayName()),
			graph.SafeInt32(created.GetSequence()))
		if line := AccountInfoLine(ctx); line != "" {
			response += "\n" + line
		}
		return mcp.NewToolResultText(response), nil
	}
}
