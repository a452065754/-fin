package server

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"
)

// PreviewCache keeps recently rendered previews so the same report with the
// same parameters is not rendered twice.
//
// Rendering a large report can take seconds, and people tend to reload the
// same page or flip back and forth between two templates. A short lived cache
// removes that cost without serving stale data for long.
type PreviewCache struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry
	// maxEntries bounds the memory use. The oldest entries are dropped first.
	maxEntries int
	// ttl is how long an entry stays valid.
	ttl time.Duration
	// hits and misses count the cache effectiveness.
	hits   int64
	misses int64
}

type cacheEntry struct {
	data      []byte
	created   time.Time
	expiresAt time.Time
}

// NewPreviewCache creates a cache. A ttl of zero disables caching.
func NewPreviewCache(ttl time.Duration, maxEntries int) *PreviewCache {
	if maxEntries <= 0 {
		maxEntries = 32
	}
	return &PreviewCache{
		entries:    map[string]*cacheEntry{},
		maxEntries: maxEntries,
		ttl:        ttl,
	}
}

// Enabled reports whether the cache is in use.
func (c *PreviewCache) Enabled() bool {
	return c != nil && c.ttl > 0
}

// key builds the cache key from the template path and the parameters.
func previewKey(path string, params map[string]string) string {
	names := make([]string, 0, len(params))
	for k := range params {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(path)
	for _, n := range names {
		b.WriteByte(0)
		b.WriteString(n)
		b.WriteByte('=')
		b.WriteString(params[n])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// Get returns a cached payload if it is still valid.
func (c *PreviewCache) Get(key string) ([]byte, bool) {
	if !c.Enabled() {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		delete(c.entries, key)
		c.misses++
		return nil, false
	}
	c.hits++
	return e.data, true
}

// Put stores a payload in the cache.
func (c *PreviewCache) Put(key string, data []byte) {
	if !c.Enabled() {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	// Drop the expired entries first so the size limit is not wasted.
	for k, e := range c.entries {
		if now.After(e.expiresAt) {
			delete(c.entries, k)
		}
	}
	// Still full: drop the oldest entry.
	if len(c.entries) >= c.maxEntries {
		var oldestKey string
		var oldest time.Time
		for k, e := range c.entries {
			if oldestKey == "" || e.created.Before(oldest) {
				oldestKey = k
				oldest = e.created
			}
		}
		if oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}
	c.entries[key] = &cacheEntry{
		data:      data,
		created:   now,
		expiresAt: now.Add(c.ttl),
	}
}

// Invalidate drops every entry. It is used when the underlying templates
// change on disk.
func (c *PreviewCache) Invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]*cacheEntry{}
}

// templateTimes records the modification time of the template a cached entry
// was rendered from, so an edited template is not served from the cache.
type templateTimes struct {
	mu    sync.Mutex
	times map[string]time.Time
}

func newTemplateTimes() *templateTimes {
	return &templateTimes{times: map[string]time.Time{}}
}

func (t *templateTimes) get(key string) (time.Time, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	m, ok := t.times[key]
	return m, ok
}

func (t *templateTimes) set(key string, m time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.times[key] = m
}

// Stats reports the cache effectiveness.
func (c *PreviewCache) Stats() map[string]any {
	if c == nil {
		return map[string]any{"enabled": false}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]any{
		"enabled":    c.ttl > 0,
		"entries":    len(c.entries),
		"maxEntries": c.maxEntries,
		"ttlSeconds": int(c.ttl.Seconds()),
		"hits":       c.hits,
		"misses":     c.misses,
	}
}
