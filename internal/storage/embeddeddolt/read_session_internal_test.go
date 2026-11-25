//go:build cgo

package embeddeddolt

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/versioncontrolops"
)

// engineCounter stands in for openSQL: it hands out a sqlmock database per
// "engine open" and records opens and closes per data directory, so a test can
// assert how many engines a call path opened.
type engineCounter struct {
	mu     sync.Mutex
	opens  map[string]int
	closes map[string]int
	// block, when set, makes an open wait for its context instead of returning,
	// the way an open does while another handle holds the engine's lock.
	block bool
}

func newEngineCounter(t *testing.T) *engineCounter {
	t.Helper()
	c := &engineCounter{opens: map[string]int{}, closes: map[string]int{}}
	orig := openSQL
	openSQL = func(ctx context.Context, dir, _, _ string) (*sql.DB, func() error, error) {
		if c.block {
			<-ctx.Done()
			return nil, nil, ctx.Err()
		}
		db, mock, err := sqlmock.New()
		if err != nil {
			return nil, nil, err
		}
		// Loose matching: a test makes a handful of reads on one handle and the
		// expectations only have to cover them.
		mock.MatchExpectationsInOrder(false)
		for i := 0; i < 8; i++ {
			mock.ExpectBegin()
			mock.ExpectRollback()
		}
		mock.ExpectClose()
		c.mu.Lock()
		c.opens[dir]++
		c.mu.Unlock()
		return db, func() error {
			c.mu.Lock()
			c.closes[dir]++
			c.mu.Unlock()
			return db.Close()
		}, nil
	}
	t.Cleanup(func() { openSQL = orig })
	return c
}

func (c *engineCounter) opened(dir string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.opens[dir]
}

func (c *engineCounter) closed(dir string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closes[dir]
}

func newSessionTestStore(t *testing.T) *EmbeddedDoltStore {
	t.Helper()
	return &EmbeddedDoltStore{dataDir: t.TempDir(), database: "sess", branch: "main"}
}

func readOnce(ctx context.Context, s *EmbeddedDoltStore) error {
	return s.withConn(ctx, false, func(tx *sql.Tx) error { return nil })
}

func TestReadSessionSharesOneEngineOpen(t *testing.T) {
	c := newEngineCounter(t)
	s := newSessionTestStore(t)

	err := s.WithReadSession(t.Context(), func(ctx context.Context) error {
		for i := 0; i < 5; i++ {
			if err := readOnce(ctx, s); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithReadSession: %v", err)
	}
	if got := c.opened(s.dataDir); got != 1 {
		t.Fatalf("5 reads in one session opened %d engines, want 1", got)
	}
	if got := c.closed(s.dataDir); got != 1 {
		t.Fatalf("session closed %d engines, want 1", got)
	}
	if s.sessionsOpen.Load() != 0 {
		t.Fatalf("sessionsOpen = %d after the session, want 0", s.sessionsOpen.Load())
	}

	// Outside a session each read opens its own engine, as before.
	if err := readOnce(t.Context(), s); err != nil {
		t.Fatalf("read outside a session: %v", err)
	}
	if got := c.opened(s.dataDir); got != 2 {
		t.Fatalf("a read outside a session made %d total opens, want 2", got)
	}
}

func TestReadSessionNestsOnOneEngine(t *testing.T) {
	c := newEngineCounter(t)
	s := newSessionTestStore(t)

	err := s.WithReadSession(t.Context(), func(ctx context.Context) error {
		return s.WithReadSession(ctx, func(ctx context.Context) error {
			if err := readOnce(ctx, s); err != nil {
				return err
			}
			return readOnce(ctx, s)
		})
	})
	if err != nil {
		t.Fatalf("nested WithReadSession: %v", err)
	}
	if got := c.opened(s.dataDir); got != 1 {
		t.Fatalf("nested sessions opened %d engines, want 1", got)
	}
}

func TestReadSessionReturnsTheCallbackErrorUnwrapped(t *testing.T) {
	newEngineCounter(t)
	s := newSessionTestStore(t)
	sentinel := errors.New("callback failed")

	err := s.WithReadSession(t.Context(), func(context.Context) error { return sentinel })
	if err != sentinel { //nolint:errorlint // the point is that nothing wrapped it
		t.Fatalf("WithReadSession error = %#v, want the callback's own error", err)
	}
}

func TestReadSessionRefusesWrites(t *testing.T) {
	c := newEngineCounter(t)
	s := newSessionTestStore(t)

	err := s.WithReadSession(t.Context(), func(ctx context.Context) error {
		return s.withConn(ctx, true, func(tx *sql.Tx) error {
			t.Error("write callback ran inside a read session")
			return nil
		})
	})
	if !errors.Is(err, errWriteInReadSession) {
		t.Fatalf("write in a read session error = %v, want errWriteInReadSession", err)
	}
	if got := c.opened(s.dataDir); got != 1 {
		t.Fatalf("opened %d engines, want only the session's", got)
	}
}

func TestReadSessionRefusesVersionControlMutations(t *testing.T) {
	newEngineCounter(t)
	s := newSessionTestStore(t)
	noop := func(db versioncontrolops.DBConn) error { return nil }

	err := s.WithReadSession(t.Context(), func(ctx context.Context) error {
		if err := s.withMutatingDBConn(ctx, noop); !errors.Is(err, errVCInReadSession) {
			t.Errorf("withMutatingDBConn in a session = %v, want errVCInReadSession", err)
		}
		if err := s.withMutatingPinnedDBConn(ctx, noop); !errors.Is(err, errVCInReadSession) {
			t.Errorf("withMutatingPinnedDBConn in a session = %v, want errVCInReadSession", err)
		}
		if err := s.withPinnedDBConn(ctx, noop); !errors.Is(err, errVCInReadSession) {
			t.Errorf("withPinnedDBConn in a session = %v, want errVCInReadSession", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithReadSession: %v", err)
	}
}

func TestReadSessionServesReadOnlyDBConnOnTheSharedEngine(t *testing.T) {
	c := newEngineCounter(t)
	s := newSessionTestStore(t)

	err := s.WithReadSession(t.Context(), func(ctx context.Context) error {
		return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
			if db == nil {
				t.Error("withDBConn in a session passed a nil handle")
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("WithReadSession: %v", err)
	}
	if got := c.opened(s.dataDir); got != 1 {
		t.Fatalf("session plus a withDBConn read opened %d engines, want 1", got)
	}
}

func TestReadSessionIsPrivateToItsStore(t *testing.T) {
	c := newEngineCounter(t)
	a := newSessionTestStore(t)
	b := newSessionTestStore(t)

	err := a.WithReadSession(t.Context(), func(ctx context.Context) error {
		if err := readOnce(ctx, a); err != nil {
			return err
		}
		// The same context reaches store B, which must open its own engine
		// instead of borrowing A's connection.
		return readOnce(ctx, b)
	})
	if err != nil {
		t.Fatalf("WithReadSession: %v", err)
	}
	if got := c.opened(a.dataDir); got != 1 {
		t.Fatalf("store A opened %d engines, want 1", got)
	}
	if got := c.opened(b.dataDir); got != 1 {
		t.Fatalf("store B opened %d engines, want its own 1", got)
	}
}

func TestReadSessionBypassFailsLoudlyInsteadOfHanging(t *testing.T) {
	c := newEngineCounter(t)
	s := newSessionTestStore(t)

	orig := sessionBypassWait
	sessionBypassWait = 200 * time.Millisecond
	t.Cleanup(func() { sessionBypassWait = orig })

	err := s.WithReadSession(t.Context(), func(ctx context.Context) error {
		// From here an open can only wait on the session's lock, as a real
		// second engine would.
		c.mu.Lock()
		c.block = true
		c.mu.Unlock()
		defer func() {
			c.mu.Lock()
			c.block = false
			c.mu.Unlock()
		}()

		start := time.Now()
		// A call that lost the session's context.
		err := readOnce(context.Background(), s)
		if !errors.Is(err, errSessionBypass) {
			t.Errorf("bypass error = %v, want errSessionBypass", err)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("bypass took %v, want about sessionBypassWait", elapsed)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithReadSession: %v", err)
	}
}

func TestReadSessionOnClosedStoreFails(t *testing.T) {
	newEngineCounter(t)
	s := newSessionTestStore(t)
	s.closed.Store(true)

	err := s.WithReadSession(t.Context(), func(context.Context) error {
		t.Error("callback ran on a closed store")
		return nil
	})
	if !errors.Is(err, errClosed) {
		t.Fatalf("WithReadSession on a closed store = %v, want errClosed", err)
	}
}

func TestStorageWithReadSessionUsesTheStoreSession(t *testing.T) {
	c := newEngineCounter(t)
	s := newSessionTestStore(t)

	err := storage.WithReadSession(t.Context(), s, func(ctx context.Context) error {
		if err := readOnce(ctx, s); err != nil {
			return err
		}
		return readOnce(ctx, s)
	})
	if err != nil {
		t.Fatalf("storage.WithReadSession: %v", err)
	}
	if got := c.opened(s.dataDir); got != 1 {
		t.Fatalf("storage.WithReadSession opened %d engines for two reads, want 1", got)
	}
}

func TestLockWaitBackOffIsShortAndUnbounded(t *testing.T) {
	bo := newLockWaitBackOff()
	if bo.InitialInterval != 50*time.Millisecond {
		t.Errorf("InitialInterval = %v, want 50ms", bo.InitialInterval)
	}
	if bo.MaxInterval != time.Second {
		t.Errorf("MaxInterval = %v, want 1s", bo.MaxInterval)
	}
	if bo.MaxElapsedTime != 0 {
		t.Errorf("MaxElapsedTime = %v, want 0 (wait until the context ends)", bo.MaxElapsedTime)
	}
	// First retry: 50ms +-50%.
	if first := bo.NextBackOff(); first < 25*time.Millisecond || first > 75*time.Millisecond {
		t.Errorf("first retry delay = %v, want within 25ms..75ms", first)
	}
	// It grows but never past the cap (plus its jitter).
	var last time.Duration
	for i := 0; i < 40; i++ {
		last = bo.NextBackOff()
	}
	if last > 1500*time.Millisecond {
		t.Errorf("retry delay after 40 attempts = %v, want capped near 1s", last)
	}
}
