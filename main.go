package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
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
	// log.Println("msg: ", message) // too noisy

	message = strings.TrimSpace(message)
	messageFields := strings.Fields(message)
	if len(messageFields) < 2 {
		return
	}
	method := messageFields[0]
	url := messageFields[1]

	// log.Println("method: ", method)
	// log.Println("url: ", url)

	// Determine host for blocking check
	host := url
	if strings.Contains(host, "://") {
		host = host[strings.Index(host, "://")+3:]
	}
	if idx := strings.Index(host, "/"); idx != -1 {
		host = host[:idx]
	}
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	// Check if blocked
	if state.IsBlocked(host) {
		log.Printf("BLOCKED: %s", host)
		state.LogRequest(RequestLog{
			Time:   time.Now(),
			Method: method,
			URL:    url,
			Status: "Blocked",
			SrcIP:  clientConn.RemoteAddr().String(),
		})
		clientConn.Write([]byte("HTTP/1.1 403 Forbidden\r\n\r\n<h3>Access Denied</h3>"))
		return
	}
	
	// Check if in cache
	// TODO: implement cache
	
	state.LogRequest(RequestLog{
		Time:   time.Now(),
		Method: method,
		URL:    url,
		Status: "Allowed",
		SrcIP:  clientConn.RemoteAddr().String(),
	})

	if method == "CONNECT" {
		// CONNECT method (HTTPS)
		handleHTTPS(clientConn, url)

	} else {
		// other methods (HTTP)
		handleHTTP(clientConn, reader, method, url)
	}
}

func handleHTTPS(clientConn net.Conn, target string) {
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

	// Copy bytes both directions
	go io.Copy(serverConn, clientConn)
	io.Copy(clientConn, serverConn)
}

func handleHTTP(clientConn net.Conn, reader *bufio.Reader, method, url string) {
	var headers []string
	host := ""

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		trimmedLine := strings.TrimSpace(line)
		// Extract host
		if strings.HasPrefix(strings.ToLower(trimmedLine), "host:") {
			host = strings.TrimSpace(trimmedLine[5:])
		}

		headers = append(headers, trimmedLine)
		log.Println("headers: ", headers)
		log.Println("host: ", host)
		if line == "\r\n" {
			break
		}
	}

	if !strings.Contains(host, ":") {
		host = host + ":80"
	}
	
	// Connect to server
	serverConn, err := net.Dial("tcp", host)
	if err != nil {
		clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer serverConn.Close()

	// Get path from URL
	path := getPathFromURL(url)

	// Send request line
	fmt.Fprintf(serverConn, "%s %s HTTP/1.1\r\n", method, path)

	// Forward headers
	for _, header := range headers {
		// strip proxy-related headers eg. Proxy-Authorization
		if strings.HasPrefix(strings.ToLower(header), "proxy-") {
			continue
		}
		fmt.Fprintf(serverConn, "%s\r\n", header)
	}
	fmt.Fprintf(serverConn, "\r\n")
	log.Printf("  → TUNNEL to %s", host)

	// Forward body (if any) and get response
	go io.Copy(serverConn, reader)
	io.Copy(clientConn, serverConn)
}

func getPathFromURL(url string) string {
	// if url contains http:// or https://, strip it
	if strings.Contains(url, "http://") || strings.Contains(url, "https://") {
		url = url[strings.Index(url, "://")+3:]
	}
	slashIdx := strings.Index(url, "/")
    if slashIdx == -1 {
        return "/"
    }
	return url[slashIdx:]
}
