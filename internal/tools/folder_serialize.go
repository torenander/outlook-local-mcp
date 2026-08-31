// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file holds the two JSON projections of a folder listing. Together with
// the markdown formatter in text_format.go they make the three output tiers of
// list_folders genuinely distinct (CR-0066 FR-9), where previously `summary`
// and `raw` produced byte-identical payloads.
//
// The split is deliberate:
//
//   - summary speaks the tool's own vocabulary (name, unread, total, path) and
//     omits everything that is zero-information, for programmatic reasoning.
//   - raw speaks Microsoft Graph's vocabulary (displayName, unreadItemCount,
//     childFolderCount) and omits nothing, for debugging against the API.
package tools

// SerializeSummaryFolders projects a folder listing into the compact `summary`
// tier: renamed, tool-native keys with empty values omitted.
//
// Parameters:
//   - listing: the folder listing to project.
//
// Returns a map with "folders" (the projected nodes), "count" (the recursive
// folder total) and, when the root listing was capped, "truncated": true.
//
// Side effects: none.
func SerializeSummaryFolders(listing FolderListing) map[string]any {
	out := map[string]any{
		"folders": summaryNodes(listing.Nodes),
		"count":   CountFolders(listing.Nodes),
	}
	if listing.Truncated {
		out["truncated"] = true
	}
	if listing.Root != "" {
		out["parent"] = listing.Root
	}
	return out
}

// summaryNodes projects a slice of nodes into summary-tier maps, recursing
// into children. Returns an empty (non-nil) slice for an empty input so the
// JSON encoder emits [] rather than null.
func summaryNodes(nodes []FolderNode) []map[string]any {
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		m := map[string]any{
			"name":            n.Name,
			"path":            n.Path,
			"unread":          n.Unread,
			"total":           n.Total,
			"subfolder_count": n.SubfolderCount,
			"id":              n.ID,
		}
		if len(n.Children) > 0 {
			m["children"] = summaryNodes(n.Children)
		}
		if n.Truncated {
			m["truncated"] = true
		}
		if n.Err != "" {
			m["error"] = n.Err
		}
		out = append(out, m)
	}
	return out
}

// SerializeRawFolders projects a folder listing into the `raw` tier: the
// unmodified Microsoft Graph field names and collection shape, with nothing
// elided.
//
// Parameters:
//   - listing: the folder listing to project.
//
// Returns a map shaped like a Graph mailFolder collection response: "value"
// holds the folders, each nesting its fetched children under the Graph
// navigation-property name "childFolders". "_truncated" is added when Graph
// reported more results than were returned, because dropping that fact would
// make raw output silently incomplete.
//
// Side effects: none.
func SerializeRawFolders(listing FolderListing) map[string]any {
	out := map[string]any{
		"value": rawNodes(listing.Nodes),
	}
	if listing.Truncated {
		out["_truncated"] = true
	}
	return out
}

// rawNodes projects a slice of nodes into Graph-shaped maps, recursing into
// children. Every field is always present, matching the raw tier's contract
// that nothing is filtered out.
func rawNodes(nodes []FolderNode) []map[string]any {
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		m := map[string]any{
			"id":               n.ID,
			"displayName":      n.Name,
			"unreadItemCount":  n.Unread,
			"totalItemCount":   n.Total,
			"childFolderCount": n.SubfolderCount,
		}
		if len(n.Children) > 0 {
			m["childFolders"] = rawNodes(n.Children)
		}
		if n.Truncated {
			m["_truncated"] = true
		}
		if n.Err != "" {
			m["_error"] = n.Err
		}
		out = append(out, m)
	}
	return out
}
