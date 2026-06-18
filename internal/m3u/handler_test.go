package m3u

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/janaz/iptvp/internal/config"
)

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

func TestServeCatchup_ReconstructsFromPrefixSpanSuffix(t *testing.T) {
	// Core catch-up: the upstream URL is reassembled from the base64 prefix (p) and
	// suffix (s) plus the substituted template span (t), with time values merged in.
	var gotQuery url.Values
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	// Template: {upstream}/mono.m3u8?token=abc&utc={utc}&lutc={lutc}
	prefix := base64.URLEncoding.EncodeToString([]byte(upstream.URL + "/mono.m3u8?token=abc&utc="))
	suffix := base64.URLEncoding.EncodeToString([]byte(""))

	h := &Handler{cfg: &config.Config{ProxyBaseURL: "http://proxy"}}
	rec := httptest.NewRecorder()
	// Player has substituted {utc}=…600 and {lutc}=…200 into the span.
	req := httptest.NewRequest("GET",
		"/proxy/catchup?p="+prefix+"&t=1748646600%26lutc%3D1748650200&s="+suffix, nil)
	h.ServeCatchup(rec, req)

	if gotPath != "/mono.m3u8" {
		t.Errorf("upstream path = %q, want /mono.m3u8", gotPath)
	}
	if gotQuery.Get("token") != "abc" {
		t.Errorf("original token not preserved; upstream query: %v", gotQuery)
	}
	if gotQuery.Get("utc") != "1748646600" {
		t.Errorf("utc not reconstructed; upstream query: %v", gotQuery)
	}
	if gotQuery.Get("lutc") != "1748650200" {
		t.Errorf("lutc not reconstructed; upstream query: %v", gotQuery)
	}
}

func TestServeCatchup_PathTemplateReconstructed(t *testing.T) {
	// flussonic-style: the substituted span sits in the path, with a hidden token suffix.
	var gotPath, gotToken string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.URL.Query().Get("token")
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	prefix := base64.URLEncoding.EncodeToString([]byte(upstream.URL + "/ch5/index-"))
	suffix := base64.URLEncoding.EncodeToString([]byte(".m3u8?token=secret"))

	h := &Handler{cfg: &config.Config{ProxyBaseURL: "http://proxy"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/proxy/catchup?p="+prefix+"&t=1748-3600&s="+suffix, nil)
	h.ServeCatchup(rec, req)

	if gotPath != "/ch5/index-1748-3600.m3u8" {
		t.Errorf("upstream path = %q, want /ch5/index-1748-3600.m3u8", gotPath)
	}
	if gotToken != "secret" {
		t.Errorf("hidden token not preserved, got %q", gotToken)
	}
}

func TestServeCatchup_InvalidPrefix(t *testing.T) {
	h := &Handler{cfg: &config.Config{ProxyBaseURL: "http://proxy"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/proxy/catchup?p=!!!notbase64!!!&t=x&s=", nil)
	h.ServeCatchup(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
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
