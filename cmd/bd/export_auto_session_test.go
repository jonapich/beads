package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/types"
)

// sessionSpyStore is a store with a read session that logs when it begins and
// ends, into the same log as the other events a test cares about.
type sessionSpyStore struct {
	*fakeHeadOnlyStore
	log *[]string
}

func (s sessionSpyStore) BeginReadSession(ctx context.Context) (context.Context, func() error, error) {
	*s.log = append(*s.log, "session begin")
	ended := false
	return ctx, func() error {
		if !ended {
			ended = true
			*s.log = append(*s.log, "session end")
		}
		return nil
	}, nil
}

// TestAutoExportEndsTheReadSessionBeforeTheGitAdd: the export's reads share one
// read session, which holds the store's lock and so makes every other bd wait.
// The git add, the state save and the warnings that follow the last read touch
// no store, so the session must be over before they run.
func TestAutoExportEndsTheReadSessionBeforeTheGitAdd(t *testing.T) {
	initConfigForTest(t)
	config.Set("export.auto", true)
	config.Set("export.git-add", true)
	config.Set("export.interval", "-1")

	saveAndRestoreGlobals(t)
	var log []string
	store = sessionSpyStore{
		fakeHeadOnlyStore: &fakeHeadOnlyStore{issues: []*types.Issue{{ID: "ae-1", Title: "exported"}}},
		log:               &log,
	}

	dir := autoExportTestDir(t)
	initCmd := gitCommand("init", "-q")
	initCmd.Dir = dir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}

	oldAdd := autoExportGitAdd
	t.Cleanup(func() { autoExportGitAdd = oldAdd })
	autoExportGitAdd = func(path string) error {
		log = append(log, "git add")
		return nil
	}

	if err := maybeAutoExport(context.Background(), false); err != nil {
		t.Fatalf("maybeAutoExport: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".beads", "issues.jsonl")); err != nil {
		t.Fatalf("the export did not write issues.jsonl: %v", err)
	}
	want := []string{"session begin", "session end", "git add"}
	if !slices.Equal(log, want) {
		t.Fatalf("events = %v, want %v: the session must end before the git add", log, want)
	}
}

// TestAutoExportEndsTheReadSessionWhenNothingIsExported: every early return
// releases the session too (here, no change since the last export).
func TestAutoExportEndsTheReadSessionWhenNothingIsExported(t *testing.T) {
	initConfigForTest(t)
	config.Set("export.auto", true)

	saveAndRestoreGlobals(t)
	var log []string
	store = sessionSpyStore{fakeHeadOnlyStore: &fakeHeadOnlyStore{}, log: &log}

	dir := autoExportTestDir(t)
	saveExportAutoState(filepath.Join(dir, ".beads"), &exportAutoState{LastDoltCommit: "head-commit-hash"})

	if err := maybeAutoExport(context.Background(), false); err != nil {
		t.Fatalf("maybeAutoExport: %v", err)
	}
	if want := []string{"session begin", "session end"}; !slices.Equal(log, want) {
		t.Fatalf("events = %v, want %v", log, want)
	}
}
