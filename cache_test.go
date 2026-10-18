package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestChannelCacheReturnsCachedValue(t *testing.T) {
	cache := NewChannelCache(time.Minute, 10)
	var loads atomic.Int32
	loader := func(context.Context) ([]Channel, error) {
		loads.Add(1)
		return []Channel{{ID: 1, Number: 1, Name: "Первый"}}, nil
	}
	first, err := cache.GetOrLoad(context.Background(), 7, loader)
	if err != nil {
		t.Fatalf("first GetOrLoad: %v", err)
	}
	first[0].Name = "mutated by caller"
	second, err := cache.GetOrLoad(context.Background(), 7, loader)
	if err != nil {
		t.Fatalf("second GetOrLoad: %v", err)
	}
	if loads.Load() != 1 {
		t.Fatalf("loader called %d times, want 1", loads.Load())
	}
	if second[0].Name != "Первый" {
		t.Fatalf("cache data was mutated: %q", second[0].Name)
	}
}

func TestChannelCacheInvalidation(t *testing.T) {
	cache := NewChannelCache(time.Minute, 10)
	var loads atomic.Int32
	loader := func(context.Context) ([]Channel, error) {
		n := loads.Add(1)
		return []Channel{{ID: int64(n), Number: int(n), Name: "channel"}}, nil
	}
	if _, err := cache.GetOrLoad(context.Background(), 1, loader); err != nil {
		t.Fatal(err)
	}
	cache.InvalidateAll()
	channels, err := cache.GetOrLoad(context.Background(), 1, loader)
	if err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 2 || channels[0].ID != 2 {
		t.Fatalf("cache was not invalidated: loads=%d channels=%+v", loads.Load(), channels)
	}
}

func TestChannelCacheDoesNotStoreAfterInvalidationDuringLoad(t *testing.T) {
	cache := NewChannelCache(time.Minute, 10)
	started := make(chan struct{})
	resume := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var calls atomic.Int32
		_, _ = cache.GetOrLoad(context.Background(), 1, func(context.Context) ([]Channel, error) {
			if calls.Add(1) == 1 {
				close(started)
				<-resume
				return []Channel{{ID: 1, Name: "stale"}}, nil
			}
			return []Channel{{ID: 2, Name: "fresh"}}, nil
		})
	}()
	<-started
	cache.InvalidateAll()
	close(resume)
	<-done
	var loads atomic.Int32
	channels, err := cache.GetOrLoad(context.Background(), 1, func(context.Context) ([]Channel, error) {
		loads.Add(1)
		return []Channel{{ID: 2, Name: "fresh"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 0 || channels[0].Name != "fresh" {
		t.Fatalf("expected cached fresh result, got %+v, external loads=%d", channels, loads.Load())
	}
}

func TestChannelCacheExpiresAtSubscriptionDeadline(t *testing.T) {
	cache := NewChannelCache(time.Minute, 10)
	var loads atomic.Int32
	deadline := time.Now().Add(25 * time.Millisecond)
	loader := func(context.Context) ([]Channel, error) {
		n := loads.Add(1)
		if n == 1 {
			return []Channel{{ID: 1, Name: "time-limited", ValidUntil: &deadline}}, nil
		}
		return []Channel{}, nil
	}
	if _, err := cache.GetOrLoad(context.Background(), 5, loader); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	channels, err := cache.GetOrLoad(context.Background(), 5, loader)
	if err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 2 || len(channels) != 0 {
		t.Fatalf("expired subscription remained cached: loads=%d channels=%+v", loads.Load(), channels)
	}
}
