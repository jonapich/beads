package main

import (
	"context"
	"errors"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
)

type failingRemoteHeadStore struct{ storage.DoltStorage }

func (failingRemoteHeadStore) RemoteHasHead(context.Context, string) (bool, error) {
	return true, errors.New("boom")
}

type storeWithoutRemoteHead struct{ storage.DoltStorage }

// When the check cannot answer, a real push must decide. Reaching these paths
// needs a store the embedded command never has.
func TestPushAlreadyCurrentFailsOpen(t *testing.T) {
	ctx := context.Background()

	if pushAlreadyCurrent(ctx, failingRemoteHeadStore{}, "") {
		t.Error("a failed check skipped the push")
	}
	if pushAlreadyCurrent(ctx, storeWithoutRemoteHead{}, "") {
		t.Error("a store that cannot check skipped the push")
	}
}
