//go:build cgo

package embeddeddolt

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/steveyegge/beads/internal/storage"
)

var _ storage.ReadSessioner = (*EmbeddedDoltStore)(nil)

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
}

func (s *EmbeddedDoltStore) sessionFrom(ctx context.Context) *readSession {
	rs, _ := ctx.Value(readSessionKey{owner: s}).(*readSession)
	return rs
}

// WithReadSession implements storage.ReadSessioner: it opens the engine once and
// runs fn with a context that makes every read of this store reuse it. See
// storage.ReadSessioner for the contract.
//
// The pool is pinned to one connection. database/sql then serializes the
// transactions of any goroutines sharing the session, and the USE/SET OpenSQL
// ran stays on the only connection there is (the invariant version_control.go
// documents for withPinnedDBConn).
func (s *EmbeddedDoltStore) WithReadSession(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if s.closed.Load() {
		return errClosed
	}
	if s.sessionFrom(ctx) != nil {
		return fn(ctx)
	}

	db, cleanup, err := s.openEngine(ctx)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s.sessionsOpen.Add(1)
	defer func() {
		s.sessionsOpen.Add(-1)
		// Joined only when the close failed: errors.Join would wrap even a
		// lone fn error, and callers compare what fn returned.
		if cerr := cleanup(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()
	return fn(context.WithValue(ctx, readSessionKey{owner: s}, &readSession{db: db}))
}

// openEngine opens the engine for one store call. While a read session of this
// store is open in the process, a call that does not carry it would wait on the
// lock the session holds: for a legitimate concurrent goroutine that wait is
// short (a session lasts well under a second), but a call that lost its context
// would sit there for good, so the wait is bounded and the error names the cause.
func (s *EmbeddedDoltStore) openEngine(ctx context.Context) (*sql.DB, func() error, error) {
	if s.sessionsOpen.Load() == 0 {
		return openSQL(ctx, s.dataDir, s.database, s.branch)
	}
	bctx, cancel := context.WithTimeout(ctx, sessionBypassWait)
	db, cleanup, err := openSQL(bctx, s.dataDir, s.database, s.branch)
	if err != nil {
		cancel()
		if ctx.Err() == nil && errors.Is(bctx.Err(), context.DeadlineExceeded) {
			return nil, nil, errSessionBypass
		}
		return nil, nil, err
	}
	// The engine may keep the context it was opened with, so it stays alive
	// until the caller is done with the engine.
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
