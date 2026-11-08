//go:build cgo

package embeddeddolt_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// With auto-commit off a write stays uncommitted, so HEAD alone cannot show it.
func TestRemoteHasHeadCountsPendingChanges(t *testing.T) {
	ctx := t.Context()
	te := newTestEnv(t, "rhh")
	remoteDir := filepath.Join(t.TempDir(), "remote")
	if err := os.MkdirAll(remoteDir, 0o700); err != nil {
		t.Fatalf("create remote dir: %v", err)
	}
	if err := te.store.AddRemote(ctx, "origin", "file://"+remoteDir); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	if err := te.store.Push(ctx); err != nil {
		t.Fatalf("Push: %v", err)
	}

	issue := &types.Issue{ID: "rhh-1", Title: "pending", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}
	if err := te.store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	current, err := te.store.RemoteHasHead(ctx, "")
	if err != nil {
		t.Fatalf("RemoteHasHead: %v", err)
	}
	if current {
		t.Fatal("RemoteHasHead = true with an uncommitted write, want false")
	}
}
