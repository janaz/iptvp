package hls

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func b64(s string) string {
	return base64.URLEncoding.EncodeToString([]byte(s))
}

func proxyURL(base, upstream string) string {
	return fmt.Sprintf("%s/proxy/stream?url=%s", base, b64(upstream))
}

// ── IsHLS ─────────────────────────────────────────────────────────────────

func TestIsHLS(t *testing.T) {
	tests := []struct {
		ct, rawURL string
		want       bool
	}{
		{"application/vnd.apple.mpegurl", "http://example.com/stream", true},
		{"application/x-mpegurl", "http://example.com/stream", true},
		{"Application/VND.APPLE.MPEGURL", "http://example.com/stream", true}, // case-insensitive
		{"video/mp2t", "http://example.com/stream.m3u8", true},               // detected by URL extension
		{"video/mp2t", "http://example.com/stream.M3U8", true},               // case-insensitive extension
		{"video/mp2t", "http://example.com/segment.ts", false},
		{"", "http://example.com/segment.ts", false},
		{"text/html", "http://example.com/page.html", false},
	}
	for _, tt := range tests {
		t.Run(tt.ct+"/"+tt.rawURL, func(t *testing.T) {
			if got := IsHLS(tt.ct, tt.rawURL); got != tt.want {
				t.Errorf("IsHLS(%q, %q) = %v, want %v", tt.ct, tt.rawURL, got, tt.want)
			}
		})
	}
}

// ── Rewrite ───────────────────────────────────────────────────────────────

func TestRewrite_RelativeSegmentsResolvedAgainstBase(t *testing.T) {
	manifest := "#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:5.760,\n001.ts\n#EXTINF:5.760,\n002.ts\n"
	base, _ := url.Parse("http://server.example.com/ch/mono.m3u8")
	var out bytes.Buffer
	if err := Rewrite(&out, strings.NewReader(manifest), base, "http://proxy"); err != nil {
		t.Fatal(err)
	}
	result := out.String()

	// Raw relative names must be gone.
	if strings.Contains(result, "\n001.ts\n") || strings.Contains(result, "\n002.ts\n") {
		t.Errorf("relative segment names still present: %q", result)
	}
	// Must be proxied.
	if !strings.Contains(result, "/proxy/stream?url=") {
		t.Errorf("no proxy URL in output: %q", result)
	}
	// Resolved URL check: relative to base path /ch/mono.m3u8 → /ch/001.ts.
	want001 := proxyURL("http://proxy", "http://server.example.com/ch/001.ts")
	if !strings.Contains(result, want001) {
		t.Errorf("expected %q in output\ngot: %q", want001, result)
	}
}

func TestRewrite_AbsoluteSegmentsProxied(t *testing.T) {
	manifest := "#EXTM3U\n#EXTINF:5.760,\nhttp://cdn.example.com/seg/001.ts\n"
	base, _ := url.Parse("http://server.example.com/ch/mono.m3u8")
	var out bytes.Buffer
	if err := Rewrite(&out, strings.NewReader(manifest), base, "http://proxy"); err != nil {
		t.Fatal(err)
	}
	result := out.String()
	if strings.Contains(result, "http://cdn.example.com") {
		t.Errorf("absolute upstream URL still present: %q", result)
	}
	want := proxyURL("http://proxy", "http://cdn.example.com/seg/001.ts")
	if !strings.Contains(result, want) {
		t.Errorf("expected %q in output\ngot: %q", want, result)
	}
}

func TestRewrite_URIAttributeInEXTXKEY(t *testing.T) {
	manifest := "#EXT-X-KEY:METHOD=AES-128,URI=\"http://keys.example.com/key.bin\"\n#EXTINF:5.0,\nseg.ts\n"
	base, _ := url.Parse("http://server.example.com/ch/mono.m3u8")
	var out bytes.Buffer
	if err := Rewrite(&out, strings.NewReader(manifest), base, "http://proxy"); err != nil {
		t.Fatal(err)
	}
	result := out.String()
	if strings.Contains(result, "http://keys.example.com") {
		t.Errorf("URI attribute not rewritten: %q", result)
	}
	wantKey := proxyURL("http://proxy", "http://keys.example.com/key.bin")
	if !strings.Contains(result, wantKey) {
		t.Errorf("expected %q in output\ngot: %q", wantKey, result)
	}
}

func TestRewrite_SubPlaylistProxied(t *testing.T) {
	// Master playlist: contains sub-playlist .m3u8 lines.
	manifest := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1280000\nhigh/playlist.m3u8\n"
	base, _ := url.Parse("http://server.example.com/master.m3u8")
	var out bytes.Buffer
	if err := Rewrite(&out, strings.NewReader(manifest), base, "http://proxy"); err != nil {
		t.Fatal(err)
	}
	result := out.String()
	if strings.Contains(result, "high/playlist.m3u8") {
		t.Errorf("sub-playlist line not rewritten: %q", result)
	}
	wantSub := proxyURL("http://proxy", "http://server.example.com/high/playlist.m3u8")
	if !strings.Contains(result, wantSub) {
		t.Errorf("expected %q in output\ngot: %q", wantSub, result)
	}
}

func TestRewrite_EmptyLinesPreserved(t *testing.T) {
	manifest := "#EXTM3U\n\n#EXTINF:5.0,\nseg.ts\n"
	base, _ := url.Parse("http://server.example.com/mono.m3u8")
	var out bytes.Buffer
	if err := Rewrite(&out, strings.NewReader(manifest), base, "http://proxy"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	// Input has 4 lines (including blank); output should too.
	if len(lines) < 4 {
		t.Errorf("expected ≥4 lines, got %d:\n%s", len(lines), out.String())
	}
}

func TestRewrite_CommentLinesPreserved(t *testing.T) {
	manifest := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n#EXTINF:5.0,\nseg.ts\n"
	base, _ := url.Parse("http://server.example.com/mono.m3u8")
	var out bytes.Buffer
	if err := Rewrite(&out, strings.NewReader(manifest), base, "http://proxy"); err != nil {
		t.Fatal(err)
	}
	result := out.String()
	if !strings.Contains(result, "#EXT-X-VERSION:3") {
		t.Errorf("comment line removed: %q", result)
	}
	if !strings.Contains(result, "#EXT-X-TARGETDURATION:6") {
		t.Errorf("comment line removed: %q", result)
	}
}

func TestRewrite_DVRDateSegments(t *testing.T) {
	// Regression: date-based DVR segment paths (used for rewind) must be proxied.
	manifest := "#EXTM3U\n#EXTINF:5.76,\n2026/05/31/00/11/45-05760.ts?token=abc\n"
	base, _ := url.Parse("http://stream.example.com/ch1/mono.m3u8?token=abc")
	var out bytes.Buffer
	if err := Rewrite(&out, strings.NewReader(manifest), base, "http://proxy"); err != nil {
		t.Fatal(err)
	}
	result := out.String()
	if strings.Contains(result, "2026/05/31/00/11/45-05760.ts?token=abc") {
		t.Errorf("DVR segment not rewritten: %q", result)
	}
	wantSeg := proxyURL("http://proxy", "http://stream.example.com/ch1/2026/05/31/00/11/45-05760.ts?token=abc")
	if !strings.Contains(result, wantSeg) {
		t.Errorf("expected DVR proxy URL %q\ngot: %q", wantSeg, result)
	}
}
