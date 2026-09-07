// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file holds the small helper used to accept more than one spelling of a
// parameter. It exists so that renaming a parameter to natural language (for
// example folder_id -> folder) never breaks a caller that learned the old name
// from an earlier tool schema.
package tools

import (
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// firstStringParam returns the first non-blank string argument found among the
// given parameter names, searched in order, with surrounding whitespace
// trimmed.
//
// The first name is the canonical, documented one; later names are accepted
// aliases retained for backward compatibility.
//
// Parameters:
//   - request: the MCP tool call request.
//   - names: parameter names in preference order.
//
// Returns the first non-blank value, or "" when none of the names carries one.
//
// Side effects: none.
func firstStringParam(request mcp.CallToolRequest, names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(request.GetString(name, "")); v != "" {
			return v
		}
	}
	return ""
}
