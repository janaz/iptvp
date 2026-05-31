package stalker

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func b64(s string) string {
	return base64.URLEncoding.EncodeToString([]byte(s))
}

func wantProxyURL(upstream string) string {
	return fmt.Sprintf("http://proxy/proxy/stream?url=%s", b64(upstream))
}

// ── rewriteString ─────────────────────────────────────────────────────────

func TestRewriteString_PlainHTTPURL(t *testing.T) {
	s := "http://stream.example.com/logo.png"
	got := rewriteString(s, "http://proxy", "http://portal.example.com")
	want := wantProxyURL(s)
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestRewriteString_FfrtHTTPURL(t *testing.T) {
	// "ffrt " prefix must be preserved; the URL portion gets proxied.
	stream := "http://stream.example.com/ch/1.m3u8"
	got := rewriteString("ffrt "+stream, "http://proxy", "http://portal.example.com")
	want := "ffrt " + wantProxyURL(stream)
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestRewriteString_FfrtRTMPUnchanged(t *testing.T) {
	// RTMP streams cannot be HTTP-proxied — leave them as-is.
	s := "ffrt rtmp://stream.example.com/live/channel"
	got := rewriteString(s, "http://proxy", "http://portal.example.com")
	if got != s {
		t.Errorf("RTMP stream should be unchanged, got %q", got)
	}
}

func TestRewriteString_PlainRTMPUnchanged(t *testing.T) {
	s := "rtmp://stream.example.com/live/channel"
	got := rewriteString(s, "http://proxy", "http://portal.example.com")
	if got != s {
		t.Errorf("plain RTMP should be unchanged, got %q", got)
	}
}

func TestRewriteString_RelativePathUnchanged(t *testing.T) {
	// Relative /media/... paths are API parameters, not stream URLs.
	s := "/media/channel_id.m3u8"
	got := rewriteString(s, "http://proxy", "http://portal.example.com")
	if got != s {
		t.Errorf("relative path should be unchanged, got %q", got)
	}
}

func TestRewriteString_PlainStringUnchanged(t *testing.T) {
	s := "Channel Name"
	got := rewriteString(s, "http://proxy", "http://portal.example.com")
	if got != s {
		t.Errorf("plain string should be unchanged, got %q", got)
	}
}

// ── ParseAndRewrite ───────────────────────────────────────────────────────

func TestParseAndRewrite_ChannelList(t *testing.T) {
	// Typical get_ordered_list response.
	input := `{"js":{"total_items":2,"data":[` +
		`{"id":"1","name":"Ch1","cmd":"ffrt http://stream.example.com/ch1.m3u8","logo":"http://logos.example.com/ch1.png"},` +
		`{"id":"2","name":"Ch2","cmd":"ffrt http://stream.example.com/ch2.m3u8","logo":"http://logos.example.com/ch2.png"}` +
		`]}}`
	got, err := ParseAndRewrite([]byte(input), "http://proxy", "http://portal.example.com")
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, got)
	}

	result := string(got)
	if strings.Contains(result, "http://stream.example.com/ch1.m3u8") {
		t.Errorf("stream URL not rewritten: %s", result)
	}
	if strings.Contains(result, "http://logos.example.com/ch1.png") {
		t.Errorf("logo URL not rewritten: %s", result)
	}
	if !strings.Contains(result, "ffrt http://proxy/proxy/stream?url=") {
		t.Errorf("ffrt prefix missing from rewritten cmd: %s", result)
	}
}

func TestParseAndRewrite_CreateLinkResponse(t *testing.T) {
	// Typical create_link response — cmd and link both contain the stream URL.
	streamURL := "http://stream.example.com/live/token123/ch1.m3u8"
	input := fmt.Sprintf(`{"js":{"cmd":"ffrt %s","link":"%s"}}`, streamURL, streamURL)
	got, err := ParseAndRewrite([]byte(input), "http://proxy", "http://portal.example.com")
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]interface{}
	json.Unmarshal(got, &out) //nolint:errcheck
	js := out["js"].(map[string]interface{})

	cmd := js["cmd"].(string)
	if !strings.HasPrefix(cmd, "ffrt ") {
		t.Errorf("ffrt prefix stripped from cmd: %q", cmd)
	}
	if strings.Contains(cmd, streamURL) {
		t.Errorf("raw stream URL still in cmd: %q", cmd)
	}
	link := js["link"].(string)
	if strings.Contains(link, streamURL) {
		t.Errorf("raw stream URL still in link: %q", link)
	}
}

func TestParseAndRewrite_HandshakeResponse(t *testing.T) {
	// Handshake response contains no URLs — must be returned unchanged.
	input := `{"js":{"token":"abc123xyz","random":"rand456"}}`
	got, err := ParseAndRewrite([]byte(input), "http://proxy", "http://portal.example.com")
	if err != nil {
		t.Fatal(err)
	}
	var orig, out map[string]interface{}
	json.Unmarshal([]byte(input), &orig) //nolint:errcheck
	json.Unmarshal(got, &out)            //nolint:errcheck
	origJS := orig["js"].(map[string]interface{})
	outJS := out["js"].(map[string]interface{})
	if origJS["token"] != outJS["token"] {
		t.Errorf("token changed: %q → %q", origJS["token"], outJS["token"])
	}
}

func TestParseAndRewrite_InvalidJSONPassthrough(t *testing.T) {
	input := []byte("not json")
	got, _ := ParseAndRewrite(input, "http://proxy", "http://portal.example.com")
	if string(got) != string(input) {
		t.Errorf("non-JSON body changed: %q", got)
	}
}

func TestParseAndRewrite_NumbersAndBoolsUnchanged(t *testing.T) {
	input := `{"js":{"id":42,"hd":true,"rating":4.5}}`
	got, err := ParseAndRewrite([]byte(input), "http://proxy", "http://portal.example.com")
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	json.Unmarshal(got, &out) //nolint:errcheck
	js := out["js"].(map[string]interface{})
	if js["id"] != float64(42) || js["hd"] != true {
		t.Errorf("non-string values changed: %v", js)
	}
}
