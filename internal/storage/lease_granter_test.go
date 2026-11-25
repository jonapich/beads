package storage_test

import (
	"testing"

	"github.com/steveyegge/beads/internal/storage"
)

type remoteLeaseStore struct {
	storage.DoltStorage
	remote bool
}

func (s remoteLeaseStore) GrantsLeasesRemotely() bool { return s.remote }

func TestGrantsLeasesRemotely(t *testing.T) {
	if storage.GrantsLeasesRemotely(nil) {
		t.Error("a nil store was reported as granting leases remotely")
	}
	if storage.GrantsLeasesRemotely(&stubStore{}) {
		t.Error("a store that runs its own claims was reported as granting leases remotely")
	}
	if storage.GrantsLeasesRemotely(remoteLeaseStore{remote: false}) {
		t.Error("a store answering false was reported as granting leases remotely")
	}
	if !storage.GrantsLeasesRemotely(remoteLeaseStore{remote: true}) {
		t.Error("a store answering true was not reported as granting leases remotely")
	}
	// Through a decorator, as the HTTP store sits under the hook and telemetry
	// layers.
	if !storage.GrantsLeasesRemotely(decorated{inner: remoteLeaseStore{remote: true}}) {
		t.Error("the answer was not read through the decorator's Unwrap")
	}
}
