package httpapi

import (
	"crypto/sha256"
	"sync"
	"time"
)

const textCacheSize = 256

type textCache struct {
	mu       sync.Mutex
	entries  map[[sha256.Size]byte]cachedText
	order    [][sha256.Size]byte
	inFlight map[[sha256.Size]byte]bool
}

type cachedText struct {
	text    string
	retryAt time.Time
}

func newTextCache() *textCache {
	return &textCache{
		entries:  make(map[[sha256.Size]byte]cachedText),
		inFlight: make(map[[sha256.Size]byte]bool),
	}
}

func (c *textCache) claim(key [sha256.Size]byte, now time.Time) (text string, fresh, start bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if ok && (e.retryAt.IsZero() || now.Before(e.retryAt)) {
		return e.text, true, false
	}
	if c.inFlight[key] {
		return e.text, false, false
	}
	c.inFlight[key] = true
	return e.text, false, true
}

func (c *textCache) finish(key [sha256.Size]byte, text string, retryAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inFlight, key)
	if _, ok := c.entries[key]; !ok {
		if len(c.order) >= textCacheSize {
			delete(c.entries, c.order[0])
			c.order = c.order[1:]
		}
		c.order = append(c.order, key)
	}
	c.entries[key] = cachedText{text: text, retryAt: retryAt}
}
