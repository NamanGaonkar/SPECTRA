package main

import "testing"

func TestNormalizeURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"example.com", "https://example.com"},
		{"  https://example.com/page  ", "https://example.com/page"},
		{"<https://example.com>", "https://example.com"},
		{"\"https://example.com\"", "https://example.com"},
		{"(https://example.com)", "https://example.com"},
		{"https://example.com/page.", "https://example.com/page"},
		{"https://example.com/page,", "https://example.com/page"},
		{"https://example.com?a=b&utm_source=x", "https://example.com?a=b"},
		{"https://example.com/#section", "https://example.com/"},
		{"https://ex\u200bample.com", "https://example.com"},           // zero-width space
		{"https://example.com/pa\u00a0th", "https://example.com/path"}, // NBSP
		{"http://example.com", "http://example.com"},                   // scheme kept
	}
	for _, c := range cases {
		if got := normalizeURL(c.in); got != c.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidTargetURL(t *testing.T) {
	if err := validTargetURL("https://example.com"); err != nil {
		t.Errorf("valid URL rejected: %v", err)
	}
	if err := validTargetURL("ftp://example.com"); err == nil {
		t.Error("ftp scheme should be rejected")
	}
	if err := validTargetURL("https://"); err == nil {
		t.Error("missing host should be rejected")
	}
}
