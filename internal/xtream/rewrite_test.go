package xtream

import (
	"encoding/json"
	"strings"
	"testing"
)

// ── replaceCredentials ────────────────────────────────────────────────────

func TestReplaceCredentials_Basic(t *testing.T) {
	path := "/live/realuser/realpass/12345.ts"
	got := replaceCredentials(path, "realuser", "realpass", "proxy", "proxy")
	want := "/live/proxy/proxy/12345.ts"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReplaceCredentials_EmptyUser(t *testing.T) {
	// Empty upstream user means no credential replacement.
	path := "/live/realuser/realpass/12345.ts"
	got := replaceCredentials(path, "", "realpass", "proxy", "proxy")
	if got != path {
		t.Errorf("empty user: expected unchanged path, got %q", got)
	}
}

func TestReplaceCredentials_PreservesRest(t *testing.T) {
	path := "/movie/user/pass/99999.mp4"
	got := replaceCredentials(path, "user", "pass", "p", "q")
	want := "/movie/p/q/99999.mp4"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ── rewriteURL ────────────────────────────────────────────────────────────

func TestRewriteURL_LiveStream(t *testing.T) {
	s := "http://upstream:8080/live/user/pass/12345.ts"
	got := rewriteURL(s, "http://proxy", "http://upstream:8080", "user", "pass")
	want := "http://proxy/live/proxy/proxy/12345.ts"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteURL_MovieStream(t *testing.T) {
	s := "http://upstream:8080/movie/user/pass/42.mp4"
	got := rewriteURL(s, "http://proxy", "http://upstream:8080", "user", "pass")
	want := "http://proxy/movie/proxy/proxy/42.mp4"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteURL_TimeshiftStream(t *testing.T) {
	// Timeshift paths include extra segments (duration, start time, id).
	s := "http://upstream:8080/timeshift/user/pass/120/2024-01-15:20-00/12345.ts"
	got := rewriteURL(s, "http://proxy", "http://upstream:8080", "user", "pass")
	want := "http://proxy/timeshift/proxy/proxy/120/2024-01-15:20-00/12345.ts"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteURL_NonMatchingBaseUnchanged(t *testing.T) {
	// URLs from other hosts must not be modified.
	s := "http://other.cdn.com/image.png"
	got := rewriteURL(s, "http://proxy", "http://upstream:8080", "user", "pass")
	if got != s {
		t.Errorf("non-matching URL should be unchanged, got %q", got)
	}
}

func TestRewriteURL_PlainStringUnchanged(t *testing.T) {
	// Non-URL strings (channel names etc.) must pass through.
	s := "My Channel Name"
	got := rewriteURL(s, "http://proxy", "http://upstream:8080", "user", "pass")
	if got != s {
		t.Errorf("plain string should be unchanged, got %q", got)
	}
}

// ── ParseAndRewrite ───────────────────────────────────────────────────────

func TestParseAndRewrite_FlatObject(t *testing.T) {
	input := `{"stream_url":"http://upstream:8080/live/user/pass/1.ts","name":"Channel"}`
	got, err := ParseAndRewrite([]byte(input), "http://proxy", "http://upstream:8080", "user", "pass")
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if out["stream_url"] != "http://proxy/live/proxy/proxy/1.ts" {
		t.Errorf("stream_url = %q", out["stream_url"])
	}
	if out["name"] != "Channel" {
		t.Errorf("name should be unchanged, got %q", out["name"])
	}
}

func TestParseAndRewrite_NestedArray(t *testing.T) {
	input := `[{"url":"http://upstream:8080/movie/user/pass/1.mp4"},{"url":"http://other.com/img.png"}]`
	got, err := ParseAndRewrite([]byte(input), "http://proxy", "http://upstream:8080", "user", "pass")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "http://proxy/movie/proxy/proxy/1.mp4") {
		t.Errorf("nested upstream URL not rewritten: %s", got)
	}
	if !strings.Contains(string(got), "http://other.com/img.png") {
		t.Errorf("non-upstream URL should be unchanged: %s", got)
	}
}

func TestParseAndRewrite_DeeplyNested(t *testing.T) {
	input := `{"info":{"streams":[{"url":"http://upstream:8080/live/u/p/99.ts"}]}}`
	got, err := ParseAndRewrite([]byte(input), "http://proxy", "http://upstream:8080", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "http://proxy/live/proxy/proxy/99.ts") {
		t.Errorf("deeply nested URL not rewritten: %s", got)
	}
}

func TestParseAndRewrite_InvalidJSONPassthrough(t *testing.T) {
	// Non-JSON bodies (e.g. error pages) must be returned unchanged.
	input := []byte("upstream error 503")
	got, err := ParseAndRewrite(input, "http://proxy", "http://upstream:8080", "u", "p")
	if err != nil {
		t.Fatal("should not error on invalid JSON")
	}
	if string(got) != string(input) {
		t.Errorf("invalid JSON changed: got %q, want %q", got, input)
	}
}

func TestParseAndRewrite_NumbersAndBoolsUnchanged(t *testing.T) {
	input := `{"id":42,"active":true,"rating":4.5}`
	got, err := ParseAndRewrite([]byte(input), "http://proxy", "http://upstream:8080", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if out["id"] != float64(42) {
		t.Errorf("id changed: %v", out["id"])
	}
	if out["active"] != true {
		t.Errorf("active changed: %v", out["active"])
	}
}
