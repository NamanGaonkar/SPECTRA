package main

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------------------
// url.go — pasted-URL hygiene. Users paste from browsers, notes, terminals;
// the paste frequently carries whitespace, surrounding quotes/angle brackets,
// trailing punctuation, tracking query params, or hidden zero-width
// characters. normalizeURL cleans all of that; validTargetURL rejects
// anything that would waste a Chrome run (no host, unsupported scheme).
// ---------------------------------------------------------------------------

// stripZeroWidth removes invisible Unicode characters that sneak in when
// copying from web pages, chat apps, and PDFs. Chrome would navigate to a
// mangled URL and the user would see an error that makes no sense.
func stripZeroWidth(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', // zero-width space
			'\u200c', // zero-width non-joiner
			'\u200d', // zero-width joiner
			'\u2060', // word joiner
			'\ufeff': // BOM / zero-width no-break space
			return -1
		}
		if unicode.IsSpace(r) {
			return -1 // tabs, newlines, NBSP inside a URL are never wanted
		}
		return r
	}, s)
}

// normalizeURL turns whatever the user pasted into a clean, canonical URL
// string: trims space and control chars, strips wrapping quotes/brackets and
// trailing punctuation, removes common tracking parameters, drops URL
// fragments, and prepends https:// when no scheme was given.
func normalizeURL(raw string) string {
	s := stripZeroWidth(raw)

	// Trim whitespace/control characters from both ends.
	s = strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	})

	// Strip common delimiters picked up when copying from markdown,
	// HTML snippets, chat messages, or shell commands.
	s = strings.Trim(s, "\"'`<>()[]{}")

	// Trailing punctuation that a sentence or note commonly leaves behind.
	s = strings.TrimRight(s, ".,;:!?,\u201d\u2019") // includes curly quotes

	if s == "" {
		return ""
	}

	// Drop the fragment: it never affects what Chrome renders and only
	// leaks scroll targets into exported metadata.
	if u, err := url.Parse(s); err == nil {
		u.Fragment = ""
		// Remove well-known tracking params so exports cite clean URLs.
		if u.RawQuery != "" {
			q := u.Query()
			for key := range q {
				if isTrackingParam(key) {
					q.Del(key)
				}
			}
			u.RawQuery = q.Encode()
		}
		s = u.String()
	}

	// Default the scheme. A pasted "example.com/path" is almost always
	// meant as https. Leave ftp:// etc. to fail validation below.
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		// "http://" typed by hand still lands here only if malformed
		// (e.g. after bracket stripping) — guard by checking for a scheme
		// pattern at all.
		if i := strings.Index(s, "://"); i > 0 {
			return s // unknown scheme; let validation explain the problem
		}
		s = "https://" + s
	}
	return s
}

// isTrackingParam reports whether a query key is a known advertising or
// analytics marker. Purely cosmetic: keeps exported citations tidy.
func isTrackingParam(key string) bool {
	k := strings.ToLower(key)
	switch k {
	case "utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content",
		"utm_id", "gclid", "fbclid", "mc_cid", "mc_eid", "ref", "ref_src",
		"igshid", "si", "spm", "yclid", "twclid", "ttclid", "li_fat_id",
		"_hsenc", "_hsmi", "vero_id", "wickedid", "hsa_cam", "msclkid":
		return true
	}
	return false
}

// validTargetURL ensures the normalized URL is something Chrome can
// actually navigate to, returning a human-readable problem otherwise.
func validTargetURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("that URL doesn't parse: %v", err)
	}
	switch u.Scheme {
	case "http", "https":
		// fine
	default:
		return fmt.Errorf("unsupported scheme %q — use http(s):// (got: %s)", u.Scheme, raw)
	}
	if u.Host == "" {
		return fmt.Errorf("URL has no host — example: https://example.com/page")
	}
	return nil
}
