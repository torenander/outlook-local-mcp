package auth

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestIsRecoveryOperation verifies which aggregate tools are treated as
// authentication recovery surface (CR-0067 A4).
func TestIsRecoveryOperation(t *testing.T) {
	tests := []struct {
		tool string
		want bool
	}{
		{"account", true},
		{"calendar", false},
		{"mail", false},
		{"system", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			var req mcp.CallToolRequest
			req.Params.Name = tt.tool
			if got := isRecoveryOperation(req); got != tt.want {
				t.Errorf("isRecoveryOperation(%q) = %v, want %v", tt.tool, got, tt.want)
			}
		})
	}
}
