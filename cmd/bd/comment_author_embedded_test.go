//go:build cgo

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

// TestEmbeddedCommentAuthorEnv drives `bd comments add` end to end: with
// BEADS_COMMENT_AUTHOR set the comment is signed with it, --author wins over
// the variable, and with neither the author is the actor, as before.
func TestEmbeddedCommentAuthorEnv(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "ca")
	issue := bdCreate(t, bd, dir, "Signed comments", "--type", "task")

	add := func(t *testing.T, env []string, args ...string) {
		t.Helper()
		cmd := exec.Command(bd, append([]string{"comments", "add", issue.ID}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(bdEnv(dir), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bd comments add %v: %v\n%s", args, err, out)
		}
	}

	add(t, nil, "no env, no flag", "--actor", "the-actor")
	add(t, []string{"BEADS_COMMENT_AUTHOR=coder · Sonnet 5.5 · high"}, "env only", "--actor", "the-actor")
	add(t, []string{"BEADS_COMMENT_AUTHOR=coder · Sonnet 5.5 · high"}, "flag wins", "--author", "planner", "--actor", "the-actor")

	cmd := exec.Command(bd, "comments", issue.ID, "--json")
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bd comments %s --json: %v", issue.ID, err)
	}
	var comments []struct {
		Author string `json:"author"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal(out, &comments); err != nil {
		t.Fatalf("unmarshal comments: %v\n%s", err, out)
	}
	want := map[string]string{
		"no env, no flag": "the-actor",
		"env only":        "coder · Sonnet 5.5 · high",
		"flag wins":       "planner",
	}
	got := map[string]string{}
	for _, c := range comments {
		got[c.Text] = c.Author
	}
	for text, author := range want {
		if got[text] != author {
			t.Errorf("comment %q has author %q, want %q (all: %v)", text, got[text], author, got)
		}
	}
}
