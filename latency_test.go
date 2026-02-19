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
// With TTL caching, a cache hit skips the origin entirely — zero bytes are
// fetched from the origin on the second request. The proxy serves the body
// directly from memory without contacting the origin at all.
//
//   - Request 1 (cache miss):  origin sends the full body; proxy stores it.
//   - Request 2 (TTL cache hit): origin receives no request; proxy serves from memory.
func TestBandwidthSavedOnCacheHit(t *testing.T) {
	const bodySize = 100 * 1024 // 100 KB
	body := strings.Repeat("a", bodySize)

	var requestCount atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
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

	if originBytes1 < bodySize {
		t.Errorf("expected origin to send >= %d bytes on cache miss, got %d",
			bodySize, originBytes1)
	}

	// ── Request 2: TTL cache hit ──────────────────────────────────────────
	counter.written.Store(0)
	_, clientBytes2 := proxyRequest(t, proxyAddr, targetURL)
	originBytes2 := int(counter.written.Load())

	t.Logf("Request 2 (TTL cache hit):")
	t.Logf("  Origin → proxy : %d bytes  (origin not contacted)", originBytes2)
	t.Logf("  Proxy  → client: %d bytes  (served from memory)", clientBytes2)

	// ── Assertions ────────────────────────────────────────────────────────

	// With TTL caching the origin is only hit once — the second request is
	// served entirely from memory without an origin round-trip.
	if got := int(requestCount.Load()); got != 1 {
		t.Errorf("origin hit %d times, want 1 (TTL cache should skip origin)", got)
	}

	// Zero bytes from origin on cache hit.
	if originBytes2 != 0 {
		t.Errorf("cached origin bytes = %d, want 0 (no origin contact within TTL)",
			originBytes2)
	}

	savedBytes := originBytes1 - originBytes2
	savingPct := 100 * float64(savedBytes) / float64(originBytes1)
	t.Logf("Bandwidth saved: %d bytes (%.1f%%)", savedBytes, savingPct)

	// Client must receive the full body from the proxy's memory.
	if clientBytes2 < bodySize {
		t.Errorf("cached response delivered only %d bytes to client, want >= %d",
			clientBytes2, bodySize)
	}
}

// TestBandwidthSavedAcrossMultipleRequests checks that repeated requests within
// the TTL window all serve from memory — zero origin bytes after the first fetch.
func TestBandwidthSavedAcrossMultipleRequests(t *testing.T) {
	const bodySize = 50 * 1024 // 50 KB
	body := strings.Repeat("b", bodySize)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		// Within TTL: origin should receive no request at all — 0 bytes.
		if originBytes != 0 {
			t.Errorf("request %d: origin sent %d bytes, expected 0 (TTL hit, no origin contact)",
				i, originBytes)
		}
		// Client must still receive the full body from the proxy's cache.
		if clientBytes < bodySize {
			t.Errorf("request %d: client received only %d bytes, want >= %d",
				i, clientBytes, bodySize)
		}
	}
}

// TestCacheExpiry verifies that after the TTL elapses the proxy treats the
// entry as a miss and re-fetches the full body from the origin.
// We shrink the TTL to 50 ms for the duration of this test.
func TestCacheExpiry(t *testing.T) {
	const bodySize = 4 * 1024
	body := strings.Repeat("c", bodySize)
	var requestCount atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		fmt.Fprint(w, body)
	})

	origin, counter := newCountingServer(handler)
	defer origin.Close()

	proxyAddr, stop := startProxy(t)
	defer stop()

	targetURL := "http://" + origin.Listener.Addr().String() + "/expiry"

	// Request 1: populates the cache.
	counter.written.Store(0)
	proxyRequest(t, proxyAddr, targetURL)
	originBytes1 := int(counter.written.Load())
	t.Logf("Request 1 (miss): origin bytes = %d", originBytes1)

	// Request 2: within TTL — should hit cache, zero origin bytes.
	counter.written.Store(0)
	proxyRequest(t, proxyAddr, targetURL)
	originBytes2 := int(counter.written.Load())
	t.Logf("Request 2 (TTL hit): origin bytes = %d", originBytes2)
	if originBytes2 != 0 {
		t.Errorf("request 2: expected 0 origin bytes (TTL hit), got %d", originBytes2)
	}

	// Wait for the entry to expire by temporarily patching the CachedAt to the past.
	state.mu.Lock()
	if entry, ok := state.Cache.entries[targetURL]; ok {
		entry.CachedAt = entry.CachedAt.Add(-cacheTTL - time.Second)
		state.Cache.entries[targetURL] = entry
	}
	state.mu.Unlock()

	// Request 3: TTL expired — proxy must re-fetch from origin.
	counter.written.Store(0)
	proxyRequest(t, proxyAddr, targetURL)
	originBytes3 := int(counter.written.Load())
	t.Logf("Request 3 (expired, re-fetch): origin bytes = %d", originBytes3)

	if originBytes3 < bodySize {
		t.Errorf("request 3: expected origin to send >= %d bytes after TTL expiry, got %d",
			bodySize, originBytes3)
	}
	if got := int(requestCount.Load()); got != 2 {
		t.Errorf("origin hit %d times, want 2 (miss + re-fetch after expiry)", got)
	}
}

// TestExpiredTTLUnchangedData measures the bandwidth and latency cost when the
// TTL has expired but the origin content is unchanged.
//
// After expiry the proxy re-fetches the full body from the origin even though
// the data has not changed — there is no conditional GET optimisation once the
// TTL cache is in use. Bandwidth and latency revert to full-miss cost.
//
// Sequence:
//
//	Request 1 (miss)     — full fetch, origin bytes = bodySize, CachedAt set
//	Request 2 (TTL hit)  — served from memory, origin bytes = 0
//	[expire cache entry by backdating CachedAt]
//	Request 3 (expired)  — full re-fetch, origin bytes ≈ bodySize (same as miss)
func TestExpiredTTLUnchangedData(t *testing.T) {
	const bodySize = 50 * 1024
	body := strings.Repeat("u", bodySize)

	var requestCount atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, body)
	})

	origin, counter := newCountingServer(handler)
	defer origin.Close()

	proxyAddr, stop := startProxy(t)
	defer stop()

	targetURL := "http://" + origin.Listener.Addr().String() + "/unchanged"

	// ── Request 1: cache miss ──────────────────────────────────────────────
	counter.written.Store(0)
	start := time.Now()
	proxyRequest(t, proxyAddr, targetURL)
	missLatency := time.Since(start)
	missOriginBytes := int(counter.written.Load())
	t.Logf("Request 1 (miss):     origin bytes = %6d  latency = %v",
		missOriginBytes, missLatency.Round(time.Millisecond))

	// ── Request 2: TTL hit — origin not contacted ──────────────────────────
	counter.written.Store(0)
	start = time.Now()
	proxyRequest(t, proxyAddr, targetURL)
	hitLatency := time.Since(start)
	hitOriginBytes := int(counter.written.Load())
	t.Logf("Request 2 (TTL hit):  origin bytes = %6d  latency = %v",
		hitOriginBytes, hitLatency.Round(time.Millisecond))

	if hitOriginBytes != 0 {
		t.Errorf("request 2: expected 0 origin bytes (TTL hit), got %d", hitOriginBytes)
	}

	// ── Expire the cache entry ─────────────────────────────────────────────
	state.mu.Lock()
	if entry, ok := state.Cache.entries[targetURL]; ok {
		entry.CachedAt = entry.CachedAt.Add(-cacheTTL - time.Second)
		state.Cache.entries[targetURL] = entry
	}
	state.mu.Unlock()

	// ── Request 3: expired — full re-fetch, data unchanged ─────────────────
	counter.written.Store(0)
	start = time.Now()
	proxyRequest(t, proxyAddr, targetURL)
	expiredLatency := time.Since(start)
	expiredOriginBytes := int(counter.written.Load())
	t.Logf("Request 3 (expired):  origin bytes = %6d  latency = %v  (full re-fetch)",
		expiredOriginBytes, expiredLatency.Round(time.Millisecond))

	// After expiry, bandwidth cost reverts to the full-miss cost.
	if expiredOriginBytes < bodySize {
		t.Errorf("request 3: expected >= %d origin bytes after expiry, got %d",
			bodySize, expiredOriginBytes)
	}
	// Origin was contacted on request 1 (miss) and request 3 (expired re-fetch).
	if got := int(requestCount.Load()); got != 2 {
		t.Errorf("origin hit %d times, want 2 (initial miss + expiry re-fetch)", got)
	}

	bandwidthSaved := 100 * float64(missOriginBytes-hitOriginBytes) / float64(missOriginBytes)
	t.Logf("Summary:")
	t.Logf("  TTL hit  bandwidth saving : %.1f%%  (0 origin bytes)", bandwidthSaved)
	t.Logf("  TTL hit  latency saving   : %v  (%.1fx faster than miss)",
		(missLatency - hitLatency).Round(time.Millisecond),
		float64(missLatency)/float64(hitLatency))
	t.Logf("  After expiry bandwidth    : %d bytes  (= full miss cost, no saving)", expiredOriginBytes)
	t.Logf("  After expiry latency      : %v  (= full miss cost)", expiredLatency.Round(time.Millisecond))
}

// TestExpiredTTLModifiedData verifies proxy correctness and measures cost when
// the TTL has expired and the origin content has changed.
//
// When the cache expires the proxy performs a full unconditional GET. If the
// origin has changed its response, the proxy caches and serves the new content.
// Bandwidth and latency are identical to a cache miss (full re-fetch cost).
//
// Sequence:
//
//	Request 1 (miss)       — fetches v1 body, origin bytes = bodySize
//	Request 2 (TTL hit)    — serves v1 from memory, origin bytes = 0
//	[expire cache; origin now serves v2]
//	Request 3 (expired)    — full re-fetch, receives v2, origin bytes ≈ bodySize
//	Request 4 (TTL hit v2) — serves v2 from memory, origin bytes = 0
func TestExpiredTTLModifiedData(t *testing.T) {
	const bodySize = 50 * 1024

	// version controls which body the origin serves.
	var version atomic.Int32
	version.Store(1)

	bodyV1 := strings.Repeat("v", bodySize)
	bodyV2 := strings.Repeat("w", bodySize)

	var requestCount atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		if version.Load() == 1 {
			fmt.Fprint(w, bodyV1)
		} else {
			fmt.Fprint(w, bodyV2)
		}
	})

	origin, counter := newCountingServer(handler)
	defer origin.Close()

	proxyAddr, stop := startProxy(t)
	defer stop()

	targetURL := "http://" + origin.Listener.Addr().String() + "/modified"

	// ── Request 1: cache miss — origin serves v1 ──────────────────────────
	counter.written.Store(0)
	start := time.Now()
	resp1, _ := proxyRequest(t, proxyAddr, targetURL)
	missLatency := time.Since(start)
	missOriginBytes := int(counter.written.Load())
	t.Logf("Request 1 (miss v1):  origin bytes = %6d  latency = %v",
		missOriginBytes, missLatency.Round(time.Millisecond))

	if !strings.Contains(resp1, bodyV1[:20]) {
		t.Errorf("request 1: expected v1 body, got: %.80s", resp1)
	}

	// ── Request 2: TTL hit — still v1 from memory ─────────────────────────
	counter.written.Store(0)
	start = time.Now()
	resp2, _ := proxyRequest(t, proxyAddr, targetURL)
	hitLatency := time.Since(start)
	hitOriginBytes := int(counter.written.Load())
	t.Logf("Request 2 (TTL hit):  origin bytes = %6d  latency = %v",
		hitOriginBytes, hitLatency.Round(time.Millisecond))

	if hitOriginBytes != 0 {
		t.Errorf("request 2: expected 0 origin bytes (TTL hit), got %d", hitOriginBytes)
	}
	if !strings.Contains(resp2, bodyV1[:20]) {
		t.Errorf("request 2: expected v1 body from cache, got different content")
	}

	// ── Expire the cache AND switch the origin to v2 ──────────────────────
	version.Store(2)
	state.mu.Lock()
	if entry, ok := state.Cache.entries[targetURL]; ok {
		entry.CachedAt = entry.CachedAt.Add(-cacheTTL - time.Second)
		state.Cache.entries[targetURL] = entry
	}
	state.mu.Unlock()

	// ── Request 3: expired — proxy re-fetches, receives new v2 ────────────
	counter.written.Store(0)
	start = time.Now()
	resp3, _ := proxyRequest(t, proxyAddr, targetURL)
	expiredLatency := time.Since(start)
	expiredOriginBytes := int(counter.written.Load())
	t.Logf("Request 3 (expired):  origin bytes = %6d  latency = %v  (re-fetch v2)",
		expiredOriginBytes, expiredLatency.Round(time.Millisecond))

	// Full re-fetch cost.
	if expiredOriginBytes < bodySize {
		t.Errorf("request 3: expected >= %d origin bytes after expiry, got %d",
			bodySize, expiredOriginBytes)
	}
	// Must deliver the NEW content.
	if !strings.Contains(resp3, bodyV2[:20]) {
		t.Errorf("request 3: expected v2 body after expiry+update, got old content")
	}

	// ── Request 4: TTL hit — now caches v2 ────────────────────────────────
	counter.written.Store(0)
	start = time.Now()
	resp4, _ := proxyRequest(t, proxyAddr, targetURL)
	hitV2Latency := time.Since(start)
	hitV2OriginBytes := int(counter.written.Load())
	t.Logf("Request 4 (TTL hit v2): origin bytes = %6d  latency = %v",
		hitV2OriginBytes, hitV2Latency.Round(time.Millisecond))

	if hitV2OriginBytes != 0 {
		t.Errorf("request 4: expected 0 origin bytes (new TTL hit), got %d", hitV2OriginBytes)
	}
	if !strings.Contains(resp4, bodyV2[:20]) {
		t.Errorf("request 4: expected v2 body served from cache")
	}

	t.Logf("Summary:")
	t.Logf("  TTL hit bandwidth saving  : 100.0%%  (0 origin bytes)")
	t.Logf("  TTL hit latency saving    : %v  (%.1fx faster)",
		(missLatency - hitLatency).Round(time.Millisecond),
		float64(missLatency)/float64(hitLatency))
	t.Logf("  Expired re-fetch bandwidth: %d bytes  (full miss cost)", expiredOriginBytes)
	t.Logf("  Expired re-fetch latency  : %v  (full miss cost)", expiredLatency.Round(time.Millisecond))
	t.Logf("  Content after update      : v2 correctly delivered (proxy re-fetched new content)")
}

// TestCacheEviction verifies the size-based eviction policy.
//
// It fills the cache to cacheMaxEntries using distinct URLs, each with a
// known CachedAt offset so the oldest entry is identifiable. Adding one more
// entry must:
//
//  1. Keep len(entries) <= cacheMaxEntries
//  2. Evict the entry with the earliest CachedAt (oldest fresh entry)
//
// A second sub-test verifies that expired entries are evicted before fresh
// ones, regardless of age.
func TestCacheEviction(t *testing.T) {
	t.Run("evicts oldest fresh entry at limit", func(t *testing.T) {
		state = NewProxyState()

		// Fill cache to the limit. Entry "url/0" gets the earliest CachedAt
		// because we backdate each entry by (cacheMaxEntries - i) seconds, so
		// url/0 is oldest, url/99 is newest.
		for i := range cacheMaxEntries {
			key := fmt.Sprintf("http://example.com/url/%d", i)
			state.AddToCache(key, CacheEntry{Body: []byte("x")})
			// Backdate so earlier entries appear older.
			state.mu.Lock()
			e := state.Cache.entries[key]
			e.CachedAt = e.CachedAt.Add(-time.Duration(cacheMaxEntries-i) * time.Second)
			state.Cache.entries[key] = e
			state.mu.Unlock()
		}

		if got := len(state.Cache.entries); got != cacheMaxEntries {
			t.Fatalf("after filling: cache size = %d, want %d", got, cacheMaxEntries)
		}

		// Adding one more entry should evict url/0 (oldest CachedAt).
		state.AddToCache("http://example.com/url/new", CacheEntry{Body: []byte("new")})

		if got := len(state.Cache.entries); got != cacheMaxEntries {
			t.Errorf("after overflow: cache size = %d, want %d (limit)", got, cacheMaxEntries)
		}
		state.mu.RLock()
		_, oldestStillPresent := state.Cache.entries["http://example.com/url/0"]
		state.mu.RUnlock()
		if oldestStillPresent {
			t.Error("url/0 (oldest entry) should have been evicted but is still present")
		}
	})

	t.Run("evicts expired entries before fresh ones", func(t *testing.T) {
		state = NewProxyState()

		// Fill with cacheMaxEntries-1 fresh entries.
		for i := range cacheMaxEntries - 1 {
			state.AddToCache(fmt.Sprintf("http://example.com/fresh/%d", i), CacheEntry{Body: []byte("f")})
		}
		// Add one expired entry.
		expiredKey := "http://example.com/expired"
		state.AddToCache(expiredKey, CacheEntry{Body: []byte("old")})
		state.mu.Lock()
		e := state.Cache.entries[expiredKey]
		e.CachedAt = e.CachedAt.Add(-cacheTTL - time.Second)
		state.Cache.entries[expiredKey] = e
		state.mu.Unlock()

		// Cache is now at cacheMaxEntries with one expired entry.
		// Adding a new entry should evict the expired entry, not a fresh one.
		state.AddToCache("http://example.com/trigger", CacheEntry{Body: []byte("new")})

		state.mu.RLock()
		_, expiredStillPresent := state.Cache.entries[expiredKey]
		cacheSize := len(state.Cache.entries)
		state.mu.RUnlock()

		if expiredStillPresent {
			t.Error("expired entry should have been evicted before any fresh entry")
		}
		if cacheSize != cacheMaxEntries {
			t.Errorf("cache size = %d, want %d", cacheSize, cacheMaxEntries)
		}
	})
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

// proxyRequestTimed sends one GET through the proxy and returns elapsed time.
func proxyRequestTimed(t *testing.T, proxyAddr, targetURL string) time.Duration {
	t.Helper()
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	host, _, _ := parseURL(targetURL)
	start := time.Now()
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n",
		targetURL, host)
	io.Copy(io.Discard, conn)
	return time.Since(start)
}

// TestCachedLatencyFasterThanUncached proves that a TTL cache hit is faster
// than a cache miss when the origin has non-trivial response time.
//
// The origin sleeps originDelay before responding. On a cache miss the proxy
// must wait for that delay. On a TTL cache hit the origin is never contacted,
// so the response time is just memory access — far below originDelay.
func TestCachedLatencyFasterThanUncached(t *testing.T) {
	const originDelay = 50 * time.Millisecond

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(originDelay)
		fmt.Fprint(w, "hello")
	}))
	defer origin.Close()

	proxyAddr, stop := startProxy(t)
	defer stop()

	targetURL := "http://" + origin.Listener.Addr().String() + "/latency"

	// Cache miss: proxy contacts origin and waits for it to respond.
	missLatency := proxyRequestTimed(t, proxyAddr, targetURL)
	t.Logf("Cache miss latency: %v  (origin delay = %v)", missLatency, originDelay)

	// Cache hit: proxy serves from memory, origin is never contacted.
	hitLatency := proxyRequestTimed(t, proxyAddr, targetURL)
	t.Logf("Cache hit  latency: %v  (served from memory)", hitLatency)

	if missLatency < originDelay {
		t.Errorf("miss latency %v < origin delay %v — origin delay not taking effect",
			missLatency, originDelay)
	}
	if hitLatency >= missLatency {
		t.Errorf("cache hit latency %v >= miss latency %v — TTL cache not helping",
			hitLatency, missLatency)
	}

	speedup := float64(missLatency) / float64(hitLatency)
	t.Logf("Speedup: %.1fx", speedup)
}

// TestLatencySummary prints a table of miss vs hit latency across different
// simulated origin delays. Run with -v to see the output.
func TestLatencySummary(t *testing.T) {
	delays := []struct {
		label string
		delay time.Duration
	}{
		{"10ms origin", 10 * time.Millisecond},
		{"50ms origin", 50 * time.Millisecond},
		{"100ms origin", 100 * time.Millisecond},
	}

	for _, tc := range delays {
		t.Run(tc.label, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(tc.delay)
				fmt.Fprint(w, strings.Repeat("x", 4096))
			}))
			defer origin.Close()

			proxyAddr, stop := startProxy(t)
			defer stop()

			targetURL := "http://" + origin.Listener.Addr().String() + "/page"

			// Warm the cache with one miss.
			missLatency := proxyRequestTimed(t, proxyAddr, targetURL)

			// Average several hits for a stable reading.
			const rounds = 5
			var total time.Duration
			for range rounds {
				total += proxyRequestTimed(t, proxyAddr, targetURL)
			}
			avgHit := total / rounds

			speedup := float64(missLatency) / float64(avgHit)
			saved := missLatency - avgHit

			t.Logf("Origin delay: %-10v | Miss: %-10v | Hit (avg): %-10v | Saved: %-10v | Speedup: %.1fx",
				tc.delay,
				missLatency.Round(time.Millisecond),
				avgHit.Round(time.Millisecond),
				saved.Round(time.Millisecond),
				speedup,
			)

			if avgHit >= missLatency {
				t.Errorf("avg hit latency %v >= miss latency %v",
					avgHit.Round(time.Millisecond), missLatency.Round(time.Millisecond))
			}
		})
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
