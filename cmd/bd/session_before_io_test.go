package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/routing"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// These tests pin the order of two things that must not happen while a read
// session holds the store's lock: the git role detection a routed `bd list`
// makes, and the file work of the auto-export (reading the existing JSONL back,
// writing and renaming the new one). Each runs against a store that logs when
// its session begins and ends, and a seam on the work that must come after.

// routingSpyStore is sessionSpyStore with the one config read the routing check
// makes, logged into the same list.
type routingSpyStore struct {
	sessionSpyStore
}

func (s routingSpyStore) GetAllConfig(_ context.Context) (map[string]string, error) {
	*s.log = append(*s.log, "read config")
	return map[string]string{}, nil
}

// routingSessionFixture sets a role-based route (contributor to a planning repo,
// maintainer to none) and a role detector that logs and answers role.
func routingSessionFixture(t *testing.T, role routing.UserRole) (routingSpyStore, *[]string) {
	t.Helper()
	initConfigForTest(t)
	config.Set("routing.mode", "auto")
	config.Set("routing.contributor", filepath.Join(t.TempDir(), "planning"))

	log := &[]string{}
	old := detectUserRoleAt
	t.Cleanup(func() { detectUserRoleAt = old })
	detectUserRoleAt = func(string) (routing.UserRole, error) {
		*log = append(*log, "detect role")
		return role, nil
	}
	return routingSpyStore{sessionSpyStore{fakeHeadOnlyStore: &fakeHeadOnlyStore{}, log: log}}, log
}

// TestListRoutingEndsTheReadSessionBeforeTheRoleDetection: the role detection
// runs git, and touches no store, so the session that read the routing config
// is over before it runs. The route it finds is opened next, so no new session.
func TestListRoutingEndsTheReadSessionBeforeTheRoleDetection(t *testing.T) {
	st, log := routingSessionFixture(t, routing.Contributor)

	ctx := context.Background()
	sessionCtx, end := storage.BeginReadSession(ctx, st)
	repoPath, rule, _, nextEnd := determineAutoRoutedRepoPathInSession(ctx, st, sessionCtx, end)
	nextEnd()

	if repoPath == "" || repoPath == "." || rule != routing.RuleContributor {
		t.Fatalf("route = %q (%v), want the contributor planning repo", repoPath, rule)
	}
	want := []string{"session begin", "read config", "session end", "detect role"}
	if !slices.Equal(*log, want) {
		t.Fatalf("events = %v, want %v: the session must end before the role detection", *log, want)
	}
}

// TestListRoutingBeginsANewSessionWhenItDoesNotRoute: when the role says the
// read stays local, bd list still has its page to read, so it gets a session
// for that, begun after the detection.
func TestListRoutingBeginsANewSessionWhenItDoesNotRoute(t *testing.T) {
	st, log := routingSessionFixture(t, routing.Maintainer) // no maintainer repo is set

	ctx := context.Background()
	sessionCtx, end := storage.BeginReadSession(ctx, st)
	repoPath, _, _, nextEnd := determineAutoRoutedRepoPathInSession(ctx, st, sessionCtx, end)
	if repoPath != "." && repoPath != "" {
		t.Fatalf("route = %q, want the local store", repoPath)
	}
	nextEnd()

	want := []string{"session begin", "read config", "session end", "detect role", "session begin", "session end"}
	if !slices.Equal(*log, want) {
		t.Fatalf("events = %v, want %v", *log, want)
	}
}

// TestListRoutingKeepsOneSessionWhenTheRoleIsNotConsulted: with no role-based
// route configured (the default) there is no detection, and the one session
// bd list began is the one it keeps, so the read stays at one engine open.
func TestListRoutingKeepsOneSessionWhenTheRoleIsNotConsulted(t *testing.T) {
	st, log := routingSessionFixture(t, routing.Contributor)
	config.Set("routing.mode", "off")

	ctx := context.Background()
	sessionCtx, end := storage.BeginReadSession(ctx, st)
	repoPath, _, _, nextEnd := determineAutoRoutedRepoPathInSession(ctx, st, sessionCtx, end)
	if repoPath != "." && repoPath != "" {
		t.Fatalf("route = %q, want the local store", repoPath)
	}
	if want := []string{"session begin", "read config"}; !slices.Equal(*log, want) {
		t.Fatalf("events before the caller ends the session = %v, want %v", *log, want)
	}
	nextEnd()
	if want := []string{"session begin", "read config", "session end"}; !slices.Equal(*log, want) {
		t.Fatalf("events = %v, want %v", *log, want)
	}
}

// fileSpyStore logs the session end and lets a test look at the disk then.
type fileSpyStore struct {
	*fakeHeadOnlyStore
	onEnd func()
}

func (s fileSpyStore) BeginReadSession(ctx context.Context) (context.Context, func() error, error) {
	ended := false
	return ctx, func() error {
		if !ended {
			ended = true
			s.onEnd()
		}
		return nil
	}, nil
}

// TestAutoExportEndsTheReadSessionBeforeTheFileIsWritten: the full export reads
// everything it needs, ends the session, and only then creates the temp file,
// writes it and renames it into place. At the moment the session ends the
// export directory holds neither the export nor a temp file for it.
func TestAutoExportEndsTheReadSessionBeforeTheFileIsWritten(t *testing.T) {
	initConfigForTest(t)
	config.Set("export.auto", true)
	config.Set("export.git-add", false)
	config.Set("export.interval", "-1")

	saveAndRestoreGlobals(t)
	dir := autoExportTestDir(t)
	beadsDir := filepath.Join(dir, ".beads")

	var atEnd []string
	store = fileSpyStore{
		fakeHeadOnlyStore: &fakeHeadOnlyStore{issues: []*types.Issue{{ID: "ae-1", Title: "exported"}}},
		onEnd: func() {
			entries, err := os.ReadDir(beadsDir)
			if err != nil {
				t.Errorf("read %s: %v", beadsDir, err)
			}
			for _, e := range entries {
				atEnd = append(atEnd, e.Name())
			}
		},
	}

	if err := maybeAutoExport(context.Background(), false); err != nil {
		t.Fatalf("maybeAutoExport: %v", err)
	}
	if want := []string{"metadata.json"}; !slices.Equal(atEnd, want) {
		t.Fatalf("files in .beads when the session ended = %v, want %v: the write must come after", atEnd, want)
	}
	data, err := os.ReadFile(filepath.Join(beadsDir, "issues.jsonl"))
	if err != nil {
		t.Fatalf("the export did not write issues.jsonl: %v", err)
	}
	if !strings.Contains(string(data), `"ae-1"`) {
		t.Fatalf("issues.jsonl lacks ae-1:\n%s", data)
	}
}

// incrementalSpyStore serves one changed issue to the incremental patch.
type incrementalSpyStore struct {
	fileSpyStore
}

func (s incrementalSpyStore) ChangedIssueIDs(_ context.Context, _, _ string) (storage.ChangedIssueIDs, error) {
	return storage.ChangedIssueIDs{Upserted: []string{"ae-1"}}, nil
}

func (s incrementalSpyStore) GetIssuesByIDs(_ context.Context, _ []string) ([]*types.Issue, error) {
	return []*types.Issue{{ID: "ae-1", Title: "patched"}}, nil
}

// TestIncrementalExportEndsTheReadSessionBeforeTheRewrite: the patch reads the
// diff and the changed issues, ends the session, and only then reads the
// existing file back and rewrites it. When the session ends the file still has
// its old content.
func TestIncrementalExportEndsTheReadSessionBeforeTheRewrite(t *testing.T) {
	initConfigForTest(t)
	saveAndRestoreGlobals(t)
	dir := autoExportTestDir(t)
	exportPath := filepath.Join(dir, ".beads", "issues.jsonl")
	old := `{"_type":"issue","id":"ae-1","title":"stale"}` + "\n"
	if err := os.WriteFile(exportPath, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	var atEnd string
	ends := 0
	store = incrementalSpyStore{fileSpyStore{
		fakeHeadOnlyStore: &fakeHeadOnlyStore{},
		onEnd:             func() {},
	}}
	end := func() {
		ends++
		data, err := os.ReadFile(exportPath)
		if err != nil {
			t.Errorf("read export: %v", err)
		}
		atEnd = string(data)
	}

	_, _, _, didIncremental, err := tryIncrementalExportSession(context.Background(), exportPath, "c1", "WORKING", nil, nil, end)
	if err != nil || !didIncremental {
		t.Fatalf("incremental = %v, %v; want it to patch", didIncremental, err)
	}
	if ends != 1 {
		t.Fatalf("the session ended %d times, want once", ends)
	}
	if atEnd != old {
		t.Fatalf("the file when the session ended = %q, want the old content %q: the rewrite must come after", atEnd, old)
	}
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"patched"`) || strings.Contains(string(data), `"stale"`) {
		t.Fatalf("export not patched:\n%s", data)
	}
}
