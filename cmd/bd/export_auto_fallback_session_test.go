package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// diffSpyStore is sessionSpyStore with a diff, so the auto-export tries the
// incremental patch first.
type diffSpyStore struct {
	sessionSpyStore
}

func (s diffSpyStore) ChangedIssueIDs(_ context.Context, _, _ string) (storage.ChangedIssueIDs, error) {
	return storage.ChangedIssueIDs{Upserted: []string{"ae-1"}}, nil
}

func (s diffSpyStore) GetIssuesByIDs(_ context.Context, _ []string) ([]*types.Issue, error) {
	return []*types.Issue{{ID: "ae-1", Title: "exported"}}, nil
}

// TestAutoExportFallbackAfterAFailedRewriteGetsItsOwnSession: the incremental
// patch ends the read session before it rewrites the file. When that rewrite
// fails the export falls back to a full export, which reads the store again, so
// it begins a session of its own and ends it before it writes: the events are
// two whole sessions, not one session used after its end.
func TestAutoExportFallbackAfterAFailedRewriteGetsItsOwnSession(t *testing.T) {
	initConfigForTest(t)
	config.Set("export.auto", true)
	config.Set("export.git-add", false)
	config.Set("export.interval", "-1")

	saveAndRestoreGlobals(t)
	var log []string
	store = diffSpyStore{sessionSpyStore{
		fakeHeadOnlyStore: &fakeHeadOnlyStore{issues: []*types.Issue{{ID: "ae-1", Title: "exported"}}},
		log:               &log,
	}}

	dir := autoExportTestDir(t)
	beadsDir := filepath.Join(dir, ".beads")
	exportPath := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(exportPath, []byte(`{"_type":"issue","id":"ae-1","title":"stale"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	saveExportAutoState(beadsDir, &exportAutoState{LastDoltCommit: "older-commit", LastDiffAnchor: "anchor-commit"})

	old := rewriteExportFileFn
	t.Cleanup(func() { rewriteExportFileFn = old })
	rewrites := 0
	rewriteExportFileFn = func(string, map[string][]byte, map[string]bool, []string) (int, int, error) {
		rewrites++
		log = append(log, "rewrite")
		return 0, 0, errors.New("disk full")
	}

	if err := maybeAutoExport(context.Background(), false); err != nil {
		t.Fatalf("maybeAutoExport: %v", err)
	}
	if rewrites != 1 {
		t.Fatalf("the incremental rewrite ran %d times, want once", rewrites)
	}
	want := []string{"session begin", "session end", "rewrite", "session begin", "session end"}
	if !slices.Equal(log, want) {
		t.Fatalf("events = %v, want %v", log, want)
	}
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "stale") || !strings.Contains(string(data), `"exported"`) {
		t.Fatalf("the full export did not replace the file:\n%s", data)
	}
}
