package stream

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── looksLikeHLS ─────────────────────────────────────────────────────────

func TestLooksLikeHLS(t *testing.T) {
	tests := []struct {
		desc string
		peek []byte
		want bool
	}{
		{"extm3u header", []byte("#EXTM3U\n#EXT-X-VERSION:3"), true},
		{"ext-x header", []byte("#EXT-X-STREAM-INF:BANDWIDTH=..."), true},
		{"leading whitespace", []byte("  #EXTM3U\n"), true},
		{"dash mpd", []byte("<MPD ...>"), false},
		{"binary ts", []byte("\x47\x40\x00\x10\x00"), false},
		{"empty", []byte{}, false},
		{"html", []byte("<!DOCTYPE html>"), false},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := looksLikeHLS(tt.peek); got != tt.want {
				t.Errorf("looksLikeHLS(%q) = %v, want %v", tt.peek, got, tt.want)
			}
		})
	}
}

// ── looksLikeDASH ────────────────────────────────────────────────────────

func TestLooksLikeDASH(t *testing.T) {
	tests := []struct {
		desc string
		peek []byte
		want bool
	}{
		{"mpd element", []byte(`<?xml version="1.0"?><MPD type="dynamic">`), true},
		{"urn string", []byte(`urn:mpeg:dash:schema:mpd:2011`), true},
		{"hls manifest", []byte("#EXTM3U\n"), false},
		{"json", []byte(`{"key":"value"}`), false},
		{"binary", []byte("\x00\x01\x02\x03"), false},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := looksLikeDASH(tt.peek); got != tt.want {
				t.Errorf("looksLikeDASH(%q) = %v, want %v", tt.peek, got, tt.want)
			}
		})
	}
}

// ── Pipe (integration) ────────────────────────────────────────────────────

// proxyStreamURL builds the expected proxy URL for an upstream URL.
func proxyStreamURL(proxyBase, upstream string) string {
	return fmt.Sprintf("%s/proxy/stream?url=%s", proxyBase,
		base64.URLEncoding.EncodeToString([]byte(upstream)))
}

func TestPipe_HLSManifestRewritten(t *testing.T) {
	// Upstream returns an HLS manifest; the proxy must rewrite segment URLs.
	manifest := "#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:5.0,\nsegment001.ts\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		fmt.Fprint(w, manifest)
	}))
	defer upstream.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/proxy/stream", nil)
	Pipe(rec, req, upstream.URL+"/ch/mono.m3u8", "http://proxy")

	body := rec.Body.String()
	if strings.Contains(body, "segment001.ts") {
		t.Errorf("raw segment name still present in output: %q", body)
	}
	// The rewritten segment must point to the proxy.
	wantSeg := proxyStreamURL("http://proxy", upstream.URL+"/ch/segment001.ts")
	if !strings.Contains(body, wantSeg) {
		t.Errorf("expected segment proxy URL %q in:\n%q", wantSeg, body)
	}
	if rec.Header().Get("Content-Type") != "application/vnd.apple.mpegurl" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
}

func TestPipe_HLSDetectedByBodySniff(t *testing.T) {
	// Upstream serves HLS with a generic Content-Type; detection is by body sniff.
	manifest := "#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:5.0,\nseg.ts\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, manifest)
	}))
	defer upstream.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	Pipe(rec, req, upstream.URL+"/stream", "http://proxy")

	if rec.Header().Get("Content-Type") != "application/vnd.apple.mpegurl" {
		t.Errorf("Content-Type not set to HLS after sniff: %q", rec.Header().Get("Content-Type"))
	}
}

func TestPipe_DASHManifestRewritten(t *testing.T) {
	mpd := `<?xml version="1.0"?><MPD><BaseURL>http://cdn.example.com/</BaseURL></MPD>`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dash+xml")
		fmt.Fprint(w, mpd)
	}))
	defer upstream.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	Pipe(rec, req, upstream.URL+"/stream.mpd", "http://proxy")

	body := rec.Body.String()
	if strings.Contains(body, "http://cdn.example.com/") {
		t.Errorf("absolute URL not rewritten in DASH output: %q", body)
	}
	if !strings.Contains(body, "/proxy/stream?url=") {
		t.Errorf("no proxy URL in DASH output: %q", body)
	}
}

func TestPipe_RawStreamPassthrough(t *testing.T) {
	payload := []byte("\x47\x40\x00\x10raw ts data here")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.Write(payload) //nolint:errcheck
	}))
	defer upstream.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	Pipe(rec, req, upstream.URL+"/segment.ts", "http://proxy")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	got := rec.Body.Bytes()
	if string(got) != string(payload) {
		t.Errorf("raw payload mismatch:\ngot  %q\nwant %q", got, payload)
	}
}

func TestPipe_ForwardsRangeHeader(t *testing.T) {
	var gotRange string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(206)
	}))
	defer upstream.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Range", "bytes=0-1023")
	Pipe(rec, req, upstream.URL+"/segment.ts", "http://proxy")

	if gotRange != "bytes=0-1023" {
		t.Errorf("Range not forwarded, upstream saw: %q", gotRange)
	}
}

func TestPipe_ForwardsUserAgent(t *testing.T) {
	var gotUA string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(200)
		io.WriteString(w, "") //nolint:errcheck
	}))
	defer upstream.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "TiViMate/4.0")
	Pipe(rec, req, upstream.URL+"/segment.ts", "http://proxy")

	if gotUA != "TiViMate/4.0" {
		t.Errorf("User-Agent not forwarded, upstream saw: %q", gotUA)
	}
}

func TestPipe_UpstreamErrorReturns502(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	// Use a URL that will fail to connect.
	Pipe(rec, req, "http://127.0.0.1:1/nonexistent", "http://proxy")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502 on upstream error, got %d", rec.Code)
	}
}

func TestPipe_FollowsRedirects(t *testing.T) {
	var finalURL string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		finalURL = r.URL.String()
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(200)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/final.ts", http.StatusFound)
	}))
	defer redirector.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	Pipe(rec, req, redirector.URL+"/redirect", "http://proxy")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 after redirect", rec.Code)
	}
	if finalURL == "" {
		t.Error("redirect not followed; target server was never reached")
	}
}
