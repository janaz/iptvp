package m3u

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/janaz/iptvp/internal/config"
)

// ── extraParams ───────────────────────────────────────────────────────────

func TestExtraParams_RemovesURLKey(t *testing.T) {
	q := url.Values{
		"url":  []string{"abc"},
		"utc":  []string{"1234"},
		"lutc": []string{"5678"},
	}
	extras := extraParams(q)
	if _, ok := extras["url"]; ok {
		t.Error("'url' key should be excluded from extras")
	}
	if extras.Get("utc") != "1234" {
		t.Errorf("utc = %q, want 1234", extras.Get("utc"))
	}
	if extras.Get("lutc") != "5678" {
		t.Errorf("lutc = %q, want 5678", extras.Get("lutc"))
	}
}

func TestExtraParams_EmptyWhenOnlyURL(t *testing.T) {
	q := url.Values{"url": []string{"abc"}}
	extras := extraParams(q)
	if len(extras) != 0 {
		t.Errorf("expected empty extras, got %v", extras)
	}
}

// ── mergeParams ───────────────────────────────────────────────────────────

func TestMergeParams_AppendsToExistingQuery(t *testing.T) {
	raw := "http://upstream/mono.m3u8?token=abc"
	extra := url.Values{
		"utc":  []string{"1748646600"},
		"lutc": []string{"1748650200"},
	}
	got := mergeParams(raw, extra)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("token") != "abc" {
		t.Errorf("original token missing: %q", got)
	}
	if q.Get("utc") != "1748646600" {
		t.Errorf("utc missing or wrong: %q", got)
	}
	if q.Get("lutc") != "1748650200" {
		t.Errorf("lutc missing or wrong: %q", got)
	}
}

func TestMergeParams_NoExistingQuery(t *testing.T) {
	raw := "http://upstream/mono.m3u8"
	extra := url.Values{"utc": []string{"123"}}
	got := mergeParams(raw, extra)
	if !strings.Contains(got, "utc=123") {
		t.Errorf("utc not present in merged URL: %q", got)
	}
}

func TestMergeParams_InvalidURL(t *testing.T) {
	raw := "://not-a-url"
	extra := url.Values{"utc": []string{"123"}}
	got := mergeParams(raw, extra)
	// Should return rawURL unchanged on parse error.
	if got != raw {
		t.Errorf("expected unchanged URL on parse error, got %q", got)
	}
}

// ── ServeStream ───────────────────────────────────────────────────────────

func TestServeStream_MissingURLParam(t *testing.T) {
	h := &Handler{cfg: &config.Config{ProxyBaseURL: "http://proxy"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/proxy/stream", nil)
	h.ServeStream(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestServeStream_InvalidBase64(t *testing.T) {
	h := &Handler{cfg: &config.Config{ProxyBaseURL: "http://proxy"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/proxy/stream?url=!!!notbase64!!!", nil)
	h.ServeStream(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestServeCatchup_ForwardsExtraParamsToUpstream(t *testing.T) {
	// Core catch-up: extra query params (filled-in template vars) must be
	// merged into the upstream URL when going through /proxy/catchup.
	var gotQuery url.Values
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	upstreamURL := upstream.URL + "/mono.m3u8?token=abc"
	encoded := base64.URLEncoding.EncodeToString([]byte(upstreamURL))

	h := &Handler{cfg: &config.Config{ProxyBaseURL: "http://proxy"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET",
		"/proxy/catchup?url="+encoded+"&utc=1748646600&lutc=1748650200", nil)
	h.ServeCatchup(rec, req)

	if gotQuery.Get("token") != "abc" {
		t.Errorf("original token not forwarded; upstream query: %v", gotQuery)
	}
	if gotQuery.Get("utc") != "1748646600" {
		t.Errorf("utc not forwarded; upstream query: %v", gotQuery)
	}
	if gotQuery.Get("lutc") != "1748650200" {
		t.Errorf("lutc not forwarded; upstream query: %v", gotQuery)
	}
}

func TestServeStream_DoesNotForwardExtraParams(t *testing.T) {
	// Live streams must NOT have utc/lutc forwarded even if the player sends them.
	// Only /proxy/catchup merges extra params.
	var gotQuery url.Values
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	upstreamURL := upstream.URL + "/mono.m3u8?token=abc"
	encoded := base64.URLEncoding.EncodeToString([]byte(upstreamURL))

	h := &Handler{cfg: &config.Config{ProxyBaseURL: "http://proxy"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET",
		"/proxy/stream?url="+encoded+"&utc=1748646600&lutc=1748650200", nil)
	h.ServeStream(rec, req)

	if gotQuery.Get("utc") != "" {
		t.Errorf("utc must not be forwarded by ServeStream, upstream saw: %v", gotQuery)
	}
	if gotQuery.Get("lutc") != "" {
		t.Errorf("lutc must not be forwarded by ServeStream, upstream saw: %v", gotQuery)
	}
	if gotQuery.Get("token") != "abc" {
		t.Errorf("original token missing: %v", gotQuery)
	}
}

func TestServeStream_NoExtraParams(t *testing.T) {
	// Without extra params, the upstream URL must be fetched as-is.
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	upstreamURL := upstream.URL + "/live/channel.ts"
	encoded := base64.URLEncoding.EncodeToString([]byte(upstreamURL))

	h := &Handler{cfg: &config.Config{ProxyBaseURL: "http://proxy"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/proxy/stream?url="+encoded, nil)
	h.ServeStream(rec, req)

	if gotPath != "/live/channel.ts" {
		t.Errorf("upstream path = %q, want /live/channel.ts", gotPath)
	}
}
