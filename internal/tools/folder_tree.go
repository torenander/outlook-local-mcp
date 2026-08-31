// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file converts Microsoft Graph mail folder models into the tier-agnostic
// FolderNode tree consumed by the list_folders output projections. Recursion
// is bounded by an explicit remaining-depth budget, and a failed subtree is
// recorded on the node rather than dropped, so incomplete results are always
// visible to the caller.
package tools

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/desek/outlook-local-mcp/internal/graph"
	msgraphsdk "github.com/microsoftgraph/msgraph-sdk-go"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// buildFolderTree converts one level of Graph folders into FolderNodes,
// descending into their children while depth budget remains.
//
// Parameters:
//   - ctx: the request context; cancellation aborts further descent.
//   - client: the Graph service client.
//   - retryCfg: retry configuration applied to each child fetch.
//   - timeout: per-request timeout for each child fetch.
//   - folders: the Graph folders at the current level.
//   - parentPath: the display path of the parent folder, or "" at the mailbox
//     root. Each node's Path is parentPath joined with its display name.
//   - remainingDepth: how many further levels of children to fetch. Zero means
//     the current level is converted but no children are requested, which is
//     what makes max_depth=1 mean exactly one level.
//   - maxResults: the per-level page size for child fetches.
//   - logger: structured logger used to warn about subtrees that failed.
//
// Returns the converted nodes with any fetched descendants attached.
//
// Side effects: issues one GET /me/mailFolders/{id}/childFolders per folder
// that has children while depth budget remains. Rate limiting is handled by
// RetryGraphCall's 429 backoff.
func buildFolderTree(
	ctx context.Context,
	client *msgraphsdk.GraphServiceClient,
	retryCfg graph.RetryConfig,
	timeout time.Duration,
	folders []models.MailFolderable,
	parentPath string,
	remainingDepth int,
	maxResults int32,
	logger *slog.Logger,
) []FolderNode {
	nodes := make([]FolderNode, 0, len(folders))
	for _, folder := range folders {
		node := newFolderNode(folder, parentPath)

		if remainingDepth > 0 && node.SubfolderCount > 0 {
			children, truncated, err := fetchChildFolders(ctx, client, retryCfg, timeout, node.ID, maxResults)
			if err != nil {
				logger.WarnContext(ctx, "failed to fetch child folders, subtree incomplete",
					"folder_path", node.Path,
					"error", graph.FormatGraphError(err))
				node.Err = fmt.Sprintf("could not load subfolders: %s", graph.RedactGraphError(err))
			} else {
				// Mark the node expanded even when children is empty: Graph
				// counting a child it declines to return (hidden folders) is a
				// different state from never having asked, and only the fetch
				// can tell them apart.
				node.Expanded = true
				node.Truncated = truncated
				node.Children = buildFolderTree(ctx, client, retryCfg, timeout, children, node.Path, remainingDepth-1, maxResults, logger)
			}
		}

		nodes = append(nodes, node)
	}
	return nodes
}

// newFolderNode converts a single Graph folder into a FolderNode without
// touching the network. All pointer fields are read through the graph.Safe*
// helpers so a sparsely populated model cannot panic.
//
// Parameters:
//   - folder: the Graph mail folder model.
//   - parentPath: the display path of the parent folder, or "" at the root.
//
// Returns the populated node, with Children left nil.
//
// Side effects: none.
func newFolderNode(folder models.MailFolderable, parentPath string) FolderNode {
	name := graph.SafeStr(folder.GetDisplayName())
	path := name
	if parentPath != "" {
		path = parentPath + "/" + name
	}
	return FolderNode{
		ID:             graph.SafeStr(folder.GetId()),
		Name:           name,
		Path:           path,
		Unread:         graph.SafeInt32(folder.GetUnreadItemCount()),
		Total:          graph.SafeInt32(folder.GetTotalItemCount()),
		SubfolderCount: graph.SafeInt32(folder.GetChildFolderCount()),
	}
}
