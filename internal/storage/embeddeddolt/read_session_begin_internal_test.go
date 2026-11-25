//go:build cgo

package embeddeddolt

import (
	"context"
	"testing"
)

func TestBeginReadSessionSharesOneEngineUntilEnded(t *testing.T) {
	c := newEngineCounter(t)
	s := newSessionTestStore(t)

	sctx, end, err := s.BeginReadSession(t.Context())
	if err != nil {
		t.Fatalf("BeginReadSession: %v", err)
	}
	for i := 0; i < 4; i++ {
		if err := readOnce(sctx, s); err != nil {
			t.Fatalf("read %d in the session: %v", i, err)
		}
	}
	if got := c.opened(s.dataDir); got != 1 {
		t.Fatalf("4 reads in a begun session opened %d engines, want 1", got)
	}
	if s.sessionsOpen.Load() != 1 {
		t.Fatalf("sessionsOpen = %d while the session is open, want 1", s.sessionsOpen.Load())
	}

	if err := end(); err != nil {
		t.Fatalf("end: %v", err)
	}
	if got := c.closed(s.dataDir); got != 1 {
		t.Fatalf("ending the session closed %d engines, want 1", got)
	}
	if s.sessionsOpen.Load() != 0 {
		t.Fatalf("sessionsOpen = %d after end, want 0", s.sessionsOpen.Load())
	}

	// end is idempotent: a second call closes nothing and reports nothing.
	if err := end(); err != nil {
		t.Fatalf("second end: %v", err)
	}
	if got := c.closed(s.dataDir); got != 1 {
		t.Fatalf("a second end closed %d engines in total, want still 1", got)
	}
	if s.sessionsOpen.Load() != 0 {
		t.Fatalf("sessionsOpen = %d after a second end, want 0", s.sessionsOpen.Load())
	}
}

func TestBeginReadSessionContextAfterEndOpensItsOwnEngine(t *testing.T) {
	c := newEngineCounter(t)
	s := newSessionTestStore(t)

	sctx, end, err := s.BeginReadSession(t.Context())
	if err != nil {
		t.Fatalf("BeginReadSession: %v", err)
	}
	if err := readOnce(sctx, s); err != nil {
		t.Fatalf("read in the session: %v", err)
	}
	if err := end(); err != nil {
		t.Fatalf("end: %v", err)
	}

	// The context outlived the session. It must not resolve to the closed
	// engine: the read opens a fresh one, and writes are no longer refused.
	if err := readOnce(sctx, s); err != nil {
		t.Fatalf("read with the context of an ended session: %v", err)
	}
	if got := c.opened(s.dataDir); got != 2 {
		t.Fatalf("session plus a late read opened %d engines, want 2", got)
	}
}

func TestBeginReadSessionNestsAndOnlyTheOwnerCloses(t *testing.T) {
	c := newEngineCounter(t)
	s := newSessionTestStore(t)

	outer, endOuter, err := s.BeginReadSession(t.Context())
	if err != nil {
		t.Fatalf("outer BeginReadSession: %v", err)
	}
	inner, endInner, err := s.BeginReadSession(outer)
	if err != nil {
		t.Fatalf("inner BeginReadSession: %v", err)
	}
	if got := c.opened(s.dataDir); got != 1 {
		t.Fatalf("a nested begin opened %d engines in total, want 1", got)
	}
	if err := endInner(); err != nil {
		t.Fatalf("inner end: %v", err)
	}
	// The inner end must not have closed the outer session's engine.
	if err := readOnce(inner, s); err != nil {
		t.Fatalf("read after the inner end: %v", err)
	}
	if got := c.opened(s.dataDir); got != 1 {
		t.Fatalf("the inner end made the next read open an engine (%d opens), want the outer session still serving", got)
	}
	if err := endOuter(); err != nil {
		t.Fatalf("outer end: %v", err)
	}
	if got := c.closed(s.dataDir); got != 1 {
		t.Fatalf("closed %d engines, want exactly the outer session's", got)
	}
}

func TestBeginReadSessionOnClosedStoreFails(t *testing.T) {
	newEngineCounter(t)
	s := newSessionTestStore(t)
	s.closed.Store(true)

	if _, _, err := s.BeginReadSession(context.Background()); err == nil {
		t.Fatal("BeginReadSession on a closed store succeeded")
	}
}

func TestEngineOpensCountsOpensForStoreCalls(t *testing.T) {
	newEngineCounter(t)
	s := newSessionTestStore(t)

	before := EngineOpens()
	if err := readOnce(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	if err := readOnce(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	if got := EngineOpens() - before; got != 2 {
		t.Fatalf("two unsessioned reads counted %d opens, want 2", got)
	}

	before = EngineOpens()
	if err := s.WithReadSession(t.Context(), func(ctx context.Context) error {
		for i := 0; i < 3; i++ {
			if err := readOnce(ctx, s); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := EngineOpens() - before; got != 1 {
		t.Fatalf("three reads in a session counted %d opens, want 1", got)
	}
}
