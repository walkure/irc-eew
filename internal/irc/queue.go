package irc

import "sync"

// Priority classifies a queued Notice for eviction purposes when the queue
// exceeds its capacity (measured in distinct pending eq_ids, not message
// count): entries with a higher Priority are evicted last. A cancellation
// or final report is the last update an earthquake will ever receive, so
// those are protected longer than an in-progress (non-final) report.
type Priority int

const (
	PriorityNormal Priority = iota
	PriorityFinal
	PriorityCancellation
)

// Notice is one pending send: the plain-text message body and the channels
// (as configured; not yet charset-encoded — see EncodeText) it should reach.
type Notice struct {
	EqID     string
	Channels []string
	Text     string
	Priority Priority
}

// Queue holds at most Capacity distinct-eq_id pending Notices. It has no
// equivalent in irc-eew.pl, which sent synchronously with no notion of a
// backlog; it exists here because a single IRC connection is one ordered
// TCP stream, and the WNI receive goroutine must never block waiting for it.
//
// Two behaviours distinguish it from a plain FIFO channel:
//
//   - Coalescing: enqueuing a Notice for an EqID that already has an unsent
//     entry replaces that entry's content in place, keeping its original
//     delivery slot. A later report for the same earthquake always
//     supersedes an earlier unsent one.
//   - Priority eviction: when a new (distinct) EqID would exceed Capacity,
//     the oldest entry among the lowest Priority present is evicted first —
//     Normal before Final before Cancellation — so a final or cancellation
//     report (an earthquake's last word) survives longest.
//
// Queue is safe for concurrent use: Enqueue is meant to be called from the
// WNI receive goroutine (and never blocks), while Dequeue is called by the
// one goroutine that owns the IRC connection's write path.
type Queue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	capacity int
	order    []string // eq_ids, oldest first
	entries  map[string]Notice
	closed   bool
}

// NewQueue creates a Queue that holds at most capacity distinct-eq_id
// entries.
func NewQueue(capacity int) *Queue {
	q := &Queue{capacity: capacity, entries: make(map[string]Notice)}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Enqueue adds or replaces the pending notice for n.EqID. It never blocks.
func (q *Queue) Enqueue(n Notice) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}

	if _, exists := q.entries[n.EqID]; exists {
		// Coalesce: replace content, keep this eq_id's existing delivery slot.
		q.entries[n.EqID] = n
		q.cond.Signal()
		return
	}

	if len(q.order) >= q.capacity {
		q.evictOne()
	}

	q.order = append(q.order, n.EqID)
	q.entries[n.EqID] = n
	q.cond.Signal()
}

// evictOne removes the oldest entry among the lowest Priority present.
// Caller must hold q.mu. A no-op if the queue is empty (capacity 0).
func (q *Queue) evictOne() {
	for _, want := range []Priority{PriorityNormal, PriorityFinal, PriorityCancellation} {
		for i, eqID := range q.order {
			if q.entries[eqID].Priority == want {
				q.removeAt(i)
				return
			}
		}
	}
}

// removeAt deletes q.order[i] and its entry. Caller must hold q.mu.
func (q *Queue) removeAt(i int) {
	eqID := q.order[i]
	delete(q.entries, eqID)
	q.order = append(q.order[:i], q.order[i+1:]...)
}

// Dequeue blocks until a Notice is available — delivered in the order each
// eq_id first entered the queue, not the order it was last updated — or the
// queue is closed, in which case ok is false.
func (q *Queue) Dequeue() (n Notice, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.order) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.order) == 0 {
		return Notice{}, false
	}
	eqID := q.order[0]
	q.order = q.order[1:]
	n = q.entries[eqID]
	delete(q.entries, eqID)
	return n, true
}

// Close wakes any blocked Dequeue call, causing it to return ok=false, and
// makes future Enqueue calls no-ops. Used during graceful shutdown.
func (q *Queue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.cond.Broadcast()
}

// Len returns the number of distinct pending eq_ids.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.order)
}
