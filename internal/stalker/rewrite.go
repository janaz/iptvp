package stalker

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// ParseAndRewrite unmarshals JSON from data, rewrites stream and image URLs,
// and re-marshals. Non-JSON bodies are returned unchanged.
func ParseAndRewrite(data []byte, proxyBase, portalBase string) ([]byte, error) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return data, nil
	}
	rewritten := rewriteJSON(v, proxyBase, portalBase)
	return json.Marshal(rewritten)
}

func rewriteJSON(v interface{}, proxyBase, portalBase string) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, child := range val {
			out[k] = rewriteJSON(child, proxyBase, portalBase)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, child := range val {
			out[i] = rewriteJSON(child, proxyBase, portalBase)
		}
		return out
	case string:
		return rewriteString(val, proxyBase, portalBase)
	default:
		return val
	}
}

// rewriteString rewrites a single string value. It handles:
//   - "ffrt http://..." — Stalker stream command prefix; the URL part is proxied,
//     the "ffrt " prefix is preserved so players that rely on it still work.
//   - Plain HTTP(S) URLs — wrapped in the proxy stream endpoint.
//   - Everything else (RTMP, relative paths, plain strings) — returned as-is.
func rewriteString(s, proxyBase, portalBase string) string {
	const ffrt = "ffrt "
	if strings.HasPrefix(s, ffrt) {
		rest := s[len(ffrt):]
		if isHTTP(rest) {
			return ffrt + toProxyURL(proxyBase, rest)
		}
		return s
	}
	if isHTTP(s) {
		return toProxyURL(proxyBase, s)
	}
	return s
}

func isHTTP(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func toProxyURL(proxyBase, upstream string) string {
	return fmt.Sprintf("%s/proxy/stream?url=%s", proxyBase,
		base64.URLEncoding.EncodeToString([]byte(upstream)))
}
