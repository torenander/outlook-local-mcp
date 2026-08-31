package auth

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// TestNewDeviceCodePrompt verifies that all three azidentity fields survive
// the conversion, since the one-click URL depends on UserCode.
func TestNewDeviceCodePrompt(t *testing.T) {
	got := NewDeviceCodePrompt(azidentity.DeviceCodeMessage{
		Message:         "To sign in, use a web browser to open https://microsoft.com/devicelogin and enter the code ABCD1234",
		UserCode:        "ABCD1234",
		VerificationURL: "https://microsoft.com/devicelogin",
	})

	if got.UserCode != "ABCD1234" {
		t.Errorf("UserCode = %q, want %q", got.UserCode, "ABCD1234")
	}
	if got.VerificationURL != "https://microsoft.com/devicelogin" {
		t.Errorf("VerificationURL = %q, want the device login page", got.VerificationURL)
	}
	if got.Message == "" {
		t.Error("Message must be preserved verbatim for the non-elicitation fallback")
	}
}

// TestDeviceCodePrompt_OneClickURL covers the URL composition rules.
func TestDeviceCodePrompt_OneClickURL(t *testing.T) {
	tests := []struct {
		name   string
		prompt DeviceCodePrompt
		want   string
	}{
		{
			name:   "code appended to verification url",
			prompt: DeviceCodePrompt{UserCode: "ABCD1234", VerificationURL: "https://microsoft.com/devicelogin"},
			want:   "https://microsoft.com/devicelogin?otc=ABCD1234",
		},
		{
			name:   "missing verification url falls back to the known page",
			prompt: DeviceCodePrompt{UserCode: "ABCD1234"},
			want:   "https://microsoft.com/devicelogin?otc=ABCD1234",
		},
		{
			name:   "no user code yields the bare page",
			prompt: DeviceCodePrompt{VerificationURL: "https://microsoft.com/devicelogin"},
			want:   "https://microsoft.com/devicelogin",
		},
		{
			name:   "existing query parameters are preserved",
			prompt: DeviceCodePrompt{UserCode: "ABCD1234", VerificationURL: "https://login.example.test/dl?tenant=contoso"},
			want:   "https://login.example.test/dl?otc=ABCD1234&tenant=contoso",
		},
		{
			name:   "empty prompt yields the default page",
			prompt: DeviceCodePrompt{},
			want:   "https://microsoft.com/devicelogin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.prompt.OneClickURL(); got != tt.want {
				t.Errorf("OneClickURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
