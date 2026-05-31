# iptvp

An IPTV proxy written in Go. Sits between your IPTV client and upstream provider, rewriting all stream, EPG, and image URLs to route through the proxy. Clients never contact the upstream directly.

Docker Hub: [`janaz/iptvp`](https://hub.docker.com/r/janaz/iptvp)

## Features

- **M3U proxy** — fetches an upstream M3U playlist and rewrites every URL (streams, logos, EPG) to go through the proxy
- **Xtream Codes proxy** — full API surface (`/player_api.php`, `/get.php`, `/xmltv.php`, stream paths), credentials swapped transparently
- **HLS rewriting** — detects HLS manifests (by Content-Type, URL extension, or body sniff) and rewrites all segment and sub-playlist URLs
- **MPEG-DASH rewriting** — rewrites absolute URLs in MPD manifests
- **Redirect following** — all upstream 302 redirects are followed internally; clients only see the proxy
- **Auto-detect Xtream config** from `M3U_URL` when it follows the `get.php` format
- Access log to stderr

## Configuration

All configuration is via environment variables.

| Variable | Default | Description |
|---|---|---|
| `PROXY_BASE_URL` | `http://localhost:$PROXY_PORT` | Public URL advertised in rewritten URLs (can differ from listen port) |
| `PROXY_PORT` | `8080` | Port the server listens on |
| `M3U_URL` | — | Upstream M3U playlist URL |
| `XTREAM_BASE_URL` | auto from `M3U_URL` | Upstream Xtream server base URL |
| `XTREAM_USERNAME` | auto from `M3U_URL` | Upstream Xtream username |
| `XTREAM_PASSWORD` | auto from `M3U_URL` | Upstream Xtream password |
| `STB_PORTAL_URL` | — | Upstream Stalker/STB portal base URL |
| `STB_MAC` | — | Real MAC address registered with the provider |

`PROXY_BASE_URL` and `PROXY_PORT` are independent — useful when the proxy sits behind a reverse proxy.

If `M3U_URL` is an Xtream `get.php` URL (e.g. `http://provider.com:8080/get.php?username=x&password=y&type=m3u_plus`), the Xtream variables are auto-detected from it.

## Endpoints

| Path | Description |
|---|---|
| `GET /m3u` | Rewritten M3U playlist |
| `GET /proxy/stream?url=<base64>` | Stream proxy (used by rewritten M3U URLs) |
| `GET /player_api.php` | Xtream Codes API (JSON rewritten) |
| `GET /get.php` | Xtream M3U_Plus playlist (URLs rewritten) |
| `GET /xmltv.php` | EPG XML passthrough |
| `GET /live/{user}/{pass}/{id}.ts` | Xtream live stream proxy |
| `GET /movie/{user}/{pass}/{id}.mp4` | Xtream VOD proxy |
| `GET /series/{user}/{pass}/{id}.mkv` | Xtream series proxy |
| `GET /portal.php` | Stalker/STB portal proxy |

## Quick start

### docker run

```bash
docker run -d \
  -p 8080:8080 \
  -e PROXY_BASE_URL=http://YOUR_IP:8080 \
  -e M3U_URL=http://provider.com:8080/get.php?username=x&password=y&type=m3u_plus \
  janaz/iptvp
```

### docker compose

```yaml
services:
  iptvp:
    image: janaz/iptvp
    ports:
      - "8080:8080"
    environment:
      PROXY_BASE_URL: http://YOUR_IP:8080
      PROXY_PORT: "8080"
      M3U_URL: http://provider.com:8080/get.php?username=x&password=y&type=m3u_plus
    restart: unless-stopped
```

### TiViMate — M3U source

Point TiViMate at `http://YOUR_IP:8080/m3u`.

### TiViMate — Xtream Codes source

| Field | Value |
|---|---|
| Server | `http://YOUR_IP:8080` |
| Username | anything |
| Password | anything |

The proxy ignores client credentials and substitutes the real upstream credentials internally.

### TiViMate — Stalker/STB portal source

| Field | Value |
|---|---|
| Portal URL | `http://YOUR_IP:8080/` |
| MAC address | anything |

The proxy ignores the client MAC and substitutes the real `STB_MAC` on every request to the upstream portal.

## Behind a reverse proxy

Set `PROXY_BASE_URL` to the public-facing URL and `PROXY_PORT` to the internal listen port independently:

```yaml
PROXY_PORT: "9952"          # Go app listens here
PROXY_BASE_URL: http://YOUR_IP:80   # nginx/Caddy terminates here
```
