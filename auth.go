package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const principalContextKey contextKey = "principal"

func principalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalContextKey).(Principal)
	return p, ok
}

type authCacheEntry struct {
	principal Principal
	expires   time.Time
}

type authCall struct {
	done      chan struct{}
	principal Principal
	err       error
	version   uint64
}

type authStore interface {
	getAuthUser(context.Context, string) (authUser, error)
}

type Authenticator struct {
	store   authStore
	ttl     time.Duration
	maxSize int
	key     []byte
	mu      sync.Mutex
	cache   map[string]authCacheEntry
	calls   map[string]*authCall
	version uint64
}

func NewAuthenticator(store authStore, ttl time.Duration, maxSize int) (*Authenticator, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return &Authenticator{store: store, ttl: ttl, maxSize: maxSize, key: key, cache: make(map[string]authCacheEntry), calls: make(map[string]*authCall)}, nil
}

func (a *Authenticator) Authenticate(ctx context.Context, username, password string) (Principal, error) {
	if username == "" || password == "" {
		return Principal{}, errUnauthenticated
	}
	key := a.cacheKey(username, password)
	for {
		now := time.Now()
		a.mu.Lock()
		if entry, ok := a.cache[key]; ok && now.Before(entry.expires) {
			p := entry.principal
			a.mu.Unlock()
			return p, nil
		}
		delete(a.cache, key)
		if call, ok := a.calls[key]; ok {
			a.mu.Unlock()
			select {
			case <-ctx.Done():
				return Principal{}, ctx.Err()
			case <-call.done:
				a.mu.Lock()
				invalidated := call.version != a.version
				a.mu.Unlock()
				if invalidated {
					continue
				}
				return call.principal, call.err
			}
		}
		call := &authCall{done: make(chan struct{}), version: a.version}
		a.calls[key] = call
		a.mu.Unlock()

		principal, err := a.authenticateUncached(ctx, username, password)

		a.mu.Lock()
		if err == nil && a.ttl > 0 && a.maxSize > 0 && call.version == a.version {
			a.pruneLocked(time.Now())
			if len(a.cache) >= a.maxSize {
				a.evictEarliestLocked()
			}
			a.cache[key] = authCacheEntry{principal: principal, expires: time.Now().Add(a.ttl)}
		}
		call.principal, call.err = principal, err
		if a.calls[key] == call {
			delete(a.calls, key)
		}
		close(call.done)
		a.mu.Unlock()
		return principal, err
	}
}

func (a *Authenticator) authenticateUncached(ctx context.Context, username, password string) (Principal, error) {
	u, err := a.store.getAuthUser(ctx, username)
	if err != nil {
		if errors.Is(err, errUnauthenticated) {
			return Principal{}, errUnauthenticated
		}
		return Principal{}, err
	}
	if !u.Active || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return Principal{}, errUnauthenticated
	}
	return u.Principal, nil
}

func (a *Authenticator) cacheKey(username, password string) string {
	mac := hmac.New(sha256.New, a.key)
	mac.Write([]byte(username))
	mac.Write([]byte{0})
	mac.Write([]byte(password))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *Authenticator) InvalidateUser(userID int64) {
	a.mu.Lock()
	a.version++
	for key, entry := range a.cache {
		if entry.principal.ID == userID {
			delete(a.cache, key)
		}
	}
	a.mu.Unlock()
}

func (a *Authenticator) pruneLocked(now time.Time) {
	for key, entry := range a.cache {
		if !now.Before(entry.expires) {
			delete(a.cache, key)
		}
	}
}

func (a *Authenticator) evictEarliestLocked() {
	var oldestKey string
	var oldest time.Time
	first := true
	for key, entry := range a.cache {
		if first || entry.expires.Before(oldest) {
			oldestKey, oldest, first = key, entry.expires, false
		}
	}
	if !first {
		delete(a.cache, oldestKey)
	}
}

func (a *Authenticator) Middleware(logger interface{ Error(string, ...any) }, requireAdmin bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			writeAuthRequired(w)
			return
		}
		principal, err := a.Authenticate(r.Context(), username, password)
		if err != nil {
			if errors.Is(err, errUnauthenticated) {
				writeAuthRequired(w)
				return
			}
			logger.Error("authentication lookup failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if requireAdmin && principal.Role != "admin" {
			writeError(w, http.StatusForbidden, "administrator access required")
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeAuthRequired(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="IPTV API", charset="UTF-8"`)
	writeError(w, http.StatusUnauthorized, "authentication required")
}
