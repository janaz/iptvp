package xtream

import (
	"encoding/json"
	"fmt"
	"strings"
)

// rewriteJSON walks parsed JSON and replaces Xtream stream URLs with proxy URLs.
// It handles both objects and arrays at any depth.
func rewriteJSON(v interface{}, proxyBase, upstreamBase, upstreamUser, upstreamPass string) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, child := range val {
			out[k] = rewriteJSON(child, proxyBase, upstreamBase, upstreamUser, upstreamPass)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, child := range val {
			out[i] = rewriteJSON(child, proxyBase, upstreamBase, upstreamUser, upstreamPass)
		}
		return out
	case string:
		return rewriteURL(val, proxyBase, upstreamBase, upstreamUser, upstreamPass)
	default:
		return val
	}
}

// rewriteURL rewrites a single URL if it looks like an Xtream stream URL from
// the upstream server. Other strings are returned unchanged.
func rewriteURL(s, proxyBase, upstreamBase, upstreamUser, upstreamPass string) string {
	if !strings.HasPrefix(s, upstreamBase) {
		return s
	}
	// Replace upstreamBase with proxyBase and swap credentials.
	path := strings.TrimPrefix(s, upstreamBase)
	// Replace upstream credentials in path segments like /live/user/pass/...
	path = replaceCredentials(path, upstreamUser, upstreamPass, "proxy", "proxy")
	return proxyBase + path
}

// replaceCredentials replaces /user/pass/ segments with /newUser/newPass/ in paths
// of the form /{type}/{user}/{pass}/...
func replaceCredentials(path, oldUser, oldPass, newUser, newPass string) string {
	if oldUser == "" {
		return path
	}
	old := fmt.Sprintf("/%s/%s/", oldUser, oldPass)
	neu := fmt.Sprintf("/%s/%s/", newUser, newPass)
	return strings.ReplaceAll(path, old, neu)
}

// ParseAndRewrite unmarshals JSON from data, rewrites URLs, and re-marshals.
func ParseAndRewrite(data []byte, proxyBase, upstreamBase, upstreamUser, upstreamPass string) ([]byte, error) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return data, nil // not JSON — return as-is
	}
	rewritten := rewriteJSON(v, proxyBase, upstreamBase, upstreamUser, upstreamPass)
	return json.Marshal(rewritten)
}
