//go:build cgo

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
	"github.com/steveyegge/beads/internal/types"
)

// routingHangBudget is how long a command may take before these tests call it
// hung. A healthy run finishes in a few seconds; the failure they guard against
// waits for ever, so the budget only has to be far above a slow machine.
const routingHangBudget = 60 * time.Second

// newSelfRoutingWorkspace is a workspace whose store the routing tiers can name
// as their own target: <dir>/.beads with a metadata.json (admission and the
// database lookup need one) and an embedded store with one issue.
func newSelfRoutingWorkspace(t *testing.T) (beadsDir string, s *embeddeddolt.EmbeddedDoltStore, issueID string) {
	t.Helper()
	beadsDir = filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beadsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	meta := `{"database":"dolt","backend":"dolt","dolt_mode":"embedded","dolt_database":"beads"}`
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := embeddeddolt.Open(t.Context(), beadsDir, "beads", "main")
	if err != nil {
		t.Fatalf("open embedded store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.SetConfig(t.Context(), "issue_prefix", "sr"); err != nil {
		t.Fatalf("set issue_prefix: %v", err)
	}
	issue := &types.Issue{Title: "self route subject", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}
	if err := s.CreateIssue(t.Context(), issue, "tester"); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return beadsDir, s, issue.ID
}

// returnsWithin runs fn and fails the test if it has not returned in d. A hung
// fn is left behind (its goroutine cannot be stopped), which is the price of
// reporting the hang as a failure instead of as the package timeout.
func returnsWithin(t *testing.T, d time.Duration, what string, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		t.Fatalf("%s did not return within %v: it is waiting on a lock its own process holds", what, d)
		return nil
	}
}

// redirectStdout sends os.Stdout to a file for the rest of the test and returns
// a reader of what was written. It does not take stdioMutex, which a hung
// command would otherwise keep for every later test.
func redirectStdout(t *testing.T) func() string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = f
	t.Cleanup(func() { os.Stdout = old; _ = f.Close() })
	return func() string {
		b, _ := os.ReadFile(f.Name())
		return string(b)
	}
}

// TestEmbeddedShowDoesNotHangOnAPrefixRouteToItsOwnStore: `bd show` holds a read
// session on the local store while it looks an id up. A routes.jsonl entry that
// names the current database under a spelling other than "." ("./") sends the
// prefix tier to open that same data directory, and that open waits on the lock
// the session holds. The session must be over before any routed store is opened,
// so the answer is the plain not-found it was before sessions existed.
func TestEmbeddedShowDoesNotHangOnAPrefixRouteToItsOwnStore(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	beadsDir, s, _ := newSelfRoutingWorkspace(t)
	if err := os.WriteFile(filepath.Join(beadsDir, "routes.jsonl"), []byte(`{"prefix":"zz-","path":"./"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	oldStore, oldCtx, oldJSON, oldDB := store, rootCtx, jsonOutput, dbPath
	store, rootCtx, jsonOutput, dbPath = s, t.Context(), false, filepath.Join(beadsDir, "beads.db")
	t.Cleanup(func() { store, rootCtx, jsonOutput, dbPath = oldStore, oldCtx, oldJSON, oldDB })

	err := returnsWithin(t, routingHangBudget, "bd show of an unknown id with a route to the same store", func() error {
		return showCmd.RunE(showCmd, []string{"zz-nope"})
	})
	if err == nil {
		t.Fatal("bd show of an unknown id succeeded, want the silent not-found exit")
	}
}

// TestEmbeddedListDoesNotHangOnARouteToItsOwnStore is the same for `bd list`: it
// reads the routing settings through the session, then opens the routed target.
// routing.default naming the store's own repository (an absolute path, not ".")
// makes that target the same data directory.
func TestEmbeddedListDoesNotHangOnARouteToItsOwnStore(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	beadsDir, s, issueID := newSelfRoutingWorkspace(t)
	if err := s.SetConfig(t.Context(), "routing.default", filepath.Dir(beadsDir)); err != nil {
		t.Fatalf("set routing.default: %v", err)
	}

	oldStore, oldCtx, oldJSON := store, rootCtx, jsonOutput
	store, rootCtx, jsonOutput = s, t.Context(), true
	t.Cleanup(func() { store, rootCtx, jsonOutput = oldStore, oldCtx, oldJSON })
	out := redirectStdout(t)

	err := returnsWithin(t, routingHangBudget, "bd list with routing.default naming the same store", func() error {
		return runListCore(newListFlagsCommand(t), nil)
	})
	if err != nil {
		t.Fatalf("bd list with a route to its own store: %v", err)
	}
	if got := out(); !strings.Contains(got, issueID) {
		t.Errorf("bd list output lacks %s:\n%s", issueID, got)
	}
}
