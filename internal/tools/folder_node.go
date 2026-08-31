// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file defines the tier-agnostic in-memory representation of a mail
// folder hierarchy produced by the list_folders verb (CR-0066 amended).
//
// The representation is deliberately a typed struct rather than a
// map[string]any: the three output tiers (text, summary, raw) are separate
// projections of the same tree, and a typed node removes the type-assertion
// fragility that the earlier map-based tree formatter depended on (it only
// worked because the tree was never round-tripped through JSON).
package tools

// FolderNode is one mail folder in a hierarchy, decoupled from both the
// Microsoft Graph wire shape and any particular output tier.
//
// The node carries a natural-language Path in addition to the opaque Graph ID
// so that the default text tier can drop 150-character folder IDs and still
// remain chainable: any Path emitted by list_folders is accepted back by the
// `folder`, `parent`, and `destination` parameters (see ResolveFolderRef).
type FolderNode struct {
	// ID is the opaque Microsoft Graph folder identifier.
	ID string

	// Name is the folder's display name as shown in Outlook.
	Name string

	// Path is the slash-separated display-name path from the mailbox root,
	// e.g. "Inbox/01 Projects/Swedfund". It is accepted verbatim by the
	// folder-reference parameters of every folder-aware verb.
	Path string

	// Unread is the number of unread items directly in this folder.
	Unread int32

	// Total is the total number of items directly in this folder.
	Total int32

	// SubfolderCount is the number of immediate child folders reported by
	// Graph, regardless of whether Children was populated.
	SubfolderCount int32

	// Children holds the child folders that were actually fetched. It is nil
	// when recursion did not descend into this node, when the node has no
	// children, or when the child fetch failed (see Err).
	Children []FolderNode

	// Expanded records that a child fetch was attempted for this node AND
	// returned successfully. It is what separates "not looked at" from
	// "looked at, found nothing", two states that are indistinguishable from
	// Children alone and that call for opposite advice: the first is fixed by
	// recursing deeper, the second cannot be fixed by the caller at all.
	Expanded bool

	// Err is a PII-redacted message describing why this node's children could
	// not be loaded. Empty when the subtree loaded successfully or was never
	// requested. Consumers MUST surface it so incomplete subtrees are visible.
	Err string

	// Truncated is true when this node's child listing hit the per-level
	// max_results cap and Graph reported more results were available.
	Truncated bool
}

// FolderListing is the complete result of one list_folders invocation: the
// folders at the requested level, plus the metadata needed to render an
// honest, non-silently-truncated response in every output tier.
type FolderListing struct {
	// Nodes holds the folders at the requested level, each with any fetched
	// descendants attached.
	Nodes []FolderNode

	// Root is the display path of the parent folder whose children were
	// listed, or the empty string when the mailbox root (top level) was
	// listed. Used only for human-readable framing.
	Root string

	// Truncated is true when the root-level listing hit max_results and Graph
	// reported that more folders were available.
	Truncated bool

	// Recursive records whether the caller asked to descend. It selects the
	// wording of the unexplored-subfolder hint: without it the fix is
	// recursive=true, with it the fix is a larger max_depth.
	Recursive bool
}

// UnreturnedChildren reports how many immediate children Graph counted but did
// not hand back when this node was expanded.
//
// Graph routinely reports a childFolderCount that exceeds what
// GET /childFolders returns, because hidden folders are counted but withheld
// unless includeHiddenFolders=true. The Teams "Team Chat" folder under
// "Conversation History" is the common example: childFolderCount is 1 and the
// child listing is empty.
//
// Returns 0 when the node was never expanded (nothing was asked, so nothing
// can be missing), 0 when every counted child came back, and 0 when the child
// listing was truncated — paging already explains that gap, and reporting it
// as withheld as well would double-count the same missing folders under two
// incompatible explanations.
//
// Side effects: none.
func (n FolderNode) UnreturnedChildren() int {
	if !n.Expanded || n.Truncated {
		return 0
	}
	missing := int(n.SubfolderCount) - len(n.Children)
	if missing < 0 {
		return 0
	}
	return missing
}

// UnexploredChildren reports how many immediate children this node is known to
// have but was never asked about, because the listing was not recursive or the
// depth budget ran out. Unlike UnreturnedChildren, this state IS actionable by
// the caller: recursing or raising max_depth will reveal them.
//
// Returns 0 once the node has been expanded, whatever the fetch returned.
//
// Side effects: none.
func (n FolderNode) UnexploredChildren() int {
	if n.Expanded || n.SubfolderCount <= 0 {
		return 0
	}
	return int(n.SubfolderCount)
}

// CountFolders returns the total number of folders in the listing, including
// every fetched descendant.
//
// Parameters:
//   - nodes: the folders to count, each potentially carrying Children.
//
// Returns the recursive node count. Returns 0 for a nil or empty slice.
//
// Side effects: none.
func CountFolders(nodes []FolderNode) int {
	count := len(nodes)
	for i := range nodes {
		count += CountFolders(nodes[i].Children)
	}
	return count
}
