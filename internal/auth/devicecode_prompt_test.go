package auth

import (
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// TestNewDeviceCodePrompt verifies that all three azidentity fields survive
// the conversion, since the sign-in URL and the elicitation message both
// depend on UserCode.
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

// TestDeviceCodePrompt_SignInURL covers the URL composition rules. The URL is
// a navigation shortcut only: Microsoft preserves the otc parameter through
// the redirect but does not pre-fill the code field, so these tests assert
// URL shape and nothing about pre-filling.
func TestDeviceCodePrompt_SignInURL(t *testing.T) {
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
			if got := tt.prompt.SignInURL(); got != tt.want {
				t.Errorf("SignInURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDeviceCodeElicitMessage_QuotesTheCode is a regression guard. URL-mode
// elicitation shows the user a link and this message and nothing else, and the
// sign-in page does not pre-fill the code — so if the message omits the code,
// the user has nothing to type. An earlier revision of CR-0067 shipped exactly
// that bug.
func TestDeviceCodeElicitMessage_QuotesTheCode(t *testing.T) {
	got := deviceCodeElicitMessage(DeviceCodePrompt{
		Message:  "To sign in, ... enter the code XYZ123 to authenticate.",
		UserCode: "XYZ123",
	})
	if !strings.Contains(got, "XYZ123") {
		t.Errorf("deviceCodeElicitMessage() = %q, must quote the user code", got)
	}
	if strings.Contains(strings.ToLower(got), "already filled in") {
		t.Errorf("deviceCodeElicitMessage() = %q, must not claim the code is pre-filled", got)
	}

	// With no code there is nothing to quote, but the message must still make
	// sense on its own.
	bare := deviceCodeElicitMessage(DeviceCodePrompt{})
	if bare == "" || strings.Contains(bare, "code ") {
		t.Errorf("deviceCodeElicitMessage() with no code = %q", bare)
	}
}
