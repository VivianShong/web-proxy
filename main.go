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

func main() {
	// Initialize shared state
	state := NewProxyState()
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
		go handleConnection(conn, state)
	}
}

func handleConnection(clientConn net.Conn, state *ProxyState) {

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
		handleBlocked(clientConn, reader, state, method, url)
		return
	}

	if method == "CONNECT" {
		handleHTTPS(clientConn, url, state)
	} else {
		handleHTTP(clientConn, reader, method, url, state)
	}
}

func handleBlocked(clientConn net.Conn, reader *bufio.Reader, state *ProxyState, method, url string) {
	// Consume remaining headers so connection is clean
	cleanConnection(reader)

	log.Printf("BLOCKED: %s", url)
	state.LogRequest(RequestLog{
		Time:   time.Now(),
		Method: method,
		URL:    url,
		Status: "Blocked",
		SrcIP:  clientConn.RemoteAddr().String(),
	})
	clientConn.Write([]byte("HTTP/1.1 403 Forbidden\r\n\r\n<h1>Access Denied</h1>"))
}

func cleanConnection(reader *bufio.Reader) {
	for {
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			break
		}
	}
}

func handleHTTPS(clientConn net.Conn, target string, state *ProxyState) {
	state.LogRequest(RequestLog{
		Time:   time.Now(),
		Method: "CONNECT",
		URL:    target,
		Status: "Allowed",
		SrcIP:  clientConn.RemoteAddr().String(),
	})

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

func handleHTTP(clientConn net.Conn, reader *bufio.Reader, method, url string, state *ProxyState) {
	var headers []string
	host, port, path := parseURL(url)
	address := host + ":" + port

	// Connect to server
	serverConn, err := net.Dial("tcp", address)
	if err != nil {
		clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer serverConn.Close()

	// Read headers from client
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" {
			break // Done reading headers
		}
		headers = append(headers, trimmedLine)
	}

	// Check Cache
	cacheKey := url
	cachedEntry, hasCache := state.GetFromCache(cacheKey)

	// Send Request Line
	fmt.Fprintf(serverConn, "%s %s HTTP/1.1\r\n", method, path)

	// Forward headers
	for _, header := range headers {
		if strings.HasPrefix(strings.ToLower(header), "proxy-") {
			continue
		}
		lowerHeader := strings.ToLower(header)
		if strings.HasPrefix(lowerHeader, "connection:") ||
			strings.HasPrefix(lowerHeader, "if-modified-since:") ||
			strings.HasPrefix(lowerHeader, "if-none-match:") {
			continue
		}
		fmt.Fprintf(serverConn, "%s\r\n", header)
	}

	// Explicitly set Connection to close so server closes connection after response,
	// allowing io.Copy to return EOF and us to cache the response.
	fmt.Fprintf(serverConn, "Connection: close\r\n")

	// Add If-Modified-Since if cached
	if hasCache {
		if lastMod := cachedEntry.Header.Get("Last-Modified"); lastMod != "" {
			fmt.Fprintf(serverConn, "If-Modified-Since: %s\r\n", lastMod)
		}
	}

	fmt.Fprintf(serverConn, "\r\n")
	log.Printf("  → request to %s", address)

	// Forward request body (async)
	go io.Copy(serverConn, reader)

	// Read Response Status
	serverReader := bufio.NewReader(serverConn)
	statusLine, err := serverReader.ReadString('\n')
	if err != nil {
		return
	}

	// Check 304 (Not Modified)
	is304 := false
	parts := strings.Fields(statusLine)
	if len(parts) >= 2 {
		if code, err := strconv.Atoi(parts[1]); err == nil && code == 304 {
			is304 = true
		}
	}

	if hasCache && is304 {
		log.Printf("CACHE HIT: %s", url)
		state.LogRequest(RequestLog{
			Time:   time.Now(),
			Method: method,
			URL:    url,
			Status: "Cached",
			SrcIP:  clientConn.RemoteAddr().String(),
		})
		sendCachedResponse(clientConn, cachedEntry)
		return
	}

	// Not 304: Serve from Server & Update Cache
	state.LogRequest(RequestLog{
		Time:   time.Now(),
		Method: method,
		URL:    url,
		Status: "Allowed",
		SrcIP:  clientConn.RemoteAddr().String(),
	})

	// Cache response
	streamAndCacheResponse(clientConn, serverReader, statusLine, url, state)
}

func streamAndCacheResponse(clientConn net.Conn, reader *bufio.Reader, statusLine, url string, state *ProxyState) {
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
