package main

import (
	"net/http"
	"sync"
	"time"
)

const cacheTTL = 5 * time.Minute
const cacheMaxEntries = 100

type CacheEntry struct {
	Body     []byte
	Header   http.Header
	CachedAt time.Time // when this entry was stored
}

// Fresh returns true if the entry is still within the TTL window.
func (e CacheEntry) Fresh() bool {
	return time.Since(e.CachedAt) < cacheTTL
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

// evict makes room for one new entry.
// Pass 1: remove all expired entries.
// Pass 2: if still at the limit, remove the single oldest fresh entry.
// Caller must already hold the ProxyState write lock — do NOT lock c.mu here.
func (c *Cache) evict() {
	for k, e := range c.entries {
		if !e.Fresh() {
			delete(c.entries, k)
		}
	}
	if len(c.entries) < cacheMaxEntries {
		return
	}
	var oldestKey string
	var oldestTime time.Time
	for k, e := range c.entries {
		if oldestKey == "" || e.CachedAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.CachedAt
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}


