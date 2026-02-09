# Go Quick Reference for Web Proxy Project

A comprehensive guide covering everything you need to build the proxy server.

---

## Table of Contents

1. [Basic Syntax](#1-basic-syntax)
2. [Data Structures](#2-data-structures)
3. [Structs (Like Classes)](#3-structs-like-classes)
4. [Imports & Packages](#4-imports--packages)
5. [Networking (TCP Sockets)](#5-networking-tcp-sockets)
6. [Concurrency (Goroutines & Sync)](#6-concurrency-goroutines--sync)
7. [String Operations](#7-string-operations)
8. [Reading from Stdin (Console Input)](#8-reading-from-stdin-console-input)
9. [Time Operations](#9-time-operations)
10. [Error Handling](#10-error-handling)
11. [Logging](#11-logging)
12. [Complete Mini Example](#12-complete-mini-example)
13. [Quick Reference Card](#13-quick-reference-card)

---

## 1. Basic Syntax

### Variables & Constants

```go
// Variable declaration
var name string = "proxy"
var port int = 4000

// Short declaration (infers type) - most common
name := "proxy"
port := 4000

// Constants
const DEFAULT_PORT = 4000
const CACHE_TTL = 5 * time.Minute
```

### Functions

```go
// Basic function
func sayHello() {
    fmt.Println("Hello")
}

// With parameters and return value
func add(a int, b int) int {
    return a + b
}

// Multiple return values (very common in Go)
func divide(a, b int) (int, error) {
    if b == 0 {
        return 0, errors.New("cannot divide by zero")
    }
    return a / b, nil
}

// Using multiple returns
result, err := divide(10, 2)
if err != nil {
    fmt.Println("Error:", err)
} else {
    fmt.Println("Result:", result)
}
```

### If/Else

```go
if x > 10 {
    fmt.Println("big")
} else if x > 5 {
    fmt.Println("medium")
} else {
    fmt.Println("small")
}

// If with initialization (very common)
if err := doSomething(); err != nil {
    fmt.Println("Error:", err)
}
```

### Loops

```go
// Go only has 'for' - no while

// Standard for loop
for i := 0; i < 10; i++ {
    fmt.Println(i)
}

// While-style loop
for x < 100 {
    x += 10
}

// Infinite loop
for {
    // runs forever
    // use 'break' to exit
}

// Range over slice
items := []string{"a", "b", "c"}
for index, value := range items {
    fmt.Printf("%d: %s\n", index, value)
}

// Range over map
for key, value := range myMap {
    fmt.Printf("%s = %s\n", key, value)
}

// Ignore index with underscore
for _, value := range items {
    fmt.Println(value)
}
```

### Switch

```go
switch command {
case "block":
    fmt.Println("blocking")
case "unblock":
    fmt.Println("unblocking")
case "quit", "exit":  // Multiple matches
    fmt.Println("goodbye")
default:
    fmt.Println("unknown command")
}
```

---

## 2. Data Structures

### Slices (Dynamic Arrays)

```go
// Create empty slice
var items []string
items := []string{}

// Create with initial values
items := []string{"apple", "banana", "cherry"}

// Append
items = append(items, "date")

// Access
first := items[0]

// Length
length := len(items)

// Slice a slice
subset := items[1:3]  // ["banana", "cherry"]

// Iterate
for i, item := range items {
    fmt.Printf("%d: %s\n", i, item)
}

// Create with capacity (optimization)
items := make([]string, 0, 100)  // len=0, cap=100
```

### Maps (Dictionaries/Hash Tables)

```go
// Create empty map
headers := make(map[string]string)

// Create with initial values
headers := map[string]string{
    "Host":         "example.com",
    "Content-Type": "text/html",
}

// Set value
headers["Accept"] = "text/html"

// Get value
contentType := headers["Content-Type"]

// Check if key exists (important!)
value, exists := headers["Host"]
if exists {
    fmt.Println("Host is:", value)
} else {
    fmt.Println("Host header not found")
}

// Delete
delete(headers, "Accept")

// Iterate
for key, value := range headers {
    fmt.Printf("%s: %s\n", key, value)
}

// Length
count := len(headers)
```

### Sets (Using Maps)

Go doesn't have a built-in set, but you use `map[string]bool`:

```go
// Create a set
blocked := make(map[string]bool)

// Add to set
blocked["ads.com"] = true
blocked["tracker.net"] = true

// Check membership
if blocked["ads.com"] {
    fmt.Println("ads.com is blocked")
}

// Remove from set
delete(blocked, "ads.com")

// Get all items as slice
func getAll(set map[string]bool) []string {
    keys := make([]string, 0, len(set))
    for key := range set {
        keys = append(keys, key)
    }
    return keys
}
```

### Bytes and Strings

```go
// String to bytes
data := []byte("Hello, World!")

// Bytes to string
text := string(data)

// Byte slice operations
buffer := make([]byte, 4096)  // Create buffer
n, err := conn.Read(buffer)    // Read into buffer
received := buffer[:n]         // Slice to actual length
```

---

## 3. Structs (Like Classes)

### Defining Structs

```go
type CacheEntry struct {
    Response  []byte
    Timestamp time.Time
}

type Cache struct {
    entries map[string]CacheEntry
    ttl     time.Duration
    mutex   sync.RWMutex
}
```

### Creating Instances

```go
// Direct creation
entry := CacheEntry{
    Response:  []byte("HTTP/1.1 200 OK..."),
    Timestamp: time.Now(),
}

// Partial initialization (others get zero values)
entry := CacheEntry{
    Response: []byte("..."),
}

// Using new (returns pointer)
entry := new(CacheEntry)
entry.Response = []byte("...")
entry.Timestamp = time.Now()

// Constructor function (common pattern)
func NewCache(ttl time.Duration) *Cache {
    return &Cache{
        entries: make(map[string]CacheEntry),
        ttl:     ttl,
    }
}

cache := NewCache(5 * time.Minute)
```

### Methods (Functions on Structs)

```go
// Method with value receiver (doesn't modify original)
func (c Cache) Size() int {
    return len(c.entries)
}

// Method with pointer receiver (can modify original) - more common
func (c *Cache) Store(url string, response []byte) {
    c.entries[url] = CacheEntry{
        Response:  response,
        Timestamp: time.Now(),
    }
}

// Usage
cache := NewCache(5 * time.Minute)
cache.Store("http://example.com", data)
size := cache.Size()
```

### Pointers

```go
// & gets address (creates pointer)
cache := &Cache{}

// * dereferences pointer (gets value)
value := *pointer

// In practice, you mostly use pointers implicitly:
func NewCache() *Cache {
    return &Cache{
        entries: make(map[string]CacheEntry),
    }
}

// Methods with pointer receivers automatically handle this
cache := NewCache()
cache.Store(url, data)  // Go handles pointer dereferencing
```

---

## 4. Imports & Packages

### Import Syntax

```go
// Single import
import "fmt"

// Multiple imports (common style)
import (
    "bufio"
    "fmt"
    "net"
    "strings"
    "sync"
    "time"
)

// Using imported packages
fmt.Println("Hello")
strings.Split("a,b,c", ",")
```

### Common Packages You'll Need

```go
import (
    "bufio"     // Buffered I/O - reading lines from connections
    "bytes"     // Byte slice operations
    "errors"    // Creating error values
    "fmt"       // Printing, formatting strings
    "io"        // io.Copy for HTTPS tunneling
    "log"       // Logging with timestamps
    "net"       // TCP sockets
    "net/url"   // URL parsing (optional, can do manually)
    "os"        // Exit, stdin
    "strconv"   // String to int conversion
    "strings"   // String manipulation
    "sync"      // Mutexes, WaitGroups
    "time"      // Time, durations
)
```

### Package Declaration

Every Go file starts with a package declaration:

```go
// For executables (your proxy)
package main

// main package must have main() function
func main() {
    // entry point
}
```

### Exported vs Unexported

```go
// Uppercase = exported (public)
func HandleConnection() {}  // Can be called from other packages
type Cache struct {}        // Visible outside package

// Lowercase = unexported (private)
func parseRequest() {}      // Only visible within package
type cacheEntry struct {}   // Only visible within package
```

---

## 5. Networking (TCP Sockets)

### Creating a TCP Server

```go
import "net"

func runServer(port int) {
    // Create listener
    address := fmt.Sprintf(":%d", port)
    listener, err := net.Listen("tcp", address)
    if err != nil {
        log.Fatal("Failed to start server:", err)
    }
    defer listener.Close()
    
    fmt.Printf("Listening on port %d\n", port)
    
    // Accept connections forever
    for {
        conn, err := listener.Accept()
        if err != nil {
            log.Println("Accept error:", err)
            continue
        }
        
        // Handle in new goroutine
        go handleConnection(conn)
    }
}
```

### Connecting to a Server (Client)

```go
// Basic connection
serverConn, err := net.Dial("tcp", "example.com:80")
if err != nil {
    log.Println("Failed to connect:", err)
    return
}
defer serverConn.Close()

// With timeout (recommended)
serverConn, err := net.DialTimeout("tcp", "example.com:80", 10*time.Second)
if err != nil {
    log.Println("Connection timeout:", err)
    return
}
defer serverConn.Close()
```

### Reading from Connections

```go
// Read raw bytes into buffer
buffer := make([]byte, 4096)
n, err := conn.Read(buffer)
if err != nil {
    if err == io.EOF {
        // Connection closed normally
    } else {
        log.Println("Read error:", err)
    }
    return
}
data := buffer[:n]

// Read line by line (better for HTTP headers)
reader := bufio.NewReader(conn)
line, err := reader.ReadString('\n')
if err != nil {
    return
}
line = strings.TrimSpace(line)

// Read all bytes until connection closes
import "io"
data, err := io.ReadAll(conn)

// Read exact number of bytes
buffer := make([]byte, contentLength)
_, err := io.ReadFull(conn, buffer)
```

### Writing to Connections

```go
// Write byte slice
conn.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))

// Write string directly
io.WriteString(conn, "HTTP/1.1 200 OK\r\n\r\n")

// Write formatted string
fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\n", statusCode, statusText)
fmt.Fprintf(conn, "Content-Length: %d\r\n", len(body))
fmt.Fprintf(conn, "\r\n")
conn.Write(body)
```

### Copy Between Connections (For HTTPS Tunneling)

```go
import "io"

// Copy all data from src to dst (blocks until src closes)
bytesWritten, err := io.Copy(dst, src)

// For HTTPS tunnel - bidirectional copy
go io.Copy(serverConn, clientConn)  // Client → Server
go io.Copy(clientConn, serverConn)  // Server → Client
```

### Setting Timeouts

```go
// Set read deadline (absolute time)
conn.SetReadDeadline(time.Now().Add(30 * time.Second))

// Set write deadline
conn.SetWriteDeadline(time.Now().Add(30 * time.Second))

// Set both read and write deadline
conn.SetDeadline(time.Now().Add(30 * time.Second))

// Remove deadline (wait forever)
conn.SetDeadline(time.Time{})
```

### Get Connection Info

```go
// Remote address (client's address)
remoteAddr := conn.RemoteAddr().String()  // "192.168.1.1:54321"

// Local address
localAddr := conn.LocalAddr().String()  // ":4000"
```

---

## 6. Concurrency (Goroutines & Sync)

### Goroutines (Lightweight Threads)

```go
// Start a goroutine - just add 'go' before function call
go doSomething()

// With anonymous function
go func() {
    fmt.Println("Running in background")
}()

// Pass arguments (important - captures current value)
go func(conn net.Conn) {
    handleConnection(conn)
}(clientConn)

// Common pattern: handle connection
for {
    conn, _ := listener.Accept()
    go handleConnection(conn)  // Each connection in its own goroutine
}
```

### WaitGroup (Wait for Goroutines to Finish)

```go
import "sync"

var wg sync.WaitGroup

// Add count of goroutines to wait for
wg.Add(2)

go func() {
    defer wg.Done()  // Decrements counter when done
    io.Copy(serverConn, clientConn)
}()

go func() {
    defer wg.Done()
    io.Copy(clientConn, serverConn)
}()

wg.Wait()  // Blocks until counter reaches 0
```

### Mutex (Thread-Safe Access)

```go
import "sync"

// Regular Mutex - one goroutine at a time
type Counter struct {
    value int
    mutex sync.Mutex
}

func (c *Counter) Increment() {
    c.mutex.Lock()
    defer c.mutex.Unlock()
    c.value++
}

// RWMutex - multiple readers OR one writer (more efficient for read-heavy)
type Cache struct {
    entries map[string]CacheEntry
    mutex   sync.RWMutex
}

// Write operation - exclusive lock
func (c *Cache) Store(url string, data []byte) {
    c.mutex.Lock()
    defer c.mutex.Unlock()
    
    c.entries[url] = CacheEntry{Response: data}
}

// Read operation - shared lock (multiple readers OK)
func (c *Cache) Get(url string) ([]byte, bool) {
    c.mutex.RLock()
    defer c.mutex.RUnlock()
    
    entry, exists := c.entries[url]
    if !exists {
        return nil, false
    }
    return entry.Response, true
}
```

### Channels (Communication Between Goroutines)

```go
// Create channel
ch := make(chan string)

// Send to channel
ch <- "hello"

// Receive from channel
msg := <-ch

// Buffered channel (non-blocking until full)
ch := make(chan string, 10)

// Close channel (signals no more values)
close(ch)

// Range over channel (until closed)
for msg := range ch {
    fmt.Println(msg)
}

// Select (wait on multiple channels)
select {
case msg := <-ch1:
    fmt.Println("From ch1:", msg)
case msg := <-ch2:
    fmt.Println("From ch2:", msg)
case <-time.After(5 * time.Second):
    fmt.Println("Timeout!")
}
```

---

## 7. String Operations

```go
import "strings"

// Split by delimiter
parts := strings.Split("a,b,c", ",")
// parts = ["a", "b", "c"]

// Split by whitespace (handles multiple spaces)
parts := strings.Fields("GET   /path   HTTP/1.1")
// parts = ["GET", "/path", "HTTP/1.1"]

// Split only N times (useful for headers)
parts := strings.SplitN("Host: example.com:8080", ":", 2)
// parts = ["Host", " example.com:8080"]

// Join slice into string
line := strings.Join([]string{"a", "b", "c"}, ", ")
// line = "a, b, c"

// Contains
if strings.Contains(line, "200 OK") {
    fmt.Println("Success!")
}

// HasPrefix / HasSuffix
if strings.HasPrefix(url, "http://") {
    // handle http
}
if strings.HasSuffix(filename, ".html") {
    // handle html
}

// Trim whitespace
trimmed := strings.TrimSpace("  hello  ")  // "hello"

// Trim specific characters
clean := strings.Trim(line, "\r\n")

// Replace
clean := strings.Replace(line, "\r\n", "", -1)  // -1 = replace all

// ToLower / ToUpper
lower := strings.ToLower("HELLO")  // "hello"
upper := strings.ToUpper("hello")  // "HELLO"

// Index (find substring)
idx := strings.Index(url, "://")
if idx != -1 {
    scheme := url[:idx]
}

// Cut (Go 1.18+) - split at first occurrence
before, after, found := strings.Cut("host:8080", ":")
// before = "host", after = "8080", found = true
```

### String Conversion

```go
import "strconv"

// String to int
port, err := strconv.Atoi("8080")
if err != nil {
    // handle error
}

// Int to string
portStr := strconv.Itoa(8080)

// Format int with base
binary := strconv.FormatInt(255, 2)  // "11111111"

// Parse bool
b, err := strconv.ParseBool("true")
```

### String Formatting

```go
// Sprintf - format to string
msg := fmt.Sprintf("Port: %d, Host: %s", port, host)

// Format specifiers
%s  - string
%d  - integer
%f  - float
%v  - any value (default format)
%+v - struct with field names
%t  - boolean
%x  - hex
%p  - pointer
%%  - literal %
```

---

## 8. Reading from Stdin (Console Input)

```go
import (
    "bufio"
    "fmt"
    "os"
    "strings"
)

func runConsole() {
    scanner := bufio.NewScanner(os.Stdin)
    
    fmt.Print("> ")
    
    for scanner.Scan() {  // Reads one line per iteration
        input := scanner.Text()
        parts := strings.Fields(input)
        
        if len(parts) == 0 {
            fmt.Print("> ")
            continue
        }
        
        command := parts[0]
        
        switch command {
        case "block":
            if len(parts) > 1 {
                url := parts[1]
                fmt.Printf("Blocked: %s\n", url)
            } else {
                fmt.Println("Usage: block <url>")
            }
        case "list":
            fmt.Println("Blocked URLs:")
            // print list
        case "stats":
            fmt.Println("Statistics:")
            // print stats
        case "quit", "exit":
            fmt.Println("Goodbye!")
            os.Exit(0)
        default:
            fmt.Println("Unknown command. Try: block, list, stats, quit")
        }
        
        fmt.Print("> ")
    }
    
    // Check for scanner errors
    if err := scanner.Err(); err != nil {
        log.Println("Scanner error:", err)
    }
}
```

---

## 9. Time Operations

```go
import "time"

// Current time
now := time.Now()

// Duration constants
ttl := 5 * time.Minute
timeout := 30 * time.Second
delay := 100 * time.Millisecond

// Time arithmetic
expiresAt := now.Add(5 * time.Minute)
twoHoursAgo := now.Add(-2 * time.Hour)

// Compare times
isExpired := time.Now().After(expiresAt)
isFuture := expiresAt.After(time.Now())
isEqual := time1.Equal(time2)

// Duration between times
elapsed := time.Since(startTime)           // time.Now() - startTime
remaining := time.Until(deadline)          // deadline - time.Now()
diff := endTime.Sub(startTime)             // endTime - startTime

// Get duration components
ms := elapsed.Milliseconds()  // int64
sec := elapsed.Seconds()      // float64

// Measure elapsed time
start := time.Now()
// ... do work ...
elapsed := time.Since(start)
fmt.Printf("Took %dms\n", elapsed.Milliseconds())

// Format time to string
timestamp := time.Now().Format("15:04:05")           // "14:30:45"
datetime := time.Now().Format("2006-01-02 15:04:05") // "2024-02-15 14:30:45"
// Note: Go uses specific reference time: Mon Jan 2 15:04:05 MST 2006

// Sleep
time.Sleep(1 * time.Second)

// Timer (one-shot)
timer := time.NewTimer(5 * time.Second)
<-timer.C  // Blocks for 5 seconds

// Ticker (repeating)
ticker := time.NewTicker(1 * time.Second)
for t := range ticker.C {
    fmt.Println("Tick at", t)
}
ticker.Stop()
```

---

## 10. Error Handling

```go
// Functions return errors as last return value
conn, err := net.Dial("tcp", "example.com:80")
if err != nil {
    log.Println("Error:", err)
    return
}

// Create custom errors
import "errors"
err := errors.New("something went wrong")

// Formatted errors
import "fmt"
err := fmt.Errorf("failed to connect to %s: %v", host, originalErr)

// Error wrapping (Go 1.13+)
err := fmt.Errorf("connection failed: %w", originalErr)

// Check wrapped errors
if errors.Is(err, io.EOF) {
    // handle EOF
}

// Common pattern: early return on error
func doSomething() error {
    result, err := step1()
    if err != nil {
        return fmt.Errorf("step1 failed: %w", err)
    }
    
    err = step2(result)
    if err != nil {
        return fmt.Errorf("step2 failed: %w", err)
    }
    
    return nil
}

// Defer for cleanup (runs when function returns)
func handleConnection(conn net.Conn) {
    defer conn.Close()  // Will ALWAYS run when function exits
    
    // ... rest of function
    // conn.Close() called automatically
}

// Multiple defers (run in reverse order - LIFO)
func example() {
    defer fmt.Println("third")
    defer fmt.Println("second")
    defer fmt.Println("first")
}
// Output: first, second, third

// Panic and recover (use sparingly)
func mightPanic() {
    defer func() {
        if r := recover(); r != nil {
            log.Println("Recovered from:", r)
        }
    }()
    
    panic("something terrible happened")
}
```

---

## 11. Logging

```go
import "log"

// Basic logging (includes timestamp)
log.Println("Server started")
// Output: 2024/02/15 14:30:45 Server started

log.Printf("Listening on port %d\n", port)
// Output: 2024/02/15 14:30:45 Listening on port 4000

// Fatal (logs and calls os.Exit(1))
log.Fatal("Cannot start server")

// Panic (logs and panics)
log.Panic("Unrecoverable error")

// Custom log flags
log.SetFlags(log.Ltime)                    // Only time: 14:30:45
log.SetFlags(log.Ldate | log.Ltime)        // Date and time (default)
log.SetFlags(log.Lshortfile)               // Include filename:line
log.SetFlags(log.Ltime | log.Lmicroseconds) // Include microseconds

// Custom prefix
log.SetPrefix("[PROXY] ")
log.Println("Started")
// Output: [PROXY] 2024/02/15 14:30:45 Started

// Log to file
file, _ := os.OpenFile("proxy.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
log.SetOutput(file)

// Create custom logger
logger := log.New(os.Stdout, "[HTTP] ", log.Ltime)
logger.Println("Request received")
```

---

## 12. Complete Mini Example

A working TCP echo server demonstrating many concepts:

```go
package main

import (
    "bufio"
    "fmt"
    "io"
    "log"
    "net"
    "os"
    "strings"
    "sync"
    "time"
)

// ============================================================
// STATS - Thread-safe statistics
// ============================================================

type Stats struct {
    connections int
    mutex       sync.Mutex
}

func NewStats() *Stats {
    return &Stats{}
}

func (s *Stats) Increment() {
    s.mutex.Lock()
    defer s.mutex.Unlock()
    s.connections++
}

func (s *Stats) Get() int {
    s.mutex.Lock()
    defer s.mutex.Unlock()
    return s.connections
}

// ============================================================
// BLOCKLIST - Thread-safe set of blocked items
// ============================================================

type Blocklist struct {
    items map[string]bool
    mutex sync.RWMutex
}

func NewBlocklist() *Blocklist {
    return &Blocklist{
        items: make(map[string]bool),
    }
}

func (b *Blocklist) Add(item string) {
    b.mutex.Lock()
    defer b.mutex.Unlock()
    b.items[item] = true
}

func (b *Blocklist) Remove(item string) {
    b.mutex.Lock()
    defer b.mutex.Unlock()
    delete(b.items, item)
}

func (b *Blocklist) Contains(item string) bool {
    b.mutex.RLock()
    defer b.mutex.RUnlock()
    return b.items[item]
}

func (b *Blocklist) GetAll() []string {
    b.mutex.RLock()
    defer b.mutex.RUnlock()
    
    result := make([]string, 0, len(b.items))
    for item := range b.items {
        result = append(result, item)
    }
    return result
}

// ============================================================
// CONNECTION HANDLER
// ============================================================

func handleConnection(conn net.Conn, stats *Stats, blocklist *Blocklist) {
    defer conn.Close()
    
    stats.Increment()
    
    start := time.Now()
    remoteAddr := conn.RemoteAddr().String()
    log.Printf("[CONNECT] %s", remoteAddr)
    
    reader := bufio.NewReader(conn)
    
    for {
        // Set read timeout
        conn.SetReadDeadline(time.Now().Add(30 * time.Second))
        
        // Read a line
        line, err := reader.ReadString('\n')
        if err != nil {
            if err != io.EOF {
                log.Printf("[ERROR] %s: %v", remoteAddr, err)
            }
            break
        }
        
        line = strings.TrimSpace(line)
        
        // Check blocklist
        if blocklist.Contains(line) {
            conn.Write([]byte("BLOCKED\n"))
            continue
        }
        
        // Echo back
        response := fmt.Sprintf("ECHO: %s\n", line)
        conn.Write([]byte(response))
    }
    
    elapsed := time.Since(start)
    log.Printf("[DISCONNECT] %s (duration: %v)", remoteAddr, elapsed)
}

// ============================================================
// CONSOLE
// ============================================================

func runConsole(stats *Stats, blocklist *Blocklist) {
    scanner := bufio.NewScanner(os.Stdin)
    
    fmt.Println("Commands: stats, block <word>, unblock <word>, list, quit")
    fmt.Print("> ")
    
    for scanner.Scan() {
        input := scanner.Text()
        parts := strings.Fields(input)
        
        if len(parts) == 0 {
            fmt.Print("> ")
            continue
        }
        
        switch parts[0] {
        case "stats":
            fmt.Printf("Total connections: %d\n", stats.Get())
            
        case "block":
            if len(parts) > 1 {
                blocklist.Add(parts[1])
                fmt.Printf("Blocked: %s\n", parts[1])
            } else {
                fmt.Println("Usage: block <word>")
            }
            
        case "unblock":
            if len(parts) > 1 {
                blocklist.Remove(parts[1])
                fmt.Printf("Unblocked: %s\n", parts[1])
            } else {
                fmt.Println("Usage: unblock <word>")
            }
            
        case "list":
            items := blocklist.GetAll()
            if len(items) == 0 {
                fmt.Println("Blocklist is empty")
            } else {
                fmt.Println("Blocked items:")
                for _, item := range items {
                    fmt.Printf("  - %s\n", item)
                }
            }
            
        case "quit", "exit":
            fmt.Println("Goodbye!")
            os.Exit(0)
            
        default:
            fmt.Println("Unknown command")
        }
        
        fmt.Print("> ")
    }
}

// ============================================================
// SERVER
// ============================================================

func runServer(port int, stats *Stats, blocklist *Blocklist) {
    address := fmt.Sprintf(":%d", port)
    
    listener, err := net.Listen("tcp", address)
    if err != nil {
        log.Fatal("Failed to start server:", err)
    }
    defer listener.Close()
    
    log.Printf("Server listening on port %d", port)
    
    for {
        conn, err := listener.Accept()
        if err != nil {
            log.Println("Accept error:", err)
            continue
        }
        
        go handleConnection(conn, stats, blocklist)
    }
}

// ============================================================
// MAIN
// ============================================================

func main() {
    // Initialize shared state
    stats := NewStats()
    blocklist := NewBlocklist()
    
    // Start server in background
    go runServer(8080, stats, blocklist)
    
    // Run console in foreground
    runConsole(stats, blocklist)
}
```

### Testing the Example

```bash
# Terminal 1: Run the server
go run main.go

# Terminal 2: Connect with netcat
nc localhost 8080
hello        # Type this
ECHO: hello  # Server responds

# Terminal 1: Block a word
> block hello
Blocked: hello

# Terminal 2: Try blocked word
hello
BLOCKED
```

---

## 13. Quick Reference Card

### Data Structures

| Task | Code |
|------|------|
| Create map | `m := make(map[string]string)` |
| Create set | `s := make(map[string]bool)` |
| Create slice | `items := []string{}` |
| Append to slice | `items = append(items, "x")` |
| Check map key exists | `val, ok := m["key"]` |
| Delete from map | `delete(m, "key")` |
| Length | `len(m)` or `len(items)` |

### Networking

| Task | Code |
|------|------|
| TCP listen | `net.Listen("tcp", ":4000")` |
| TCP connect | `net.Dial("tcp", "host:80")` |
| TCP connect with timeout | `net.DialTimeout("tcp", "host:80", 10*time.Second)` |
| Accept connection | `conn, err := listener.Accept()` |
| Read line | `bufio.NewReader(conn).ReadString('\n')` |
| Read all | `io.ReadAll(conn)` |
| Write bytes | `conn.Write([]byte("data"))` |
| Write formatted | `fmt.Fprintf(conn, "Status: %d", code)` |
| Copy streams | `io.Copy(dst, src)` |
| Set timeout | `conn.SetDeadline(time.Now().Add(30*time.Second))` |
| Close | `conn.Close()` |

### Concurrency

| Task | Code |
|------|------|
| Start goroutine | `go func() { ... }()` |
| Wait for goroutines | `wg.Add(1); defer wg.Done(); wg.Wait()` |
| Lock mutex | `mutex.Lock(); defer mutex.Unlock()` |
| Read lock | `mutex.RLock(); defer mutex.RUnlock()` |

### Strings

| Task | Code |
|------|------|
| Split | `strings.Split(s, ",")` |
| Split by whitespace | `strings.Fields(s)` |
| Split N times | `strings.SplitN(s, ":", 2)` |
| Join | `strings.Join(slice, ", ")` |
| Contains | `strings.Contains(s, "text")` |
| HasPrefix | `strings.HasPrefix(s, "http://")` |
| Trim whitespace | `strings.TrimSpace(s)` |
| To lower | `strings.ToLower(s)` |

### Time

| Task | Code |
|------|------|
| Current time | `time.Now()` |
| Duration | `5 * time.Minute` |
| Add duration | `now.Add(5 * time.Minute)` |
| Time since | `time.Since(start)` |
| Compare | `time.Now().After(deadline)` |
| Sleep | `time.Sleep(1 * time.Second)` |
| Format | `time.Now().Format("15:04:05")` |

### Common Patterns

```go
// Error handling
if err != nil {
    return err
}

// Defer cleanup
defer conn.Close()

// Check map key
if val, ok := m["key"]; ok {
    // key exists
}

// Read loop
for scanner.Scan() {
    line := scanner.Text()
}

// Accept loop
for {
    conn, _ := listener.Accept()
    go handle(conn)
}
```

---

## Running Your Code

```bash
# Initialize module (first time only)
go mod init web-proxy

# Run directly
go run .
go run main.go

# Build executable
go build -o proxy
./proxy

# Run tests
go test ./...

# Format code
go fmt ./...

# Check for issues
go vet ./...
```
