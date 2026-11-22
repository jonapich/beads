//go:build cgo

package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestProxiedServerAgentSurface checks that the agent-memory surface answers
// the same on the proxied-server route as on the embedded one.
func TestProxiedServerAgentSurface(t *testing.T) {
	requireSharedProxiedServer(t)
	t.Parallel()

	bd := buildEmbeddedBD(t)
	p := newSharedProxiedProject(t, bd, "ap")
	parent := bdProxiedCreate(t, bd, p.dir, "Epic")
	issue := bdProxiedCreate(t, bd, p.dir, "Slice")

	ok := func(args ...string) string {
		t.Helper()
		stdout, stderr, err := bdProxiedRunBuffers(t, bd, p.dir, args...)
		if err != nil {
			t.Fatalf("bd %s failed: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, stdout, stderr)
		}
		return stdout
	}
	jsonPart := func(s string) string {
		if i := strings.IndexAny(s, "[{"); i >= 0 {
			return s[i:]
		}
		return s
	}

	ok("update", parent.ID, "--notes", "## Decisions\n- keep it small")
	ok("update", issue.ID, "--parent", parent.ID, "--design", "## Step A\naaa\n## Step B\nbbb", "--stage", "plan")
	if got := ok("show", issue.ID, "--section", "Step B"); got != "## Step B\nbbb\n" {
		t.Errorf("--section Step B = %q", got)
	}
	if got := ok("show", issue.ID, "--field", "stage"); got != "plan\n" {
		t.Errorf("--field stage = %q", got)
	}

	ok("link", "add", issue.ID, "--kind", "artifact", "--url", "https://x/a.html")
	var links []typedLink
	if err := json.Unmarshal([]byte(jsonPart(ok("link", "list", issue.ID, "--json"))), &links); err != nil || len(links) != 1 || links[0].Kind != "artifact" {
		t.Errorf("link list = %+v (%v)", links, err)
	}

	ok("comments", "add", issue.ID, "--kind", "handoff", "--author", "tester", `{"summary":"done"}`)
	var last kindedComment
	if err := json.Unmarshal([]byte(jsonPart(ok("comments", issue.ID, "--kind", "handoff", "--last", "--json"))), &last); err != nil || last.Author != "tester" {
		t.Errorf("comments --kind handoff --last = %+v (%v)", last, err)
	}

	ctx := ok("show", issue.ID, "--context")
	for _, want := range []string{"stage: plan", "keep it small", "artifact https://x/a.html", "tester [handoff]"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("context lacks %q:\n%s", want, ctx)
		}
	}

	var rows []struct{ ID string }
	if err := json.Unmarshal([]byte(jsonPart(ok("list", "--stage", "plan", "--has-link", "artifact", "--json", "--flat"))), &rows); err != nil || len(rows) != 1 || rows[0].ID != issue.ID {
		t.Errorf("list --stage plan --has-link artifact = %+v (%v)", rows, err)
	}
}
