package github

import "sync"

// cache holds the ETag and body of the last 200 answer of each REST URL, for the life of
// the client (research G7): a request sent with If-None-Match that GitHub answers 304 Not
// Modified is not billed. Nothing is written to disk.
type cache struct {
	mu      sync.Mutex
	entries map[string]entry // keyed by full request URL
}

type entry struct {
	etag string
	body []byte
}

func newCache() *cache { return &cache{entries: map[string]entry{}} }

func (c *cache) get(u string) (entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[u]
	return e, ok
}

func (c *cache) put(u string, e entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[u] = e
}
