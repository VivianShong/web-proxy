package main

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

// RequestLog represents a single request handled by the proxy
type RequestLog struct {
	Time   time.Time `json:"time"`
	Method string    `json:"method"`
	URL    string    `json:"url"`
	Status string    `json:"status"` // "Blocked", "Allowed", "Error"
	SrcIP  string    `json:"src_ip"`
}

// ProxyState holds the shared state for the proxy
type ProxyState struct {
	mu           sync.RWMutex
	BlockedHosts map[string]bool
	RequestLogs  []RequestLog
	LogLimit     int
	Cache        *Cache
}

// NewProxyState creates a new ProxyState
func NewProxyState() *ProxyState {
	return &ProxyState{
		BlockedHosts: make(map[string]bool),
		RequestLogs:  make([]RequestLog, 0),
		LogLimit:     100, // Keep last 100 logs
		Cache:        NewCache(),
	}
}

// Block adds a host to the blocked list
func (s *ProxyState) Block(host string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.BlockedHosts[strings.ToLower(host)] = true
}

// Unblock removes a host from the blocked list
func (s *ProxyState) Unblock(host string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.BlockedHosts, strings.ToLower(host))
}

// IsBlocked checks if a host is in the blocked list
func (s *ProxyState) IsBlocked(host string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Check for exact match or suffix match (e.g., "google.com" blocks "www.google.com")
	// For simplicity, we'll start with exact match and maybe simple subdomain check
	// A more robust check would split by dots.
	host = strings.ToLower(host)
	if s.BlockedHosts[host] {
		return true
	}
	// Check if parent domains are blocked
	for blocked := range s.BlockedHosts {
		if strings.HasSuffix(host, "."+blocked) {
			return true
		}
	}
	return false
}

// AddToCache adds a response to the cache
func (s *ProxyState) AddToCache(key string, resp CacheEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Cache.entries[key] = resp
}

// GetFromCache retrieves a response from the cache
func (s *ProxyState) GetFromCache(key string) (CacheEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, exists := s.Cache.entries[key]
    return entry, exists
}

// LogRequest adds a request log
func (s *ProxyState) LogRequest(req RequestLog) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Prepend log (newest first)
	s.RequestLogs = append([]RequestLog{req}, s.RequestLogs...)

	// Trim if exceeds limit
	if len(s.RequestLogs) > s.LogLimit {
		s.RequestLogs = s.RequestLogs[:s.LogLimit]
	}
}

// GetLogs returns a copy of the logs
func (s *ProxyState) GetLogs() []RequestLog {
	s.mu.RLock()
	defer s.mu.RUnlock()

	logs := make([]RequestLog, len(s.RequestLogs))
	copy(logs, s.RequestLogs)
	return logs
}

// GetBlocked returns a list of blocked hosts
func (s *ProxyState) GetBlocked() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	blocked := make([]string, 0, len(s.BlockedHosts))
	for host := range s.BlockedHosts {
		blocked = append(blocked, host)
	}
	return blocked
}

// SaveBlocked saves the blocked list to a file
func (s *ProxyState) SaveBlocked(filename string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s.BlockedHosts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

// LoadBlocked loads the blocked list from a file
func (s *ProxyState) LoadBlocked(filename string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, just ignore
		}
		return err
	}

	return json.Unmarshal(data, &s.BlockedHosts)
}
