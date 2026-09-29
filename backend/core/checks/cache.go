// core/checks/cache.go

package checks

import (
	"container/list"
	"sync"
)

// cacheSize bounds the reports kept; a report with a rated timetable of a large school runs to hundreds of kilobytes.
const cacheSize = 64

// cache keeps the most recently used reports by check key: the stored object, and the rules when a profile replaced the document's.
type cache struct {
	mu    sync.Mutex
	order *list.List
	items map[string]*list.Element
}

type cached struct {
	key    string
	report Report
}

func newCache() *cache {
	return &cache{order: list.New(), items: map[string]*list.Element{}}
}

func (c *cache) get(key string) (Report, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.items[key]
	if !ok {
		return Report{}, false
	}
	c.order.MoveToFront(element)
	return element.Value.(*cached).report, true
}

func (c *cache) put(key string, report Report) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.items[key]; ok {
		element.Value.(*cached).report = report
		c.order.MoveToFront(element)
		return
	}
	c.items[key] = c.order.PushFront(&cached{key: key, report: report})
	for c.order.Len() > cacheSize {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*cached).key)
	}
}
