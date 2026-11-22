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

// TestEmbeddedAgentSurface drives the agent-memory surface end to end through
// the bd binary: field and section reads, the context bundle, stages, typed
// links, kinded comments, the awaiting-user inbox, the ready exclusion config
// and the stale-claim listing.
func TestEmbeddedAgentSurface(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "ag")

	run := func(t *testing.T, env []string, args ...string) (string, string, error) {
		t.Helper()
		cmd := exec.Command(bd, args...)
		cmd.Dir = dir
		cmd.Env = append(bdEnv(dir), env...)
		stdout, stderr, err := runCommandBuffers(t, cmd)
		return stdout.String(), stderr.String(), err
	}
	ok := func(t *testing.T, args ...string) string {
		t.Helper()
		out, errOut, err := run(t, nil, args...)
		if err != nil {
			t.Fatalf("bd %s failed: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, out, errOut)
		}
		return out
	}
	fails := func(t *testing.T, args ...string) string {
		t.Helper()
		out, errOut, err := run(t, nil, args...)
		if err == nil {
			t.Fatalf("bd %s succeeded, want failure\nstdout:\n%s", strings.Join(args, " "), out)
		}
		return out + errOut
	}
	decode := func(t *testing.T, raw string, v interface{}) {
		t.Helper()
		if err := json.Unmarshal([]byte(raw), v); err != nil {
			t.Fatalf("decoding %q: %v", raw, err)
		}
	}

	epic := bdCreate(t, bd, dir, "Epic", "--type", "epic", "--notes", "intro\n\n## Decisions\n- use ECS\n\n## Later\nx")
	design := "# Plan\n## Step A\naaa\n### Detail\nddd\n## Step B\nbbb\n" + strings.Repeat("filler line for the long design\n", 300)
	slice := bdCreate(t, bd, dir, "Slice", "--type", "task", "--parent", epic.ID,
		"--description", "Do the slice", "--design", design, "--acceptance", "It works")
	dep := bdCreate(t, bd, dir, "Dep", "--type", "task")
	ok(t, "dep", "add", slice.ID, dep.ID)
	child := bdCreate(t, bd, dir, "Child", "--type", "task", "--parent", slice.ID)

	t.Run("field_and_section", func(t *testing.T) {
		if got := ok(t, "show", slice.ID, "--field", "acceptance"); got != "It works\n" {
			t.Errorf("--field acceptance = %q", got)
		}
		if got := ok(t, "show", slice.ID, "--section", "step a"); got != "## Step A\naaa\n### Detail\nddd\n" {
			t.Errorf("--section step a = %q", got)
		}
		if got := ok(t, "show", epic.ID, "--field", "notes", "--section", "Decisions"); got != "## Decisions\n- use ECS\n" {
			t.Errorf("--field notes --section Decisions = %q", got)
		}
		var f struct{ ID, Field, Value string }
		decode(t, ok(t, "show", slice.ID, "--field", "title", "--json"), &f)
		if f.ID != slice.ID || f.Field != "title" || f.Value != "Slice" {
			t.Errorf("--field title --json = %+v", f)
		}
		if out := fails(t, "show", slice.ID, "--section", "nope"); !strings.Contains(out, "Step A") {
			t.Errorf("missing-section error should list headings: %s", out)
		}
		fails(t, "show", slice.ID, "--field", "bogus")
		fails(t, "show", slice.ID, dep.ID, "--field", "title")
	})

	t.Run("stage", func(t *testing.T) {
		ok(t, "update", slice.ID, "--stage", "code")
		if got := ok(t, "show", slice.ID, "--field", "stage"); got != "code\n" {
			t.Errorf("stage = %q", got)
		}
		if got := ok(t, "show", slice.ID, "--field", "metadata.stage_at.code"); !strings.HasPrefix(got, "20") {
			t.Errorf("stage_at.code = %q", got)
		}
		var rows []struct{ ID string }
		decode(t, ok(t, "list", "--stage", "code", "--json", "--flat"), &rows)
		if len(rows) != 1 || rows[0].ID != slice.ID {
			t.Errorf("list --stage code = %+v", rows)
		}
		fails(t, "update", slice.ID, "--stage", "Not-Valid")
	})

	t.Run("links", func(t *testing.T) {
		ok(t, "link", "add", slice.ID, "--kind", "mock", "--url", "https://x/m.png", "--note", "v1")
		ok(t, "link", "add", slice.ID, "--kind", "mock", "--url", "https://x/m.png", "--note", "v2")
		ok(t, "link", "add", slice.ID, "--kind", "artifact", "--url", "https://x/a.html")
		fails(t, "link", "add", slice.ID, "--kind", "bogus", "--url", "u")
		var links []typedLink
		decode(t, ok(t, "link", "list", slice.ID, "--json"), &links)
		if len(links) != 2 || links[0].Kind != "artifact" || links[1].Kind != "mock" || links[1].Note != "v2" {
			t.Errorf("link list = %+v", links)
		}
		var rows []struct{ ID string }
		decode(t, ok(t, "list", "--has-link", "mock", "--json", "--flat"), &rows)
		if len(rows) != 1 || rows[0].ID != slice.ID {
			t.Errorf("list --has-link mock = %+v", rows)
		}
		ok(t, "link", "rm", slice.ID, "--url", "https://x/a.html")
		decode(t, ok(t, "list", "--has-link", "artifact", "--json", "--flat"), &rows)
		if len(rows) != 0 {
			t.Errorf("list --has-link artifact after rm = %+v", rows)
		}
		fails(t, "link", "rm", slice.ID, "--url", "https://x/a.html")
		// The dependency shorthand still works next to the subcommands.
		ok(t, "link", dep.ID, child.ID, "--type", "related")
	})

	t.Run("kinded_comments", func(t *testing.T) {
		plan := filepath.Join(t.TempDir(), "plan.json")
		if err := os.WriteFile(plan, []byte(`{"summary":"planned","steps":["a","b"]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		ok(t, "comments", "add", slice.ID, "--kind", "handoff", "--author", "planner", "--json-file", plan)
		ok(t, "comments", "add", slice.ID, "plain words")
		ok(t, "comments", "add", slice.ID, "--kind", "handoff", "--author", "reviewer", `{"summary":"reviewed"}`)
		fails(t, "comments", "add", slice.ID, "--json-file", plan)

		var all []kindedComment
		decode(t, ok(t, "comments", slice.ID, "--kind", "handoff", "--json"), &all)
		if len(all) != 2 || all[0].Author != "planner" || all[1].Author != "reviewer" {
			t.Fatalf("handoffs = %+v", all)
		}
		var data struct{ Summary string }
		decode(t, string(all[0].Data), &data)
		if data.Summary != "planned" {
			t.Errorf("handoff data = %s", all[0].Data)
		}
		var last kindedComment
		decode(t, ok(t, "comments", slice.ID, "--kind", "handoff", "--last", "--json"), &last)
		if last.Author != "reviewer" {
			t.Errorf("--last = %+v", last)
		}
	})

	t.Run("context", func(t *testing.T) {
		out := ok(t, "show", slice.ID, "--context", "--max-chars", "1500")
		if n := len([]rune(out)); n > 1500 {
			t.Errorf("context is %d chars, over budget", n)
		}
		for _, want := range []string{
			"# " + slice.ID, "stage: code", "parent: " + epic.ID, "It works", "## Decisions",
			dep.ID, child.ID, "mock https://x/m.png", "[truncated: bd show " + slice.ID + " --field design]",
			"reviewer [handoff]",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("context lacks %q:\n%s", want, out)
			}
		}
		var b struct {
			Stage     string
			Parent    struct{ ID, Decisions string }
			Truncated []contextTruncation
			Children  []contextRef
		}
		raw := ok(t, "show", slice.ID, "--context", "--max-chars", "3000", "--json")
		decode(t, raw, &b)
		if b.Stage != "code" || b.Parent.ID != epic.ID || !strings.Contains(b.Parent.Decisions, "use ECS") ||
			len(b.Children) != 1 || len(b.Truncated) == 0 {
			t.Errorf("context JSON = %+v", b)
		}
	})

	t.Run("awaiting_user_and_ready_exclusion", func(t *testing.T) {
		call := bdCreate(t, bd, dir, "Needs a call", "--type", "task", "--label", "needs-decision")
		judge := bdCreate(t, bd, dir, "Needs design, has mock", "--type", "task", "--label", "needs-design")
		ok(t, "link", "add", judge.ID, "--kind", "mock", "--url", "https://x/j.png")
		var rows []struct{ ID string }
		decode(t, ok(t, "list", "--awaiting-user", "--json", "--flat"), &rows)
		if len(rows) != 2 || rows[0].ID != judge.ID || rows[1].ID != call.ID {
			t.Errorf("awaiting-user = %+v, want %s then %s", rows, judge.ID, call.ID)
		}
		ok(t, "config", "set", "ready.exclude_labels", "needs-decision,needs-design")
		ready := ok(t, "ready", "--json")
		if strings.Contains(ready, call.ID) || strings.Contains(ready, judge.ID) {
			t.Errorf("ready should exclude awaiting-user issues:\n%s", ready)
		}
		if !strings.Contains(ready, dep.ID) {
			t.Errorf("ready lost an ordinary issue:\n%s", ready)
		}
	})

	t.Run("claim_session_and_stale_claims", func(t *testing.T) {
		if out, errOut, err := run(t, []string{"CLAUDE_SESSION_ID=sess-42"}, "update", dep.ID, "--claim"); err != nil {
			t.Fatalf("claim failed: %v\n%s\n%s", err, out, errOut)
		}
		if got := ok(t, "show", dep.ID, "--field", "metadata.claim.session"); got != "sess-42\n" {
			t.Errorf("claim.session = %q", got)
		}
		// An in_progress issue with no lease falls back to its last update,
		// so at a 0s window it is stale while the live lease above is not.
		orphan := bdCreate(t, bd, dir, "In progress without a lease", "--type", "task")
		ok(t, "update", orphan.ID, "--status", "in_progress")
		ids := func(args ...string) []string {
			var rows []struct{ ID string }
			decode(t, ok(t, append([]string{"list", "--json", "--flat"}, args...)...), &rows)
			var out []string
			for _, r := range rows {
				out = append(out, r.ID)
			}
			return out
		}
		if got := ids("--stale-claims", "1h"); len(got) != 0 {
			t.Errorf("--stale-claims 1h = %v, want none", got)
		}
		if got := ids("--stale-claims", "0s"); len(got) != 1 || got[0] != orphan.ID {
			t.Errorf("--stale-claims 0s = %v, want only %s", got, orphan.ID)
		}
		fails(t, "list", "--stale-claims", "soon")
	})
}
