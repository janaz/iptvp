package stream

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/janaz/iptvp/internal/dash"
	"github.com/janaz/iptvp/internal/hls"
)

var client = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return nil // follow all redirects
	},
}

const sniffLen = 512

// Pipe fetches upstreamURL, detects the content type, rewrites any manifest
// URLs to go through the proxy, and streams the result to w.
func Pipe(w http.ResponseWriter, r *http.Request, upstreamURL, proxyBase string) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		http.Error(w, "bad upstream url", http.StatusBadGateway)
		return
	}

	if ua := r.Header.Get("User-Agent"); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("stream: upstream error fetching %s: %v", upstreamURL, err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	finalURL := resp.Request.URL.String() // URL after all redirects

	// Peek at the first bytes to detect manifest types that upstream serves
	// with a generic Content-Type (e.g. text/plain).
	peek := make([]byte, sniffLen)
	n, _ := io.ReadFull(resp.Body, peek)
	peek = peek[:n]
	body := io.MultiReader(bytes.NewReader(peek), resp.Body)

	copyHeaders(w.Header(), resp.Header)

	switch {
	case hls.IsHLS(ct, finalURL) || looksLikeHLS(peek):
		log.Printf("stream: HLS  status=%d ct=%q url=%s", resp.StatusCode, ct, finalURL)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Del("Content-Length") // length changes after rewrite
		w.WriteHeader(resp.StatusCode)
		base, _ := url.Parse(finalURL)
		hls.Rewrite(w, body, base, proxyBase) //nolint:errcheck

	case dash.IsDASH(ct, finalURL) || looksLikeDASH(peek):
		log.Printf("stream: DASH status=%d ct=%q url=%s", resp.StatusCode, ct, finalURL)
		all, err := io.ReadAll(body)
		if err != nil {
			return
		}
		rewritten := dash.Rewrite(all, proxyBase)
		w.Header().Set("Content-Type", "application/dash+xml")
		w.Header().Set("Content-Length", strconv.Itoa(len(rewritten)))
		w.WriteHeader(resp.StatusCode)
		w.Write(rewritten) //nolint:errcheck

	default:
		log.Printf("stream: RAW  status=%d ct=%q url=%s peek=%q", resp.StatusCode, ct, finalURL, string(peek[:min(n, 80)]))
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, body) //nolint:errcheck
	}
}

// looksLikeHLS checks whether the peeked bytes match an HLS manifest header.
func looksLikeHLS(peek []byte) bool {
	s := strings.TrimSpace(string(peek))
	return strings.HasPrefix(s, "#EXTM3U") || strings.HasPrefix(s, "#EXT-X-")
}

// looksLikeDASH checks whether the peeked bytes look like an MPD XML document.
func looksLikeDASH(peek []byte) bool {
	s := strings.TrimSpace(string(peek))
	return strings.Contains(s, "<MPD") || strings.Contains(s, "urn:mpeg:dash")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func copyHeaders(dst, src http.Header) {
	for _, key := range []string{
		"Content-Type", "Content-Length", "Content-Range",
		"Accept-Ranges", "Transfer-Encoding", "Cache-Control",
	} {
		if v := src.Get(key); v != "" {
			dst.Set(key, v)
		}
	}
}
