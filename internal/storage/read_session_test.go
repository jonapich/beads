package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
)

type sessionKey struct{}

// beginnerStore is a store that supports sessions, with the outcome of each
// begin controlled by the test.
type beginnerStore struct {
	storage.DoltStorage
	beginErr error
	begun    int
	ended    int
}

func (b *beginnerStore) BeginReadSession(ctx context.Context) (context.Context, func() error, error) {
	if b.beginErr != nil {
		return ctx, nil, b.beginErr
	}
	b.begun++
	return context.WithValue(ctx, sessionKey{}, true), func() error { b.ended++; return nil }, nil
}

// decorated hides a beginner behind Unwrap, as the hook, telemetry and
// external-dependency decorators do.
type decorated struct {
	storage.DoltStorage
	inner storage.DoltStorage
}

func (d decorated) Unwrap() storage.DoltStorage { return d.inner }

func TestBeginReadSessionReturnsTheSessionContextAndEnds(t *testing.T) {
	b := &beginnerStore{}
	sctx, end := storage.BeginReadSession(t.Context(), decorated{inner: b})
	if sctx.Value(sessionKey{}) != true {
		t.Fatal("BeginReadSession did not return the session's context (the decorator's Unwrap was not followed)")
	}
	if b.begun != 1 || b.ended != 0 {
		t.Fatalf("begun=%d ended=%d after begin, want 1 and 0", b.begun, b.ended)
	}
	end()
	if b.ended != 1 {
		t.Fatalf("ended=%d after end, want 1", b.ended)
	}
}

func TestBeginReadSessionFallsBackWhenTheStoreHasNoSessions(t *testing.T) {
	ctx := t.Context()
	sctx, end := storage.BeginReadSession(ctx, &stubStore{})
	if sctx != ctx {
		t.Fatal("a store without sessions changed the context")
	}
	end()
	end() // safe, and a no-op

	sctx, end = storage.BeginReadSession(ctx, nil)
	if sctx != ctx {
		t.Fatal("a nil store changed the context")
	}
	end()
}

func TestBeginReadSessionFallsBackWhenTheEngineCannotBeOpened(t *testing.T) {
	ctx := t.Context()
	b := &beginnerStore{beginErr: errors.New("engine open failed")}
	sctx, end := storage.BeginReadSession(ctx, b)
	if sctx != ctx {
		t.Fatal("a failed begin changed the context; the reads that follow must open their own engines and report the error")
	}
	end()
	if b.ended != 0 {
		t.Fatalf("a failed begin was ended %d times", b.ended)
	}
}

type stubStore struct{ storage.DoltStorage }
