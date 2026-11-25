package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	storeops "github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/workapi"
	"github.com/steveyegge/beads/issueops"
)

// leaseContext is the context a claim, a ready-claim, a claiming update or a
// heartbeat is made with: the request's own, carrying the workspace's lease.ttl.
//
// The client's context values do not cross the wire, so a lease granted here was
// always the built-in default no matter what `bd config set lease.ttl` said. The
// setting is read per request through the settings role of whichever source this
// server answers from, and BEFORE the role runs (never from inside its
// transaction), on the same resolver bd's own commands use
// (workapi.ResolveLeaseTTL). Reading it per request, rather than once at start,
// costs one settings read per grant and means a changed setting needs no
// restart; there is nothing to cache or invalidate.
//
// It is on only when Config.LeaseTTLFromSettings says so; off, it hands back the
// request's context as it was.
func (s *Server) leaseContext(r *http.Request) (context.Context, error) {
	ctx := r.Context()
	if !s.cfg.LeaseTTLFromSettings {
		return ctx, nil
	}
	settings, err := s.workspaceConfig(r)
	if err != nil {
		return nil, err
	}
	ttl, err := workapi.ResolveLeaseTTL(ctx, settings, storeops.DefaultLeaseTTL)
	if err != nil {
		return nil, err
	}
	return storeops.WithLeaseTTL(ctx, ttl), nil
}

// failLeaseTTL answers a request whose lease.ttl could not be resolved. A stored
// value that does not parse is the workspace's fault, not the caller's, so it
// is a 500 with the cause in the operator's log rather than a 400 that blames a
// request body that was fine. Anything else (the database being busy or down)
// keeps the classification it would have had.
func (s *Server) failLeaseTTL(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, issueops.ErrValidation) {
		err = fmt.Errorf("the workspace's lease.ttl setting cannot be honored: %v", err)
	}
	s.failErr(w, r, err)
}
