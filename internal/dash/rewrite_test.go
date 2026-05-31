package dash

import (
	"strings"
	"testing"
)

// ── IsDASH ────────────────────────────────────────────────────────────────

func TestIsDASH(t *testing.T) {
	tests := []struct {
		ct, rawURL string
		want       bool
	}{
		{"application/dash+xml", "http://example.com/stream", true},
		{"video/vnd.mpeg.dash", "http://example.com/stream", true},
		{"APPLICATION/DASH+XML", "http://example.com/stream", true}, // case-insensitive
		{"text/plain", "http://example.com/manifest.mpd", true},     // detected by extension
		{"text/plain", "http://example.com/manifest.MPD", true},     // case-insensitive extension
		{"video/mp2t", "http://example.com/segment.ts", false},
		{"application/vnd.apple.mpegurl", "http://example.com/stream.m3u8", false},
		{"", "http://example.com/segment.ts", false},
	}
	for _, tt := range tests {
		t.Run(tt.ct+"/"+tt.rawURL, func(t *testing.T) {
			if got := IsDASH(tt.ct, tt.rawURL); got != tt.want {
				t.Errorf("IsDASH(%q, %q) = %v, want %v", tt.ct, tt.rawURL, got, tt.want)
			}
		})
	}
}

// ── Rewrite ───────────────────────────────────────────────────────────────

func TestRewrite_AbsoluteBaseURLProxied(t *testing.T) {
	mpd := `<?xml version="1.0"?><MPD><BaseURL>http://cdn.example.com/content/</BaseURL></MPD>`
	got := string(Rewrite([]byte(mpd), "http://proxy"))
	if strings.Contains(got, "http://cdn.example.com/content/") {
		t.Errorf("absolute URL not rewritten: %q", got)
	}
	if !strings.Contains(got, "/proxy/stream?url=") {
		t.Errorf("no proxy URL in output: %q", got)
	}
}

func TestRewrite_MultipleAbsoluteURLs(t *testing.T) {
	mpd := `<MPD>` +
		`<Rep><BaseURL>http://cdn1.example.com/a/</BaseURL></Rep>` +
		`<Rep><BaseURL>http://cdn2.example.com/b/</BaseURL></Rep>` +
		`</MPD>`
	got := string(Rewrite([]byte(mpd), "http://proxy"))
	if strings.Contains(got, "http://cdn1.example.com") {
		t.Errorf("first URL not rewritten: %q", got)
	}
	if strings.Contains(got, "http://cdn2.example.com") {
		t.Errorf("second URL not rewritten: %q", got)
	}
	if strings.Count(got, "/proxy/stream?url=") < 2 {
		t.Errorf("expected at least 2 proxy URLs in output: %q", got)
	}
}

func TestRewrite_RelativeURLsUnchanged(t *testing.T) {
	// Relative segment URLs in DASH are resolved by the player against the
	// manifest URL, which now points to the proxy — so they must not be touched.
	mpd := `<MPD><SegmentTemplate media="chunk$Number$.ts" /></MPD>`
	got := string(Rewrite([]byte(mpd), "http://proxy"))
	if got != mpd {
		t.Errorf("relative URL was modified:\ngot  %q\nwant %q", got, mpd)
	}
}

func TestRewrite_HTTPSURLProxied(t *testing.T) {
	mpd := `<MPD><BaseURL>https://secure.cdn.example.com/stream/</BaseURL></MPD>`
	got := string(Rewrite([]byte(mpd), "http://proxy"))
	if strings.Contains(got, "https://secure.cdn.example.com") {
		t.Errorf("HTTPS URL not rewritten: %q", got)
	}
	if !strings.Contains(got, "/proxy/stream?url=") {
		t.Errorf("no proxy URL in output: %q", got)
	}
}

func TestRewrite_URLInAttribute(t *testing.T) {
	// URLs that appear as attribute values (not element text) must also be rewritten.
	mpd := `<MPD xlink:href="http://external.example.com/mpd.xml" />`
	got := string(Rewrite([]byte(mpd), "http://proxy"))
	if strings.Contains(got, "http://external.example.com") {
		t.Errorf("URL in attribute not rewritten: %q", got)
	}
}
