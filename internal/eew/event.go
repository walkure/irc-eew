// Package eew holds the source-independent model of an Earthquake Early
// Warning report, the parser that builds it from JMA's VXSE45 XML (as
// delivered by DMDATA.JP), and small helpers shared by the notifiers.
package eew

import (
	"container/list"
	"time"
)

// Event is what the notification dispatchers (Slack tiers, IRC channels)
// need to know about a report: which earthquake it belongs to and whether
// it ends that earthquake's series.
type Event struct {
	// EventID is JMA's earthquake identifier (Head/EventID, 14 digits).
	EventID string
	// Final reports the series' last report (Body/NextAdvisory).
	Final bool
	// Cancelled reports a cancellation (Head/InfoType 取消).
	Cancelled bool
}

// DefaultRecentCapacity is how many distinct earthquakes the bounded
// per-EventID memories (RecentSet, OriginCache) keep. Reports for several
// earthquakes often interleave, but never anywhere near this many at once.
const DefaultRecentCapacity = 64

// RecentSet remembers the most recently seen EventIDs, up to a fixed
// capacity (oldest forgotten first). Not safe for concurrent use.
type RecentSet struct {
	capacity int
	order    *list.List // front = most recent
	index    map[string]*list.Element
}

// NewRecentSet returns a RecentSet holding at most capacity IDs
// (DefaultRecentCapacity if capacity <= 0).
func NewRecentSet(capacity int) *RecentSet {
	if capacity <= 0 {
		capacity = DefaultRecentCapacity
	}
	return &RecentSet{capacity: capacity, order: list.New(), index: map[string]*list.Element{}}
}

// OriginCache remembers the latest known origin time per EventID, for the
// most recent earthquakes only (oldest forgotten first). A cancellation
// carries no origin time of its own; the notifier shows the one from the
// same earthquake's latest earlier report instead. Kept per EventID since
// several earthquakes' reports often interleave. Not safe for concurrent
// use.
type OriginCache struct {
	ids     *RecentSet
	origins map[string]time.Time
}

// NewOriginCache returns an OriginCache for at most capacity earthquakes
// (DefaultRecentCapacity if capacity <= 0).
func NewOriginCache(capacity int) *OriginCache {
	return &OriginCache{ids: NewRecentSet(capacity), origins: map[string]time.Time{}}
}

// Observe records r's origin time (if it has one) and returns the origin
// time to show for r: its own, or the remembered one for its EventID, or
// nil.
func (c *OriginCache) Observe(r *Report) *time.Time {
	if r.EventID == "" {
		return r.OriginTime
	}
	c.ids.Add(r.EventID)
	// Drop entries the bounded ID set has forgotten.
	for id := range c.origins {
		if _, ok := c.ids.index[id]; !ok {
			delete(c.origins, id)
		}
	}
	if r.OriginTime != nil {
		c.origins[r.EventID] = *r.OriginTime
		return r.OriginTime
	}
	if t, ok := c.origins[r.EventID]; ok {
		return &t
	}
	return nil
}

// Add records id and reports whether it was new (not already remembered).
func (s *RecentSet) Add(id string) bool {
	if el, ok := s.index[id]; ok {
		s.order.MoveToFront(el)
		return false
	}
	s.index[id] = s.order.PushFront(id)
	for s.order.Len() > s.capacity {
		oldest := s.order.Back()
		s.order.Remove(oldest)
		delete(s.index, oldest.Value.(string))
	}
	return true
}
