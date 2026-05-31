package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/janaz/iptvp/internal/config"
	"github.com/janaz/iptvp/internal/m3u"
	"github.com/janaz/iptvp/internal/stalker"
	"github.com/janaz/iptvp/internal/xtream"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	mux := http.NewServeMux()

	m3uH := m3u.NewHandler(cfg)
	mux.HandleFunc("/m3u", m3uH.ServePlaylist)
	mux.HandleFunc("/proxy/stream", m3uH.ServeStream)
	mux.HandleFunc("/proxy/catchup", m3uH.ServeCatchup)

	if cfg.STBPortalURL != "" {
		sH := stalker.NewHandler(cfg)
		mux.HandleFunc("/portal.php", sH.ServePortal)
	}

	if cfg.XtreamBaseURL != "" {
		xH := xtream.NewHandler(cfg)
		mux.HandleFunc("/player_api.php", xH.ServeAPI)
		mux.HandleFunc("/get.php", xH.ServeM3UPlus)
		mux.HandleFunc("/xmltv.php", xH.ServeXMLTV)
		// Catch-all for /{type}/{user}/{pass}/{id}.{ext} stream paths.
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if isXtreamStreamPath(r.URL.Path) {
				xH.ServeXtreamStream(w, r)
				return
			}
			http.NotFound(w, r)
		})
	}

	addr := ":" + cfg.Port
	log.Printf("iptvp listening on %s (proxy base: %s)", addr, cfg.ProxyBaseURL)
	if err := http.ListenAndServe(addr, accessLog(mux)); err != nil {
		log.Fatal(err)
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)

		remote := upstreamURL(r)
		if remote != "" {
			log.Printf("%s %s %s → %s %d %s",
				r.RemoteAddr, r.Method, r.URL.Path, remote, sw.status, time.Since(start).Round(time.Millisecond))
		} else {
			log.Printf("%s %s %s %d %s",
				r.RemoteAddr, r.Method, r.RequestURI, sw.status, time.Since(start).Round(time.Millisecond))
		}
	})
}

// upstreamURL decodes the ?url= parameter for /proxy/stream and /proxy/catchup
// requests, or reconstructs the upstream path for Xtream stream routes.
func upstreamURL(r *http.Request) string {
	if r.URL.Path == "/proxy/stream" || r.URL.Path == "/proxy/catchup" {
		if enc := r.URL.Query().Get("url"); enc != "" {
			if b, err := base64.URLEncoding.DecodeString(enc); err == nil {
				return string(b)
			}
		}
		return ""
	}
	// Xtream stream paths: /{type}/{user}/{pass}/{id}.{ext}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 4)
	if len(parts) == 4 {
		streamTypes := map[string]bool{"live": true, "movie": true, "series": true, "timeshift": true}
		if streamTypes[parts[0]] {
			return fmt.Sprintf("xtream:/%s/{upstream_creds}/%s", parts[0], parts[3])
		}
	}
	return ""
}

// isXtreamStreamPath returns true for paths like /live/user/pass/id.ts
func isXtreamStreamPath(path string) bool {
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 4)
	if len(parts) < 4 {
		return false
	}
	streamTypes := map[string]bool{"live": true, "movie": true, "series": true, "timeshift": true}
	return streamTypes[parts[0]]
}
