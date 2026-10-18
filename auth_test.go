package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type fakeAuthStore struct {
	user    authUser
	calls   atomic.Int32
	start   chan struct{}
	release chan struct{}
}

func (s *fakeAuthStore) getAuthUser(ctx context.Context, username string) (authUser, error) {
	s.calls.Add(1)
	if s.start != nil {
		select {
		case s.start <- struct{}{}:
		default:
		}
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return authUser{}, ctx.Err()
		}
	}
	if username != s.user.Username {
		return authUser{}, errUnauthenticated
	}
	return s.user, nil
}

func newFakeAuthStore(t *testing.T) *fakeAuthStore {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("viewer-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeAuthStore{user: authUser{
		Principal:    Principal{ID: 42, Username: "viewer", Role: "user"},
		PasswordHash: string(hash),
		Active:       true,
	}}
}

func TestAuthenticatorSingleflightsConcurrentRequests(t *testing.T) {
	store := newFakeAuthStore(t)
	store.start = make(chan struct{}, 1)
	store.release = make(chan struct{})
	auth, err := NewAuthenticator(store, time.Minute, 100)
	if err != nil {
		t.Fatal(err)
	}

	const workers = 20
	var wg sync.WaitGroup
	wg.Add(workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			p, err := auth.Authenticate(context.Background(), "viewer", "viewer-password")
			if err == nil && (p.ID != 42 || p.Role != "user") {
				err = errInvalid
			}
			errs <- err
		}()
	}
	<-store.start
	time.Sleep(20 * time.Millisecond)
	close(store.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Authenticate() error: %v", err)
		}
	}
	if got := store.calls.Load(); got != 1 {
		t.Fatalf("database lookup count = %d, want 1", got)
	}
}

func TestAuthenticatorCachesOnlySuccessfulCredentials(t *testing.T) {
	store := newFakeAuthStore(t)
	auth, err := NewAuthenticator(store, time.Minute, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(context.Background(), "viewer", "wrong-password"); err == nil {
		t.Fatal("wrong password unexpectedly authenticated")
	}
	if _, err := auth.Authenticate(context.Background(), "viewer", "viewer-password"); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
	if _, err := auth.Authenticate(context.Background(), "viewer", "viewer-password"); err != nil {
		t.Fatalf("cached valid password rejected: %v", err)
	}
	if got := store.calls.Load(); got != 2 {
		t.Fatalf("database lookup count = %d, want 2 (one failed, one successful)", got)
	}
}
