package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	ProxyBaseURL   string
	Port           string
	M3UURL         string
	XtreamBaseURL  string
	XtreamUsername string
	XtreamPassword string
	STBPortalURL   string
	STBMAC         string
}

func Load() (*Config, error) {
	port := env("PROXY_PORT", "8080")
	baseURL := env("PROXY_BASE_URL", fmt.Sprintf("http://localhost:%s", port))
	baseURL = strings.TrimRight(baseURL, "/")

	m3uURL := os.Getenv("M3U_URL")
	xtreamBase := strings.TrimRight(os.Getenv("XTREAM_BASE_URL"), "/")
	xtreamUser := os.Getenv("XTREAM_USERNAME")
	xtreamPass := os.Getenv("XTREAM_PASSWORD")
	stbPortalURL := strings.TrimRight(os.Getenv("STB_PORTAL_URL"), "/")
	stbMAC := os.Getenv("STB_MAC")

	// Auto-detect Xtream config from M3U_URL when it follows the Xtream
	// get.php format and the Xtream vars are not explicitly set.
	if m3uURL != "" && xtreamBase == "" {
		if b, u, p, ok := parseXtreamM3UURL(m3uURL); ok {
			xtreamBase = b
			if xtreamUser == "" {
				xtreamUser = u
			}
			if xtreamPass == "" {
				xtreamPass = p
			}
		}
	}

	return &Config{
		ProxyBaseURL:   baseURL,
		Port:           port,
		M3UURL:         m3uURL,
		XtreamBaseURL:  xtreamBase,
		XtreamUsername: xtreamUser,
		XtreamPassword: xtreamPass,
		STBPortalURL:   stbPortalURL,
		STBMAC:         stbMAC,
	}, nil
}

// parseXtreamM3UURL detects URLs of the form:
//
//	http://host:port/get.php?username=x&password=y&...
//
// and returns (baseURL, username, password, true) when matched.
func parseXtreamM3UURL(raw string) (base, username, password string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return
	}
	if !strings.EqualFold(u.Path, "/get.php") {
		return
	}
	q := u.Query()
	username = q.Get("username")
	password = q.Get("password")
	if username == "" || password == "" {
		return
	}
	base = fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	ok = true
	return
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
