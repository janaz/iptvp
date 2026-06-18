# iptvp — Claude project notes

## What this project does

An IPTV reverse proxy written in Go. It sits between an IPTV player and an upstream provider, rewriting all stream/EPG/image URLs to route through the proxy. The main use-case is hiding upstream credentials and making token-based or Xtream Codes streams work on any local network device.

## Architecture

```
cmd/iptvp/main.go          — HTTP server, route table, access log
internal/config/config.go  — env-var config, auto-detects Xtream creds from M3U_URL
internal/m3u/              — M3U playlist proxy (/m3u, /proxy/stream, /proxy/catchup)
internal/xtream/           — Xtream Codes API proxy (/player_api.php, /get.php, /xmltv.php, stream paths)
internal/stream/proxy.go   — generic upstream pipe; detects HLS/DASH, rewrites manifests
internal/hls/rewrite.go    — HLS manifest rewriter (segment lines + URI= attrs)
internal/dash/rewrite.go   — DASH MPD rewriter (absolute URLs)
```

## Key design decisions

- **No credentials stored in rewritten URLs.** Xtream stream paths use `proxy/proxy` as dummy creds; the real creds are substituted at request time in `ServeXtreamStream`.
- **`/proxy/stream?url=<base64>`** is the generic pipe for M3U streams and HLS segment/sub-manifest URLs. The base64 encodes the full upstream URL.
- **Catch-up / template URLs.** `catchup-source` attributes in M3U files contain URL templates with `{utc}`, `{lutc}`, etc. These placeholders must remain visible (not inside base64) so the player can substitute them. `proxyURLMaybeTemplate` (`m3u/parser.go`) handles any template URL — placeholders in the **path** (flussonic/xc) or the **query** (shift/append) — by splitting at the first `{` and last `}`:
  - `prefix` (before first `{`) and `suffix` (after last `}`) are base64-encoded → the upstream host and credentials stay hidden.
  - the placeholder span (first `{` … last `}`) stays visible, percent-encoded except for the `{`/`}` delimiters.
  - emitted as `/proxy/catchup?p=<b64prefix>&t=<span>&s=<b64suffix>`.
  `ServeCatchup` (`m3u/handler.go`) reconstructs `remote = decode(p) + t + decode(s)` after the player substitutes the placeholders.
- **Why two endpoints.** `/proxy/stream?url=<base64 full URL>` is for live/segment URLs and **ignores** any extra query params the player appends — this is deliberate: TiViMate auto-appends `utc`/`lutc` to live URLs (resume-from-last-position), and forwarding those would serve archive instead of live. Only `/proxy/catchup` carries time values back to the upstream.
- **Synthesized & append-style catch-up.** Channels that advertise archive via `timeshift`/`catchup-days`/`tvg-rec` but ship no `catchup-source` get a shift-style source synthesized (`synthCatchupSource`). A relative (non-`http`) `catchup-source` (append style) is combined with the stream URL and normalized to `catchup="default"` (`rewriteAppendCatchup`). Both route through `proxyURLMaybeTemplate`, so all catch-up shapes share one encoding path.

## Build & release

Go is not installed locally. Everything builds inside Docker:

```bash
docker build -t janaz/iptvp:latest .
docker push janaz/iptvp:latest
```

The Docker Hub image is `janaz/iptvp` (public).

## Running locally for development

Use the `docker-compose.yml`. Set env vars in a `.env` file:

```
PROXY_BASE_URL=http://192.168.x.x:8080
M3U_URL=http://provider:8080/get.php?username=x&password=y&type=m3u_plus
```

Then `docker compose up --build`.

## Testing

Run tests inside Docker (Go is not installed locally):

```bash
docker build --target builder -t janaz/iptvp-test . && docker run --rm janaz/iptvp-test sh -c "cd /src && go test ./..."
```

**Never use real provider URLs, domains, channel IDs, or tokens in tests.** Use `stream.example.com`, `upstream.example.com`, or similar RFC 2606 example domains. Real URLs leaked into git history require a force-push to clean up.

When testing catch-up/rewind: the `catchup-source` attribute in the rewritten M3U should contain `{utc}` and `{lutc}` as literal text (not hidden in base64). TiViMate and similar players require these to be visible.
