package irc

import (
	"testing"
	"time"
)

func mustDequeue(t *testing.T, q *Queue) Notice {
	t.Helper()
	n, ok := q.Dequeue()
	if !ok {
		t.Fatal("Dequeue: queue closed unexpectedly")
	}
	return n
}

func TestQueue_FIFOAcrossDistinctEqIDs(t *testing.T) {
	q := NewQueue(10)
	q.Enqueue(Notice{EqID: "A", Text: "a1"})
	q.Enqueue(Notice{EqID: "B", Text: "b1"})
	q.Enqueue(Notice{EqID: "C", Text: "c1"})

	if got := mustDequeue(t, q).Text; got != "a1" {
		t.Errorf("1st dequeue: got %q, want a1", got)
	}
	if got := mustDequeue(t, q).Text; got != "b1" {
		t.Errorf("2nd dequeue: got %q, want b1", got)
	}
	if got := mustDequeue(t, q).Text; got != "c1" {
		t.Errorf("3rd dequeue: got %q, want c1", got)
	}
}

func TestQueue_CoalescesSameEqID_KeepsOriginalSlot(t *testing.T) {
	q := NewQueue(10)
	q.Enqueue(Notice{EqID: "A", Text: "a1"})
	q.Enqueue(Notice{EqID: "B", Text: "b1"})
	// A newer report for A arrives after B was already queued; it should
	// replace A's content but not jump ahead of or behind B.
	q.Enqueue(Notice{EqID: "A", Text: "a2-final", Priority: PriorityFinal})

	if got := q.Len(); got != 2 {
		t.Fatalf("Len: got %d, want 2 (coalesced, not appended)", got)
	}

	first := mustDequeue(t, q)
	if first.EqID != "A" || first.Text != "a2-final" {
		t.Errorf("1st dequeue: got %+v, want A/a2-final (original slot, latest content)", first)
	}
	second := mustDequeue(t, q)
	if second.EqID != "B" {
		t.Errorf("2nd dequeue: got %+v, want B", second)
	}
}

func TestQueue_EvictsNormalBeforeFinalBeforeCancellation(t *testing.T) {
	q := NewQueue(2)
	q.Enqueue(Notice{EqID: "cancel-eq", Priority: PriorityCancellation})
	q.Enqueue(Notice{EqID: "normal-eq", Priority: PriorityNormal})
	// Over capacity now: normal-eq (lowest priority) should be evicted, not
	// cancel-eq.
	q.Enqueue(Notice{EqID: "final-eq", Priority: PriorityFinal})

	if q.Len() != 2 {
		t.Fatalf("Len: got %d, want 2", q.Len())
	}

	remaining := map[string]bool{}
	for i := 0; i < 2; i++ {
		remaining[mustDequeue(t, q).EqID] = true
	}
	if remaining["normal-eq"] {
		t.Error("normal-eq should have been evicted first, but is still present")
	}
	if !remaining["cancel-eq"] || !remaining["final-eq"] {
		t.Errorf("expected cancel-eq and final-eq to survive, got %v", remaining)
	}
}

func TestQueue_EvictsOldestWithinSamePriority(t *testing.T) {
	q := NewQueue(2)
	q.Enqueue(Notice{EqID: "first", Priority: PriorityNormal})
	q.Enqueue(Notice{EqID: "second", Priority: PriorityNormal})
	q.Enqueue(Notice{EqID: "third", Priority: PriorityNormal})

	remaining := map[string]bool{}
	count := q.Len()
	for i := 0; i < count; i++ {
		remaining[mustDequeue(t, q).EqID] = true
	}
	if remaining["first"] {
		t.Error("oldest entry ('first') should have been evicted")
	}
	if !remaining["second"] || !remaining["third"] {
		t.Errorf("expected second and third to survive, got %v", remaining)
	}
}

func TestQueue_EvictsCancellationAsLastResort(t *testing.T) {
	// If every pending entry is a cancellation, eviction must still make
	// progress rather than growing unbounded or deadlocking.
	q := NewQueue(1)
	q.Enqueue(Notice{EqID: "first-cancel", Priority: PriorityCancellation})
	q.Enqueue(Notice{EqID: "second-cancel", Priority: PriorityCancellation})

	if got := q.Len(); got != 1 {
		t.Fatalf("Len: got %d, want 1", got)
	}
	if got := mustDequeue(t, q).EqID; got != "second-cancel" {
		t.Errorf("got %q, want second-cancel (oldest cancellation evicted)", got)
	}
}

func TestQueue_DequeueBlocksUntilEnqueue(t *testing.T) {
	q := NewQueue(10)
	done := make(chan Notice, 1)
	go func() {
		n, ok := q.Dequeue()
		if !ok {
			return
		}
		done <- n
	}()

	select {
	case <-done:
		t.Fatal("Dequeue returned before anything was enqueued")
	case <-time.After(50 * time.Millisecond):
	}

	q.Enqueue(Notice{EqID: "A", Text: "a1"})

	select {
	case n := <-done:
		if n.EqID != "A" {
			t.Errorf("got %+v, want EqID=A", n)
		}
	case <-time.After(time.Second):
		t.Fatal("Dequeue did not wake up after Enqueue")
	}
}

func TestQueue_CloseUnblocksDequeue(t *testing.T) {
	q := NewQueue(10)
	done := make(chan bool, 1)
	go func() {
		_, ok := q.Dequeue()
		done <- ok
	}()

	select {
	case <-time.After(50 * time.Millisecond):
	case <-done:
		t.Fatal("Dequeue returned before Close")
	}

	q.Close()

	select {
	case ok := <-done:
		if ok {
			t.Error("Dequeue should report ok=false after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("Dequeue did not wake up after Close")
	}
}

func TestQueue_EnqueueAfterCloseIsNoop(t *testing.T) {
	q := NewQueue(10)
	q.Close()
	q.Enqueue(Notice{EqID: "A"})
	if got := q.Len(); got != 0 {
		t.Errorf("Len: got %d, want 0 (Enqueue after Close should be a no-op)", got)
	}
}
