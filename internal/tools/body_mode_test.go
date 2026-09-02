// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for ValidateBodyMode, the shared validator for the
// body_mode parameter added by CR-0068.
package tools

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// bodyModeRequest builds a CallToolRequest carrying the given arguments.
func bodyModeRequest(args map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

// TestValidateBodyMode_Accepted verifies the default and every accepted value,
// including the undeclared `body` alias and the alias losing to the declared
// `body_mode` spelling when both are present.
func TestValidateBodyMode_Accepted(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"omitted defaults to preview", nil, BodyModePreview},
		{"empty string defaults to preview", map[string]any{"body_mode": ""}, BodyModePreview},
		{"explicit preview", map[string]any{"body_mode": "preview"}, BodyModePreview},
		{"explicit text", map[string]any{"body_mode": "text"}, BodyModeText},
		{"explicit full", map[string]any{"body_mode": "full"}, BodyModeFull},
		{"body alias", map[string]any{"body": "text"}, BodyModeText},
		{"body_mode wins over alias", map[string]any{"body_mode": "full", "body": "text"}, BodyModeFull},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateBodyMode(bodyModeRequest(tc.args))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("ValidateBodyMode = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestValidateBodyMode_Rejected verifies that an unrecognised value is
// rejected by name rather than silently falling back to the preview default —
// a silent fallback is exactly the failure mode CR-0068 exists to remove.
func TestValidateBodyMode_Rejected(t *testing.T) {
	for _, value := range []string{"html", "raw", "Text", "summary"} {
		t.Run(value, func(t *testing.T) {
			got, err := ValidateBodyMode(bodyModeRequest(map[string]any{"body_mode": value}))
			if err == nil {
				t.Fatalf("ValidateBodyMode(%q) = %q, want an error", value, got)
			}
			want := "body_mode must be 'preview', 'text', or 'full'"
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err.Error(), want)
			}
		})
	}
}
