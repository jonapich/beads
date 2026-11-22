package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestRenderSignoffSummary(t *testing.T) {
	body := `{"where_to_see":"main menu","what_changed":"added stun","calls_made":["stun uses stagger maths"],"removed_or_renamed":[],"extra":"x"}`
	want := strings.Join([]string{
		"Calls made on your behalf:",
		"  - stun uses stagger maths",
		"What changed: added stun",
		"Removed or renamed: (none)",
		"Where to see it: main menu",
		"extra: x",
	}, "\n")
	if got := renderSignoffSummary(body); got != want {
		t.Errorf("renderSignoffSummary =\n%s\nwant\n%s", got, want)
	}
	if got := renderSignoffSummary("plain words\n"); got != "plain words" {
		t.Errorf("plain body = %q", got)
	}
}

func TestSignoffPatch(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	p := signoffPatch(signoffPending, "", "", now)
	if string(p.Metadata.Set[metaSignoff]) != `"pending"` || string(p.Metadata.Set[metaSignoffAt]) != `"2026-01-02T03:04:05Z"` {
		t.Errorf("pending set = %v", p.Metadata.Set)
	}
	if len(p.Labels.Add) != 1 || p.Labels.Add[0] != labelNeedsSignoff || len(p.Labels.Remove) != 0 {
		t.Errorf("pending labels = %+v", p.Labels)
	}
	if strings.Join(p.Metadata.Unset, ",") != metaSignoffNote+","+metaSignoffFollowup {
		t.Errorf("pending unset = %v", p.Metadata.Unset)
	}
	p = signoffPatch(signoffDisputed, "wrong", "bd-9", now)
	if string(p.Metadata.Set[metaSignoffNote]) != `"wrong"` || string(p.Metadata.Set[metaSignoffFollowup]) != `"bd-9"` {
		t.Errorf("disputed set = %v", p.Metadata.Set)
	}
	if len(p.Labels.Remove) != 1 || p.Labels.Remove[0] != labelNeedsSignoff {
		t.Errorf("disputed labels = %+v", p.Labels)
	}
}

func TestFormatSignoffBlockAndLatestSummary(t *testing.T) {
	issue := &types.Issue{Status: types.StatusClosed, Metadata: json.RawMessage(`{"signoff":"pending"}`)}
	comments := []*types.Comment{
		{Text: joinCommentKind("signoff", `{"what_changed":"old"}`)},
		{Text: "plain"},
		{Text: joinCommentKind("signoff", `{"what_changed":"new"}`)},
		{Text: joinCommentKind("handoff", `{"what_changed":"not me"}`)},
	}
	block := formatSignoffBlock(issue, comments)
	if !strings.Contains(block, "pending") || !strings.Contains(block, "What changed: new") || strings.Contains(block, "old") {
		t.Errorf("formatSignoffBlock = %q", block)
	}
	if got := formatSignoffBlock(&types.Issue{Status: types.StatusClosed}, []*types.Comment{{Text: "x"}}); got != "" {
		t.Errorf("no sign-off should render nothing, got %q", got)
	}
}
