// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file provides the list_folder_tree handler, which recursively retrieves
// the full folder hierarchy from a given root (or all top-level folders) via
// repeated calls to GET /me/mailFolders/{id}/childFolders on the Microsoft
// Graph API.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"log/slog"

	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/desek/outlook-local-mcp/internal/logging"
	"github.com/desek/outlook-local-mcp/internal/validate"
	"github.com/mark3labs/mcp-go/mcp"
	msgraphsdk "github.com/microsoftgraph/msgraph-sdk-go"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
	"github.com/microsoftgraph/msgraph-sdk-go/users"
)

// NewHandleListFolderTree creates a tool handler that recursively lists the
// mail folder hierarchy starting from a given root folder or all top-level
// folders. Each level calls GET /me/mailFolders/{id}/childFolders, recursing
// up to max_depth levels deep.
//
// Parameters:
//   - retryCfg: retry configuration for transient Graph API errors.
//   - timeout: the maximum duration for each individual Graph API call.
//
// Returns a handler function compatible with the MCP server's verb dispatch.
//
// The handler:
//   - Retrieves the Graph client from context via GraphClient.
//   - If folder_id is provided, fetches its children; otherwise starts from
//     top-level mailFolders.
//   - Recursively descends into child folders up to max_depth levels.
//   - Returns text (tree-formatted), summary JSON, or raw JSON based on
//     output mode.
//
// Side effects: makes multiple GET calls to the Microsoft Graph API (one per
// folder level). Rate limits are handled by RetryGraphCall's 429 backoff.
func NewHandleListFolderTree(retryCfg graph.RetryConfig, timeout time.Duration) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

		folderID := request.GetString("folder_id", "")
		if folderID != "" {
			if err := validate.ValidateResourceID(folderID, "folder_id"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
		}

		maxDepthFloat := request.GetFloat("max_depth", 3)
		maxDepth := int(maxDepthFloat)
		if maxDepth < 1 {
			maxDepth = 1
		}
		if maxDepth > 10 {
			maxDepth = 10
		}

		// Fetch root folders.
		var rootFolders []models.MailFolderable
		if folderID == "" {
			rootFolders, err = fetchTopLevelFolders(ctx, client, retryCfg, timeout)
		} else {
			rootFolders, err = fetchChildFolders(ctx, client, retryCfg, timeout, folderID)
		}
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

		// Build tree recursively.
		// maxDepth represents the total number of levels to return (including the
		// root level). Passing maxDepth directly means a depth of 1 returns root
		// folders with their direct children, depth 2 returns two levels below
		// root, etc.
		tree := buildFolderTree(ctx, client, retryCfg, timeout, rootFolders, maxDepth, logger)

		if outputMode == "text" {
			logger.Info("tool completed",
				"duration", time.Since(start),
				"count", countTreeNodes(tree))
			return mcp.NewToolResultText(FormatFolderTreeText(tree)), nil
		}

		jsonBytes, err := json.Marshal(tree)
		if err != nil {
			logger.Error("json serialization failed",
				"error", err.Error(),
				"duration", time.Since(start))
			return mcp.NewToolResultError(fmt.Sprintf("failed to serialize folder tree: %s", err.Error())), nil
		}

		logger.Info("tool completed",
			"duration", time.Since(start),
			"count", countTreeNodes(tree))
		return mcp.NewToolResultText(string(jsonBytes)), nil
	}
}

// fetchTopLevelFolders retrieves all top-level mail folders for the
// authenticated user.
//
// Parameters:
//   - ctx: the request context.
//   - client: the Graph service client.
//   - retryCfg: retry configuration for transient errors.
//   - timeout: the maximum duration for the Graph API call.
//
// Returns the folder slice or an error from the Graph API.
//
// Side effects: calls GET /me/mailFolders.
func fetchTopLevelFolders(ctx context.Context, client *msgraphsdk.GraphServiceClient, retryCfg graph.RetryConfig, timeout time.Duration) ([]models.MailFolderable, error) {
	top := int32(100)
	selectFields := []string{"id", "displayName", "unreadItemCount", "totalItemCount", "childFolderCount"}
	qp := &users.ItemMailFoldersRequestBuilderGetQueryParameters{
		Top:    &top,
		Select: selectFields,
	}
	cfg := &users.ItemMailFoldersRequestBuilderGetRequestConfiguration{
		QueryParameters: qp,
	}

	timeoutCtx, cancel := graph.WithTimeout(ctx, timeout)
	defer cancel()

	var resp models.MailFolderCollectionResponseable
	err := graph.RetryGraphCall(ctx, retryCfg, func() error {
		var graphErr error
		resp, graphErr = client.Me().MailFolders().Get(timeoutCtx, cfg)
		return graphErr
	})
	if err != nil {
		return nil, err
	}
	return resp.GetValue(), nil
}

// fetchChildFolders retrieves the immediate child folders of a given parent
// folder.
//
// Parameters:
//   - ctx: the request context.
//   - client: the Graph service client.
//   - retryCfg: retry configuration for transient errors.
//   - timeout: the maximum duration for the Graph API call.
//   - parentID: the ID of the parent folder.
//
// Returns the child folder slice or an error from the Graph API.
//
// Side effects: calls GET /me/mailFolders/{parentID}/childFolders.
func fetchChildFolders(ctx context.Context, client *msgraphsdk.GraphServiceClient, retryCfg graph.RetryConfig, timeout time.Duration, parentID string) ([]models.MailFolderable, error) {
	top := int32(100)
	selectFields := []string{"id", "displayName", "unreadItemCount", "totalItemCount", "childFolderCount"}
	qp := &users.ItemMailFoldersItemChildFoldersRequestBuilderGetQueryParameters{
		Top:    &top,
		Select: selectFields,
	}
	cfg := &users.ItemMailFoldersItemChildFoldersRequestBuilderGetRequestConfiguration{
		QueryParameters: qp,
	}

	timeoutCtx, cancel := graph.WithTimeout(ctx, timeout)
	defer cancel()

	var resp models.MailFolderCollectionResponseable
	err := graph.RetryGraphCall(ctx, retryCfg, func() error {
		var graphErr error
		resp, graphErr = client.Me().MailFolders().ByMailFolderId(parentID).ChildFolders().Get(timeoutCtx, cfg)
		return graphErr
	})
	if err != nil {
		return nil, err
	}
	return resp.GetValue(), nil
}

// buildFolderTree recursively builds a nested tree of serialized folder maps.
// Each node contains "children" with the nested child folder list.
//
// Parameters:
//   - ctx: the request context.
//   - client: the Graph service client.
//   - retryCfg: retry configuration for transient errors.
//   - timeout: per-call timeout.
//   - folders: the folders at the current level.
//   - remainingDepth: how many more levels of children to fetch. Zero means
//     the current level's folders are serialized but their children are not
//     fetched.
//   - logger: structured logger for warning and debug output.
//
// Returns a slice of serialized folder maps with nested "children" arrays.
// When a child fetch fails, the node's "children" is set to an empty slice
// and an "_error" key is added with a PII-redacted error message so consumers
// can detect incomplete subtrees.
//
// Side effects: makes Graph API calls for each folder with children.
func buildFolderTree(ctx context.Context, client *msgraphsdk.GraphServiceClient, retryCfg graph.RetryConfig, timeout time.Duration, folders []models.MailFolderable, remainingDepth int, logger *slog.Logger) []map[string]any {
	results := make([]map[string]any, 0, len(folders))
	for _, folder := range folders {
		node := serializeMailFolder(folder)
		childCount := graph.SafeInt32(folder.GetChildFolderCount())
		node["childFolderCount"] = childCount

		if remainingDepth > 0 && childCount > 0 {
			folderID := graph.SafeStr(folder.GetId())
			children, err := fetchChildFolders(ctx, client, retryCfg, timeout, folderID)
			if err != nil {
				logger.WarnContext(ctx, "failed to fetch child folders, skipping subtree",
					"folder_id", folderID,
					"error", graph.FormatGraphError(err))
				node["children"] = []map[string]any{}
				node["_error"] = fmt.Sprintf("failed to load children: %s", graph.RedactGraphError(err))
			} else {
				node["children"] = buildFolderTree(ctx, client, retryCfg, timeout, children, remainingDepth-1, logger)
			}
		} else {
			node["children"] = []map[string]any{}
		}

		results = append(results, node)
	}
	return results
}

// countTreeNodes counts the total number of folders in a nested tree
// structure, including all children recursively.
//
// Parameters:
//   - tree: slice of folder maps, each potentially containing a "children" key.
//
// Returns the total count of folder nodes.
//
// Side effects: none.
func countTreeNodes(tree []map[string]any) int {
	count := len(tree)
	for _, node := range tree {
		if children, ok := node["children"].([]map[string]any); ok {
			count += countTreeNodes(children)
		}
	}
	return count
}
