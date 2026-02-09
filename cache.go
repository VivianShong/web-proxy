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


