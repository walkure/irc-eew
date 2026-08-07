package app

import (
	"context"
	"errors"
	"testing"
)

func TestServerResolver_OverrideAlwaysWinsWithoutFetching(t *testing.T) {
	fetchCalled := false
	r := &serverResolver{
		override: "127.0.0.1:19000",
		fetch: func(ctx context.Context) ([]string, error) {
			fetchCalled = true
			return []string{"should-not-be-used:80"}, nil
		},
	}

	addr, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if addr != "127.0.0.1:19000" {
		t.Errorf("addr: got %q, want the override", addr)
	}
	if fetchCalled {
		t.Error("expected fetch to be skipped entirely when override is set")
	}
}

func TestServerResolver_FetchSuccess_CachesList(t *testing.T) {
	r := &serverResolver{
		fetch: func(ctx context.Context) ([]string, error) {
			return []string{"server-a:80"}, nil
		},
	}

	addr, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if addr != "server-a:80" {
		t.Errorf("addr: got %q", addr)
	}
	if len(r.cached) != 1 || r.cached[0] != "server-a:80" {
		t.Errorf("expected the fetched list to be cached, got %v", r.cached)
	}
}

func TestServerResolver_FetchFailure_NoCache_ReturnsError(t *testing.T) {
	r := &serverResolver{
		fetch: func(ctx context.Context) ([]string, error) {
			return nil, errors.New("list endpoint unreachable")
		},
	}

	_, err := r.Resolve(context.Background())
	if err == nil {
		t.Fatal("expected an error when the fetch fails and there is no cached list to fall back to")
	}
}

func TestServerResolver_FetchFailure_FallsBackToCachedList(t *testing.T) {
	callCount := 0
	r := &serverResolver{
		fetch: func(ctx context.Context) ([]string, error) {
			callCount++
			if callCount == 1 {
				return []string{"server-a:80", "server-b:80"}, nil
			}
			return nil, errors.New("list endpoint temporarily unreachable")
		},
	}

	// First call succeeds and populates the cache.
	if _, err := r.Resolve(context.Background()); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}

	// Second call's fetch fails, but should transparently fall back to the
	// list cached from the first call rather than erroring out — a
	// transient outage of the list endpoint shouldn't itself block
	// reconnecting to relay servers that may still be reachable.
	addr, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("second Resolve: %v (expected fallback to cached list, not an error)", err)
	}
	if addr != "server-a:80" && addr != "server-b:80" {
		t.Errorf("addr: got %q, want one of the cached servers", addr)
	}
}

func TestServerResolver_FetchSuccessAfterFallback_RefreshesCache(t *testing.T) {
	callCount := 0
	r := &serverResolver{
		fetch: func(ctx context.Context) ([]string, error) {
			callCount++
			switch callCount {
			case 1:
				return []string{"stale-server:80"}, nil
			case 2:
				return nil, errors.New("transient failure")
			default:
				return []string{"fresh-server:80"}, nil
			}
		},
	}

	if _, err := r.Resolve(context.Background()); err != nil { // populates cache with stale-server
		t.Fatalf("call 1: %v", err)
	}
	if _, err := r.Resolve(context.Background()); err != nil { // falls back to stale-server
		t.Fatalf("call 2: %v", err)
	}

	addr, err := r.Resolve(context.Background()) // fetch succeeds again with a new list
	if err != nil {
		t.Fatalf("call 3: %v", err)
	}
	if addr != "fresh-server:80" {
		t.Errorf("addr: got %q, want the freshly fetched server (cache should update once fetching recovers)", addr)
	}
}
