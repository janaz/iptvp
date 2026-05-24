package hls

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

// uriAttrRe matches URI="..." inside HLS tags.
var uriAttrRe = regexp.MustCompile(`URI="([^"]+)"`)

// IsHLS reports whether a response looks like an HLS manifest.
func IsHLS(contentType, rawURL string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "mpegurl") {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".m3u8")
}

// Rewrite reads an HLS manifest from r and writes it to w, replacing every
// URI — segment lines, sub-playlist lines, and URI="..." tag attributes —
// with a proxied URL. Relative URIs are resolved against baseURL first.
func Rewrite(w io.Writer, r io.Reader, baseURL *url.URL, proxyBase string) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 512*1024), 512*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if _, err := fmt.Fprintln(w, rewriteLine(line, baseURL, proxyBase)); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func rewriteLine(line string, base *url.URL, proxyBase string) string {
	if strings.HasPrefix(line, "#") {
		return uriAttrRe.ReplaceAllStringFunc(line, func(m string) string {
			inner := m[5 : len(m)-1] // strip URI=" and trailing "
			return `URI="` + toProxyURL(proxyBase, resolveRef(inner, base)) + `"`
		})
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return line
	}
	// Plain URI line: segment (.ts, .aac, .mp4, …) or sub-playlist (.m3u8).
	return toProxyURL(proxyBase, resolveRef(trimmed, base))
}

func resolveRef(ref string, base *url.URL) string {
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(u).String()
}

func toProxyURL(proxyBase, upstream string) string {
	return fmt.Sprintf("%s/proxy/stream?url=%s", proxyBase,
		base64.URLEncoding.EncodeToString([]byte(upstream)))
}
