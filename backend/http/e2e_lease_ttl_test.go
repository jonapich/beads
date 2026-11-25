//go:build cgo

package bdhttp_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/httpapi"
	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
	"github.com/steveyegge/beads/internal/storage/issueops"
)

// startLeaseTTLServer is startE2EServerWithStore with the one switch `bd serve`
// turns on that the shared harness leaves off: the leases the server grants
// follow the workspace's lease.ttl setting.
func startLeaseTTLServer(t *testing.T) (string, *embeddeddolt.EmbeddedDoltStore) {
	t.Helper()
	ctx := context.Background()
	reference, err := embeddeddolt.Open(ctx, t.TempDir(), e2eDatabase, "main")
	if err != nil {
		t.Fatalf("open the reference store: %v", err)
	}
	t.Cleanup(func() { _ = reference.Close() })
	if err := reference.SetConfig(ctx, "issue_prefix", "e2e"); err != nil {
		t.Fatalf("set the issue prefix: %v", err)
	}

	cfg := e2eServeConfig(t, reference, e2eProjectID, e2eDatabase)
	cfg.LeaseTTLFromSettings = true
	cfg.Stdout, cfg.Stderr = io.Discard, io.Discard
	srv, err := httpapi.Listen(cfg)
	if err != nil {
		t.Fatalf("bind the in-process server: %v", err)
	}
	serveCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(serveCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("the in-process server did not shut down")
		}
	})
	return srv.Addr(), reference
}

// TestE2E_ServedLeasesFollowLeaseTTL runs the real `bd` binary against a server
// whose claims and heartbeats are executed server-side, the leg whose context
// the client's lease.ttl never reached: the claim, the heartbeat that renews
// it, and a heartbeat after the setting changed (no server restart in between)
// each grant the configured length, read off the stored lease row.
func TestE2E_ServedLeasesFollowLeaseTTL(t *testing.T) {
	skipUnlessEmbeddedDolt(t)
	bin := buildBD(t)
	addr, reference := startLeaseTTLServer(t)
	f := newHTTPFixture(t, bin, addr)

	// leaseSeconds is the stored lease's length: expiry minus the moment it was
	// granted or last refreshed, so it does not depend on the test's clock.
	leaseSeconds := func(id string) int64 {
		t.Helper()
		var seconds int64
		if err := reference.RunEventsMaintenanceTx(context.Background(), func(ctx context.Context, tx issueops.DBTX) error {
			return tx.QueryRowContext(ctx,
				"SELECT TIMESTAMPDIFF(SECOND, heartbeat_at, lease_expires_at) FROM leases WHERE issue_id = ?", id,
			).Scan(&seconds)
		}); err != nil {
			t.Fatalf("read the lease length of %s: %v", id, err)
		}
		return seconds
	}
	want := func(what string, got int64, d time.Duration) {
		t.Helper()
		if diff := got - int64(d/time.Second); diff < -5 || diff > 5 {
			t.Errorf("%s: the stored lease is %ds long, want %v (the default is %v)", what, got, d, issueops.DefaultLeaseTTL)
		}
	}

	f.mustRun(nil, "config", "set", "lease.ttl", "90m")
	id := f.create("served lease target")
	f.mustRun(nil, "update", id, "--claim", "--actor", "worker-7")
	want("after bd update --claim", leaseSeconds(id), 90*time.Minute)

	f.mustRun(nil, "heartbeat", id, "--actor", "worker-7")
	want("after bd heartbeat", leaseSeconds(id), 90*time.Minute)

	// Changed while the server runs: the next heartbeat is the new length.
	f.mustRun(nil, "config", "set", "lease.ttl", "2h")
	f.mustRun(nil, "heartbeat", id, "--actor", "worker-7")
	want("after lease.ttl changed to 2h", leaseSeconds(id), 2*time.Hour)

	f.mustRun(nil, "config", "unset", "lease.ttl")
	f.mustRun(nil, "heartbeat", id, "--actor", "worker-7")
	want("after lease.ttl was unset", leaseSeconds(id), issueops.DefaultLeaseTTL)
}
