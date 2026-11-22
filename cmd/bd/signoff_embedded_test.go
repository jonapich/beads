//go:build cgo

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmbeddedSignoff drives post-close sign-off end to end: the close
// refusal, --signoff-file, approve, dispute with its follow-up, waive, the
// list filter and the show/context placement.
func TestEmbeddedSignoff(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "so")

	run := func(args ...string) (string, string, error) {
		cmd := exec.Command(bd, args...)
		cmd.Dir = dir
		cmd.Env = bdEnv(dir)
		stdout, stderr, err := runCommandBuffers(t, cmd)
		return stdout.String(), stderr.String(), err
	}
	ok := func(args ...string) string {
		t.Helper()
		out, errOut, err := run(args...)
		if err != nil {
			t.Fatalf("bd %s failed: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, out, errOut)
		}
		return out
	}
	fails := func(args ...string) string {
		t.Helper()
		out, errOut, err := run(args...)
		if err == nil {
			t.Fatalf("bd %s succeeded, want failure\nstdout:\n%s", strings.Join(args, " "), out)
		}
		return out + errOut
	}
	status := func(id string) string {
		t.Helper()
		return strings.TrimSpace(ok("show", id, "--field", "status"))
	}
	state := func(id string) string {
		t.Helper()
		out, _, err := run("show", id, "--field", "metadata.signoff")
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}
	pendingIDs := func() []string {
		t.Helper()
		var rows []struct{ ID string }
		if err := json.Unmarshal([]byte(ok("list", "--signoff", "pending", "--json", "--flat")), &rows); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		return ids
	}

	summary := filepath.Join(t.TempDir(), "summary.json")
	if err := os.WriteFile(summary, []byte(`{"what_changed":"added stun","calls_made":["stun reuses stagger maths"],"removed_or_renamed":[],"where_to_see":"arena"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	a := bdCreate(t, bd, dir, "Stun mechanic", "--type", "feature", "--priority", "1")
	b := bdCreate(t, bd, dir, "Stagger mechanic", "--type", "feature")
	c := bdCreate(t, bd, dir, "Chore", "--type", "chore")

	// Config makes features need a summary: the close refuses without one.
	ok("config", "set", "signoff.types", "feature")
	if out := fails("close", a.ID); !strings.Contains(out, "sign-off summary") {
		t.Errorf("refusal should explain the missing summary: %s", out)
	}
	if status(a.ID) == "closed" {
		t.Fatal("refused close still closed the issue")
	}
	// A chore is not named by config and closes as usual.
	ok("close", c.ID)
	if state(c.ID) != "" {
		t.Errorf("chore got sign-off state %q", state(c.ID))
	}

	ok("close", a.ID, "--signoff-file", summary)
	if status(a.ID) != "closed" || state(a.ID) != "pending" {
		t.Fatalf("after --signoff-file: status %s, signoff %q", status(a.ID), state(a.ID))
	}
	if labels := ok("show", a.ID, "--field", "labels"); !strings.Contains(labels, "needs-signoff") {
		t.Errorf("pending issue lacks needs-signoff label: %q", labels)
	}
	if got := pendingIDs(); len(got) != 1 || got[0] != a.ID {
		t.Errorf("list --signoff pending = %v", got)
	}
	if prime := ok("prime"); !strings.Contains(prime, "1 closed issue(s) await the user's sign-off") {
		t.Errorf("prime lacks the pending sign-off count:\n%s", prime)
	}
	show := ok("show", a.ID)
	if i, j := strings.Index(show, "SIGN-OFF"), strings.Index(show, "DESCRIPTION"); i < 0 || j < 0 || i > j {
		t.Errorf("show should print SIGN-OFF before DESCRIPTION:\n%s", show)
	}
	if !strings.Contains(show, "stun reuses stagger maths") {
		t.Errorf("show lacks the summary:\n%s", show)
	}
	if ctx := ok("show", a.ID, "--context"); !strings.Contains(ctx, "## sign-off: pending") || !strings.Contains(ctx, "Calls made on your behalf") {
		t.Errorf("context lacks the sign-off:\n%s", ctx)
	}
	var view signoffView
	if err := json.Unmarshal([]byte(ok("signoff", a.ID, "--json")), &view); err != nil || view.Signoff != "pending" || len(view.Summary) == 0 {
		t.Errorf("signoff --json = %+v (%v)", view, err)
	}

	// Dispute: follow-up created, issue stays closed, label gone.
	var disputed struct{ Signoff, Followup string }
	if err := json.Unmarshal([]byte(ok("signoff", a.ID, "--dispute", "stun and stagger must share one formula", "--json")), &disputed); err != nil {
		t.Fatal(err)
	}
	if disputed.Signoff != "disputed" || disputed.Followup == "" {
		t.Fatalf("dispute = %+v", disputed)
	}
	if status(a.ID) != "closed" || state(a.ID) != "disputed" {
		t.Errorf("after dispute: status %s, signoff %q", status(a.ID), state(a.ID))
	}
	if labels := ok("show", a.ID, "--field", "labels"); strings.Contains(labels, "needs-signoff") {
		t.Errorf("disputed issue kept needs-signoff: %q", labels)
	}
	if got := ok("show", disputed.Followup, "--field", "description"); !strings.Contains(got, "share one formula") {
		t.Errorf("follow-up description = %q", got)
	}
	if got := ok("show", disputed.Followup, "--field", "priority"); got != "P1\n" {
		t.Errorf("follow-up priority = %q, want P1", got)
	}
	if deps := ok("dep", "list", disputed.Followup, "--json"); !strings.Contains(deps, a.ID) || !strings.Contains(deps, "discovered-from") {
		t.Errorf("follow-up deps = %s", deps)
	}

	// Summary as an existing comment, then --signoff-needed, then approve.
	ok("comments", "add", b.ID, "--kind", "signoff", "--json-file", summary)
	ok("close", b.ID, "--signoff-needed")
	if state(b.ID) != "pending" {
		t.Errorf("--signoff-needed state = %q", state(b.ID))
	}
	ok("signoff", b.ID, "--approve")
	if state(b.ID) != "approved" || len(pendingIDs()) != 0 {
		t.Errorf("after approve: state %q, pending %v", state(b.ID), pendingIDs())
	}

	// Waive.
	d := bdCreate(t, bd, dir, "Tiny feature", "--type", "feature")
	fails("close", d.ID, "--no-signoff", "")
	ok("close", d.ID, "--no-signoff", "trivial rename")
	if state(d.ID) != "waived" {
		t.Errorf("waived state = %q", state(d.ID))
	}
}
