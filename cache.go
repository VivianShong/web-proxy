package main

import (
	"net/http"
	"sync"
)

type CacheEntry struct {
	Body   []byte
	Header http.Header
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


