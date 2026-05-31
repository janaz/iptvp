package xtream

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/janaz/iptvp/internal/config"
)

// newTestHandler creates a Handler wired to a test upstream.
func newTestHandler(upstreamURL string) *Handler {
	return NewHandler(&config.Config{
		XtreamBaseURL:  upstreamURL,
		XtreamUsername: "realuser",
		XtreamPassword: "realpass",
		ProxyBaseURL:   "http://proxy",
	})
}

// ── ServeXtreamStream ─────────────────────────────────────────────────────

func TestServeXtreamStream_SubstitutesCredentials(t *testing.T) {
	// Whatever credentials the client sends, the upstream must receive the
	// configured upstream credentials.
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	h := newTestHandler(upstream.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/live/anyclient/anysecret/12345.ts", nil)
	h.ServeXtreamStream(rec, req)

	if !strings.Contains(gotPath, "/live/realuser/realpass/12345.ts") {
		t.Errorf("upstream received wrong path: %q", gotPath)
	}
	if strings.Contains(gotPath, "anyclient") || strings.Contains(gotPath, "anysecret") {
		t.Errorf("client credentials leaked to upstream: %q", gotPath)
	}
}

func TestServeXtreamStream_LiveType(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	h := newTestHandler(upstream.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/live/u/p/891.ts", nil)
	h.ServeXtreamStream(rec, req)

	if gotPath != "/live/realuser/realpass/891.ts" {
		t.Errorf("live path = %q, want /live/realuser/realpass/891.ts", gotPath)
	}
}

func TestServeXtreamStream_MovieType(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	h := newTestHandler(upstream.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/movie/u/p/42.mp4", nil)
	h.ServeXtreamStream(rec, req)

	if gotPath != "/movie/realuser/realpass/42.mp4" {
		t.Errorf("movie path = %q, want /movie/realuser/realpass/42.mp4", gotPath)
	}
}

func TestServeXtreamStream_TimeshiftType(t *testing.T) {
	// Timeshift paths carry extra segments (duration, start, id) after credentials.
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	h := newTestHandler(upstream.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/timeshift/u/p/120/2024-01-15:20-00/12345.ts", nil)
	h.ServeXtreamStream(rec, req)

	want := "/timeshift/realuser/realpass/120/2024-01-15:20-00/12345.ts"
	if gotPath != want {
		t.Errorf("timeshift path = %q, want %q", gotPath, want)
	}
}

func TestServeXtreamStream_InvalidPath_TooFewParts(t *testing.T) {
	h := newTestHandler("http://upstream")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/live/user/pass", nil) // missing stream ID
	h.ServeXtreamStream(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid path, got %d", rec.Code)
	}
}

func TestServeXtreamStream_NoXtreamBaseURL(t *testing.T) {
	h := NewHandler(&config.Config{}) // no XtreamBaseURL
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/live/u/p/1.ts", nil)
	h.ServeXtreamStream(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when XtreamBaseURL not configured, got %d", rec.Code)
	}
}

// ── upstreamQuery ─────────────────────────────────────────────────────────

func TestUpstreamQuery_ReplacesCredentials(t *testing.T) {
	h := newTestHandler("http://upstream")
	req := httptest.NewRequest("GET", "/player_api.php?username=clientuser&password=clientpass&action=get_live_streams", nil)
	got := h.upstreamQuery(req)

	vals := parseQuery(got)
	if vals["username"] != "realuser" {
		t.Errorf("username = %q, want realuser", vals["username"])
	}
	if vals["password"] != "realpass" {
		t.Errorf("password = %q, want realpass", vals["password"])
	}
	if vals["action"] != "get_live_streams" {
		t.Errorf("action = %q, want get_live_streams", vals["action"])
	}
}

// parseQuery is a helper that turns a query string into a simple key→value map.
func parseQuery(raw string) map[string]string {
	m := map[string]string{}
	for _, part := range strings.Split(raw, "&") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			m[kv[0]] = kv[1]
		}
	}
	return m
}
