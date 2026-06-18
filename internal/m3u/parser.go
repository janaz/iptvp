package m3u

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

// attrURLRe matches any quoted HTTP(S) URL in an M3U attribute value, e.g.:
//
//	tvg-logo="http://..." or url-tvg="https://..."
var attrURLRe = regexp.MustCompile(`"(https?://[^"]+)"`)

// catchupDaysRe extracts the catch-up window (in days) from an #EXTINF line.
// Providers advertise archive availability via one of these attributes without
// necessarily supplying an explicit catchup-source URL.
var catchupDaysRe = regexp.MustCompile(`(?:timeshift|catchup-days|tvg-rec)="?(\d+)"?`)

// catchupSourceRe extracts the catchup-source attribute value from an #EXTINF line.
var catchupSourceRe = regexp.MustCompile(`catchup-source="([^"]*)"`)

// catchupTypeRe matches the catch-up type attribute (catchup="…" or catchup-type="…").
var catchupTypeRe = regexp.MustCompile(`catchup(?:-type)?="[^"]*"`)

// Rewrite reads an M3U playlist from r and writes it to w, replacing every
// HTTP(S) URL — whether a plain stream line or embedded in an attribute value
// (tvg-logo, tvg-url, url-tvg, x-tvg-url, etc.) — with a proxied URL.
//
// For channels that advertise catch-up/archive support via a
// timeshift/catchup-days/tvg-rec attribute but carry no explicit catchup-source,
// a catchup-source pointing at the proxy's /proxy/catchup endpoint is synthesized
// (with {utc}/{lutc} placeholders left visible) so the player can request archive
// content. Without this, time-shift requests would fall through to /proxy/stream,
// which never forwards time parameters to the upstream.
func Rewrite(w io.Writer, r io.Reader, proxyBase string) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var extinf string    // pending #EXTINF line awaiting its stream URL
	var between []string  // directive/blank lines between #EXTINF and the URL

	writeLine := func(s string) error {
		_, err := fmt.Fprintln(w, s)
		return err
	}
	// flushPending emits a buffered #EXTINF block that never got a URL line.
	flushPending := func() error {
		if extinf == "" {
			return nil
		}
		if err := writeLine(rewriteAttrs(extinf, proxyBase)); err != nil {
			return err
		}
		for _, b := range between {
			if err := writeLine(b); err != nil {
				return err
			}
		}
		extinf, between = "", nil
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case isURL(line):
			if extinf != "" {
				out := rewriteAttrs(extinf, proxyBase)
				out = rewriteAppendCatchup(out, line, proxyBase)
				if src := synthCatchupSource(extinf, line, proxyBase); src != "" {
					out = insertEXTINFAttrs(out, `catchup="default" catchup-source="`+src+`"`)
				}
				if err := writeLine(out); err != nil {
					return err
				}
				for _, b := range between {
					if err := writeLine(b); err != nil {
						return err
					}
				}
				extinf, between = "", nil
			}
			if err := writeLine(proxyURL(proxyBase, line)); err != nil {
				return err
			}
		case strings.HasPrefix(line, "#EXTINF"):
			if err := flushPending(); err != nil {
				return err
			}
			extinf = line
		case extinf != "":
			// Directive or blank line between #EXTINF and its URL (e.g. #EXTGRP).
			between = append(between, rewriteAttrs(line, proxyBase))
		case strings.HasPrefix(line, "#"):
			if err := writeLine(rewriteAttrs(line, proxyBase)); err != nil {
				return err
			}
		default:
			if err := writeLine(line); err != nil {
				return err
			}
		}
	}
	if err := flushPending(); err != nil {
		return err
	}
	return scanner.Err()
}

// synthCatchupSource returns a /proxy/catchup URL template for a catch-up-capable
// channel, or "" if the channel does not advertise catch-up or already carries an
// explicit catchup configuration (which is left untouched). It builds the shift-style
// upstream template (stream URL + utc/lutc placeholders) and routes it through
// proxyURLMaybeTemplate, so synthesized and explicit sources share one encoding path.
func synthCatchupSource(extinf, upstreamURL, proxyBase string) string {
	if strings.Contains(extinf, "catchup-source") ||
		strings.Contains(extinf, "catchup-type") ||
		strings.Contains(extinf, `catchup="`) {
		return "" // respect explicit catch-up configuration
	}
	m := catchupDaysRe.FindStringSubmatch(extinf)
	if m == nil || m[1] == "0" {
		return ""
	}
	sep := "?"
	if strings.Contains(upstreamURL, "?") {
		sep = "&"
	}
	return proxyURLMaybeTemplate(proxyBase, upstreamURL+sep+"utc={utc}&lutc={lutc}")
}

// rewriteAppendCatchup handles append-style catch-up, where catchup-source holds a
// relative suffix (e.g. "?utc={utc}&lutc={lutc}") appended to the stream URL rather
// than a full URL. rewriteAttrs only rewrites full http(s) URLs, so a relative source
// is still present here: it is combined with the stream URL, routed through the proxy,
// and the catch-up type normalized to "default". Full-URL and absent sources are
// left unchanged.
func rewriteAppendCatchup(extinf, streamURL, proxyBase string) string {
	m := catchupSourceRe.FindStringSubmatch(extinf)
	if m == nil {
		return extinf
	}
	val := m[1]
	if val == "" || strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://") {
		return extinf
	}
	proxied := proxyURLMaybeTemplate(proxyBase, streamURL+val)
	extinf = strings.Replace(extinf, m[0], `catchup-source="`+proxied+`"`, 1)
	return normalizeCatchupType(extinf)
}

// normalizeCatchupType forces the catch-up type to "default" (adding it if absent),
// since rewriteAppendCatchup turns a relative source into a full URL template.
func normalizeCatchupType(extinf string) string {
	if catchupTypeRe.MatchString(extinf) {
		return catchupTypeRe.ReplaceAllString(extinf, `catchup="default"`)
	}
	return insertEXTINFAttrs(extinf, `catchup="default"`)
}

// insertEXTINFAttrs inserts space-separated attributes into an #EXTINF line just
// before the comma that separates the attribute list from the channel title.
// Commas inside quoted attribute values are ignored.
func insertEXTINFAttrs(line, attrs string) string {
	inQuote := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQuote = !inQuote
		case ',':
			if !inQuote {
				return line[:i] + " " + attrs + line[i:]
			}
		}
	}
	return line + " " + attrs
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

// proxyURLMaybeTemplate routes a URL through the proxy. URLs with no template
// placeholders go to /proxy/stream with the whole URL base64-encoded — stateless and
// immune to extra query params a player may append to a live stream.
//
// URLs containing {placeholders} (catch-up/archive templates, whether in the path or
// the query) go to /proxy/catchup. The stable parts surrounding the placeholder span
// are base64-encoded (hiding the upstream host and credentials); only the minimal span
// from the first "{" to the last "}" is kept visible so the player can substitute real
// values. /proxy/catchup reconstructs prefix + substituted-span + suffix before fetching.
func proxyURLMaybeTemplate(base, upstream string) string {
	first := strings.IndexByte(upstream, '{')
	last := strings.LastIndexByte(upstream, '}')
	if first < 0 || last < first {
		return proxyURL(base, upstream)
	}
	prefix := upstream[:first]
	span := upstream[first : last+1]
	suffix := upstream[last+1:]
	return fmt.Sprintf("%s/proxy/catchup?p=%s&t=%s&s=%s", base,
		base64.URLEncoding.EncodeToString([]byte(prefix)),
		encodeTemplateSpan(span),
		base64.URLEncoding.EncodeToString([]byte(suffix)))
}

// encodeTemplateSpan percent-encodes a template span for safe inclusion in a query
// value while keeping the {} placeholder delimiters literal, so the player still
// recognizes and substitutes the placeholders.
func encodeTemplateSpan(span string) string {
	e := url.QueryEscape(span)
	e = strings.ReplaceAll(e, "%7B", "{")
	e = strings.ReplaceAll(e, "%7D", "}")
	return e
}
