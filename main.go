package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var state *ProxyState

func main() {
	// Initialize shared state
	state = NewProxyState()
	if err := state.LoadBlocked("blocked.json"); err != nil {
		log.Println("No blocked list found, starting fresh.")
	}

	// Start management server
	go StartManagementServer(":8080", state)

	listener, err := net.Listen("tcp", ":4000")
	if err != nil {
		log.Fatal("Error listening:", err)
	}
	log.Println("Proxy listening on :4000")

	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Error accepting conn:", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(clientConn net.Conn) {

	defer clientConn.Close()

	reader := bufio.NewReader(clientConn)
	message, err := reader.ReadString('\n')
	if err != nil {
		log.Printf("Read error: %v", err)
		return
	}

	message = strings.TrimSpace(message)
	messageFields := strings.Fields(message)
	if len(messageFields) < 2 {
		return
	}
	method := messageFields[0]
	url := messageFields[1]

	// Determine host for blocking check
	host, _, _ := parseURL(url)

	// Check if blocked
	if state.IsBlocked(host) {
		logRequest(method, url, "Blocked", clientConn)
		clientConn.Write([]byte("HTTP/1.1 403 Forbidden\r\n\r\n<h1>Access Denied</h1>"))
		return
	}

	if method == "CONNECT" {
		handleHTTPS(clientConn, url)
	} else {
		handleHTTP(clientConn, reader, method, url)
	}
}

func handleHTTPS(clientConn net.Conn, target string) {
	logRequest("CONNECT", target, "Allowed", clientConn)

	// Connect to target server
	serverConn, err := net.Dial("tcp", target)
	if err != nil {
		log.Printf("HTTPS failed to connect to %s: %v", target, err)
		clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer serverConn.Close()

	// Tell browser tunnel is ready
	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	log.Printf("  → TUNNEL to %s", target)

	domain, _, _ := net.SplitHostPort(target)
	state.RegisterConn(domain, clientConn)
	defer state.UnregisterConn(domain, clientConn)

	// Copy bytes both directions
	go io.Copy(serverConn, clientConn)
	io.Copy(clientConn, serverConn)
}

func handleHTTP(clientConn net.Conn, reader *bufio.Reader, method, url string) {
	host, port, path := parseURL(url)
	address := host + ":" + port

	// Read client headers
	headers := readClientHeaders(reader)
	if headers == nil {
		return
	}
	// Check cache — GetFromCache returns false for missing or expired entries.
	cachedEntry, hasCache := state.GetFromCache(url)

	// Fresh cache hit: serve directly from memory, no origin contact needed.
	if hasCache {
		log.Printf("CACHE HIT (TTL): %s", url)
		logRequest(method, url, "Cached", clientConn)
		sendCachedResponse(clientConn, cachedEntry)
		return
	}

	// Cache miss or expired: fetch from origin.
	serverConn, err := net.Dial("tcp", address)
	if err != nil {
		clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer serverConn.Close()
	// Send request to server (no conditional headers — we are doing a full fetch)
	sendRequestToServer(serverConn, method, path, headers, CacheEntry{}, false)
	log.Printf("  → request to %s", address)
	// Forward request body (async)
	go io.Copy(serverConn, reader)
	// Read response status
	serverReader := bufio.NewReader(serverConn)
	statusCode, statusLine := readResponseStatus(serverReader)
	if statusLine == "" {
		return
	}
	if statusCode == 0 {
		return
	}
	logRequest(method, url, "Allowed", clientConn)
	streamAndCacheResponse(clientConn, serverReader, statusLine, url)
}

// readClientHeaders reads all headers until empty line
func readClientHeaders(reader *bufio.Reader) []string {
	var headers []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		headers = append(headers, trimmed)
	}
	return headers
}

// sendRequestToServer sends the HTTP request with appropriate headers
func sendRequestToServer(serverConn net.Conn, method, path string, headers []string, cachedEntry CacheEntry, hasCache bool) {
	// Request line
	fmt.Fprintf(serverConn, "%s %s HTTP/1.1\r\n", method, path)

	// Forward filtered headers
	for _, header := range headers {
		if shouldSkipHeader(header) {
			continue
		}
		fmt.Fprintf(serverConn, "%s\r\n", header)
	}
	// Force connection close
	fmt.Fprintf(serverConn, "Connection: close\r\n")
	// Add conditional headers if cached
	if hasCache {
		if lastMod := cachedEntry.Header.Get("Last-Modified"); lastMod != "" {
			fmt.Fprintf(serverConn, "If-Modified-Since: %s\r\n", lastMod)
		}
		if etag := cachedEntry.Header.Get("ETag"); etag != "" {
			fmt.Fprintf(serverConn, "If-None-Match: %s\r\n", etag)
		}
	}
	// End headers
	fmt.Fprintf(serverConn, "\r\n")
}

// shouldSkipHeader returns true for headers that shouldn't be forwarded
func shouldSkipHeader(header string) bool {
	lower := strings.ToLower(header)
	skipPrefixes := []string{
		"proxy-",
		"connection:",
		"if-modified-since:",
		"if-none-match:",
	}
	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// readResponseStatus reads status line and returns code
func readResponseStatus(reader *bufio.Reader) (int, string) {
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return 0, ""
	}

	statusCode := 0
	parts := strings.Fields(statusLine)
	if len(parts) >= 2 {
		statusCode, _ = strconv.Atoi(parts[1])
	}

	return statusCode, statusLine
}

// logRequest logs request to state
func logRequest(method, url, status string, clientConn net.Conn) {
	state.LogRequest(RequestLog{
		Time:   time.Now(),
		Method: method,
		URL:    url,
		Status: status,
		SrcIP:  clientConn.RemoteAddr().String(),
	})
}

func streamAndCacheResponse(clientConn net.Conn, reader *bufio.Reader, statusLine, url string) {
	if _, err := clientConn.Write([]byte(statusLine)); err != nil {
		return
	}

	// Read, Forward, and Parse Headers
	headers := make(http.Header)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if _, err := clientConn.Write([]byte(line)); err != nil {
			return
		}
		if strings.TrimSpace(line) == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			lowerKey := strings.ToLower(key)
			if lowerKey == "connection" || lowerKey == "keep-alive" || lowerKey == "proxy-connection" || lowerKey == "te" || lowerKey == "upgrade" {
				continue
			}
			headers.Add(key, val)
		}
	}

	// TeeReader duplicates the stream:
	// - Reads from 'reader' (server)
	// - Writes to 'bodyBuf' (cache)
	// - Returns data to io.Copy for 'clientConn' (user)
	var bodyBuf bytes.Buffer
	io.Copy(clientConn, io.TeeReader(reader, &bodyBuf))

	state.AddToCache(url, CacheEntry{
		Header: headers,
		Body:   bodyBuf.Bytes(),
	})
}

func sendCachedResponse(clientConn net.Conn, entry CacheEntry) {
	// Send status line
	clientConn.Write([]byte("HTTP/1.1 200 OK\r\n"))
	// Explicitly set Connection to close (since we don't support keep-alive in proxy perfectly yet)
	fmt.Fprintf(clientConn, "Connection: close\r\n")
	// Send headers
	for key, values := range entry.Header {
		if strings.ToLower(key) == "connection" {
			continue
		}
		for _, value := range values {
			fmt.Fprintf(clientConn, "%s: %s\r\n", key, value)
		}
	}
	clientConn.Write([]byte("\r\n"))
	// Send body
	clientConn.Write(entry.Body)
}

// parseURL extracts host, port, and path from a URL
// Examples:
//
//	"http://example.com/page"      → host="example.com", port="80", path="/page"
//	"http://example.com:8080/page" → host="example.com", port="8080", path="/page"
//	"example.com:443"              → host="example.com", port="443", path=""
//	"https://example.com/page"     → host="example.com", port="443", path="/page"
func parseURL(url string) (host, port, path string) {

	// 1. Handle Scheme and Defaults
	if strings.HasPrefix(url, "http://") {
		port = "80"
		url = url[7:] // Strip "http://"
	} else if strings.HasPrefix(url, "https://") {
		port = "443"
		url = url[8:] // Strip "https://"
	} else {
		// Case: "example.com:443" (CONNECT method)
		port = "443"
	}
	// 2. Separate Path
	// If a slash exists, split it. This handles full URLs that were stripped above.
	path = "/"
	if idx := strings.Index(url, "/"); idx != -1 {
		path = url[idx:]
		url = url[:idx]
	}
	// 3. Strip Authentication (user:pass@host)
	if idx := strings.LastIndex(url, "@"); idx != -1 {
		url = url[idx+1:]
	}
	// 4. Separate Host and Port (IPv6 Safe)
	colonIdx := strings.LastIndex(url, ":")
	bracketIdx := strings.LastIndex(url, "]")

	if colonIdx != -1 && colonIdx > bracketIdx {
		host = url[:colonIdx]
		port = url[colonIdx+1:]
	} else {
		host = url
	}

	return
}
