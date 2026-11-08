package versioncontrolops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// RemoteTrackingAtHead reports whether the cached remote-tracking ref for
// remote/branch already points at HEAD. Dolt moves that ref only after a push
// or fetch succeeds, so true means the remote had this commit the last time it
// was contacted. A missing ref (never pushed, or pruned by a history squash)
// reports false.
func RemoteTrackingAtHead(ctx context.Context, db DBConn, remote, branch string) (bool, error) {
	var tracking string
	err := db.QueryRowContext(ctx,
		"SELECT hash FROM dolt_remote_branches WHERE name = ?",
		"remotes/"+remote+"/"+branch,
	).Scan(&tracking)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read remote-tracking ref for %s/%s: %w", remote, branch, err)
	}

	var head string
	if err := db.QueryRowContext(ctx, "SELECT HASHOF('HEAD')").Scan(&head); err != nil {
		return false, fmt.Errorf("read HEAD: %w", err)
	}
	return tracking != "" && tracking == head, nil
}
