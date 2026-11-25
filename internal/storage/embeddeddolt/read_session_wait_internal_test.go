//go:build cgo

package embeddeddolt

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"
)

// openedContexts records the context each engine open was given, over the
// counted fake engines of newEngineCounter.
type openedContexts struct {
	mu   sync.Mutex
	ctxs []context.Context
}

func (o *openedContexts) last(t *testing.T) context.Context {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.ctxs) == 0 {
		t.Fatal("no engine was opened")
	}
	return o.ctxs[len(o.ctxs)-1]
}

func recordOpenContexts(t *testing.T) *openedContexts {
	t.Helper()
	rec := &openedContexts{}
	inner := openSQL
	openSQL = func(ctx context.Context, dir, database, branch string) (*sql.DB, func() error, error) {
		rec.mu.Lock()
		rec.ctxs = append(rec.ctxs, ctx)
		rec.mu.Unlock()
		return inner(ctx, dir, database, branch)
	}
	t.Cleanup(func() { openSQL = inner })
	return rec
}

// TestBypassWaitLimitsTheWaitNotTheEngine: while a read session is open, a call
// that carries no session opens its own engine under a time limit on the WAIT.
// The limit must not stay attached to the engine it opened: the engine may keep
// the context it was opened with, so a read that outlives the limit (a throttled
// laptop, a large read) would otherwise fail with a deadline error long after
// its open had succeeded.
func TestBypassWaitLimitsTheWaitNotTheEngine(t *testing.T) {
	newEngineCounter(t)
	opened := recordOpenContexts(t)
	s := newSessionTestStore(t)

	orig := sessionBypassWait
	sessionBypassWait = 50 * time.Millisecond
	t.Cleanup(func() { sessionBypassWait = orig })

	err := s.WithReadSession(t.Context(), func(context.Context) error {
		// A call with no session context while one is open: the guarded open. The
		// fake engine answers at once, as the real one does once the lock frees.
		db, cleanup, err := s.openEngine(context.Background())
		if err != nil {
			return err
		}
		defer func() { _ = cleanup() }()
		if db == nil {
			t.Error("openEngine returned no engine")
		}

		// The read that follows runs well past the wait limit.
		time.Sleep(5 * sessionBypassWait)

		ctx := opened.last(t)
		if err := ctx.Err(); err != nil {
			t.Errorf("the engine's context failed %v after the wait limit passed; the limit must end with the open", err)
		}
		if _, has := ctx.Deadline(); has {
			t.Error("the engine's context carries the wait deadline")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithReadSession: %v", err)
	}
}

// TestBypassWaitCancelsTheContextOnlyWhenTheOpenTimesOut: when the wait does run
// out, the open is cancelled and the caller gets the named error, not a bare
// context error; a caller that cancels its own context gets that error instead.
func TestBypassWaitCancelsTheContextOnlyWhenTheOpenTimesOut(t *testing.T) {
	c := newEngineCounter(t)
	s := newSessionTestStore(t)

	orig := sessionBypassWait
	sessionBypassWait = 50 * time.Millisecond
	t.Cleanup(func() { sessionBypassWait = orig })

	err := s.WithReadSession(t.Context(), func(context.Context) error {
		c.mu.Lock()
		c.block = true
		c.mu.Unlock()
		defer func() {
			c.mu.Lock()
			c.block = false
			c.mu.Unlock()
		}()

		if _, _, err := s.openEngine(context.Background()); err != errSessionBypass { //nolint:errorlint // sentinel identity is the point
			t.Errorf("a wait that ran out returned %v, want errSessionBypass", err)
		}

		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		if _, _, err := s.openEngine(canceled); err == nil || err == errSessionBypass { //nolint:errorlint
			t.Errorf("a caller that gave up got %v, want its own cancellation", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithReadSession: %v", err)
	}
}
