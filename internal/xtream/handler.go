package xtream

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/janaz/iptvp/internal/config"
	"github.com/janaz/iptvp/internal/m3u"
	"github.com/janaz/iptvp/internal/stream"
)

type Handler struct {
	cfg *config.Config
}

func NewHandler(cfg *config.Config) *Handler {
	return &Handler{cfg: cfg}
}

// ServeAPI handles /player_api.php — forwards to upstream, rewrites JSON response.
func (h *Handler) ServeAPI(w http.ResponseWriter, r *http.Request) {
	if h.cfg.XtreamBaseURL == "" {
		http.Error(w, "XTREAM_BASE_URL not configured", http.StatusServiceUnavailable)
		return
	}

	upstreamURL := h.buildAPIURL(r)
	body, err := h.fetchUpstream(r, upstreamURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
		return
	}

	rewritten, err := ParseAndRewrite(body,
		h.cfg.ProxyBaseURL,
		h.cfg.XtreamBaseURL,
		h.cfg.XtreamUsername,
		h.cfg.XtreamPassword,
	)
	if err != nil {
		rewritten = body
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(rewritten) //nolint:errcheck
}

// ServeM3UPlus handles /get.php — fetches M3U_Plus playlist and rewrites URLs.
func (h *Handler) ServeM3UPlus(w http.ResponseWriter, r *http.Request) {
	if h.cfg.XtreamBaseURL == "" {
		http.Error(w, "XTREAM_BASE_URL not configured", http.StatusServiceUnavailable)
		return
	}

	upstreamURL := h.buildGetURL(r)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		http.Error(w, "bad upstream url", http.StatusBadGateway)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/x-mpegurl")
	m3u.Rewrite(w, resp.Body, h.cfg.ProxyBaseURL) //nolint:errcheck
}

// ServeXMLTV handles /xmltv.php — passes through EPG XML unchanged.
func (h *Handler) ServeXMLTV(w http.ResponseWriter, r *http.Request) {
	if h.cfg.XtreamBaseURL == "" {
		http.Error(w, "XTREAM_BASE_URL not configured", http.StatusServiceUnavailable)
		return
	}
	upstreamURL := h.cfg.XtreamBaseURL + "/xmltv.php?" + h.upstreamQuery(r)
	stream.Pipe(w, r, upstreamURL, h.cfg.ProxyBaseURL)
}

// ServeXtreamStream handles /{type}/{user}/{pass}/{id}.{ext} stream requests.
// It replaces proxy credentials with upstream credentials and pipes the stream.
func (h *Handler) ServeXtreamStream(w http.ResponseWriter, r *http.Request) {
	if h.cfg.XtreamBaseURL == "" {
		http.Error(w, "XTREAM_BASE_URL not configured", http.StatusServiceUnavailable)
		return
	}

	// Reconstruct path with upstream credentials.
	// Path: /{type}/{anyUser}/{anyPass}/{rest}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 4)
	if len(parts) < 4 {
		http.Error(w, "invalid stream path", http.StatusBadRequest)
		return
	}
	streamType, _, _, rest := parts[0], parts[1], parts[2], parts[3]

	upstreamURL := fmt.Sprintf("%s/%s/%s/%s/%s",
		h.cfg.XtreamBaseURL,
		streamType,
		h.cfg.XtreamUsername,
		h.cfg.XtreamPassword,
		rest,
	)

	stream.Pipe(w, r, upstreamURL, h.cfg.ProxyBaseURL)
}

// buildAPIURL constructs the upstream player_api.php URL, replacing client
// credentials with upstream credentials.
func (h *Handler) buildAPIURL(r *http.Request) string {
	q := h.upstreamQuery(r)
	return h.cfg.XtreamBaseURL + "/player_api.php?" + q
}

func (h *Handler) buildGetURL(r *http.Request) string {
	q := h.upstreamQuery(r)
	return h.cfg.XtreamBaseURL + "/get.php?" + q
}

// upstreamQuery copies the request query string, replacing username/password
// with the configured upstream credentials.
func (h *Handler) upstreamQuery(r *http.Request) string {
	q := r.URL.Query()
	if h.cfg.XtreamUsername != "" {
		q.Set("username", h.cfg.XtreamUsername)
		q.Set("password", h.cfg.XtreamPassword)
	}
	return url.Values(q).Encode()
}

func (h *Handler) fetchUpstream(r *http.Request, upstreamURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
