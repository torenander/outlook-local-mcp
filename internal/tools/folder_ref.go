// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file implements natural-language folder addressing (CR-0066 amended).
// Every folder-aware mail verb takes its folder argument as a *reference*
// rather than an opaque Microsoft Graph id, so an LLM can act on what it read
// in the default text output ("Inbox/01 Projects") without a second round-trip
// to recover the id.
//
// The id is worth avoiding. Mail folder ids measured against a live Microsoft
// 365 mailbox are 120 characters of URL-safe base64, for example:
//
//	AQMkADhkN2JlZQBhYi1lMjNhLTRjNDctYTVmMi0wMjhiNTliMWYyNzIALgAAAyxSSpj_kzdFju-9dZN_ICwBAIbY0YMDKXBOtEoMTonV13QAAAIBDAAAAA==
//
// Base64 tokenizes poorly — roughly 30 to 40 tokens per id — so a 40-folder
// listing spends on the order of 1,500 tokens on identifiers before a single
// folder name is transmitted. Paths cost a handful of tokens and are what the
// user actually said.
//
// A reference is resolved in this order:
//
//  1. A Graph well-known folder name ("inbox", "sentitems", ...) is passed
//     straight through; Graph accepts those wherever an id is expected.
//  2. A reference containing "/" is walked segment by segment from the mailbox
//     root, matching display names case-insensitively.
//  3. A reference that has the shape of a Graph id is passed straight through.
//  4. Anything else is matched against the top-level folder display names.
package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/desek/outlook-local-mcp/internal/graph"
	msgraphsdk "github.com/microsoftgraph/msgraph-sdk-go"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// minGraphFolderIDLen is the shortest string treated as a possible Graph
// folder id.
//
// Do not "tidy" this constant. 40 is a deliberate margin, not a guess. Mail
// folder ids measured against a live Microsoft 365 mailbox are 120 characters
// — three times the threshold — and contain no character outside the alphabet
// looksLikeGraphFolderID accepts. Real folder display names in the same
// mailbox ("Inbox", "Archive", "Conversation History", "Deleted Items") are
// under 25 characters and contain spaces, so nothing realistic sits near 40
// from the other side. Raising the threshold toward the observed 120 would
// buy nothing and would start rejecting ids from mailbox types not yet
// measured; lowering it would start capturing long display names.
const minGraphFolderIDLen = 40

// maxNamesInError is the number of sibling folder names listed in a
// resolution-failure message before the list is elided. It bounds the token
// cost of an error while still making the failure actionable.
const maxNamesInError = 20

// ResolveFolderRef resolves a user-facing folder reference to a Microsoft
// Graph folder id.
//
// The reference may be a well-known folder name ("inbox", "sentitems"), a
// display-name path from the mailbox root ("Inbox/01 Projects/Swedfund"), a
// top-level folder display name ("Archive"), or a raw Graph folder id.
//
// Parameters:
//   - ctx: the request context, used for cancellation.
//   - client: the Graph service client used for any lookup requests.
//   - retryCfg: retry configuration applied to lookup requests.
//   - timeout: per-request timeout for lookup requests.
//   - ref: the caller-supplied folder reference. Must not be blank.
//
// Returns the resolved folder id.
//
// Errors: returns an error when ref is blank, when a path segment matches no
// folder (the message names the failing segment and lists the candidates that
// were available at that level), or when a lookup request to Graph fails.
//
// Side effects: may call GET /me/mailFolders and
// GET /me/mailFolders/{id}/childFolders while walking a path.
func ResolveFolderRef(ctx context.Context, client *msgraphsdk.GraphServiceClient, retryCfg graph.RetryConfig, timeout time.Duration, ref string) (string, error) {
	id, _, err := resolveFolderRef(ctx, client, retryCfg, timeout, ref, false)
	return id, err
}

// ResolveFolderPath resolves a folder reference exactly as ResolveFolderRef
// does and additionally returns the folder's natural-language path, which the
// list_folders verb uses as the prefix for the paths it emits.
//
// Parameters: identical to ResolveFolderRef.
//
// Returns the resolved folder id and its slash-separated display path.
//
// Errors: as ResolveFolderRef, plus any error from the extra display-name
// lookup performed when the reference was given as a raw Graph id.
//
// Side effects: as ResolveFolderRef, plus one
// GET /me/mailFolders/{id}?$select=displayName when the reference was a raw id.
func ResolveFolderPath(ctx context.Context, client *msgraphsdk.GraphServiceClient, retryCfg graph.RetryConfig, timeout time.Duration, ref string) (string, string, error) {
	return resolveFolderRef(ctx, client, retryCfg, timeout, ref, true)
}

// resolveFolderRef is the shared implementation behind ResolveFolderRef and
// ResolveFolderPath. wantPath controls whether the extra display-name lookup
// for raw-id references is performed; callers that only need an id skip it.
func resolveFolderRef(ctx context.Context, client *msgraphsdk.GraphServiceClient, retryCfg graph.RetryConfig, timeout time.Duration, ref string, wantPath bool) (string, string, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "", "", fmt.Errorf("folder reference must not be empty")
	}

	if name, label, ok := wellKnownFolder(trimmed); ok {
		return name, label, nil
	}

	if strings.Contains(trimmed, "/") {
		id, path, err := walkFolderPath(ctx, client, retryCfg, timeout, trimmed)
		if err == nil {
			return id, path, nil
		}
		// A base64 folder id can itself contain "/". Fall back to treating the
		// reference as an id rather than reporting a bogus path failure.
		if !looksLikeGraphFolderID(trimmed) {
			return "", "", err
		}
		return passThroughID(ctx, client, retryCfg, timeout, trimmed, wantPath)
	}

	if looksLikeGraphFolderID(trimmed) {
		return passThroughID(ctx, client, retryCfg, timeout, trimmed, wantPath)
	}

	folders, truncated, err := fetchTopLevelFolders(ctx, client, retryCfg, timeout, folderLookupPageSize)
	if err != nil {
		return "", "", fmt.Errorf("could not list top-level folders to resolve %q: %s", trimmed, graph.RedactGraphError(err))
	}
	if match, ok := matchFolderByName(folders, trimmed); ok {
		return graph.SafeStr(match.GetId()), graph.SafeStr(match.GetDisplayName()), nil
	}
	return "", "", noMatchError(trimmed, "", folders, truncated)
}

// passThroughID accepts a reference verbatim as a Graph folder id, optionally
// resolving its display name so callers that render paths have a label.
func passThroughID(ctx context.Context, client *msgraphsdk.GraphServiceClient, retryCfg graph.RetryConfig, timeout time.Duration, id string, wantPath bool) (string, string, error) {
	if !wantPath {
		return id, "", nil
	}
	name, err := fetchFolderDisplayName(ctx, client, retryCfg, timeout, id)
	if err != nil {
		return "", "", fmt.Errorf("folder %q could not be read: %s", id, graph.RedactGraphError(err))
	}
	return id, name, nil
}

// walkFolderPath resolves a slash-separated display-name path by listing each
// level in turn. The first segment is matched against well-known names and
// top-level folders; every later segment is matched against the child folders
// of the segment before it.
//
// Returns the leaf folder's id and the canonical display path assembled from
// the matched folders (which may differ from the input in letter case).
//
// Errors: names the first segment that matched nothing, together with the
// parent path and the candidate names available at that level.
func walkFolderPath(ctx context.Context, client *msgraphsdk.GraphServiceClient, retryCfg graph.RetryConfig, timeout time.Duration, path string) (string, string, error) {
	segments := splitFolderPath(path)
	if len(segments) == 0 {
		return "", "", fmt.Errorf("folder reference %q contains no path segments", path)
	}

	var currentID string
	canonical := make([]string, 0, len(segments))

	for i, segment := range segments {
		if i == 0 {
			if name, label, ok := wellKnownFolder(segment); ok {
				currentID = name
				canonical = append(canonical, label)
				continue
			}
			folders, truncated, err := fetchTopLevelFolders(ctx, client, retryCfg, timeout, folderLookupPageSize)
			if err != nil {
				return "", "", fmt.Errorf("could not list top-level folders to resolve %q: %s", path, graph.RedactGraphError(err))
			}
			match, ok := matchFolderByName(folders, segment)
			if !ok {
				return "", "", noMatchError(segment, "", folders, truncated)
			}
			currentID = graph.SafeStr(match.GetId())
			canonical = append(canonical, graph.SafeStr(match.GetDisplayName()))
			continue
		}

		children, truncated, err := fetchChildFolders(ctx, client, retryCfg, timeout, currentID, folderLookupPageSize)
		if err != nil {
			return "", "", fmt.Errorf("could not list subfolders of %q to resolve %q: %s",
				strings.Join(canonical, "/"), path, graph.RedactGraphError(err))
		}
		match, ok := matchFolderByName(children, segment)
		if !ok {
			return "", "", noMatchError(segment, strings.Join(canonical, "/"), children, truncated)
		}
		currentID = graph.SafeStr(match.GetId())
		canonical = append(canonical, graph.SafeStr(match.GetDisplayName()))
	}

	return currentID, strings.Join(canonical, "/"), nil
}

// splitFolderPath splits a slash-separated folder path into trimmed, non-empty
// segments, so "Inbox / 01 Projects /" yields ["Inbox", "01 Projects"].
//
// Parameters:
//   - path: the raw folder path.
//
// Returns the cleaned segments; the slice is empty when path holds no content.
//
// Side effects: none.
func splitFolderPath(path string) []string {
	parts := strings.Split(path, "/")
	segments := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			segments = append(segments, trimmed)
		}
	}
	return segments
}

// matchFolderByName finds the folder whose display name equals name, ignoring
// case and surrounding whitespace.
//
// Parameters:
//   - folders: the candidate folders at one level of the hierarchy.
//   - name: the display name to match.
//
// Returns the matching folder and true, or nil and false when nothing matched.
//
// Side effects: none.
func matchFolderByName(folders []models.MailFolderable, name string) (models.MailFolderable, bool) {
	want := strings.ToLower(strings.TrimSpace(name))
	for _, f := range folders {
		if strings.ToLower(strings.TrimSpace(graph.SafeStr(f.GetDisplayName()))) == want {
			return f, true
		}
	}
	return nil, false
}

// noMatchError builds the actionable failure message for an unresolved path
// segment: which segment failed, where it was looked for, and what was there
// instead.
//
// Parameters:
//   - segment: the path segment that matched no folder.
//   - parent: the already-resolved parent path, or "" for the mailbox root.
//   - candidates: the folders that were present at that level.
//   - truncated: whether the candidate listing itself was capped by Graph.
//
// Returns the composed error.
//
// Side effects: none.
func noMatchError(segment, parent string, candidates []models.MailFolderable, truncated bool) error {
	where := "the top level"
	if parent != "" {
		where = fmt.Sprintf("%q", parent)
	}

	names := make([]string, 0, len(candidates))
	for _, f := range candidates {
		if len(names) == maxNamesInError {
			names = append(names, "...")
			break
		}
		names = append(names, fmt.Sprintf("%q", graph.SafeStr(f.GetDisplayName())))
	}

	msg := fmt.Sprintf("no folder named %q under %s", segment, where)
	if len(names) > 0 {
		msg += fmt.Sprintf("; available: %s", strings.Join(names, ", "))
	}
	if truncated {
		msg += " (folder list was truncated; the folder may exist beyond the first page)"
	}
	return fmt.Errorf("%s", msg)
}

// looksLikeGraphFolderID reports whether ref has the shape of a Microsoft
// Graph folder id: at least minGraphFolderIDLen characters, whitespace-free,
// and drawn only from the base64 alphabet plus the URL-safe substitutions.
//
// The alphabet was validated against live mailbox ids, which used only
// alphanumerics plus '_', '-' and '=' — a strict subset of what is accepted
// here. '+' and '/' are also allowed so that standard-base64 ids, should any
// mailbox type emit them, are still recognised.
//
// The check is deliberately conservative and is consulted only after path
// resolution has been attempted, so a display name that happens to look like
// an id can never shadow a real folder.
//
// Parameters:
//   - ref: the reference to classify.
//
// Returns true when ref should be passed to Graph verbatim.
//
// Side effects: none.
func looksLikeGraphFolderID(ref string) bool {
	if len(ref) < minGraphFolderIDLen {
		return false
	}
	for _, r := range ref {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '+', r == '/', r == '=', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}
