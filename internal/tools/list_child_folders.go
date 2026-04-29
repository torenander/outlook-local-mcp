// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file provides the list_child_folders handler, which retrieves the
// immediate child folders of a given parent mail folder via
// GET /me/mailFolders/{id}/childFolders on the Microsoft Graph API.
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
	"github.com/microsoftgraph/msgraph-sdk-go/users"
)

// NewHandleListChildFolders creates a tool handler that lists the immediate
// child folders of a given parent mail folder by calling
// GET /me/mailFolders/{id}/childFolders via the Graph SDK.
//
// Parameters:
//   - retryCfg: retry configuration for transient Graph API errors.
//   - timeout: the maximum duration for the Graph API call.
//
// Returns a handler function compatible with the MCP server's verb dispatch.
//
// The handler:
//   - Retrieves the Graph client from context via GraphClient.
//   - Validates the required folder_id parameter.
//   - Applies a timeout context before the Graph API call.
//   - Calls client.Me().MailFolders().ByMailFolderId(id).ChildFolders().Get()
//     with $top and $select.
//   - Serializes each folder using serializeMailFolder.
//   - Returns text, summary JSON, or raw JSON based on output mode.
//
// Side effects: calls GET /me/mailFolders/{id}/childFolders on the Microsoft
// Graph API.
func NewHandleListChildFolders(retryCfg graph.RetryConfig, timeout time.Duration) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

		folderID, err := request.RequireString("folder_id")
		if err != nil {
			return mcp.NewToolResultError("missing required parameter: folder_id"), nil
		}
		if err := validate.ValidateResourceID(folderID, "folder_id"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		maxResultsFloat := request.GetFloat("max_results", 25)
		maxResults := int32(maxResultsFloat)
		if maxResults < 1 {
			maxResults = 25
		}

		selectFields := []string{"id", "displayName", "unreadItemCount", "totalItemCount"}
		qp := &users.ItemMailFoldersItemChildFoldersRequestBuilderGetQueryParameters{
			Top:    &maxResults,
			Select: selectFields,
		}
		cfg := &users.ItemMailFoldersItemChildFoldersRequestBuilderGetRequestConfiguration{
			QueryParameters: qp,
		}

		timeoutCtx, cancel := graph.WithTimeout(ctx, timeout)
		defer cancel()

		logger.Debug("graph API request",
			"endpoint", fmt.Sprintf("GET /me/mailFolders/%s/childFolders", folderID),
			"top", maxResults)

		var resp models.MailFolderCollectionResponseable
		err = graph.RetryGraphCall(ctx, retryCfg, func() error {
			var graphErr error
			resp, graphErr = client.Me().MailFolders().ByMailFolderId(folderID).ChildFolders().Get(timeoutCtx, cfg)
			return graphErr
		})
		if err != nil {
			if graph.IsTimeoutError(err) {
				logger.ErrorContext(ctx, "request timed out",
					"timeout_seconds", int(timeout.Seconds()),
					"error", err.Error())
				return mcp.NewToolResultError(graph.TimeoutErrorMessage(int(timeout.Seconds()))), nil
			}
			logger.ErrorContext(ctx, "graph API call failed",
				"error", graph.FormatGraphError(err),
				"duration", time.Since(start))
			return mcp.NewToolResultError(graph.RedactGraphError(err)), nil
		}

		logger.Debug("graph API response",
			"endpoint", fmt.Sprintf("GET /me/mailFolders/%s/childFolders", folderID),
			"count", len(resp.GetValue()))

		folders := resp.GetValue()
		results := make([]map[string]any, 0, len(folders))
		for _, folder := range folders {
			results = append(results, serializeMailFolder(folder))
		}

		if outputMode == "text" {
			logger.Info("tool completed",
				"duration", time.Since(start),
				"count", len(results))
			return mcp.NewToolResultText(FormatMailFoldersText(results)), nil
		}

		jsonBytes, err := json.Marshal(results)
		if err != nil {
			logger.Error("json serialization failed",
				"error", err.Error(),
				"duration", time.Since(start))
			return mcp.NewToolResultError(fmt.Sprintf("failed to serialize child folders: %s", err.Error())), nil
		}

		logger.Info("tool completed",
			"duration", time.Since(start),
			"count", len(results))
		return mcp.NewToolResultText(string(jsonBytes)), nil
	}
}
