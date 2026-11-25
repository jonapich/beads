package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/steveyegge/beads/internal/storage"
	storeops "github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/issueops"
)

// ttlSettingsStore is a store whose settings role holds one lease.ttl value: the
// routed workspace an id was found in.
type ttlSettingsStore struct {
	storage.DoltStorage
	value string
	err   error
}

func (s ttlSettingsStore) WorkspaceConfig() (issueops.WorkspaceConfig, error) {
	if s.err != nil {
		return nil, s.err
	}
	return ttlSettingsRole{value: s.value}, nil
}

type ttlSettingsRole struct {
	issueops.WorkspaceConfig
	value string
}

func (r ttlSettingsRole) GetSetting(_ context.Context, req issueops.GetSettingRequest) (issueops.SettingResult, error) {
	return issueops.SettingResult{Key: req.Key, Value: r.value}, nil
}

// TestLeaseTTLContextForStoreReadsTheGivenWorkspacesSetting: a heartbeat on an
// id routed to another workspace leases for THAT workspace's lease.ttl. The root
// pre-run already put the launching workspace's length on the context; this
// replaces it with the routed store's.
func TestLeaseTTLContextForStoreReadsTheGivenWorkspacesSetting(t *testing.T) {
	launching := storeops.WithLeaseTTL(t.Context(), 5*time.Minute)

	got, err := leaseTTLContextForStore(launching, ttlSettingsStore{value: "3h"})
	if err != nil {
		t.Fatalf("leaseTTLContextForStore: %v", err)
	}
	if ttl := storeops.LeaseTTLFromContext(got); ttl != 3*time.Hour {
		t.Errorf("a routed heartbeat's lease = %v, want the routed workspace's 3h (not the launching 5m)", ttl)
	}

	got, err = leaseTTLContextForStore(launching, ttlSettingsStore{value: ""})
	if err != nil {
		t.Fatalf("leaseTTLContextForStore with no setting: %v", err)
	}
	if ttl := storeops.LeaseTTLFromContext(got); ttl != storeops.DefaultLeaseTTL {
		t.Errorf("a routed workspace without lease.ttl granted %v, want the %v default", ttl, storeops.DefaultLeaseTTL)
	}
}

func TestLeaseTTLContextForStoreRefusesAnUnusableSetting(t *testing.T) {
	if _, err := leaseTTLContextForStore(t.Context(), ttlSettingsStore{value: "soon"}); err == nil {
		t.Error("an unparsable lease.ttl was accepted")
	}
	want := errors.New("no settings role")
	if _, err := leaseTTLContextForStore(t.Context(), ttlSettingsStore{err: want}); !errors.Is(err, want) {
		t.Errorf("a store with no settings role: err = %v, want it passed through", err)
	}
}

// remoteLeaseSettingsStore is a store whose claims run in another process, and
// whose settings role fails the test if anything asks it for lease.ttl.
type remoteLeaseSettingsStore struct {
	storage.DoltStorage
	t *testing.T
}

func (remoteLeaseSettingsStore) GrantsLeasesRemotely() bool { return true }

func (s remoteLeaseSettingsStore) WorkspaceConfig() (issueops.WorkspaceConfig, error) {
	s.t.Error("lease.ttl was read through a store whose claims run in the server")
	return nil, errors.New("must not be read")
}

// TestLeaseTTLIsNotReadForAStoreWhoseServerGrantsLeases: over the HTTP backend
// the server grants the lease length (from the workspace's own lease.ttl) and
// never sees the client's context, so the root pre-run does not spend a request
// reading the setting, and the root context is left as it was.
func TestLeaseTTLIsNotReadForAStoreWhoseServerGrantsLeases(t *testing.T) {
	oldStore, oldCtx := store, rootCtx
	t.Cleanup(func() { store, rootCtx = oldStore, oldCtx })
	store, rootCtx = remoteLeaseSettingsStore{t: t}, t.Context()
	before := rootCtx

	if err := applyLeaseTTLToRootContext(&cobra.Command{Use: "heartbeat"}); err != nil {
		t.Fatalf("applyLeaseTTLToRootContext(heartbeat) = %v, want nil", err)
	}
	if rootCtx != before {
		t.Error("the root context was replaced for a store whose server grants the lease")
	}

	// The same for an id routed to such a store.
	got, err := leaseTTLContextForStore(t.Context(), remoteLeaseSettingsStore{t: t})
	if err != nil {
		t.Fatalf("leaseTTLContextForStore: %v", err)
	}
	if got != t.Context() {
		t.Error("a routed remote store changed the context")
	}
}
