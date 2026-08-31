// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file provides the list_folders handler: the single folder-browsing verb
// of the mail domain (CR-0066 amended). It replaces the former trio of
// list_folders (top level only), list_child_folders (one level down) and
// list_folder_tree (recursive), which between them occupied three slots in the
// mail tool's operation enum and description for one capability.
//
// The three collapse into one because they only ever differed by two inputs:
// where to start (`folder`) and how far to go (`recursive` / `max_depth`).
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

// Folder listing bounds. maxResults is applied per hierarchy level, and the
// depth budget caps how many Graph round-trips a single recursive call can
// generate.
const (
	// defaultFolderMaxResults is the per-level page size used when the caller
	// does not supply max_results.
	defaultFolderMaxResults = 100

	// maxFolderMaxResults is the largest per-level page size accepted.
	maxFolderMaxResults = 1000

	// defaultFolderMaxDepth is the number of hierarchy levels returned by a
	// recursive listing when the caller does not supply max_depth.
	defaultFolderMaxDepth = 3

	// maxFolderMaxDepth is the deepest recursive listing accepted, bounding the
	// number of Graph calls a single invocation can make.
	maxFolderMaxDepth = 10
)

// NewHandleListFolders creates the handler for the mail domain's list_folders
// verb, which browses the mail folder hierarchy.
//
// Behaviour is driven by four optional parameters:
//
//   - folder: where to start. A well-known name ("inbox"), a display-name path
//     ("Inbox/01 Projects"), a top-level folder name, or a raw Graph id. When
//     omitted the mailbox's top-level folders are listed. Accepts folder_id as
//     a legacy alias.
//   - recursive: descend into subfolders. Defaults to false, i.e. one level.
//   - max_depth: how many levels a recursive listing returns, counting the
//     requested level itself, so max_depth=1 is exactly one level.
//   - max_results: per-level page size. When Graph reports more folders than
//     were returned, the response says so rather than truncating silently.
//
// Parameters:
//   - retryCfg: retry configuration for transient Graph API errors and 429s.
//   - timeout: the maximum duration for each individual Graph API call.
//
// Returns a handler function compatible with the MCP server's verb dispatch.
//
// Errors: returns a tool error when no account is selected, when output is not
// one of text/summary/raw, when `folder` cannot be resolved (the message names
// the failing path segment), or when the Graph listing call fails.
//
// Side effects: calls GET /me/mailFolders or
// GET /me/mailFolders/{id}/childFolders, once per level descended.
func NewHandleListFolders(retryCfg graph.RetryConfig, timeout time.Duration) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

		maxResults := clampInt32(request.GetFloat("max_results", defaultFolderMaxResults), 1, maxFolderMaxResults, defaultFolderMaxResults)
		recursive := request.GetBool("recursive", false)

		// max_depth counts the requested level itself, so the recursion budget
		// below the requested level is one less. Without recursive=true the
		// listing is always exactly one level.
		depth := 1
		if recursive {
			depth = int(clampInt32(request.GetFloat("max_depth", defaultFolderMaxDepth), 1, maxFolderMaxDepth, defaultFolderMaxDepth))
		}

		// Resolve the starting point. An empty reference means the mailbox root.
		rootID, rootPath := "", ""
		if ref := firstStringParam(request, "folder", "folder_id"); ref != "" {
			rootID, rootPath, err = ResolveFolderPath(ctx, client, retryCfg, timeout, ref)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
		}

		var folders []models.MailFolderable
		var truncated bool
		if rootID == "" {
			folders, truncated, err = fetchTopLevelFolders(ctx, client, retryCfg, timeout, maxResults)
		} else {
			folders, truncated, err = fetchChildFolders(ctx, client, retryCfg, timeout, rootID, maxResults)
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

		listing := FolderListing{
			Nodes:     buildFolderTree(ctx, client, retryCfg, timeout, folders, rootPath, depth-1, maxResults, logger),
			Root:      rootPath,
			Truncated: truncated,
			Recursive: recursive,
		}

		logger.Info("tool completed",
			"duration", time.Since(start),
			"count", CountFolders(listing.Nodes),
			"recursive", recursive,
			"truncated", truncated)

		if outputMode == "text" {
			return mcp.NewToolResultText(FormatFolderTreeText(listing)), nil
		}

		payload := SerializeSummaryFolders(listing)
		if outputMode == "raw" {
			payload = SerializeRawFolders(listing)
		}
		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			logger.Error("json serialization failed",
				"error", err.Error(),
				"duration", time.Since(start))
			return mcp.NewToolResultError(fmt.Sprintf("failed to serialize folders: %s", err.Error())), nil
		}
		return mcp.NewToolResultText(string(jsonBytes)), nil
	}
}

// clampInt32 converts a float parameter value to an int32 constrained to
// [minValue, maxValue], substituting fallback for values below the minimum
// (which is how an omitted-or-nonsensical numeric argument is handled).
//
// Parameters:
//   - value: the raw parameter value as received from the MCP request.
//   - minValue: the smallest accepted value.
//   - maxValue: the largest accepted value; larger values are clamped down.
//   - fallback: the value substituted when value is below minValue.
//
// Returns the constrained value.
//
// Side effects: none.
func clampInt32(value float64, minValue, maxValue, fallback int32) int32 {
	v := int32(value)
	if v < minValue {
		return fallback
	}
	if v > maxValue {
		return maxValue
	}
	return v
}
