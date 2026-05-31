package m3u

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// b64 encodes s with URL-safe base64 (same as proxyURL uses internally).
func b64(s string) string {
	return base64.URLEncoding.EncodeToString([]byte(s))
}

func wantProxyURL(base, upstream string) string {
	return fmt.Sprintf("%s/proxy/stream?url=%s", base, b64(upstream))
}

func wantCatchupURL(base, upstream string) string {
	return fmt.Sprintf("%s/proxy/catchup?url=%s", base, b64(upstream))
}

// ── proxyURL ──────────────────────────────────────────────────────────────

func TestProxyURL(t *testing.T) {
	got := proxyURL("http://proxy", "http://upstream/stream.m3u8")
	want := wantProxyURL("http://proxy", "http://upstream/stream.m3u8")
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// ── proxyURLMaybeTemplate ─────────────────────────────────────────────────

func TestProxyURLMaybeTemplate_NoTemplate(t *testing.T) {
	// URLs without { should behave identically to proxyURL.
	upstream := "http://upstream/stream.m3u8?token=abc123"
	got := proxyURLMaybeTemplate("http://proxy", upstream)
	want := proxyURL("http://proxy", upstream)
	if got != want {
		t.Errorf("no-template: got %q\nwant %q", got, want)
	}
}

func TestProxyURLMaybeTemplate_QueryTemplateVarsVisible(t *testing.T) {
	// Bug fixed: catch-up URLs with {utc}/{lutc} in query params must keep
	// those placeholders visible (not hidden inside base64) so the player
	// can substitute them before requesting the URL.
	// The URL must use /proxy/catchup (not /proxy/stream) so that live stream
	// requests through /proxy/stream are never affected by time params.
	upstream := "http://stream.example.com/ch123/mono.m3u8?token=abc&utc={utc}&lutc={lutc}"
	got := proxyURLMaybeTemplate("http://proxy", upstream)

	if !strings.Contains(got, "{utc}") {
		t.Errorf("{utc} not visible in proxy URL: %q", got)
	}
	if !strings.Contains(got, "{lutc}") {
		t.Errorf("{lutc} not visible in proxy URL: %q", got)
	}
	if !strings.HasPrefix(got, "http://proxy/proxy/catchup?url=") {
		t.Errorf("must use /proxy/catchup endpoint, got: %q", got)
	}
}

func TestProxyURLMaybeTemplate_StableParamEncodedInBase64(t *testing.T) {
	// The stable (non-template) parts of the URL must be inside the base64.
	upstream := "http://stream.example.com/ch123/mono.m3u8?token=abc&utc={utc}&lutc={lutc}"
	got := proxyURLMaybeTemplate("http://proxy", upstream)

	// Extract the base64 portion (between "url=" and the next "&").
	rest := strings.TrimPrefix(got, "http://proxy/proxy/catchup?url=")
	b64part := strings.SplitN(rest, "&", 2)[0]
	decoded, err := base64.URLEncoding.DecodeString(b64part)
	if err != nil {
		t.Fatalf("base64 decode failed: %v\nURL: %q", err, got)
	}

	decodedStr := string(decoded)
	if !strings.Contains(decodedStr, "token=abc") {
		t.Errorf("stable param 'token' missing from base64, decoded: %q", decodedStr)
	}
	if strings.Contains(decodedStr, "{utc}") {
		t.Errorf("template var {utc} must NOT be inside base64, decoded: %q", decodedStr)
	}
	if strings.Contains(decodedStr, "{lutc}") {
		t.Errorf("template var {lutc} must NOT be inside base64, decoded: %q", decodedStr)
	}
}

func TestProxyURLMaybeTemplate_PathTemplateReturnsDirect(t *testing.T) {
	// If template vars appear in the path, return the URL as-is so the player
	// can still substitute and fetch catch-up content directly.
	upstream := "http://example.com/shift/{utc}/{stream_id}.m3u8?token=X"
	got := proxyURLMaybeTemplate("http://proxy", upstream)
	if got != upstream {
		t.Errorf("path template: expected direct URL\ngot %q\nwant %q", got, upstream)
	}
}

func TestProxyURLMaybeTemplate_MultipleTemplateVars(t *testing.T) {
	// e.g. start={Y}-{m}-{d}:{H}-{M} — all placeholders must survive.
	upstream := "http://example.com/mono.m3u8?token=X&start={Y}-{m}-{d}:{H}-{M}&dur=60"
	got := proxyURLMaybeTemplate("http://proxy", upstream)

	for _, v := range []string{"{Y}", "{m}", "{d}", "{H}", "{M}"} {
		if !strings.Contains(got, v) {
			t.Errorf("placeholder %s missing from proxy URL: %q", v, got)
		}
	}
	// dur=60 (stable) must be in the base64.
	rest := strings.TrimPrefix(got, "http://proxy/proxy/catchup?url=")
	b64part := strings.SplitN(rest, "&", 2)[0]
	decoded, _ := base64.URLEncoding.DecodeString(b64part)
	if !strings.Contains(string(decoded), "dur=60") {
		t.Errorf("stable param 'dur=60' missing from base64: %q", string(decoded))
	}
}

func TestProxyURLMaybeTemplate_AllParamsTemplate(t *testing.T) {
	// Edge case: all query params are template vars.
	upstream := "http://example.com/mono.m3u8?utc={utc}&lutc={lutc}"
	got := proxyURLMaybeTemplate("http://proxy", upstream)
	if !strings.Contains(got, "{utc}") || !strings.Contains(got, "{lutc}") {
		t.Errorf("template vars missing: %q", got)
	}
}

// ── Rewrite (full integration) ────────────────────────────────────────────

func TestRewrite_StreamURLsAreProxied(t *testing.T) {
	input := "#EXTM3U\n#EXTINF:-1,Channel 1\nhttp://upstream/live/stream.m3u8\n"
	var buf bytes.Buffer
	if err := Rewrite(&buf, strings.NewReader(input), "http://proxy"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "http://upstream/live/stream.m3u8") {
		t.Errorf("raw upstream URL still present: %q", out)
	}
	if !strings.Contains(out, "/proxy/stream?url=") {
		t.Errorf("no proxy URL in output: %q", out)
	}
}

func TestRewrite_AttributeURLsAreProxied(t *testing.T) {
	input := `#EXTINF:-1 tvg-logo="http://logos.example.com/ch1.png" tvg-id="ch1",Channel 1` +
		"\nhttp://upstream/stream.m3u8\n"
	var buf bytes.Buffer
	if err := Rewrite(&buf, strings.NewReader(input), "http://proxy"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "http://logos.example.com/ch1.png") {
		t.Errorf("logo URL not rewritten: %q", out)
	}
	if !strings.Contains(out, "/proxy/stream?url=") {
		t.Errorf("no proxy URL in output: %q", out)
	}
}

func TestRewrite_CatchupSourceTemplateVarsPreserved(t *testing.T) {
	// Core catch-up/rewind regression: {utc} and {lutc} must remain literal
	// in the rewritten M3U so the player can substitute real timestamps.
	input := `#EXTINF:-1 catchup="default" catchup-source="http://stream.example.com/ch123/mono.m3u8?token=abc&utc={utc}&lutc={lutc}",Ch` +
		"\nhttp://stream.example.com/ch123/mono.m3u8?token=abc\n"
	var buf bytes.Buffer
	if err := Rewrite(&buf, strings.NewReader(input), "http://proxy"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "{utc}") {
		t.Errorf("{utc} placeholder missing from rewritten playlist: %q", out)
	}
	if !strings.Contains(out, "{lutc}") {
		t.Errorf("{lutc} placeholder missing from rewritten playlist: %q", out)
	}
}

func TestRewrite_EPGUrlAttributeProxied(t *testing.T) {
	input := `#EXTM3U url-tvg="http://epg.example.com/guide.xml"` + "\n"
	var buf bytes.Buffer
	if err := Rewrite(&buf, strings.NewReader(input), "http://proxy"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "http://epg.example.com/guide.xml") {
		t.Errorf("EPG URL not rewritten: %q", out)
	}
}

func TestRewrite_NonHTTPLinesUnchanged(t *testing.T) {
	input := "#EXTM3U\n#EXT-X-VERSION:3\n"
	var buf bytes.Buffer
	if err := Rewrite(&buf, strings.NewReader(input), "http://proxy"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "/proxy/stream") {
		t.Errorf("non-URL line was rewritten: %q", out)
	}
}

func TestRewrite_MultipleChannels(t *testing.T) {
	input := "#EXTM3U\n" +
		"#EXTINF:-1,Ch1\nhttp://upstream/live/1.m3u8\n" +
		"#EXTINF:-1,Ch2\nhttp://upstream/live/2.m3u8\n"
	var buf bytes.Buffer
	if err := Rewrite(&buf, strings.NewReader(input), "http://proxy"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// Both stream URLs must be rewritten, original URLs must not appear.
	if strings.Contains(out, "http://upstream/live/1.m3u8") || strings.Contains(out, "http://upstream/live/2.m3u8") {
		t.Errorf("upstream URLs still present: %q", out)
	}
	if strings.Count(out, "/proxy/stream?url=") < 2 {
		t.Errorf("expected 2 proxy URLs, output: %q", out)
	}
}

func TestRewrite_RoundtripEncoding(t *testing.T) {
	// The base64-encoded URL inside a rewritten stream line must decode back
	// to the original upstream URL.
	upstream := "http://provider.example.com:8080/live/channel.m3u8?token=secret"
	input := "#EXTM3U\n#EXTINF:-1,Ch\n" + upstream + "\n"
	var buf bytes.Buffer
	if err := Rewrite(&buf, strings.NewReader(input), "http://proxy"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "http://proxy/proxy/stream?url=") {
			continue
		}
		enc := strings.TrimPrefix(line, "http://proxy/proxy/stream?url=")
		decoded, err := base64.URLEncoding.DecodeString(enc)
		if err != nil {
			t.Fatalf("base64 decode error: %v", err)
		}
		if string(decoded) != upstream {
			t.Errorf("decoded = %q, want %q", decoded, upstream)
		}
		return
	}
	t.Error("no proxy stream line found in output")
}
