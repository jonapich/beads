//go:build cgo

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

// TestEmbeddedCommentShorthandAuthorEnv drives the singular `bd comment` end to
// end, as TestEmbeddedCommentAuthorEnv drives `bd comments add`: with
// BEADS_COMMENT_AUTHOR set the comment is signed with it, --author wins over the
// variable, with neither the author is the actor, and --kind stores a kinded
// comment signed the same way.
func TestEmbeddedCommentShorthandAuthorEnv(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "cs")
	issue := bdCreate(t, bd, dir, "Signed shorthand comments", "--type", "task")

	comment := func(t *testing.T, env []string, args ...string) {
		t.Helper()
		cmd := exec.Command(bd, append([]string{"comment", issue.ID}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(bdEnv(dir), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bd comment %v: %v\n%s", args, err, out)
		}
	}
	const envAuthor = "BEADS_COMMENT_AUTHOR=coder · Sonnet 5.5 · high"

	comment(t, nil, "no env, no flag", "--actor", "the-actor")
	comment(t, []string{envAuthor}, "env only", "--actor", "the-actor")
	comment(t, []string{envAuthor}, "flag wins", "--author", "planner", "--actor", "the-actor")
	comment(t, []string{envAuthor}, "short flag wins", "-a", "reviewer", "--actor", "the-actor")
	comment(t, []string{"BEADS_COMMENT_AUTHOR=   "}, "blank env is unset", "--actor", "the-actor")
	comment(t, []string{envAuthor}, "kinded body", "--kind", "handoff", "--actor", "the-actor")

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
		"no env, no flag":               "the-actor",
		"env only":                      "coder · Sonnet 5.5 · high",
		"flag wins":                     "planner",
		"short flag wins":               "reviewer",
		"blank env is unset":            "the-actor",
		"bd-kind: handoff\nkinded body": "coder · Sonnet 5.5 · high",
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
