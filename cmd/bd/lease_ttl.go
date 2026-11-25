package main

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/steveyegge/beads/internal/storage"
	storeops "github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/workapi"
)

// configuredLeaseTTL resolves the workspace's claim lease length: the
// lease.ttl setting (a Go duration, set with `bd config set lease.ttl 60m`),
// or storeops.DefaultLeaseTTL (60m) when it is unset. The read goes through the
// settings role of whichever route this invocation is on, so the direct,
// proxied-server and HTTP-backend routes all see the same value.
//
// A stored value that does not parse, or is not greater than zero, is an error
// naming the key rather than a silent fall back to the default: a lease that is
// shorter than the operator asked for is how a live claim gets reclaimed.
func configuredLeaseTTL(ctx context.Context) (time.Duration, error) {
	settings, err := openWorkspaceConfig("lease TTL lookup requires direct database access")
	if err != nil {
		return 0, err
	}
	return workapi.ResolveLeaseTTL(ctx, settings, storeops.DefaultLeaseTTL)
}

// commandGrantsLease reports whether this invocation grants or renews a claim
// lease: bd heartbeat, bd update --claim, bd ready --claim, and bd close
// --continue when it claims the next step (no --no-auto). Every other command
// never touches a lease, so it never pays for the lease.ttl read.
func commandGrantsLease(cmd *cobra.Command) bool {
	flagOn := func(name string) bool {
		on, _ := cmd.Flags().GetBool(name)
		return on
	}
	switch cmd.Name() {
	case "heartbeat":
		return true
	case "update", "ready":
		return flagOn("claim")
	case "close":
		return flagOn("continue") && !flagOn("no-auto")
	}
	return false
}

// applyLeaseTTLToRootContext is the root pre-run's hook: for a command that
// grants or renews a lease it puts lease.ttl on the root context, so every
// claim and heartbeat the command makes, on any route, is for the configured
// length. It is done once here rather than at each claim site: the roles those
// sites call own their transactions, and a claim must not read settings from
// inside one. Any other command is left alone.
//
// It must run after syncCommandContext: the settings read finds the store
// through the CommandContext, and before that sync it would find none and
// open a second, undecorated store that the command then writes through.
//
// A store whose claims and heartbeats run in another process (the HTTP backend)
// is skipped: bd serve grants those leases itself, from the workspace's lease.ttl,
// and never sees this context, so the read would cost a request for nothing.
func applyLeaseTTLToRootContext(cmd *cobra.Command) error {
	if !commandGrantsLease(cmd) || storage.GrantsLeasesRemotely(getStore()) {
		return nil
	}
	ctx, err := leaseTTLContext(getRootContext())
	if err != nil {
		return err
	}
	setRootContext(ctx, rootCancel)
	return nil
}

// leaseTTLContextForStore is leaseTTLContext for a store other than the one the
// command opened: an id routed to another workspace is leased by THAT
// workspace's lease.ttl, not the launching one's, which is what the root
// pre-run put on the context.
func leaseTTLContextForStore(ctx context.Context, st storage.DoltStorage) (context.Context, error) {
	if storage.GrantsLeasesRemotely(st) {
		return ctx, nil // the server it dials grants the lease
	}
	settings, err := st.WorkspaceConfig()
	if err != nil {
		return nil, err
	}
	ttl, err := workapi.ResolveLeaseTTL(ctx, settings, storeops.DefaultLeaseTTL)
	if err != nil {
		return nil, err
	}
	return storeops.WithLeaseTTL(ctx, ttl), nil
}

// leaseTTLContext returns ctx carrying the configured lease length
// (issueops.WithLeaseTTL), which is what every lease grant and renewal in the
// storage layer reads. Call it on the context of a claim or a heartbeat.
func leaseTTLContext(ctx context.Context) (context.Context, error) {
	ttl, err := configuredLeaseTTL(ctx)
	if err != nil {
		return nil, err
	}
	return storeops.WithLeaseTTL(ctx, ttl), nil
}
