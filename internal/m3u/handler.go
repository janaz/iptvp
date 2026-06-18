package m3u

import (
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/janaz/iptvp/internal/config"
	"github.com/janaz/iptvp/internal/stream"
)

type Handler struct {
	cfg *config.Config
}

func NewHandler(cfg *config.Config) *Handler {
	return &Handler{cfg: cfg}
}

// ServePlaylist fetches the upstream M3U, rewrites URLs, and serves it.
func (h *Handler) ServePlaylist(w http.ResponseWriter, r *http.Request) {
	if h.cfg.M3UURL == "" {
		http.Error(w, "M3U_URL not configured", http.StatusServiceUnavailable)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, h.cfg.M3UURL, nil)
	if err != nil {
		http.Error(w, "bad M3U_URL", http.StatusInternalServerError)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/x-mpegurl")
	w.Header().Set("Content-Disposition", "attachment; filename=\"playlist.m3u\"")
	if err := Rewrite(w, resp.Body, h.cfg.ProxyBaseURL); err != nil {
		// headers already sent; nothing useful we can do
		return
	}
}

// ServeStream proxies a stream whose URL is passed as ?url=<base64>.
func (h *Handler) ServeStream(w http.ResponseWriter, r *http.Request) {
	encoded := r.URL.Query().Get("url")
	if encoded == "" {
		http.Error(w, "missing url parameter", http.StatusBadRequest)
		return
	}

	raw, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		http.Error(w, "invalid url parameter", http.StatusBadRequest)
		return
	}

	stream.Pipe(w, r, string(raw), h.cfg.ProxyBaseURL)
}

// ServeCatchup handles catch-up/rewind requests for template URLs produced by
// proxyURLMaybeTemplate. The upstream URL is reassembled from three query params:
// the base64-encoded stable prefix (p) and suffix (s), and the template span (t)
// with the player's substituted time values. This keeps the upstream host and
// credentials hidden while letting the player fill in the placeholders.
func (h *Handler) ServeCatchup(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	prefix, err := base64.URLEncoding.DecodeString(q.Get("p"))
	if err != nil {
		http.Error(w, "invalid p parameter", http.StatusBadRequest)
		return
	}
	suffix, err := base64.URLEncoding.DecodeString(q.Get("s"))
	if err != nil {
		http.Error(w, "invalid s parameter", http.StatusBadRequest)
		return
	}

	upstreamURL := string(prefix) + q.Get("t") + string(suffix)
	stream.Pipe(w, r, upstreamURL, h.cfg.ProxyBaseURL)
}
