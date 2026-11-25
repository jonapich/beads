package storage

import "context"

// ReadSessioner is implemented by stores whose per-call connection is
// expensive. The embedded Dolt store opens a whole engine for every call, and on
// Windows each open can stall for tens of milliseconds on the journal the
// previous open rewrote; the likely cause is the antivirus rescanning it (not
// confirmed), so a path that makes eight reads pays that eight times.
//
// WithReadSession runs fn with a context that carries one shared connection:
// every read the store serves under that context reuses it instead of opening
// its own. The session is for reads only. A write, or a version-control
// operation that changes session state, fails under it rather than quietly
// holding the engine's lock. Sessions nest: a call that already carries one
// runs fn in it.
//
// A session holds the store's lock for as long as fn runs, so it belongs on a
// bounded read path, never around a prompt, an editor or a server loop.
type ReadSessioner interface {
	WithReadSession(ctx context.Context, fn func(ctx context.Context) error) error
}

// ReadSessionBeginner is ReadSessioner for a caller whose reads are not one
// closure, such as a command that resolves an id, branches, and reads again.
// BeginReadSession opens the session and returns the context that carries it
// and a function that ends it. end is safe to call more than once; calls after
// the first do nothing. A context kept past end falls back to one engine open
// per call, as if no session had been begun.
type ReadSessionBeginner interface {
	BeginReadSession(ctx context.Context) (sessionCtx context.Context, end func() error, err error)
}

// WithReadSession runs fn in a read session when st supports one, and as
// fn(ctx) when it does not (a server store has no per-call open to share).
// fn MUST pass the context it is given to every store call it wants served by
// the session.
func WithReadSession(ctx context.Context, st DoltStorage, fn func(ctx context.Context) error) error {
	if st != nil {
		if rs, ok := UnwrapStore(st).(ReadSessioner); ok {
			return rs.WithReadSession(ctx, fn)
		}
	}
	return fn(ctx)
}

// BeginReadSession starts a read session on st for a caller that cannot wrap its
// reads in one closure, and returns the context to read with and the function
// that ends the session. The caller MUST call end on every path out (it may be
// called more than once), MUST pass the returned context to every store call
// made before end, and MUST end the session before anything long-lived such as
// a watch loop, a prompt or a write, because the session holds the store's
// lock.
//
// It never fails: a store without sessions, or one whose engine cannot be
// opened, yields ctx unchanged and a no-op end, and the reads that follow open
// their own engines and report any real error themselves.
func BeginReadSession(ctx context.Context, st DoltStorage) (context.Context, func()) {
	if st != nil {
		if rs, ok := UnwrapStore(st).(ReadSessionBeginner); ok {
			if sctx, end, err := rs.BeginReadSession(ctx); err == nil {
				return sctx, func() { _ = end() }
			}
		}
	}
	return ctx, func() {}
}
