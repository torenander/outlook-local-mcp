package tools

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestIsTruncatedPreview pins the boundary of the only truncation signal the
// server has. Graph returns no truncation flag, so length is the whole test,
// and CR-0068's in-band "the preview was cut off" notice is driven entirely by
// this predicate.
//
// The interesting case is exactly 255. A body whose plain-text rendering is
// exactly at the cap is indistinguishable from one Graph cut at the cap, so it
// is reported as truncated when it is not. That false positive is deliberate:
// it costs one line of notice, against silently losing the tail of every long
// message. This test exists to make the choice explicit, so that changing it
// is a decision rather than an accident.
func TestIsTruncatedPreview(t *testing.T) {
	tests := []struct {
		name  string
		runes int
		want  bool
	}{
		{"empty", 0, false},
		{"single character", 1, false},
		{"one below the cap", bodyPreviewCapRunes - 1, false},
		{"exactly at the cap is reported truncated", bodyPreviewCapRunes, true},
		{"one above the cap", bodyPreviewCapRunes + 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preview := strings.Repeat("a", tt.runes)
			if got := isTruncatedPreview(preview); got != tt.want {
				t.Errorf("isTruncatedPreview(%d chars) = %v, want %v", tt.runes, got, tt.want)
			}
		})
	}
}

// TestIsTruncatedPreview_CountsRunesNotBytes guards the cap's unit. Graph caps
// bodyPreview at 255 characters, not bytes, so a preview of non-ASCII text sits
// well past 255 bytes while still being under the cap. Counting bytes would
// mark ordinary CJK, Cyrillic or accented previews as truncated and print the
// escalation notice on messages that were never cut.
func TestIsTruncatedPreview_CountsRunesNotBytes(t *testing.T) {
	// Three bytes per rune, so 254 runes is 762 bytes -- far beyond the cap if
	// the implementation were counting bytes.
	under := strings.Repeat("日", bodyPreviewCapRunes-1)
	if got, want := utf8.RuneCountInString(under), bodyPreviewCapRunes-1; got != want {
		t.Fatalf("fixture has %d runes, want %d", got, want)
	}
	if len(under) <= bodyPreviewCapRunes {
		t.Fatalf("fixture is %d bytes, expected it to exceed the %d-char cap so the "+
			"byte/rune distinction is actually exercised", len(under), bodyPreviewCapRunes)
	}
	if isTruncatedPreview(under) {
		t.Errorf("a %d-rune (%d-byte) preview was reported truncated; the cap counts characters",
			utf8.RuneCountInString(under), len(under))
	}

	at := strings.Repeat("日", bodyPreviewCapRunes)
	if !isTruncatedPreview(at) {
		t.Errorf("a %d-rune preview was not reported truncated", utf8.RuneCountInString(at))
	}
}
