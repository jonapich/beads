package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/issueops"
)

func TestMarkdownSection(t *testing.T) {
	doc := strings.Join([]string{
		"# Plan",
		"intro",
		"## Step A",
		"aaa",
		"### Detail",
		"ddd",
		"## Step B:",
		"bbb",
		"```",
		"## not a heading",
		"```",
		"tail",
		"## Step Bravo",
		"x",
	}, "\n")
	cases := []struct {
		want, heading string
		ok            bool
	}{
		{"## Step A\naaa\n### Detail\nddd", "step a", true},
		{"### Detail\nddd", "## Detail", true},
		{"## Step B:\nbbb\n```\n## not a heading\n```\ntail", "Step B", true},
		{"## Step Bravo\nx", "step br", true},
		{"", "not a heading", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := markdownSection(doc, tc.heading)
		if ok != tc.ok || got != tc.want {
			t.Errorf("markdownSection(%q) = %q, %v; want %q, %v", tc.heading, got, ok, tc.want, tc.ok)
		}
	}
	if got := markdownHeadings(doc); strings.Join(got, "|") != "Plan|Step A|Detail|Step B:|Step Bravo" {
		t.Errorf("markdownHeadings = %v", got)
	}
}

func TestWaterFill(t *testing.T) {
	texts := []string{strings.Repeat("a", 10), strings.Repeat("b", 1000), strings.Repeat("c", 50), strings.Repeat("d", 1000)}
	shares := waterFill(texts, 500)
	want := []int{10, 220, 50, 220}
	for i := range want {
		if shares[i] != want[i] {
			t.Fatalf("waterFill shares = %v, want %v", shares, want)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("héllo wörld", 5, "…"); got != "héll…" {
		t.Errorf("truncateRunes = %q", got)
	}
	if got := truncateRunes("short", 10, "…"); got != "short" {
		t.Errorf("truncateRunes kept = %q", got)
	}
	long := "line one is here\nline two is here\nline three"
	if got := truncateRunes(long, 36, ""); got != "line one is here\nline two is here" {
		t.Errorf("truncateRunes newline cut = %q", got)
	}
}

func testContextBundle() *contextBundle {
	var design strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&design, "design line %d with some words in it\n", i)
	}
	return &contextBundle{
		ID: "bd-1", Title: "Slice", Type: "task", Status: "open",
		Description:  "short description",
		Design:       design.String(),
		Acceptance:   "It works",
		Parent:       &contextParent{ID: "bd-0", Title: "Epic", Status: "open", Decisions: "## Decisions\n- a\n- b"},
		Dependencies: []contextRef{{ID: "bd-2", Status: "open", Title: "Dep", Type: "blocks"}},
		Comments:     []contextComment{{Author: "a", CreatedAt: "2026-01-01 00:00", Text: strings.Repeat("c", 500)}},
		CommentCount: 1,
	}
}

func TestFitContextBundleText(t *testing.T) {
	for _, max := range []int{800, 2000, 6000} {
		b := testContextBundle()
		measure := func() int { return utf8.RuneCountInString(renderContextText(b)) }
		fitContextBundle(b, max, measure)
		got := measure()
		if got > max {
			t.Errorf("max %d: rendered %d chars", max, got)
		}
		if got < max*3/4 {
			t.Errorf("max %d: rendered only %d chars, budget underused", max, got)
		}
		if b.Acceptance != "It works" || b.Description != "short description" {
			t.Errorf("max %d: short parts were cut: %q %q", max, b.Acceptance, b.Description)
		}
		if !strings.HasSuffix(b.Design, "[truncated: bd show bd-1 --field design]") {
			t.Errorf("max %d: design lacks truncation pointer: %q", max, b.Design[len(b.Design)-60:])
		}
		if len(b.Truncated) == 0 || b.Truncated[0].Part != "design" {
			t.Errorf("max %d: truncated = %+v", max, b.Truncated)
		}
	}
}

func TestFitContextBundleJSON(t *testing.T) {
	b := testContextBundle()
	measure := func() int {
		raw, _ := json.Marshal(b)
		return len(raw)
	}
	fitContextBundle(b, 3000, measure)
	if got := measure(); got > 3000 || got < 2000 {
		t.Errorf("JSON bundle is %d bytes, want 2000..3000", got)
	}
}

func TestFitContextBundleUntouchedWhenSmall(t *testing.T) {
	b := testContextBundle()
	b.Design = "small"
	b.Comments[0].Text = "hi"
	fitContextBundle(b, 8000, func() int { return utf8.RuneCountInString(renderContextText(b)) })
	if b.Design != "small" || len(b.Truncated) != 0 {
		t.Errorf("small bundle was modified: %q %+v", b.Design, b.Truncated)
	}
}

func TestCommentKindRoundTrip(t *testing.T) {
	text := joinCommentKind("handoff", `{"summary":"x"}`)
	kind, body := splitCommentKind(text)
	if kind != "handoff" || body != `{"summary":"x"}` {
		t.Fatalf("split = %q %q", kind, body)
	}
	if string(commentData(body)) != `{"summary":"x"}` {
		t.Errorf("commentData JSON = %s", commentData(body))
	}
	if string(commentData("plain text")) != `"plain text"` {
		t.Errorf("commentData text = %s", commentData("plain text"))
	}
	for _, plain := range []string{"hello", "bd-kind: Bad Kind\nx", "bd-kindhandoff"} {
		if k, b := splitCommentKind(plain); k != "" || b != plain {
			t.Errorf("splitCommentKind(%q) = %q, %q", plain, k, b)
		}
	}
}

func TestIssueLinksAndStage(t *testing.T) {
	meta := json.RawMessage(`{
		"stage": "code", "stage_changed_at": "2026-01-01T00:00:00Z",
		"link.mock": [{"url":"m1","note":"n"},{"url":"m2"}],
		"link.artifact": [{"url":"a1"}],
		"link.broken": "not an array",
		"other": 5}`)
	links := issueLinks(meta)
	var got []string
	for _, l := range links {
		got = append(got, l.Kind+":"+l.URL)
	}
	if strings.Join(got, ",") != "artifact:a1,mock:m1,mock:m2" {
		t.Errorf("issueLinks = %v", got)
	}
	if !hasLinkOfKind(meta, "mock") || hasLinkOfKind(meta, "doc") || hasLinkOfKind(meta, "broken") {
		t.Error("hasLinkOfKind wrong")
	}
	if stage, at := issueStage(meta); stage != "code" || at != "2026-01-01T00:00:00Z" {
		t.Errorf("issueStage = %q %q", stage, at)
	}
	if stage, _ := issueStage(nil); stage != "" {
		t.Errorf("issueStage(nil) = %q", stage)
	}
}

func TestStageAndClaimEdits(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.FixedZone("x", 3600))
	got := strings.Join(stageMetadataEdits("code", now), " ")
	if got != "stage=code stage_changed_at=2026-03-04T04:06:07Z stage_at.code=2026-03-04T04:06:07Z" {
		t.Errorf("stageMetadataEdits = %s", got)
	}
	got = strings.Join(claimMetadataEdits("s1", now), " ")
	if got != "claim.session=s1 claim.at=2026-03-04T04:06:07Z" {
		t.Errorf("claimMetadataEdits = %s", got)
	}
	if err := validateStage("Bad-Stage"); err == nil {
		t.Error("validateStage accepted Bad-Stage")
	}
}

func TestClaimIsStale(t *testing.T) {
	now := time.Now()
	ago := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	cases := []struct {
		name  string
		issue types.Issue
		stale bool
	}{
		{"lease expired long ago", types.Issue{LeaseExpiresAt: ago(2 * time.Hour), UpdatedAt: now}, true},
		{"lease expired recently", types.Issue{LeaseExpiresAt: ago(10 * time.Minute)}, false},
		{"live lease", types.Issue{LeaseExpiresAt: ago(-time.Hour)}, false},
		{"no lease, old update", types.Issue{UpdatedAt: *ago(3 * time.Hour)}, true},
		{"no lease, recent heartbeat", types.Issue{UpdatedAt: *ago(3 * time.Hour), HeartbeatAt: ago(time.Minute)}, false},
	}
	for _, tc := range cases {
		if got := claimIsStale(&tc.issue, time.Hour, now); got != tc.stale {
			t.Errorf("%s: claimIsStale = %v, want %v", tc.name, got, tc.stale)
		}
	}
}

func TestApplyListAgentPageAwaitingUserOrder(t *testing.T) {
	row := func(id, meta string) *types.IssueWithCounts {
		return &types.IssueWithCounts{Issue: &types.Issue{ID: id, Metadata: json.RawMessage(meta)}}
	}
	page := issueops.IssuePage{Items: []*types.IssueWithCounts{
		row("a", `{}`),
		row("b", `{"link.mock":[{"url":"u"}]}`),
		row("c", `{"link.doc":[{"url":"u"}]}`),
		row("d", `{"link.artifact":[{"url":"u"}]}`),
	}}
	in := listInput{agent: listAgentFilter{awaitingUser: true}}
	var ids []string
	for _, r := range applyListAgentPage(in, page).Items {
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, "") != "bdac" {
		t.Errorf("awaiting-user order = %v, want b d a c", ids)
	}
}

func TestParentDecisions(t *testing.T) {
	pd := &types.IssueDetails{Issue: types.Issue{Notes: "n1", Design: "x\n## Decisions\n- d1\n## Next\ny"}}
	if got := parentDecisions(pd); got != "## Decisions\n- d1" {
		t.Errorf("parentDecisions = %q", got)
	}
	pd = &types.IssueDetails{Issue: types.Issue{Notes: "just notes"}}
	if got := parentDecisions(pd); got != "just notes" {
		t.Errorf("parentDecisions fallback = %q", got)
	}
}
