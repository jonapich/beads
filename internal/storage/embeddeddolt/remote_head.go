//go:build cgo

package embeddeddolt

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/storage/versioncontrolops"
)

// RemoteHasHead reports whether remote already has this store's HEAD, judged
// from the local remote-tracking ref and without contacting the remote. An
// empty remote means the default remote. Pending changes are committed first,
// as Push does, so an uncommitted write is never mistaken for nothing to send.
func (s *EmbeddedDoltStore) RemoteHasHead(ctx context.Context, remote string) (bool, error) {
	if remote == "" {
		remote = defaultRemote
	}
	if _, err := s.CommitPending(ctx, "beads"); err != nil {
		return false, fmt.Errorf("commit pending before push check: %w", err)
	}
	var current bool
	err := s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		var err error
		current, err = versioncontrolops.RemoteTrackingAtHead(ctx, db, remote, s.branch)
		return err
	})
	return current, err
}
