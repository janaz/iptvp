package dash

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// absURLRe matches absolute HTTP(S) URLs inside XML attribute values or element text.
var absURLRe = regexp.MustCompile(`https?://[^\s"'<>]+`)

// IsDASH reports whether a response looks like an MPEG-DASH manifest.
func IsDASH(contentType, rawURL string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "dash+xml") || strings.Contains(ct, "dash") {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".mpd")
}

// Rewrite replaces all absolute HTTP(S) URLs in a DASH MPD manifest with
// proxied URLs. Relative URLs in DASH are left as-is; the client resolves
// them against the manifest URL which now points to the proxy, so subsequent
// requests will also pass through /proxy/stream.
func Rewrite(data []byte, proxyBase string) []byte {
	return absURLRe.ReplaceAllFunc(data, func(match []byte) []byte {
		return []byte(toProxyURL(proxyBase, string(match)))
	})
}

func toProxyURL(proxyBase, upstream string) string {
	return fmt.Sprintf("%s/proxy/stream?url=%s", proxyBase,
		base64.URLEncoding.EncodeToString([]byte(upstream)))
}
