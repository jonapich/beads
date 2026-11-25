package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/config"
)

// TestCommentAuthorPrecedence pins how `bd comments add` picks the author: the
// --author flag, then BEADS_COMMENT_AUTHOR, then the actor as before.
func TestCommentAuthorPrecedence(t *testing.T) {
	oldActor := actor
	actor = "the-actor"
	t.Cleanup(func() { actor = oldActor })

	tests := []struct {
		name string
		flag string
		env  string
		want string
	}{
		{"neither set keeps the actor", "", "", "the-actor"},
		{"env alone signs the comment", "", "coder · Sonnet 5.5 · high", "coder · Sonnet 5.5 · high"},
		{"flag alone", "planner", "", "planner"},
		{"flag wins over env", "planner", "coder · Sonnet 5.5 · high", "planner"},
		{"env is trimmed", "", "  coder  ", "coder"},
		{"blank env counts as unset", "", "   ", "the-actor"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(commentAuthorEnv, tc.env)
			if got := commentAuthor(tc.flag); got != tc.want {
				t.Errorf("commentAuthor(%q) with %s=%q = %q, want %q", tc.flag, commentAuthorEnv, tc.env, got, tc.want)
			}
		})
	}
}

// TestPrimeTellsAgentsToSignComments pins the harness-neutral rule in every
// shape of the briefing: the Core Rules block of the minimal (MCP) and the full
// CLI context, and the comments line of the command reference.
func TestPrimeTellsAgentsToSignComments(t *testing.T) {
	defer stubPrimeStoreUnavailable()()
	defer stubPrimeAgentProfile(config.ProfileConservative)()
	defer stubPrimeHasGitRemote(true)()
	defer stubIsEphemeralBranch(false)()
	defer stubPrimeNoPushConfigured(false)()

	const rule = "When you add a comment as an agent, pass `--author` naming yourself"
	const quoting = "comments that quote the user keep the default author"

	for _, mcpMode := range []bool{true, false} {
		var buf bytes.Buffer
		if err := outputPrimeContext(&buf, mcpMode, false); err != nil {
			t.Fatalf("outputPrimeContext(mcp=%v): %v", mcpMode, err)
		}
		out := buf.String()
		for _, want := range []string{rule, quoting} {
			if !strings.Contains(out, want) {
				t.Errorf("mcp=%v: briefing is missing %q", mcpMode, want)
			}
		}
		// The rule belongs to Core Rules, not somewhere later.
		if core, rest, ok := strings.Cut(out, "## Core Rules"); ok {
			if i := strings.Index(rest, rule); i < 0 || strings.Contains(rest[:i], "\n## ") {
				t.Errorf("mcp=%v: the comment rule is not inside the Core Rules block (before it: %d chars)", mcpMode, len(core))
			}
		} else {
			t.Errorf("mcp=%v: briefing has no Core Rules block", mcpMode)
		}
	}

	var buf bytes.Buffer
	if err := outputPrimeContext(&buf, false, false); err != nil {
		t.Fatalf("outputPrimeContext: %v", err)
	}
	if !strings.Contains(buf.String(), "bd comments add <id> \"text\" --author") || !strings.Contains(buf.String(), "BEADS_COMMENT_AUTHOR") {
		t.Errorf("the command reference's comments line does not show --author and BEADS_COMMENT_AUTHOR")
	}
}
