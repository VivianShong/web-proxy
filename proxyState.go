package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	mu              sync.RWMutex
	BlockedPatterns []string
	RequestLogs     []RequestLog
	LogLimit        int
	Cache           *Cache
}

// NewProxyState creates a new ProxyState
func NewProxyState() *ProxyState {
	return &ProxyState{
		BlockedPatterns: make([]string, 0),
		RequestLogs:     make([]RequestLog, 0),
		LogLimit:        100, // Keep last 100 logs
		Cache:           NewCache(),
	}
}

// Block adds a pattern to the blocked list
func (s *ProxyState) Block(pattern string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Check if already exists to avoid duplicates
	for _, p := range s.BlockedPatterns {
		if p == pattern {
			return
		}
	}
	s.BlockedPatterns = append(s.BlockedPatterns, pattern)
}

// Unblock removes a pattern from the blocked list
func (s *ProxyState) Unblock(pattern string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	newPatterns := make([]string, 0, len(s.BlockedPatterns))
	for _, p := range s.BlockedPatterns {
		if p != pattern {
			newPatterns = append(newPatterns, p)
		}
	}
	s.BlockedPatterns = newPatterns
}

// IsBlocked checks if a url matches any blocked pattern
func (s *ProxyState) IsBlocked(url string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	for _, pattern := range s.BlockedPatterns {
		matched, _ := filepath.Match(pattern, url)
		if matched {
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

// GetBlocked returns a list of blocked patterns
func (s *ProxyState) GetBlocked() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	// Return a copy to be safe
	blocked := make([]string, len(s.BlockedPatterns))
	copy(blocked, s.BlockedPatterns)
	return blocked
}

// SaveBlocked saves the blocked list to a file
func (s *ProxyState) SaveBlocked(filename string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s.BlockedPatterns, "", "  ")
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

	return json.Unmarshal(data, &s.BlockedPatterns)
}
