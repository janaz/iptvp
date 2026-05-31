package config

import (
	"testing"
)

// ── parseXtreamM3UURL ─────────────────────────────────────────────────────

func TestParseXtreamM3UURL_Valid(t *testing.T) {
	raw := "http://provider.com:8080/get.php?username=myuser&password=mypass&type=m3u_plus"
	base, user, pass, ok := parseXtreamM3UURL(raw)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if base != "http://provider.com:8080" {
		t.Errorf("base = %q, want http://provider.com:8080", base)
	}
	if user != "myuser" {
		t.Errorf("user = %q, want myuser", user)
	}
	if pass != "mypass" {
		t.Errorf("pass = %q, want mypass", pass)
	}
}

func TestParseXtreamM3UURL_HTTPS(t *testing.T) {
	raw := "https://provider.com/get.php?username=u&password=p"
	base, _, _, ok := parseXtreamM3UURL(raw)
	if !ok {
		t.Fatal("expected ok=true for HTTPS")
	}
	if base != "https://provider.com" {
		t.Errorf("base = %q, want https://provider.com", base)
	}
}

func TestParseXtreamM3UURL_CaseInsensitivePath(t *testing.T) {
	// Providers sometimes use uppercase or mixed-case path.
	raw := "http://provider.com:8080/GET.PHP?username=u&password=p"
	_, _, _, ok := parseXtreamM3UURL(raw)
	if !ok {
		t.Error("expected ok=true for uppercase GET.PHP")
	}
}

func TestParseXtreamM3UURL_NotGetPHP(t *testing.T) {
	raw := "http://provider.com:8080/playlist.m3u?username=u&password=p"
	_, _, _, ok := parseXtreamM3UURL(raw)
	if ok {
		t.Error("expected ok=false for non-get.php URL")
	}
}

func TestParseXtreamM3UURL_MissingBothCredentials(t *testing.T) {
	raw := "http://provider.com:8080/get.php?type=m3u_plus"
	_, _, _, ok := parseXtreamM3UURL(raw)
	if ok {
		t.Error("expected ok=false when credentials are absent")
	}
}

func TestParseXtreamM3UURL_MissingPassword(t *testing.T) {
	raw := "http://provider.com:8080/get.php?username=myuser"
	_, _, _, ok := parseXtreamM3UURL(raw)
	if ok {
		t.Error("expected ok=false when password is missing")
	}
}

func TestParseXtreamM3UURL_MissingUsername(t *testing.T) {
	raw := "http://provider.com:8080/get.php?password=mypass"
	_, _, _, ok := parseXtreamM3UURL(raw)
	if ok {
		t.Error("expected ok=false when username is missing")
	}
}

func TestParseXtreamM3UURL_InvalidURL(t *testing.T) {
	_, _, _, ok := parseXtreamM3UURL("not a url at all")
	if ok {
		t.Error("expected ok=false for unparsable URL")
	}
}

func TestParseXtreamM3UURL_EmptyString(t *testing.T) {
	_, _, _, ok := parseXtreamM3UURL("")
	if ok {
		t.Error("expected ok=false for empty string")
	}
}

// ── Load (env-var integration) ────────────────────────────────────────────

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("PROXY_PORT", "")
	t.Setenv("PROXY_BASE_URL", "")
	t.Setenv("M3U_URL", "")
	t.Setenv("XTREAM_BASE_URL", "")
	t.Setenv("XTREAM_USERNAME", "")
	t.Setenv("XTREAM_PASSWORD", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8080" {
		t.Errorf("default port = %q, want 8080", cfg.Port)
	}
	if cfg.ProxyBaseURL != "http://localhost:8080" {
		t.Errorf("default base URL = %q", cfg.ProxyBaseURL)
	}
}

func TestLoad_AutoDetectsXtreamFromM3UURL(t *testing.T) {
	t.Setenv("M3U_URL", "http://provider.com:8080/get.php?username=u&password=p&type=m3u_plus")
	t.Setenv("XTREAM_BASE_URL", "")
	t.Setenv("XTREAM_USERNAME", "")
	t.Setenv("XTREAM_PASSWORD", "")
	t.Setenv("PROXY_PORT", "8080")
	t.Setenv("PROXY_BASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.XtreamBaseURL != "http://provider.com:8080" {
		t.Errorf("XtreamBaseURL = %q, want http://provider.com:8080", cfg.XtreamBaseURL)
	}
	if cfg.XtreamUsername != "u" {
		t.Errorf("XtreamUsername = %q, want u", cfg.XtreamUsername)
	}
	if cfg.XtreamPassword != "p" {
		t.Errorf("XtreamPassword = %q, want p", cfg.XtreamPassword)
	}
}

func TestLoad_ExplicitXtreamOverridesAutoDetect(t *testing.T) {
	t.Setenv("M3U_URL", "http://provider.com:8080/get.php?username=autouser&password=autopass")
	t.Setenv("XTREAM_BASE_URL", "http://other.com:9090")
	t.Setenv("XTREAM_USERNAME", "explicit")
	t.Setenv("XTREAM_PASSWORD", "secret")
	t.Setenv("PROXY_PORT", "8080")
	t.Setenv("PROXY_BASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.XtreamBaseURL != "http://other.com:9090" {
		t.Errorf("XtreamBaseURL = %q, want explicit value", cfg.XtreamBaseURL)
	}
	if cfg.XtreamUsername != "explicit" {
		t.Errorf("XtreamUsername = %q, want explicit", cfg.XtreamUsername)
	}
}

func TestLoad_TrailingSlashStripped(t *testing.T) {
	t.Setenv("PROXY_BASE_URL", "http://myhost:8080/")
	t.Setenv("PROXY_PORT", "8080")
	t.Setenv("M3U_URL", "")
	t.Setenv("XTREAM_BASE_URL", "http://upstream:8080/")
	t.Setenv("XTREAM_USERNAME", "")
	t.Setenv("XTREAM_PASSWORD", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProxyBaseURL == "http://myhost:8080/" {
		t.Error("trailing slash not stripped from ProxyBaseURL")
	}
	if cfg.XtreamBaseURL == "http://upstream:8080/" {
		t.Error("trailing slash not stripped from XtreamBaseURL")
	}
}
