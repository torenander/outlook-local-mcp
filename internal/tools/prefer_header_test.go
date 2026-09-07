// Package tools provides MCP tool definitions and handler constructors for the
// Outlook Calendar MCP Server.
//
// This file contains tests for the Prefer header composition helpers added by
// CR-0068. The load-bearing property is that multiple preferences arrive as a
// single header value: Kiota's RequestHeaders.Add stores repeated values in a
// set and the HTTP adapter emits one header line per stored value, so a naive
// second Add would produce two Prefer lines in non-deterministic order.
package tools

import "testing"

// TestBodyContentTypePreference verifies that only body_mode=text asks Graph to
// convert the body. preview needs no body at all, and full wants the body
// exactly as stored.
func TestBodyContentTypePreference(t *testing.T) {
	cases := map[string]string{
		BodyModePreview: "",
		BodyModeFull:    "",
		BodyModeText:    `outlook.body-content-type="text"`,
	}
	for mode, want := range cases {
		if got := bodyContentTypePreference(mode); got != want {
			t.Errorf("bodyContentTypePreference(%q) = %q, want %q", mode, got, want)
		}
	}
}

// TestNewPreferHeaders_NilWhenNothingToSend verifies that a call with no
// non-empty preference returns nil, so callers can assign the result to a
// request configuration's Headers field unconditionally.
func TestNewPreferHeaders_NilWhenNothingToSend(t *testing.T) {
	if h := newPreferHeaders(); h != nil {
		t.Errorf("newPreferHeaders() = %v, want nil", h)
	}
	if h := newPreferHeaders("", ""); h != nil {
		t.Errorf("newPreferHeaders(\"\", \"\") = %v, want nil", h)
	}
}

// TestNewPreferHeaders_SingleValue verifies the one-preference case.
func TestNewPreferHeaders_SingleValue(t *testing.T) {
	h := newPreferHeaders(preferBodyContentTypeText)
	if h == nil {
		t.Fatal("newPreferHeaders returned nil for a non-empty preference")
	}
	values := h.Get("Prefer")
	if len(values) != 1 || values[0] != preferBodyContentTypeText {
		t.Errorf("Prefer values = %v, want [%q]", values, preferBodyContentTypeText)
	}
}

// TestNewPreferHeaders_CombinesIntoOneValue verifies that several preferences
// are joined into one comma-separated header value rather than stored as
// separate values that the adapter would emit as separate header lines. A
// timezone preference silently dropped or reordered behind a body preference
// would be a regression that no test elsewhere would catch.
func TestNewPreferHeaders_CombinesIntoOneValue(t *testing.T) {
	h := newPreferHeaders(`outlook.timezone="Europe/Stockholm"`, "", preferBodyContentTypeText)
	if h == nil {
		t.Fatal("newPreferHeaders returned nil")
	}
	values := h.Get("Prefer")
	if len(values) != 1 {
		t.Fatalf("Prefer produced %d values (%v), want exactly 1 combined value", len(values), values)
	}
	want := `outlook.timezone="Europe/Stockholm", outlook.body-content-type="text"`
	if values[0] != want {
		t.Errorf("Prefer = %q, want %q", values[0], want)
	}
}
