package stalker

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"

	"github.com/janaz/iptvp/internal/config"
)

// Handler proxies Stalker Middleware / STB portal requests.
type Handler struct {
	cfg *config.Config
}

func NewHandler(cfg *config.Config) *Handler {
	return &Handler{cfg: cfg}
}

var client = &http.Client{}

// macRe matches the mac= cookie value (17-char colon-separated MAC address).
var macRe = regexp.MustCompile(`(mac=)[0-9A-Fa-f:]{17}`)

// ServePortal handles /portal.php requests:
//  1. Substitutes the real MAC address in the Cookie header.
//  2. Forwards the request to the upstream portal.
//  3. Rewrites stream and image URLs in the JSON response.
func (h *Handler) ServePortal(w http.ResponseWriter, r *http.Request) {
	if h.cfg.STBPortalURL == "" {
		http.Error(w, "STB_PORTAL_URL not configured", http.StatusServiceUnavailable)
		return
	}

	upstreamURL := h.cfg.STBPortalURL + "/portal.php"
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, r.Body)
	if err != nil {
		http.Error(w, "bad upstream url", http.StatusBadGateway)
		return
	}

	for _, key := range []string{"User-Agent", "Authorization", "Accept", "Accept-Language", "X-User-Agent"} {
		if v := r.Header.Get(key); v != "" {
			req.Header.Set(key, v)
		}
	}
	if cookie := r.Header.Get("Cookie"); cookie != "" {
		req.Header.Set("Cookie", h.substituteMACInCookie(cookie))
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("stalker: upstream error: %v", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "read error", http.StatusBadGateway)
		return
	}

	rewritten, _ := ParseAndRewrite(body, h.cfg.ProxyBaseURL, h.cfg.STBPortalURL)

	for _, key := range []string{"Content-Type", "Cache-Control", "Set-Cookie"} {
		if v := resp.Header.Get(key); v != "" {
			w.Header().Set(key, v)
		}
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(rewritten)))
	w.WriteHeader(resp.StatusCode)
	w.Write(rewritten) //nolint:errcheck
}

func (h *Handler) substituteMACInCookie(cookie string) string {
	if h.cfg.STBMAC == "" {
		return cookie
	}
	return macRe.ReplaceAllString(cookie, "${1}"+h.cfg.STBMAC)
}
