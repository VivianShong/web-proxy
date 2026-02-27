# Web Proxy

A forward HTTP/HTTPS proxy server written in Go with no external dependencies. It proxies browser traffic, caches responses intelligently, and provides a web management console for blocking hosts at runtime.

## Features

- **HTTP and HTTPS proxying** — handles plain HTTP requests and CONNECT tunnels for HTTPS
- **Dynamic host blocking** — block/unblock hostnames at runtime via the management console; subdomain matching included (blocking `google.com` also blocks `www.google.com`)
- **Response caching** — in-memory LRU cache with TTL and conditional GET (ETag / If-Modified-Since) to minimize redundant fetches
- **Request logging** — tracks method, URL, status, timestamp, and source IP for the last 100 requests
- **Concurrent connections** — goroutine-per-connection model

## Requirements

- Go 1.25+

## Running the Proxy

```bash
go run .
```

Or build first:

```bash
go build -o proxy
./proxy
```

The proxy listens on two ports:

| Port | Purpose |
|------|---------|
| `4000` | Proxy server — point your browser here |
| `8080` | Management console — web dashboard |

## Browser Setup

Configure your browser to use an HTTP proxy at `localhost:4000`. In most browsers this is under **Settings → Network → Manual proxy configuration**.

## Management Console

Open `http://localhost:8080` in your browser to access the dashboard. It shows:

- **Blocked hosts** — currently blocked domains with unblock buttons
- **Cached URLs** — responses held in the in-memory cache
- **Recent requests** — last 100 proxied requests with status

### Blocking a host

From the dashboard, enter a hostname (e.g. `ads.example.com`) and click **Block**. Active connections to that host are closed immediately.

### API endpoints

```
POST /block    body: hostname=<host>
POST /unblock  body: hostname=<host>
```

## Configuration

Tunable constants in the source:

| File | Constant | Default | Description |
|------|----------|---------|-------------|
| `cache.go` | `cacheTTL` | `5m` | Time before a cached response is considered stale |
| `cache.go` | `cacheMaxEntries` | `100` | Maximum cached responses (LRU eviction) |
| `proxyState.go` | `LogLimit` | `100` | Number of recent requests kept in memory |

Blocked hosts are persisted to `blocked.json` in the working directory and reloaded automatically on startup.

## Running Tests

```bash
go test ./...
```

Latency benchmarks:

```bash
go test -bench=. -benchtime=10s
```
