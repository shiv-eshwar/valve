package lease

import (
	"sync"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
)

type denyEntry struct {
	until     time.Time
	limitType api.LimitType
}

// DenyCache remembers subjects that are known over-limit until a time.
type DenyCache struct {
	mu    sync.Mutex
	byKey map[string]denyEntry
	now   func() time.Time
}

// NewDenyCache creates an empty deny cache.
func NewDenyCache() *DenyCache {
	return &DenyCache{
		byKey: make(map[string]denyEntry),
		now:   time.Now,
	}
}

func cacheKey(key api.Key) string {
	m := key.Model
	if m == "" {
		m = "-"
	}
	return key.Subject + "\x00" + m
}

// Get returns a deny decision if still cached.
func (c *DenyCache) Get(key api.Key) (api.LimitType, time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byKey[cacheKey(key)]
	if !ok {
		return "", 0, false
	}
	now := c.now()
	if !now.Before(e.until) {
		delete(c.byKey, cacheKey(key))
		return "", 0, false
	}
	return e.limitType, e.until.Sub(now), true
}

// Set records a deny until now+retryAfter.
func (c *DenyCache) Set(key api.Key, limitType api.LimitType, retryAfter time.Duration) {
	if retryAfter <= 0 {
		retryAfter = time.Second
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byKey[cacheKey(key)] = denyEntry{
		until:     c.now().Add(retryAfter),
		limitType: limitType,
	}
}

// Clear removes a deny entry (e.g. after refund restores headroom).
func (c *DenyCache) Clear(key api.Key) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.byKey, cacheKey(key))
}
