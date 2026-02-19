package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ── Test helpers ─────────────────────────────────────────────────────────────

// startProxy starts the proxy on a random port and returns its address.
// The returned stop function closes the listener.
func startProxy(t *testing.T) (addr string, stop func()) {
	t.Helper()
	state = NewProxyState()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

// proxyRequest sends one GET through the proxy and returns the full response
// text and how many bytes were received by the client (proxy → browser side).
func proxyRequest(t *testing.T, proxyAddr, targetURL string) (resp string, clientBytes int) {
	t.Helper()

	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	host, _, _ := parseURL(targetURL)
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n",
		targetURL, host)

	var sb strings.Builder
	n, _ := io.Copy(&sb, conn)
	return sb.String(), int(n)
}

// countingListener wraps a net.Listener and counts total bytes written by
// every connection it accepts. This measures origin → proxy bandwidth.
type countingListener struct {
	net.Listener
	written atomic.Int64 // bytes sent from origin to proxy
}

type countingConn struct {
	net.Conn
	written *atomic.Int64
}

func (c countingConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	c.written.Add(int64(n))
	return n, err
}

func (l *countingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return countingConn{Conn: conn, written: &l.written}, nil
}

// newCountingServer starts an httptest.Server whose listener counts every byte
// the origin sends. Returns the server and the counter.
func newCountingServer(handler http.Handler) (*httptest.Server, *countingListener) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	cl := &countingListener{Listener: ln}
	s := &httptest.Server{
		Listener: cl,
		Config:   &http.Server{Handler: handler},
	}
	s.Start()
	return s, cl
}

// ── Bandwidth tests ───────────────────────────────────────────────────────────

// TestBandwidthSavedOnCacheHit is the core bandwidth test.
//
// The origin returns a large body (bodySize bytes) with a Last-Modified header.
// We send the same URL twice through the proxy:
//
//   - Request 1 (cache miss): origin must send the full body to the proxy.
//   - Request 2 (cache hit):  proxy sends If-Modified-Since; origin replies 304
//     with no body. The proxy serves the cached body to the browser from memory.
//
// We count origin→proxy bytes for each request and assert that the second
// request transferred significantly fewer bytes from the origin.
func TestBandwidthSavedOnCacheHit(t *testing.T) {
	const bodySize = 100 * 1024 // 100 KB — large enough to measure clearly
	body := strings.Repeat("a", bodySize)

	// requestCount tracks how many times the origin was actually contacted.
	var requestCount atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if r.Header.Get("If-Modified-Since") != "" {
			// Conditional GET — content unchanged, send no body.
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Last-Modified", "Wed, 01 Jan 2025 00:00:00 GMT")
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, body)
	})

	origin, counter := newCountingServer(handler)
	defer origin.Close()

	proxyAddr, stop := startProxy(t)
	defer stop()

	targetURL := "http://" + origin.Listener.Addr().String() + "/resource"

	// ── Request 1: cache miss ─────────────────────────────────────────────
	counter.written.Store(0)
	_, clientBytes1 := proxyRequest(t, proxyAddr, targetURL)
	originBytes1 := int(counter.written.Load())

	t.Logf("Request 1 (uncached):")
	t.Logf("  Origin → proxy : %d bytes", originBytes1)
	t.Logf("  Proxy  → client: %d bytes", clientBytes1)

	// The origin must have sent at least the body.
	if originBytes1 < bodySize {
		t.Errorf("expected origin to send >= %d bytes on cache miss, got %d",
			bodySize, originBytes1)
	}

	// ── Request 2: cache hit ──────────────────────────────────────────────
	counter.written.Store(0)
	_, clientBytes2 := proxyRequest(t, proxyAddr, targetURL)
	originBytes2 := int(counter.written.Load())

	t.Logf("Request 2 (cached):")
	t.Logf("  Origin → proxy : %d bytes  (304, no body)", originBytes2)
	t.Logf("  Proxy  → client: %d bytes  (served from memory)", clientBytes2)

	// ── Assertions ────────────────────────────────────────────────────────

	// The origin should have been hit twice (once for each request).
	if got := int(requestCount.Load()); got != 2 {
		t.Errorf("origin hit %d times, want 2", got)
	}

	// On the cached request the origin sends far fewer bytes (just the 304
	// response headers, no body). Assert it sent less than 10% of the original.
	threshold := originBytes1 / 10
	if originBytes2 >= threshold {
		t.Errorf("cached origin bytes %d >= 10%% of uncached %d (%d) — body was re-sent",
			originBytes2, originBytes1, threshold)
	}

	savedBytes := originBytes1 - originBytes2
	savingPct := 100 * float64(savedBytes) / float64(originBytes1)
	t.Logf("Bandwidth saved: %d bytes (%.1f%%)", savedBytes, savingPct)

	// The client must receive the full body both times (cache serves it from memory).
	if clientBytes2 < bodySize {
		t.Errorf("cached response delivered only %d bytes to client, want >= %d",
			clientBytes2, bodySize)
	}
}

// TestBandwidthSavedAcrossMultipleRequests checks that repeated requests keep
// saving bandwidth — not just the second one.
func TestBandwidthSavedAcrossMultipleRequests(t *testing.T) {
	const bodySize = 50 * 1024 // 50 KB
	body := strings.Repeat("b", bodySize)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-Modified-Since") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Last-Modified", "Fri, 01 Jan 2021 00:00:00 GMT")
		fmt.Fprint(w, body)
	})

	origin, counter := newCountingServer(handler)
	defer origin.Close()

	proxyAddr, stop := startProxy(t)
	defer stop()

	targetURL := "http://" + origin.Listener.Addr().String() + "/repeated"

	// First request — populates the cache.
	counter.written.Store(0)
	proxyRequest(t, proxyAddr, targetURL)
	uncachedOriginBytes := int(counter.written.Load())
	t.Logf("Request 1 (cache miss) origin bytes: %d", uncachedOriginBytes)

	// Requests 2–6 — all should be cache hits with near-zero origin bytes.
	const repeats = 5
	for i := 2; i <= repeats+1; i++ {
		counter.written.Store(0)
		_, clientBytes := proxyRequest(t, proxyAddr, targetURL)
		originBytes := int(counter.written.Load())

		t.Logf("Request %d (cached) origin bytes: %d, client bytes: %d",
			i, originBytes, clientBytes)

		// Origin should send << full body (just a 304 header).
		if originBytes >= uncachedOriginBytes/10 {
			t.Errorf("request %d: origin sent %d bytes, expected < %d (10%% of full body)",
				i, originBytes, uncachedOriginBytes/10)
		}
		// Client must still receive the full body from the proxy's cache.
		if clientBytes < bodySize {
			t.Errorf("request %d: client received only %d bytes, want >= %d",
				i, clientBytes, bodySize)
		}
	}
}

// TestNoBandwidthSavingWithoutLastModified confirms that the cache does NOT
// send conditional GET headers when the origin omitted Last-Modified.
// Without a validator, the proxy cannot do a conditional GET, so it must
// re-fetch the full body each time.
func TestNoBandwidthSavingWithoutLastModified(t *testing.T) {
	const bodySize = 10 * 1024
	body := strings.Repeat("c", bodySize)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliberately no Last-Modified header — proxy cannot cache-validate.
		fmt.Fprint(w, body)
	})

	origin, counter := newCountingServer(handler)
	defer origin.Close()

	proxyAddr, stop := startProxy(t)
	defer stop()

	targetURL := "http://" + origin.Listener.Addr().String() + "/no-validator"

	counter.written.Store(0)
	proxyRequest(t, proxyAddr, targetURL)
	firstOriginBytes := int(counter.written.Load())

	counter.written.Store(0)
	proxyRequest(t, proxyAddr, targetURL)
	secondOriginBytes := int(counter.written.Load())

	t.Logf("Without Last-Modified — request 1 origin bytes: %d, request 2 origin bytes: %d",
		firstOriginBytes, secondOriginBytes)

	// Both requests should transfer the full body from the origin.
	if secondOriginBytes < bodySize {
		t.Errorf("expected origin to re-send full body (%d bytes) without Last-Modified, got %d",
			bodySize, secondOriginBytes)
	}
}

// BenchmarkUncachedHTTP measures bytes-per-operation for cache-miss requests.
func BenchmarkUncachedHTTP(b *testing.B) {
	state = NewProxyState()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 4096))
	}))
	defer origin.Close()

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn)
		}
	}()

	proxyAddr := ln.Addr().String()
	host := origin.Listener.Addr().String()

	var i int
	for b.Loop() {
		// Unique path each iteration forces a cache miss every time.
		targetURL := fmt.Sprintf("http://%s/bench/%d", host, i)
		i++
		conn, _ := net.Dial("tcp", proxyAddr)
		fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n",
			targetURL, host)
		n, _ := io.Copy(io.Discard, conn)
		conn.Close()
		b.SetBytes(n)
	}
}

// BenchmarkCachedHTTP measures bytes-per-operation when every request hits
// the cache. After the warm-up, the origin only receives 304-level traffic.
func BenchmarkCachedHTTP(b *testing.B) {
	state = NewProxyState()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-Modified-Since") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Last-Modified", "Wed, 01 Jan 2025 00:00:00 GMT")
		fmt.Fprint(w, strings.Repeat("y", 4096))
	}))
	defer origin.Close()

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn)
		}
	}()

	proxyAddr := ln.Addr().String()
	host := origin.Listener.Addr().String()
	targetURL := fmt.Sprintf("http://%s/bench/cached", host)

	// Warm the cache before timing starts.
	warmConn, _ := net.Dial("tcp", proxyAddr)
	fmt.Fprintf(warmConn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n",
		targetURL, host)
	io.Copy(io.Discard, warmConn)
	warmConn.Close()

	for b.Loop() {
		conn, _ := net.Dial("tcp", proxyAddr)
		fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n",
			targetURL, host)
		n, _ := io.Copy(io.Discard, conn)
		conn.Close()
		b.SetBytes(n)
	}
}

// TestCacheLogsStatus verifies the proxy log records "Allowed" on a cache miss
// and "Cached" on a cache hit.
func TestCacheLogsStatus(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-Modified-Since") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Last-Modified", "Thu, 01 Jan 2026 00:00:00 GMT")
		fmt.Fprint(w, "body")
	}))
	defer origin.Close()

	proxyAddr, stop := startProxy(t)
	defer stop()

	targetURL := "http://" + origin.Listener.Addr().String() + "/log-test"

	proxyRequest(t, proxyAddr, targetURL) // miss  → Allowed
	proxyRequest(t, proxyAddr, targetURL) // hit   → Cached

	logs := state.GetLogs()
	if len(logs) < 2 {
		t.Fatalf("expected >= 2 log entries, got %d", len(logs))
	}
	// Logs are newest-first.
	if logs[0].Status != "Cached" {
		t.Errorf("second request status = %q, want Cached", logs[0].Status)
	}
	if logs[1].Status != "Allowed" {
		t.Errorf("first request status = %q, want Allowed", logs[1].Status)
	}
}

// TestCachedResponseBodyMatchesOriginal checks correctness: the body delivered
// from cache must be byte-for-byte identical to the original response.
func TestCachedResponseBodyMatchesOriginal(t *testing.T) {
	const wantBody = "deterministic content 12345"

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-Modified-Since") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Last-Modified", "Wed, 01 Jan 2025 00:00:00 GMT")
		fmt.Fprint(w, wantBody)
	}))
	defer origin.Close()

	proxyAddr, stop := startProxy(t)
	defer stop()

	targetURL := "http://" + origin.Listener.Addr().String() + "/page"

	resp1, _ := proxyRequest(t, proxyAddr, targetURL)
	resp2, _ := proxyRequest(t, proxyAddr, targetURL)

	if !strings.Contains(resp1, wantBody) {
		t.Errorf("uncached response missing body:\n%s", resp1)
	}
	if !strings.Contains(resp2, wantBody) {
		t.Errorf("cached response missing body:\n%s", resp2)
	}
}

// TestBandwidthSummary prints a human-readable summary table — useful for
// documentation and the assignment write-up.
func TestBandwidthSummary(t *testing.T) {
	sizes := []struct {
		label string
		bytes int
	}{
		{"10 KB", 10 * 1024},
		{"100 KB", 100 * 1024},
		{"500 KB", 500 * 1024},
	}

	for _, tc := range sizes {
		t.Run(tc.label, func(t *testing.T) {
			body := strings.Repeat("s", tc.bytes)

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("If-Modified-Since") != "" {
					w.WriteHeader(http.StatusNotModified)
					return
				}
				w.Header().Set("Last-Modified", "Mon, 06 Jan 2025 00:00:00 GMT")
				fmt.Fprint(w, body)
			})

			origin, counter := newCountingServer(handler)
			defer origin.Close()

			proxyAddr, stop := startProxy(t)
			defer stop()

			targetURL := "http://" + origin.Listener.Addr().String() + "/summary"

			// Miss
			counter.written.Store(0)
			start := time.Now()
			proxyRequest(t, proxyAddr, targetURL)
			missLatency := time.Since(start)
			missOriginBytes := int(counter.written.Load())

			// Hit
			counter.written.Store(0)
			start = time.Now()
			proxyRequest(t, proxyAddr, targetURL)
			hitLatency := time.Since(start)
			hitOriginBytes := int(counter.written.Load())

			saved := missOriginBytes - hitOriginBytes
			pct := 100.0 * float64(saved) / float64(missOriginBytes)

			t.Logf("Body size: %-8s | Miss: %6d B  %-8v | Hit: %6d B  %-8v | Saved: %6d B  (%.1f%%)",
				tc.label,
				missOriginBytes, missLatency.Round(time.Millisecond),
				hitOriginBytes, hitLatency.Round(time.Millisecond),
				saved, pct,
			)
		})
	}
}
