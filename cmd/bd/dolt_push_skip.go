package main

import (
	"context"

	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/storage"
)

const pushUpToDateMessage = "Nothing to push: the remote already has your latest commit."

// remoteHeadChecker is a store that can tell, without contacting the remote,
// whether the remote already has HEAD.
type remoteHeadChecker interface {
	RemoteHasHead(ctx context.Context, remote string) (bool, error)
}

// pushAlreadyCurrent reports whether a push can be skipped because the remote
// already has HEAD. Each push opens several connections to the remote, so
// skipping one that has nothing to send saves seconds. Any failure here reports
// false, which leaves the decision to a real push.
func pushAlreadyCurrent(ctx context.Context, st storage.DoltStorage, remote string) bool {
	checker, ok := storage.UnwrapStore(st).(remoteHeadChecker)
	if !ok {
		return false
	}
	current, err := checker.RemoteHasHead(ctx, remote)
	if err != nil {
		debug.Logf("dolt push: could not compare HEAD with the tracking ref: %v\n", err)
		return false
	}
	return current
}
