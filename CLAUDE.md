# iptvp — Claude project notes

## What this project does

An IPTV reverse proxy written in Go. It sits between an IPTV player and an upstream provider, rewriting all stream/EPG/image URLs to route through the proxy. The main use-case is hiding upstream credentials and making token-based or Xtream Codes streams work on any local network device.

## Architecture

```
cmd/iptvp/main.go          — HTTP server, route table, access log
internal/config/config.go  — env-var config, auto-detects Xtream creds from M3U_URL
internal/m3u/              — M3U playlist proxy (/m3u, /proxy/stream)
internal/xtream/           — Xtream Codes API proxy (/player_api.php, /get.php, /xmltv.php, stream paths)
internal/stream/proxy.go   — generic upstream pipe; detects HLS/DASH, rewrites manifests
internal/hls/rewrite.go    — HLS manifest rewriter (segment lines + URI= attrs)
internal/dash/rewrite.go   — DASH MPD rewriter (absolute URLs)
```

## Key design decisions

- **No credentials stored in rewritten URLs.** Xtream stream paths use `proxy/proxy` as dummy creds; the real creds are substituted at request time in `ServeXtreamStream`.
- **`/proxy/stream?url=<base64>`** is the generic pipe for M3U streams and HLS segment/sub-manifest URLs. The base64 encodes the full upstream URL.
- **Catch-up / template URLs.** `catchup-source` attributes in M3U files contain URL templates with `{utc}`, `{lutc}`, etc. These placeholders must remain visible (not inside base64) so the player can substitute them. The `proxyURLMaybeTemplate` function in `m3u/parser.go` splits template query params out of the base64-encoded stable part and appends them as plain query params. `ServeStream` merges any extra query params back into the upstream URL before fetching.

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

No automated tests. Verify by watching the container logs — look for `stream: HLS`, `stream: RAW`, and confirm segment URLs decode to the expected upstream.

When testing catch-up/rewind: the `catchup-source` attribute in the rewritten M3U should contain `{utc}` and `{lutc}` as literal text (not hidden in base64). TiViMate and similar players require these to be visible.
