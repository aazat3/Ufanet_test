package main

import (
	"context"
	"sync"
	"time"
)

type channelCacheEntry struct {
	channels []Channel
	expires  time.Time
}

type channelCall struct {
	done     chan struct{}
	channels []Channel
	err      error
	version  uint64
}

type ChannelCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	maxSize int
	version uint64
	entries map[int64]channelCacheEntry
	calls   map[int64]*channelCall
}

func NewChannelCache(ttl time.Duration, maxSize int) *ChannelCache {
	return &ChannelCache{ttl: ttl, maxSize: maxSize, entries: make(map[int64]channelCacheEntry), calls: make(map[int64]*channelCall)}
}

func (c *ChannelCache) GetOrLoad(ctx context.Context, userID int64, loader func(context.Context) ([]Channel, error)) ([]Channel, error) {
	for {
		c.mu.Lock()
		if entry, ok := c.entries[userID]; ok && time.Now().Before(entry.expires) {
			result := cloneChannels(entry.channels)
			c.mu.Unlock()
			return result, nil
		}
		delete(c.entries, userID)
		if call, ok := c.calls[userID]; ok {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-call.done:
				c.mu.Lock()
				invalidated := call.version != c.version
				c.mu.Unlock()
				if invalidated {
					continue
				}
				if call.err != nil {
					return nil, call.err
				}
				return cloneChannels(call.channels), nil
			}
		}
		version := c.version
		call := &channelCall{done: make(chan struct{}), version: version}
		c.calls[userID] = call
		c.mu.Unlock()

		channels, err := loader(ctx)
		c.mu.Lock()
		call.channels, call.err = cloneChannels(channels), err
		if err == nil && c.ttl > 0 && version == c.version {
			now := time.Now()
			expires := now.Add(c.ttl)
			for _, channel := range channels {
				if channel.ValidUntil != nil && channel.ValidUntil.Before(expires) {
					expires = *channel.ValidUntil
				}
			}
			c.pruneExpiredLocked(now)
			if len(c.entries) >= c.maxSize && c.maxSize > 0 {
				c.evictEarliestLocked()
			}
			if c.maxSize > 0 && expires.After(now) {
				c.entries[userID] = channelCacheEntry{channels: cloneChannels(channels), expires: expires}
			}
		}
		invalidated := version != c.version
		delete(c.calls, userID)
		close(call.done)
		c.mu.Unlock()
		if invalidated && err == nil {
			continue
		}
		return cloneChannels(channels), err
	}
}

func (c *ChannelCache) InvalidateAll() {
	c.mu.Lock()
	c.version++
	clear(c.entries)
	c.mu.Unlock()
}

func (c *ChannelCache) pruneExpiredLocked(now time.Time) {
	for id, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, id)
		}
	}
}

func (c *ChannelCache) evictEarliestLocked() {
	var oldestID int64
	var oldest time.Time
	first := true
	for id, entry := range c.entries {
		if first || entry.expires.Before(oldest) {
			oldestID, oldest, first = id, entry.expires, false
		}
	}
	if !first {
		delete(c.entries, oldestID)
	}
}

func cloneChannels(in []Channel) []Channel {
	out := make([]Channel, len(in))
	copy(out, in)
	for i := range out {
		if in[i].ValidUntil != nil {
			value := *in[i].ValidUntil
			out[i].ValidUntil = &value
		}
	}
	return out
}
