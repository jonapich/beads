package main

// show_agent.go: the size-bounded reads of `bd show` for agents.
//
//	bd show <id> --field <name>          one field, raw
//	bd show <id> [--field <name>] --section "<heading>"
//	bd show <id> --context [--max-chars N]
//
// All three go through issueops.Reader, so they read the same on the direct
// and the proxied-server route.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/issueops"
)

const (
	defaultContextMaxChars     = 8000
	defaultContextComments     = 3
	contextCommentMaxChars     = 600
	contextListMax             = 25
	contextMetadataValueMax    = 200
	contextMinTextShare        = 120
	showAgentFieldUsage        = "Print one field raw: description, design, notes, acceptance, title, status, type, priority, assignee, stage, labels, metadata, or metadata.KEY"
	showAgentSectionUsage      = "Print one markdown section (heading through the next heading of the same or higher level). Searches --field, or description, design, notes and acceptance in that order"
	showAgentContextUsage      = "Print a compact, size-bounded briefing for an agent starting cold: the issue's text, its parent's decisions, deps, children, links, metadata and recent comments"
	showAgentMaxCharsUsage     = "Character budget for --context (truncated parts name the command that fetches them in full)"
	showAgentContextTailUsage  = "With --context: number of most recent comments to include (default 3)"
	showAgentFieldSectionUsage = "--field, --section and --context take exactly one issue ID"
)

type showAgentInput struct {
	field    string
	section  string
	context  bool
	maxChars int
	comments int
}

func registerShowAgentFlags(cmd *cobra.Command) {
	cmd.Flags().String("field", "", showAgentFieldUsage)
	cmd.Flags().String("section", "", showAgentSectionUsage)
	cmd.Flags().Bool("context", false, showAgentContextUsage)
	cmd.Flags().Int("max-chars", defaultContextMaxChars, showAgentMaxCharsUsage)
	cmd.Flags().Int("context-comments", defaultContextComments, showAgentContextTailUsage)
}

// gatherShowAgentInput reports whether this invocation is an agent read.
func gatherShowAgentInput(cmd *cobra.Command) (showAgentInput, bool) {
	var in showAgentInput
	in.field, _ = cmd.Flags().GetString("field")
	in.section, _ = cmd.Flags().GetString("section")
	in.context, _ = cmd.Flags().GetBool("context")
	in.maxChars, _ = cmd.Flags().GetInt("max-chars")
	in.comments, _ = cmd.Flags().GetInt("context-comments")
	return in, in.field != "" || in.section != "" || in.context
}

func runShowAgent(ctx context.Context, cmd *cobra.Command, args []string, in showAgentInput) error {
	idFlags, _ := cmd.Flags().GetStringArray("id")
	args = append(args, idFlags...)
	if len(args) != 1 {
		return HandleErrorRespectJSON(showAgentFieldSectionUsage)
	}
	if in.context && (in.field != "" || in.section != "") {
		return HandleErrorRespectJSON("--context cannot be combined with --field or --section")
	}
	if in.maxChars < 200 {
		return HandleErrorRespectJSON("--max-chars must be at least 200")
	}
	if in.comments < 0 {
		return HandleErrorRespectJSON("--context-comments must be >= 0")
	}
	id, err := resolveLabelTarget(ctx, args[0])
	if err != nil {
		return HandleErrorRespectJSON("%v", err)
	}
	rd, err := openIssueReader()
	if err != nil {
		return HandleErrorRespectJSON("%v", err)
	}
	details, err := rd.Get(ctx, issueops.GetRequest{
		ID:                id,
		IncludeDependents: in.context,
		IncludeComments:   in.context,
		BriefDeps:         true,
	})
	if err != nil {
		return HandleErrorRespectJSON("%v", err)
	}
	SetLastTouchedID(id)
	if in.context {
		return runShowContext(ctx, rd, details, in)
	}
	return runShowField(details, in)
}

// ---------------------------------------------------------------------------
// --field / --section

// searchableTextFields are the fields --section searches when no --field is
// given, in order.
var searchableTextFields = []string{"description", "design", "notes", "acceptance"}

func normalizeFieldName(name string) string {
	switch n := strings.ToLower(strings.TrimSpace(name)); n {
	case "acceptance_criteria", "acceptance-criteria", "ac":
		return "acceptance"
	case "desc", "body":
		return "description"
	case "issue_type":
		return "type"
	default:
		return n
	}
}

// issueFieldValue returns the field's raw text, and for metadata a JSON value.
func issueFieldValue(details *types.IssueDetails, field string) (text string, jsonValue json.RawMessage, err error) {
	is := &details.Issue
	switch field {
	case "description":
		return is.Description, nil, nil
	case "design":
		return is.Design, nil, nil
	case "notes":
		return is.Notes, nil, nil
	case "acceptance":
		return is.AcceptanceCriteria, nil, nil
	case "title":
		return is.Title, nil, nil
	case "status":
		return string(is.Status), nil, nil
	case "type":
		return string(is.IssueType), nil, nil
	case "priority":
		return fmt.Sprintf("P%d", is.Priority), nil, nil
	case "assignee":
		return is.Assignee, nil, nil
	case "stage":
		stage, _ := issueStage(is.Metadata)
		return stage, nil, nil
	case "labels":
		return strings.Join(details.Labels, "\n"), nil, nil
	case "metadata":
		meta := is.Metadata
		if len(meta) == 0 {
			meta = json.RawMessage("{}")
		}
		return string(meta), meta, nil
	}
	if key, ok := strings.CutPrefix(field, "metadata."); ok && key != "" {
		raw, found := metadataMap(is.Metadata)[key]
		if !found {
			return "", nil, fmt.Errorf("metadata key %q is not set on %s", key, is.ID)
		}
		return metadataString(raw), raw, nil
	}
	return "", nil, fmt.Errorf("unknown field %q (%s)", field, strings.TrimPrefix(showAgentFieldUsage, "Print one field raw: "))
}

func runShowField(details *types.IssueDetails, in showAgentInput) error {
	field := normalizeFieldName(in.field)
	var text string
	var jsonValue json.RawMessage
	if in.section == "" {
		var err error
		if text, jsonValue, err = issueFieldValue(details, field); err != nil {
			return HandleErrorRespectJSON("%v", err)
		}
	} else {
		fields := searchableTextFields
		if field != "" {
			fields = []string{field}
		}
		found := false
		var headings []string
		for _, f := range fields {
			body, _, err := issueFieldValue(details, f)
			if err != nil {
				return HandleErrorRespectJSON("%v", err)
			}
			if sec, ok := markdownSection(body, in.section); ok {
				text, field, found = sec, f, true
				break
			}
			for _, h := range markdownHeadings(body) {
				headings = append(headings, f+": "+h)
			}
		}
		if !found {
			hint := "no headings found"
			if len(headings) > 0 {
				hint = "headings: " + strings.Join(headings, " | ")
			}
			return HandleErrorRespectJSON("section %q not found on %s (%s)", in.section, details.ID, hint)
		}
	}
	if jsonOutput {
		out := map[string]interface{}{"id": details.ID, "field": field}
		if in.section != "" {
			out["section"] = in.section
		}
		if jsonValue != nil {
			out["value"] = jsonValue
		} else {
			out["value"] = text
		}
		return outputJSON(out)
	}
	if text == "" {
		return nil
	}
	if strings.HasSuffix(text, "\n") {
		fmt.Print(text)
	} else {
		fmt.Println(text)
	}
	return nil
}

// markdownHeading parses an ATX heading line into its level and text.
func markdownHeading(line string) (level int, text string, ok bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return 0, "", false
	}
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return 0, "", false
	}
	rest := trimmed[level:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	text = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(rest), "#"))
	return level, text, true
}

// forEachMarkdownLine walks lines, reporting whether each is outside a code fence.
func forEachMarkdownLine(text string, fn func(i int, line string, inFence bool)) {
	inFence := false
	for i, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fn(i, line, true)
			inFence = !inFence
			continue
		}
		fn(i, line, inFence)
	}
}

func markdownHeadings(text string) []string {
	var out []string
	forEachMarkdownLine(text, func(_ int, line string, inFence bool) {
		if inFence {
			return
		}
		if _, h, ok := markdownHeading(line); ok {
			out = append(out, h)
		}
	})
	return out
}

func headingMatchKey(s string) string {
	s = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(s), "#"))
	s = strings.TrimRight(s, ": ")
	return strings.ToLower(s)
}

// markdownSection returns the section under the heading that matches want
// (case-insensitive; an exact match wins over a prefix match), from the
// heading line through the line before the next heading of the same or a
// higher level. Headings inside code fences are ignored.
func markdownSection(text, want string) (string, bool) {
	if text == "" {
		return "", false
	}
	key := headingMatchKey(want)
	if key == "" {
		return "", false
	}
	type heading struct{ line, level int }
	var heads []heading
	exact, prefix := -1, -1
	forEachMarkdownLine(text, func(i int, line string, inFence bool) {
		if inFence {
			return
		}
		level, h, ok := markdownHeading(line)
		if !ok {
			return
		}
		heads = append(heads, heading{i, level})
		hk := headingMatchKey(h)
		if hk == key && exact < 0 {
			exact = len(heads) - 1
		} else if strings.HasPrefix(hk, key) && prefix < 0 {
			prefix = len(heads) - 1
		}
	})
	idx := exact
	if idx < 0 {
		idx = prefix
	}
	if idx < 0 {
		return "", false
	}
	lines := strings.Split(text, "\n")
	end := len(lines)
	for _, h := range heads[idx+1:] {
		if h.level <= heads[idx].level {
			end = h.line
			break
		}
	}
	return strings.TrimRight(strings.Join(lines[heads[idx].line:end], "\n"), "\n"), true
}

// ---------------------------------------------------------------------------
// --context

type contextRef struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Type   string `json:"type,omitempty"`
}

type contextComment struct {
	Author    string `json:"author"`
	CreatedAt string `json:"created_at"`
	Kind      string `json:"kind,omitempty"`
	Text      string `json:"text"`
}

type contextParent struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Decisions string `json:"decisions,omitempty"`
}

type contextTruncation struct {
	Part  string `json:"part"`
	Fetch string `json:"fetch"`
}

type contextBundle struct {
	ID             string              `json:"id"`
	Title          string              `json:"title"`
	Type           string              `json:"type"`
	Status         string              `json:"status"`
	Priority       int                 `json:"priority"`
	Assignee       string              `json:"assignee,omitempty"`
	Stage          string              `json:"stage,omitempty"`
	StageChangedAt string              `json:"stage_changed_at,omitempty"`
	Labels         []string            `json:"labels,omitempty"`
	Signoff        string              `json:"signoff,omitempty"`
	SignoffSummary string              `json:"signoff_summary,omitempty"`
	Parent         *contextParent      `json:"parent,omitempty"`
	Description    string              `json:"description,omitempty"`
	Design         string              `json:"design,omitempty"`
	Acceptance     string              `json:"acceptance,omitempty"`
	Dependencies   []contextRef        `json:"dependencies,omitempty"`
	Children       []contextRef        `json:"children,omitempty"`
	Links          []typedLink         `json:"links,omitempty"`
	Metadata       map[string]string   `json:"metadata,omitempty"`
	CommentCount   int                 `json:"comment_count"`
	Comments       []contextComment    `json:"comments,omitempty"`
	Truncated      []contextTruncation `json:"truncated,omitempty"`
	moreDeps       int
	moreChildren   int
}

// contextPart is one budgeted piece of text in the bundle.
type contextPart struct {
	name  string
	fetch string
	text  *string
}

func runShowContext(ctx context.Context, rd issueops.Reader, details *types.IssueDetails, in showAgentInput) error {
	b := buildContextBundle(ctx, rd, details, in.comments)
	if jsonOutput {
		fitContextBundle(b, in.maxChars, func() int {
			raw, _ := json.Marshal(b)
			return len(raw)
		})
		return outputJSON(b)
	}
	fitContextBundle(b, in.maxChars, func() int { return utf8.RuneCountInString(renderContextText(b)) })
	fmt.Print(renderContextText(b))
	return nil
}

func buildContextBundle(ctx context.Context, rd issueops.Reader, d *types.IssueDetails, commentTail int) *contextBundle {
	is := &d.Issue
	b := &contextBundle{
		ID:          is.ID,
		Title:       is.Title,
		Type:        string(is.IssueType),
		Status:      string(is.Status),
		Priority:    is.Priority,
		Assignee:    is.Assignee,
		Labels:      d.Labels,
		Description: is.Description,
		Design:      is.Design,
		Acceptance:  is.AcceptanceCriteria,
		Links:       issueLinks(is.Metadata),
	}
	b.Stage, b.StageChangedAt = issueStage(is.Metadata)
	b.Signoff = signoffState(is.Metadata)
	if is.Status == types.StatusClosed {
		if c, body := latestSignoffSummary(d.Comments); c != nil {
			b.SignoffSummary = renderSignoffSummary(body)
		}
	}

	parentID := ""
	if d.Parent != nil {
		parentID = *d.Parent
	}
	for _, dep := range d.Dependencies {
		if dep.DependencyType == types.DepParentChild {
			if parentID == "" {
				parentID = dep.ID
			}
			continue
		}
		b.Dependencies = append(b.Dependencies, contextRef{ID: dep.ID, Status: string(dep.Status), Title: dep.Title, Type: string(dep.DependencyType)})
	}
	for _, dep := range d.Dependents {
		if dep.DependencyType == types.DepParentChild {
			b.Children = append(b.Children, contextRef{ID: dep.ID, Status: string(dep.Status), Title: dep.Title})
		}
	}
	if n := len(b.Dependencies); n > contextListMax {
		b.moreDeps = n - contextListMax
		b.Dependencies = b.Dependencies[:contextListMax]
	}
	if n := len(b.Children); n > contextListMax {
		b.moreChildren = n - contextListMax
		b.Children = b.Children[:contextListMax]
	}

	if parentID != "" {
		if pd, err := rd.Get(ctx, issueops.GetRequest{ID: parentID, BriefDeps: true}); err == nil {
			b.Parent = &contextParent{ID: pd.ID, Title: pd.Title, Status: string(pd.Status), Decisions: parentDecisions(pd)}
		} else {
			fmt.Fprintf(os.Stderr, "warning: reading parent %s: %v\n", parentID, err)
		}
	}

	meta := metadataMap(is.Metadata)
	keys := make([]string, 0, len(meta))
	for k := range meta {
		if k == metaStage || k == metaStageChangedAt || strings.HasPrefix(k, metaLinkPrefix) {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		b.Metadata = make(map[string]string, len(keys))
		for _, k := range keys {
			b.Metadata[k] = truncateRunes(metadataString(meta[k]), contextMetadataValueMax, "…")
		}
	}

	b.CommentCount = len(d.Comments)
	if d.CommentCount != nil && int(*d.CommentCount) > b.CommentCount {
		b.CommentCount = int(*d.CommentCount)
	}
	start := len(d.Comments) - commentTail
	if start < 0 {
		start = 0
	}
	for _, c := range d.Comments[start:] {
		kind, body := splitCommentKind(c.Text)
		b.Comments = append(b.Comments, contextComment{
			Author:    c.Author,
			CreatedAt: c.CreatedAt.UTC().Format("2006-01-02 15:04"),
			Kind:      kind,
			Text:      truncateRunes(strings.TrimSpace(body), contextCommentMaxChars, "…"),
		})
	}
	return b
}

// parentDecisions prefers a "Decisions" section anywhere in the parent's
// text, and falls back to the parent's notes.
func parentDecisions(pd *types.IssueDetails) string {
	for _, body := range []string{pd.Notes, pd.Design, pd.Description} {
		if sec, ok := markdownSection(body, "Decisions"); ok {
			return sec
		}
	}
	return pd.Notes
}

func (b *contextBundle) parts() []contextPart {
	parts := []contextPart{
		{"signoff_summary", fmt.Sprintf("bd signoff %s", b.ID), &b.SignoffSummary},
		{"acceptance", fmt.Sprintf("bd show %s --field acceptance", b.ID), &b.Acceptance},
		{"description", fmt.Sprintf("bd show %s --field description", b.ID), &b.Description},
		{"design", fmt.Sprintf("bd show %s --field design", b.ID), &b.Design},
	}
	if b.Parent != nil {
		fetch := fmt.Sprintf("bd show %s --section Decisions", b.Parent.ID)
		parts = append(parts, contextPart{"parent.decisions", fetch, &b.Parent.Decisions})
	}
	for i := range b.Comments {
		parts = append(parts, contextPart{"comments", fmt.Sprintf("bd comments %s", b.ID), &b.Comments[i].Text})
	}
	return parts
}

// fitContextBundle shrinks the bundle's text parts until measure() fits in
// maxChars. The budget is shared water-filling style: short parts keep all
// their text and the slack goes to the long ones, so one 80 KB design cannot
// starve the acceptance criteria. Every cut part is recorded with the
// command that fetches it in full.
func fitContextBundle(b *contextBundle, maxChars int, measure func() int) {
	parts := b.parts()
	originals := make([]string, len(parts))
	for i, p := range parts {
		originals[i] = *p.text
	}
	total := measure()
	if total <= maxChars {
		return
	}
	textLen := 0
	for _, s := range originals {
		textLen += utf8.RuneCountInString(s)
	}
	fixed := total - textLen
	budget := maxChars - fixed
	// Each pass re-measures, because markers and JSON escaping move the total.
	for attempt := 0; attempt < 6; attempt++ {
		if budget < 0 {
			budget = 0
		}
		shares := waterFill(originals, budget)
		b.Truncated = nil
		seen := map[string]bool{}
		for i, p := range parts {
			if shares[i] >= utf8.RuneCountInString(originals[i]) {
				*p.text = originals[i]
				continue
			}
			marker := "[truncated: " + p.fetch + "]"
			keep := shares[i] - utf8.RuneCountInString(marker) - 1
			if keep < 0 {
				keep = 0
			}
			*p.text = truncateRunes(originals[i], keep, "") + "\n" + marker
			if !seen[p.name] {
				seen[p.name] = true
				b.Truncated = append(b.Truncated, contextTruncation{Part: p.name, Fetch: p.fetch})
			}
		}
		over := measure() - maxChars
		if over <= 0 {
			return
		}
		budget -= over + 16
		if budget < contextMinTextShare*len(parts)/4 {
			break
		}
	}
	// The fixed sections alone overflow; keep what fits and say so.
	for i, p := range parts {
		if originals[i] != "" {
			*p.text = "[truncated: " + p.fetch + "]"
		}
	}
	b.Truncated = nil
	seen := map[string]bool{}
	for i, p := range parts {
		if originals[i] != "" && !seen[p.name] {
			seen[p.name] = true
			b.Truncated = append(b.Truncated, contextTruncation{Part: p.name, Fetch: p.fetch})
		}
	}
}

// waterFill divides budget runes among texts: each gets min(len, share) where
// the share grows as shorter texts leave slack behind.
func waterFill(texts []string, budget int) []int {
	n := len(texts)
	shares := make([]int, n)
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	lens := make([]int, n)
	for i, s := range texts {
		lens[i] = utf8.RuneCountInString(s)
	}
	sort.SliceStable(order, func(a, b int) bool { return lens[order[a]] < lens[order[b]] })
	remaining := budget
	for k, idx := range order {
		left := n - k
		share := remaining / left
		if lens[idx] <= share {
			shares[idx] = lens[idx]
		} else {
			shares[idx] = share
		}
		remaining -= shares[idx]
	}
	return shares
}

// truncateRunes cuts s to at most max runes (suffix included), preferring a
// line break in the last fifth of the kept text.
func truncateRunes(s string, max int, suffix string) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	keep := max - utf8.RuneCountInString(suffix)
	if keep <= 0 {
		return ""
	}
	r := []rune(s)[:keep]
	cut := string(r)
	if nl := strings.LastIndex(cut, "\n"); nl > 0 && utf8.RuneCountInString(cut[:nl]) >= keep*4/5 {
		cut = cut[:nl]
	}
	return strings.TrimRight(cut, " \t\n") + suffix
}

func renderContextText(b *contextBundle) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s [%s P%d %s] %s\n", b.ID, b.Type, b.Priority, b.Status, b.Title)
	var facts []string
	if b.Stage != "" {
		s := "stage: " + b.Stage
		if b.StageChangedAt != "" {
			s += " (since " + b.StageChangedAt + ")"
		}
		facts = append(facts, s)
	}
	if b.Assignee != "" {
		facts = append(facts, "assignee: "+b.Assignee)
	}
	if len(b.Labels) > 0 {
		facts = append(facts, "labels: "+strings.Join(b.Labels, ", "))
	}
	if len(facts) > 0 {
		sb.WriteString(strings.Join(facts, " | ") + "\n")
	}
	if b.Parent != nil {
		fmt.Fprintf(&sb, "parent: %s [%s] %s\n", b.Parent.ID, b.Parent.Status, b.Parent.Title)
	}
	section := func(title, body string) {
		if strings.TrimSpace(body) == "" {
			return
		}
		fmt.Fprintf(&sb, "\n## %s\n%s\n", title, strings.TrimRight(body, "\n"))
	}
	if b.Signoff != "" || b.SignoffSummary != "" {
		state := b.Signoff
		if state == "" {
			state = "no state"
		}
		fmt.Fprintf(&sb, "\n## sign-off: %s\n", state)
		if b.SignoffSummary != "" {
			sb.WriteString(strings.TrimRight(b.SignoffSummary, "\n") + "\n")
		}
	}
	section("description", b.Description)
	section("design", b.Design)
	section("acceptance", b.Acceptance)
	if b.Parent != nil {
		section("parent decisions ("+b.Parent.ID+")", b.Parent.Decisions)
	}
	refs := func(title string, list []contextRef, more int, fetch string) {
		if len(list) == 0 {
			return
		}
		fmt.Fprintf(&sb, "\n## %s\n", title)
		for _, r := range list {
			if r.Type != "" && r.Type != string(types.DepBlocks) {
				fmt.Fprintf(&sb, "- %s [%s] %s (%s)\n", r.ID, r.Status, r.Title, r.Type)
			} else {
				fmt.Fprintf(&sb, "- %s [%s] %s\n", r.ID, r.Status, r.Title)
			}
		}
		if more > 0 {
			fmt.Fprintf(&sb, "[+%d more: %s]\n", more, fetch)
		}
	}
	refs("deps", b.Dependencies, b.moreDeps, "bd dep list "+b.ID)
	refs("children", b.Children, b.moreChildren, "bd show "+b.ID+" --children")
	if len(b.Links) > 0 {
		sb.WriteString("\n## links\n")
		for _, l := range b.Links {
			if l.Note != "" {
				fmt.Fprintf(&sb, "- %s %s — %s\n", l.Kind, l.URL, l.Note)
			} else {
				fmt.Fprintf(&sb, "- %s %s\n", l.Kind, l.URL)
			}
		}
	}
	if len(b.Metadata) > 0 {
		sb.WriteString("\n## metadata\n")
		keys := make([]string, 0, len(b.Metadata))
		for k := range b.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&sb, "%s=%s\n", k, b.Metadata[k])
		}
	}
	if len(b.Comments) > 0 {
		fmt.Fprintf(&sb, "\n## comments (last %d of %d)\n", len(b.Comments), b.CommentCount)
		for _, c := range b.Comments {
			kind := ""
			if c.Kind != "" {
				kind = " [" + c.Kind + "]"
			}
			fmt.Fprintf(&sb, "- %s %s%s: %s\n", c.CreatedAt, c.Author, kind, c.Text)
		}
	}
	return sb.String()
}
