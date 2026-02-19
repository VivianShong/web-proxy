package main

import (
	"net/http"
	"sync"
	"time"
)

const cacheTTL = 5 * time.Minute

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


