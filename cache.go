package main

import (
	"net/http"
	"sync"
	"time"
)

type CacheEntry struct {
	Body      []byte
	Header    http.Header
	ExpiresAt time.Time
}

type Cache struct {
	mu      sync.RWMutex
	entries map[string]CacheEntry
}

func NewCache() *Cache {
	return &Cache{
		entries: make(map[string]CacheEntry),
	}
}

func (c *Cache) Get(key string) (CacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok {
		return CacheEntry{}, false
	}
	if time.Now().After(entry.ExpiresAt) {
		// Optimization: delete expired entry lazily or here?
		// Since we have RLock, we can't delete directly without upgrading lock (complex)
		// or we just return false and let a background cleaner or Set handle it.
		// For simplicity, just return false.
		return CacheEntry{}, false
	}
	return entry, true
}

func (c *Cache) Set(key string, entry CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry
}
