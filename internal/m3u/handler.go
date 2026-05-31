package m3u

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"

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

// ServeCatchup handles catch-up/rewind requests where the player has substituted
// time template variables (utc, lutc, etc.) into the URL. Extra query parameters
// beyond "url" are merged into the upstream URL before fetching, so the upstream
// receives the requested time range. Live stream requests use ServeStream instead,
// which never forwards extra parameters.
func (h *Handler) ServeCatchup(w http.ResponseWriter, r *http.Request) {
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

	upstreamURL := string(raw)
	if extras := extraParams(r.URL.Query()); len(extras) > 0 {
		upstreamURL = mergeParams(upstreamURL, extras)
	}

	stream.Pipe(w, r, upstreamURL, h.cfg.ProxyBaseURL)
}

func extraParams(q url.Values) url.Values {
	out := url.Values{}
	for k, vs := range q {
		if k != "url" {
			out[k] = vs
		}
	}
	return out
}

func mergeParams(rawURL string, extra url.Values) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	for k, vs := range extra {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
