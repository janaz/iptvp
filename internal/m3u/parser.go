package m3u

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// attrURLRe matches any quoted HTTP(S) URL in an M3U attribute value, e.g.:
//
//	tvg-logo="http://..." or url-tvg="https://..."
var attrURLRe = regexp.MustCompile(`"(https?://[^"]+)"`)

// Rewrite reads an M3U playlist from r and writes it to w, replacing every
// HTTP(S) URL — whether a plain stream line or embedded in an attribute value
// (tvg-logo, tvg-url, url-tvg, x-tvg-url, etc.) — with a proxied URL.
func Rewrite(w io.Writer, r io.Reader, proxyBase string) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if isURL(line) {
			line = proxyURL(proxyBase, line)
		} else if strings.HasPrefix(line, "#") {
			line = rewriteAttrs(line, proxyBase)
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// rewriteAttrs replaces all quoted HTTP(S) URLs in M3U directive lines.
func rewriteAttrs(line, proxyBase string) string {
	return attrURLRe.ReplaceAllStringFunc(line, func(match string) string {
		inner := match[1 : len(match)-1] // strip surrounding quotes
		return `"` + proxyURLMaybeTemplate(proxyBase, inner) + `"`
	})
}

func isURL(line string) bool {
	return strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://")
}

func proxyURL(base, upstream string) string {
	return fmt.Sprintf("%s/proxy/stream?url=%s", base,
		base64.URLEncoding.EncodeToString([]byte(upstream)))
}

func catchupURL(base, upstream string) string {
	return fmt.Sprintf("%s/proxy/catchup?url=%s", base,
		base64.URLEncoding.EncodeToString([]byte(upstream)))
}

// proxyURLMaybeTemplate handles catch-up URLs that contain template variables
// like {utc} and {lutc}. Template query-param values are kept visible in the
// proxy URL so the player can substitute them; only the stable (non-template)
// portion of the URL is base64-encoded. This preserves the placeholder syntax
// while still routing the eventual request through the proxy.
//
// If template variables appear in the URL path (not query params), the URL is
// returned as-is so the player can still substitute and fetch content directly.
func proxyURLMaybeTemplate(base, upstream string) string {
	if !strings.Contains(upstream, "{") {
		return proxyURL(base, upstream)
	}
	u, err := url.Parse(upstream)
	if err != nil || strings.Contains(u.Path, "{") {
		return upstream
	}
	stable := url.Values{}
	var templateParts []string
	for k, vs := range u.Query() {
		for _, v := range vs {
			if strings.Contains(v, "{") {
				templateParts = append(templateParts, k+"="+v)
			} else {
				stable.Set(k, v)
			}
		}
	}
	if len(templateParts) == 0 {
		return proxyURL(base, upstream)
	}
	sort.Strings(templateParts)
	u.RawQuery = stable.Encode()
	return catchupURL(base, u.String()) + "&" + strings.Join(templateParts, "&")
}
