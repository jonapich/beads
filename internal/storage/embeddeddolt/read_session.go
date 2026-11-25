//go:build cgo

package embeddeddolt

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/steveyegge/beads/internal/storage"
)

var (
	_ storage.ReadSessioner       = (*EmbeddedDoltStore)(nil)
	_ storage.ReadSessionBeginner = (*EmbeddedDoltStore)(nil)
)

// engineOpens counts the engines opened for store calls and read sessions.
var engineOpens atomic.Uint64

// EngineOpens reports how many embedded engines this process has tried to open
// to serve store calls and read sessions (a failed attempt counts). It does not
// count the open-time opens a store makes in Open and OpenReadOnly, which do not
// go through openEngine. It is a diagnostic: a test reads it before and after a
// command path to pin how many opens that path makes.
func EngineOpens() uint64 { return engineOpens.Load() }

// openSQL is OpenSQL behind a variable so tests can count engine opens.
var openSQL = OpenSQL

// sessionBypassWait bounds how long a store call that carries no session waits
// for an engine while a read session of the same store is open in this process.
// It is a variable so tests can shorten it.
var sessionBypassWait = 30 * time.Second

var (
	errWriteInReadSession = errors.New("embeddeddolt: write inside a read session")
	errVCInReadSession    = errors.New("embeddeddolt: version-control operation inside a read session")
	errSessionBypass      = errors.New("embeddeddolt: store call outside the open read session (lost ctx?)")
)

// readSessionKey keys a session on its owning store, so a second store reached
// with the same context never borrows a connection that is not its own.
type readSessionKey struct{ owner *EmbeddedDoltStore }

// readSession is one engine open shared by the reads made under its context.
type readSession struct {
	db *sql.DB
	// ended is set when the session's engine has been closed. A context that
	// outlives the session then no longer resolves to it (sessionFrom), so a late
	// call opens its own engine instead of using a closed handle.
	ended atomic.Bool
}

func (s *EmbeddedDoltStore) sessionFrom(ctx context.Context) *readSession {
	rs, _ := ctx.Value(readSessionKey{owner: s}).(*readSession)
	if rs == nil || rs.ended.Load() {
		return nil
	}
	return rs
}

// BeginReadSession implements storage.ReadSessionBeginner: it opens the engine
// once and returns a context that makes every read of this store reuse it, with
// the function that closes it. A context that already carries a session of this
// store is returned as it is with a no-op end: the outer session owns the
// engine. See storage.ReadSessioner for the contract.
//
// The pool is pinned to one connection. database/sql then serializes the
// transactions of any goroutines sharing the session, and the USE/SET OpenSQL
// ran stays on the only connection there is (the invariant version_control.go
// documents for withPinnedDBConn).
func (s *EmbeddedDoltStore) BeginReadSession(ctx context.Context) (context.Context, func() error, error) {
	if s.closed.Load() {
		return ctx, nil, errClosed
	}
	if s.sessionFrom(ctx) != nil {
		return ctx, func() error { return nil }, nil
	}

	db, cleanup, err := s.openEngine(ctx)
	if err != nil {
		return ctx, nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	rs := &readSession{db: db}
	s.sessionsOpen.Add(1)
	var once sync.Once
	end := func() (err error) {
		once.Do(func() {
			rs.ended.Store(true)
			s.sessionsOpen.Add(-1)
			err = cleanup()
		})
		return err
	}
	return context.WithValue(ctx, readSessionKey{owner: s}, rs), end, nil
}

// WithReadSession implements storage.ReadSessioner: BeginReadSession around fn.
func (s *EmbeddedDoltStore) WithReadSession(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	sctx, end, err := s.BeginReadSession(ctx)
	if err != nil {
		return err
	}
	defer func() {
		// Joined only when the close failed: errors.Join would wrap even a
		// lone fn error, and callers compare what fn returned.
		if cerr := end(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()
	return fn(sctx)
}

// openEngine opens the engine for one store call. While a read session of this
// store is open in the process, a call that does not carry it would wait on the
// lock the session holds: for a legitimate concurrent goroutine that wait is
// short (a session lasts well under a second), but a call that lost its context
// would sit there for good, so the wait is bounded and the error names the cause.
func (s *EmbeddedDoltStore) openEngine(ctx context.Context) (*sql.DB, func() error, error) {
	engineOpens.Add(1)
	if s.sessionsOpen.Load() == 0 {
		return openSQL(ctx, s.dataDir, s.database, s.branch)
	}
	// The time limit applies to the WAIT for the engine, not to the engine. A
	// context with a deadline would stay attached to what it opened, and the
	// engine may keep the context it was opened with, so a call that got its
	// engine after a short wait and then ran past the limit (a throttled
	// machine, a big read) would have it cancelled under it. So the engine gets a
	// plain cancelable context, and a timer cancels it only if the open has not
	// finished by the deadline. Whoever settles first (the open or the timer)
	// decides, so a timer firing just after a successful open does nothing.
	bctx, cancel := context.WithCancel(ctx)
	var settled atomic.Bool
	timer := time.AfterFunc(sessionBypassWait, func() {
		if settled.CompareAndSwap(false, true) {
			cancel()
		}
	})
	db, cleanup, err := openSQL(bctx, s.dataDir, s.database, s.branch)
	if !settled.CompareAndSwap(false, true) {
		// The wait ran out first: the context the open ran under is cancelled.
		// Whatever it returned is not an engine to keep.
		if err == nil {
			_ = cleanup()
		}
		cancel()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, errSessionBypass
	}
	timer.Stop()
	if err != nil {
		cancel()
		return nil, nil, err
	}
	// The context stays alive until the caller is done with the engine.
	return db, func() error {
		defer cancel()
		return cleanup()
	}, nil
}

// callDB returns the database handle for one store call: the session's when ctx
// carries one (with a no-op cleanup, the session owns the engine), else a fresh
// engine open.
func (s *EmbeddedDoltStore) callDB(ctx context.Context) (*sql.DB, func() error, error) {
	if rs := s.sessionFrom(ctx); rs != nil {
		return rs.db, func() error { return nil }, nil
	}
	return s.openEngine(ctx)
}
