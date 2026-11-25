//go:build cgo

package embeddeddolt_test

import (
	"context"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// TestReadSessionServesReadsAndRefusesWrites runs a read session against a real
// embedded engine: the reads it serves answer what the same reads outside a
// session answer, and a write inside it fails instead of committing.
func TestReadSessionServesReadsAndRefusesWrites(t *testing.T) {
	skipUnlessEmbeddedDolt(t)
	te := newTestEnv(t, "rs")
	ctx := t.Context()

	issue := &types.Issue{
		Title:     "Session subject",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := te.store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	wantIssue, err := te.store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue outside a session: %v", err)
	}
	wantPrefix, err := te.store.GetConfig(ctx, "issue_prefix")
	if err != nil {
		t.Fatalf("GetConfig outside a session: %v", err)
	}

	err = storage.WithReadSession(ctx, te.store, func(ctx context.Context) error {
		got, err := te.store.GetIssue(ctx, issue.ID)
		if err != nil {
			return err
		}
		if got.ID != wantIssue.ID || got.Title != wantIssue.Title || got.ContentHash != wantIssue.ContentHash {
			t.Errorf("GetIssue in a session = %+v, want %+v", got, wantIssue)
		}
		prefix, err := te.store.GetConfig(ctx, "issue_prefix")
		if err != nil {
			return err
		}
		if prefix != wantPrefix {
			t.Errorf("GetConfig in a session = %q, want %q", prefix, wantPrefix)
		}
		if _, err := te.store.GetCurrentCommit(ctx); err != nil {
			t.Errorf("GetCurrentCommit in a session: %v", err)
		}

		// A write goes through withConn(commit=true) and must be refused.
		other := &types.Issue{Title: "Written in a session", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}
		werr := te.store.CreateIssue(ctx, other, "tester")
		if werr == nil || !strings.Contains(werr.Error(), "write inside a read session") {
			t.Errorf("CreateIssue in a read session error = %v, want the read-session refusal", werr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read session: %v", err)
	}

	// The refused write left nothing behind, and the store is usable once the
	// session has closed.
	if _, err := te.store.GetIssue(ctx, issue.ID); err != nil {
		t.Fatalf("GetIssue after the session: %v", err)
	}
	third := &types.Issue{Title: "Written after the session", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}
	if err := te.store.CreateIssue(ctx, third, "tester"); err != nil {
		t.Fatalf("CreateIssue after the session: %v", err)
	}
}
