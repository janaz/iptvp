package m3u

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/url"
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

// roundtripCatchup mirrors what a player + ServeCatchup do: it substitutes the given
// placeholder replacements into the catch-up URL string (as a player would), then
// reassembles the upstream URL the way ServeCatchup does (decode p + span t + decode s).
func roundtripCatchup(t *testing.T, catchupURL string, repl map[string]string) string {
	t.Helper()
	sub := catchupURL
	for k, v := range repl {
		sub = strings.ReplaceAll(sub, k, v)
	}
	u, err := url.Parse(sub)
	if err != nil {
		t.Fatalf("parse %q: %v", sub, err)
	}
	q := u.Query()
	p, err := base64.URLEncoding.DecodeString(q.Get("p"))
	if err != nil {
		t.Fatalf("decode p: %v", err)
	}
	s, err := base64.URLEncoding.DecodeString(q.Get("s"))
	if err != nil {
		t.Fatalf("decode s: %v", err)
	}
	return string(p) + q.Get("t") + string(s)
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
	// Catch-up URLs with {utc}/{lutc} must keep those placeholders visible (not
	// hidden inside base64) so the player can substitute them, and must use the
	// /proxy/catchup endpoint so live /proxy/stream requests are never affected.
	upstream := "http://stream.example.com/ch123/mono.m3u8?token=abc&utc={utc}&lutc={lutc}"
	got := proxyURLMaybeTemplate("http://proxy", upstream)

	if !strings.Contains(got, "{utc}") || !strings.Contains(got, "{lutc}") {
		t.Errorf("placeholders not visible in proxy URL: %q", got)
	}
	if !strings.HasPrefix(got, "http://proxy/proxy/catchup?p=") {
		t.Errorf("must use /proxy/catchup endpoint, got: %q", got)
	}
	// Round-trips back to the original upstream once the player substitutes values.
	if rt := roundtripCatchup(t, got, map[string]string{"{utc}": "{utc}", "{lutc}": "{lutc}"}); rt != upstream {
		t.Errorf("round-trip = %q, want %q", rt, upstream)
	}
}

func TestProxyURLMaybeTemplate_StablePartsHiddenInBase64(t *testing.T) {
	// The host and credentials (stable parts) must NOT appear in cleartext; they
	// are only recoverable by base64-decoding p and s.
	upstream := "http://stream.example.com/ch123/mono.m3u8?token=secret&utc={utc}&lutc={lutc}"
	got := proxyURLMaybeTemplate("http://proxy", upstream)

	if strings.Contains(got, "stream.example.com") || strings.Contains(got, "token=secret") {
		t.Errorf("upstream host/token leaked in cleartext: %q", got)
	}
	// But the template span between the first { and last } stays visible.
	if !strings.Contains(got, "{utc}") || !strings.Contains(got, "{lutc}") {
		t.Errorf("template span not visible: %q", got)
	}
}

func TestProxyURLMaybeTemplate_PathTemplateProxied(t *testing.T) {
	// flussonic-style: placeholders in the PATH must now be routed through the proxy
	// (previously returned raw, leaking the upstream). Host/token stay hidden; the
	// URL round-trips after substitution.
	upstream := "http://stream.example.com/ch5/index-{utc}-{duration}.m3u8?token=secret"
	got := proxyURLMaybeTemplate("http://proxy", upstream)

	if !strings.HasPrefix(got, "http://proxy/proxy/catchup?p=") {
		t.Errorf("path template not proxied: %q", got)
	}
	if strings.Contains(got, "stream.example.com") || strings.Contains(got, "token=secret") {
		t.Errorf("path-template upstream leaked in cleartext: %q", got)
	}
	if !strings.Contains(got, "{utc}") || !strings.Contains(got, "{duration}") {
		t.Errorf("path placeholders not visible: %q", got)
	}
	rt := roundtripCatchup(t, got, map[string]string{"{utc}": "1748", "{duration}": "3600"})
	want := "http://stream.example.com/ch5/index-1748-3600.m3u8?token=secret"
	if rt != want {
		t.Errorf("round-trip = %q, want %q", rt, want)
	}
}

func TestProxyURLMaybeTemplate_XtreamXCStyle(t *testing.T) {
	// xc-style timeshift.php with credentials before the first placeholder: creds
	// must be hidden in the base64 prefix, all placeholders preserved.
	upstream := "http://stream.example.com/timeshift.php?username=u&password=p&stream={id}&start={Y}-{m}-{d}:{H}-{M}&duration={dur}"
	got := proxyURLMaybeTemplate("http://proxy", upstream)

	if strings.Contains(got, "username=u") || strings.Contains(got, "password=p") {
		t.Errorf("xc credentials leaked: %q", got)
	}
	for _, v := range []string{"{id}", "{Y}", "{m}", "{d}", "{H}", "{M}", "{dur}"} {
		if !strings.Contains(got, v) {
			t.Errorf("placeholder %s missing: %q", v, got)
		}
	}
	rt := roundtripCatchup(t, got, map[string]string{
		"{id}": "12345", "{Y}": "2026", "{m}": "06", "{d}": "17",
		"{H}": "20", "{M}": "00", "{dur}": "60",
	})
	want := "http://stream.example.com/timeshift.php?username=u&password=p&stream=12345&start=2026-06-17:20-00&duration=60"
	if rt != want {
		t.Errorf("round-trip = %q, want %q", rt, want)
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

func TestRewrite_TimeshiftSynthesizesCatchupSource(t *testing.T) {
	// Providers like iptv.team advertise archive via a timeshift attribute with
	// no catchup-source. The proxy must synthesize a /proxy/catchup source so the
	// player can request archive content (time params reach the upstream).
	upstream := "http://stream.example.com/ch001/mono.m3u8?token=abc"
	input := `#EXTINF:0 tvg-id="ch001" timeshift="7", Channel One` + "\n" + upstream + "\n"
	var buf bytes.Buffer
	if err := Rewrite(&buf, strings.NewReader(input), "http://proxy"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, `catchup="default"`) {
		t.Errorf("catchup type not added: %q", out)
	}
	if !strings.Contains(out, "/proxy/catchup?p=") {
		t.Errorf("catchup-source not routed through /proxy/catchup: %q", out)
	}
	if !strings.Contains(out, "{utc}") || !strings.Contains(out, "{lutc}") {
		t.Errorf("time placeholders missing from catchup-source: %q", out)
	}
	if !strings.Contains(out, `timeshift="7"`) {
		t.Errorf("original timeshift attribute lost: %q", out)
	}
	// The original title must remain after the injected attributes.
	if !strings.Contains(out, ", Channel One") {
		t.Errorf("channel title corrupted: %q", out)
	}
	// Substituting placeholders into the synthesized source must reconstruct the
	// upstream with utc/lutc merged in.
	src := catchupSourceValue(t, out)
	rt := roundtripCatchup(t, src, map[string]string{"{utc}": "1748", "{lutc}": "1750"})
	want := upstream + "&utc=1748&lutc=1750"
	if rt != want {
		t.Errorf("round-trip = %q, want %q", rt, want)
	}
}

// catchupSourceValue extracts the first catchup-source="…" attribute value from text.
func catchupSourceValue(t *testing.T, s string) string {
	t.Helper()
	m := catchupSourceRe.FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("no catchup-source found in: %q", s)
	}
	return m[1]
}

func TestRewrite_NoTimeshiftNoCatchupSource(t *testing.T) {
	input := `#EXTINF:0 tvg-id="ch1", Plain Channel` + "\nhttp://stream.example.com/ch1/mono.m3u8\n"
	var buf bytes.Buffer
	if err := Rewrite(&buf, strings.NewReader(input), "http://proxy"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "catchup") {
		t.Errorf("catchup added to non-archive channel: %q", buf.String())
	}
}

func TestRewrite_TimeshiftZeroNotSynthesized(t *testing.T) {
	input := `#EXTINF:0 timeshift="0", Channel` + "\nhttp://stream.example.com/ch1/mono.m3u8\n"
	var buf bytes.Buffer
	if err := Rewrite(&buf, strings.NewReader(input), "http://proxy"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "catchup-source") {
		t.Errorf("catchup-source added for timeshift=0: %q", buf.String())
	}
}

func TestRewrite_ExistingCatchupSourceNotDuplicated(t *testing.T) {
	// An explicit catchup-source must be respected, not augmented with a synthesized one.
	input := `#EXTINF:-1 timeshift="7" catchup="default" catchup-source="http://stream.example.com/ch1/mono.m3u8?utc={utc}", Ch` +
		"\nhttp://stream.example.com/ch1/mono.m3u8\n"
	var buf bytes.Buffer
	if err := Rewrite(&buf, strings.NewReader(input), "http://proxy"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Count(out, "catchup-source") != 1 {
		t.Errorf("expected exactly one catchup-source, got: %q", out)
	}
}

func TestRewrite_AppendStyleCatchupCombined(t *testing.T) {
	// append-style: catchup-source is a relative suffix (no http://) appended to the
	// stream URL. It must be combined with the stream URL, routed through the proxy,
	// and normalized to catchup="default".
	stream := "http://stream.example.com/ch9/mono.m3u8?token=abc"
	input := `#EXTINF:-1 catchup="append" catchup-source="&utc={utc}&lutc={lutc}", Ch` +
		"\n" + stream + "\n"
	var buf bytes.Buffer
	if err := Rewrite(&buf, strings.NewReader(input), "http://proxy"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, `catchup="default"`) || strings.Contains(out, `catchup="append"`) {
		t.Errorf("catch-up type not normalized to default: %q", out)
	}
	if !strings.Contains(out, "/proxy/catchup?p=") {
		t.Errorf("append source not routed through /proxy/catchup: %q", out)
	}
	src := catchupSourceValue(t, out)
	rt := roundtripCatchup(t, src, map[string]string{"{utc}": "1748", "{lutc}": "1750"})
	want := stream + "&utc=1748&lutc=1750"
	if rt != want {
		t.Errorf("round-trip = %q, want %q", rt, want)
	}
}

func TestRewrite_EXTGRPBetweenEXTINFAndURLPreserved(t *testing.T) {
	// A #EXTGRP directive sits between #EXTINF and the URL; ordering must be kept
	// and catchup-source must still attach to the #EXTINF line.
	input := `#EXTINF:0 timeshift="7", Ch` + "\n#EXTGRP:News\nhttp://stream.example.com/ch1/mono.m3u8\n"
	var buf bytes.Buffer
	if err := Rewrite(&buf, strings.NewReader(input), "http://proxy"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], "#EXTINF") || !strings.Contains(lines[0], "catchup-source") {
		t.Errorf("EXTINF line wrong: %q", lines[0])
	}
	if lines[1] != "#EXTGRP:News" {
		t.Errorf("EXTGRP not preserved in order: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "http://proxy/proxy/stream?url=") {
		t.Errorf("stream URL line wrong: %q", lines[2])
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
