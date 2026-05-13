package noncecache

import (
	"sync"
	"time"
)

type Cache struct {
	mu   sync.Mutex
	m    map[string]time.Time
	ttl  time.Duration
	skew time.Duration
}

func New(ttl, skew time.Duration) *Cache {
	return &Cache{m: make(map[string]time.Time), ttl: ttl, skew: skew}
}

func (c *Cache) Seen(nonce string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, t := range c.m {
		if now.Sub(t) > c.ttl {
			delete(c.m, k)
		}
	}
	if _, ok := c.m[nonce]; ok {
		return true
	}
	c.m[nonce] = now
	return false
}

func (c *Cache) Skew() time.Duration { return c.skew }
