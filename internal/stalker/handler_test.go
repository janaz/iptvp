package stalker

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/janaz/iptvp/internal/config"
)

func newHandler(portalURL, mac string) *Handler {
	return NewHandler(&config.Config{
		STBPortalURL: portalURL,
		STBMAC:       mac,
		ProxyBaseURL: "http://proxy",
	})
}

// ── substituteMACInCookie ─────────────────────────────────────────────────

func TestSubstituteMACInCookie_Basic(t *testing.T) {
	h := newHandler("http://portal.example.com", "AA:BB:CC:DD:EE:FF")
	got := h.substituteMACInCookie("mac=00:11:22:33:44:55; stb_lang=en; timezone=UTC")
	if !strings.Contains(got, "mac=AA:BB:CC:DD:EE:FF") {
		t.Errorf("MAC not substituted: %q", got)
	}
	if strings.Contains(got, "00:11:22:33:44:55") {
		t.Errorf("client MAC still present: %q", got)
	}
	if !strings.Contains(got, "stb_lang=en") {
		t.Errorf("other cookie fields lost: %q", got)
	}
}

func TestSubstituteMACInCookie_EmptyMAC(t *testing.T) {
	h := newHandler("http://portal.example.com", "")
	cookie := "mac=00:11:22:33:44:55; stb_lang=en"
	got := h.substituteMACInCookie(cookie)
	if got != cookie {
		t.Errorf("empty real MAC: cookie should be unchanged, got %q", got)
	}
}

func TestSubstituteMACInCookie_MACOnlyField(t *testing.T) {
	h := newHandler("http://portal.example.com", "AA:BB:CC:DD:EE:FF")
	got := h.substituteMACInCookie("mac=00:11:22:33:44:55")
	if got != "mac=AA:BB:CC:DD:EE:FF" {
		t.Errorf("got %q", got)
	}
}

// ── ServePortal ───────────────────────────────────────────────────────────

func TestServePortal_NotConfigured(t *testing.T) {
	h := newHandler("", "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/portal.php?action=handshake", nil)
	h.ServePortal(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when STBPortalURL not set, got %d", rec.Code)
	}
}

func TestServePortal_ForwardsQueryString(t *testing.T) {
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		fmt.Fprint(w, `{"js":{}}`)
	}))
	defer upstream.Close()

	h := newHandler(upstream.URL, "AA:BB:CC:DD:EE:FF")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/portal.php?action=handshake&type=stb&token=", nil)
	h.ServePortal(rec, req)

	if gotQuery != "action=handshake&type=stb&token=" {
		t.Errorf("query not forwarded: %q", gotQuery)
	}
}

func TestServePortal_SubstitutesMACInCookie(t *testing.T) {
	var gotCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		fmt.Fprint(w, `{"js":{}}`)
	}))
	defer upstream.Close()

	h := newHandler(upstream.URL, "AA:BB:CC:DD:EE:FF")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/portal.php?action=handshake", nil)
	req.Header.Set("Cookie", "mac=00:11:22:33:44:55; stb_lang=en")
	h.ServePortal(rec, req)

	if !strings.Contains(gotCookie, "mac=AA:BB:CC:DD:EE:FF") {
		t.Errorf("real MAC not sent to upstream, cookie: %q", gotCookie)
	}
	if strings.Contains(gotCookie, "00:11:22:33:44:55") {
		t.Errorf("client MAC leaked to upstream, cookie: %q", gotCookie)
	}
}

func TestServePortal_ForwardsUserAgent(t *testing.T) {
	var gotUA string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		fmt.Fprint(w, `{"js":{}}`)
	}))
	defer upstream.Close()

	h := newHandler(upstream.URL, "AA:BB:CC:DD:EE:FF")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/portal.php", nil)
	req.Header.Set("User-Agent", "TiViMate/4.0")
	h.ServePortal(rec, req)

	if gotUA != "TiViMate/4.0" {
		t.Errorf("User-Agent not forwarded: %q", gotUA)
	}
}

func TestServePortal_RewritesStreamURLsInResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"js":{"cmd":"ffrt http://stream.example.com/ch1.m3u8"}}`)
	}))
	defer upstream.Close()

	h := newHandler(upstream.URL, "AA:BB:CC:DD:EE:FF")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/portal.php?action=create_link", nil)
	h.ServePortal(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "http://stream.example.com/ch1.m3u8") {
		t.Errorf("stream URL not rewritten in response: %s", body)
	}
	if !strings.Contains(body, "ffrt http://proxy/proxy/stream?url=") {
		t.Errorf("proxy URL with ffrt prefix missing: %s", body)
	}
}

func TestServePortal_UpstreamErrorReturns502(t *testing.T) {
	h := newHandler("http://127.0.0.1:1", "AA:BB:CC:DD:EE:FF")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/portal.php?action=handshake", nil)
	h.ServePortal(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502 on upstream error, got %d", rec.Code)
	}
}
