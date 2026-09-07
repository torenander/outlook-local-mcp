// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file wraps the two Microsoft Graph calls that enumerate mail folders:
// GET /me/mailFolders (top level) and GET /me/mailFolders/{id}/childFolders
// (one level down). Both report whether Graph had more results than the caller
// asked for, so no listing is ever silently capped.
package tools

import (
	"context"
	"time"

	"github.com/desek/outlook-local-mcp/internal/graph"
	msgraphsdk "github.com/microsoftgraph/msgraph-sdk-go"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
	"github.com/microsoftgraph/msgraph-sdk-go/users"
)

// folderSelectFields is the $select projection used by every folder listing.
// childFolderCount is required so the tree builder knows whether descending is
// worthwhile without issuing a probe request per leaf.
var folderSelectFields = []string{"id", "displayName", "unreadItemCount", "totalItemCount", "childFolderCount"}

// folderLookupPageSize is the per-level page size used when resolving a folder
// path to an id. It is deliberately larger than the listing default because a
// path walk must see every sibling to find a match.
const folderLookupPageSize int32 = 200

// fetchTopLevelFolders retrieves the mailbox's top-level mail folders.
//
// Parameters:
//   - ctx: the request context, used for cancellation and retry budgeting.
//   - client: the Graph service client.
//   - retryCfg: retry configuration for transient errors and 429 backoff.
//   - timeout: the maximum duration for the Graph API call.
//   - maxResults: the per-level page size passed as $top.
//
// Returns the folders, a truncated flag that is true when Graph reported an
// @odata.nextLink (more folders exist than were returned), and any Graph error.
//
// Side effects: calls GET /me/mailFolders.
func fetchTopLevelFolders(ctx context.Context, client *msgraphsdk.GraphServiceClient, retryCfg graph.RetryConfig, timeout time.Duration, maxResults int32) ([]models.MailFolderable, bool, error) {
	top := maxResults
	cfg := &users.ItemMailFoldersRequestBuilderGetRequestConfiguration{
		QueryParameters: &users.ItemMailFoldersRequestBuilderGetQueryParameters{
			Top:    &top,
			Select: folderSelectFields,
		},
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
		return nil, false, err
	}
	return resp.GetValue(), resp.GetOdataNextLink() != nil, nil
}

// fetchChildFolders retrieves the immediate child folders of a parent folder.
//
// Parameters:
//   - ctx: the request context, used for cancellation and retry budgeting.
//   - client: the Graph service client.
//   - retryCfg: retry configuration for transient errors and 429 backoff.
//   - timeout: the maximum duration for the Graph API call.
//   - parentID: the parent folder id or Graph well-known folder name.
//   - maxResults: the per-level page size passed as $top.
//
// Returns the child folders, a truncated flag that is true when Graph reported
// an @odata.nextLink, and any Graph error.
//
// Side effects: calls GET /me/mailFolders/{parentID}/childFolders.
func fetchChildFolders(ctx context.Context, client *msgraphsdk.GraphServiceClient, retryCfg graph.RetryConfig, timeout time.Duration, parentID string, maxResults int32) ([]models.MailFolderable, bool, error) {
	top := maxResults
	cfg := &users.ItemMailFoldersItemChildFoldersRequestBuilderGetRequestConfiguration{
		QueryParameters: &users.ItemMailFoldersItemChildFoldersRequestBuilderGetQueryParameters{
			Top:    &top,
			Select: folderSelectFields,
		},
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
		return nil, false, err
	}
	return resp.GetValue(), resp.GetOdataNextLink() != nil, nil
}

// fetchFolderDisplayName retrieves just the display name of a single folder.
// It exists so that a listing addressed by opaque Graph id can still label its
// results with a human-readable path.
//
// Parameters:
//   - ctx: the request context.
//   - client: the Graph service client.
//   - retryCfg: retry configuration for transient errors.
//   - timeout: the maximum duration for the Graph API call.
//   - folderID: the folder id or well-known name to look up.
//
// Returns the display name, or an error from the Graph API.
//
// Side effects: calls GET /me/mailFolders/{folderID}?$select=displayName.
func fetchFolderDisplayName(ctx context.Context, client *msgraphsdk.GraphServiceClient, retryCfg graph.RetryConfig, timeout time.Duration, folderID string) (string, error) {
	cfg := &users.ItemMailFoldersMailFolderItemRequestBuilderGetRequestConfiguration{
		QueryParameters: &users.ItemMailFoldersMailFolderItemRequestBuilderGetQueryParameters{
			Select: []string{"id", "displayName"},
		},
	}

	timeoutCtx, cancel := graph.WithTimeout(ctx, timeout)
	defer cancel()

	var folder models.MailFolderable
	err := graph.RetryGraphCall(ctx, retryCfg, func() error {
		var graphErr error
		folder, graphErr = client.Me().MailFolders().ByMailFolderId(folderID).Get(timeoutCtx, cfg)
		return graphErr
	})
	if err != nil {
		return "", err
	}
	return graph.SafeStr(folder.GetDisplayName()), nil
}
