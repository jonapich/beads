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
