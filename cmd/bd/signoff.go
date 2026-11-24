package main

// signoff.go: post-close sign-off by the user.
//
// An agent that closes a bead may have made calls the user never took. The
// closing agent leaves a summary (a kinded comment, `bd-kind: signoff`) and
// marks the bead pending; the user reads it and approves or disputes it.
//
//	bd close <id> --signoff-file summary.json   add the summary, close, mark pending
//	bd close <id> --signoff-needed              close and mark pending (summary must exist)
//	bd close <id> --no-signoff "reason"         close a bead config says needs one, waived
//	bd signoff <id>                             show the state and the summary
//	bd signoff <id> --approve [--note "..."]
//	bd signoff <id> --dispute "text"            follow-up bead (discovered-from), bead stays closed
//	bd signoff <id> --request                   mark an already-closed bead pending
//	bd list --signoff pending
//
// State lives in metadata (signoff, signoff_at, signoff_by, signoff_note,
// signoff_followup), and the needs-signoff label tracks "pending" so label
// views see it too. Config signoff.types / signoff.labels name the beads whose
// close requires a summary.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/remedy"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/issueops"
)

const (
	metaSignoff         = "signoff"
	metaSignoffAt       = "signoff_at"
	metaSignoffBy       = "signoff_by"
	metaSignoffNote     = "signoff_note"
	metaSignoffFollowup = "signoff_followup"

	signoffPending  = "pending"
	signoffApproved = "approved"
	signoffDisputed = "disputed"
	signoffWaived   = "waived"

	labelNeedsSignoff  = "needs-signoff"
	commentKindSignoff = "signoff"

	cfgSignoffTypes  = "signoff.types"
	cfgSignoffLabels = "signoff.labels"
)

var signoffStates = []string{signoffPending, signoffApproved, signoffDisputed, signoffWaived}

// signoffSummaryKeys are the summary's JSON keys, in display order: the calls
// made on the user's behalf first, because they are what the user signs off.
var signoffSummaryKeys = []struct{ key, title string }{
	{"calls_made", "Calls made on your behalf"},
	{"what_changed", "What changed"},
	{"removed_or_renamed", "Removed or renamed"},
	{"where_to_see", "Where to see it"},
	{"commits", "Commits"},
	{"captures", "Captures"},
}

// signoffSummaryExample is quoted in refusals so an agent can fix its call.
const signoffSummaryExample = `{"what_changed":"...","calls_made":["..."],"removed_or_renamed":[],"where_to_see":"..."}`

// signoffState returns the bead's sign-off state ("" when none).
func signoffState(meta json.RawMessage) string {
	if v, ok := metadataMap(meta)[metaSignoff]; ok {
		return metadataString(v)
	}
	return ""
}

// latestSignoffSummary returns the body of the most recent signoff comment.
func latestSignoffSummary(comments []*types.Comment) (*types.Comment, string) {
	for i := len(comments) - 1; i >= 0; i-- {
		if kind, body := splitCommentKind(comments[i].Text); kind == commentKindSignoff {
			return comments[i], body
		}
	}
	return nil, ""
}

// renderSignoffSummary renders a summary body: the known JSON keys in order,
// lists as bullets, other keys after them, and a non-JSON body as is.
func renderSignoffSummary(body string) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &obj); err != nil {
		return strings.TrimSpace(body)
	}
	var sb strings.Builder
	write := func(title string, raw json.RawMessage) {
		var list []string
		if err := json.Unmarshal(raw, &list); err == nil {
			if len(list) == 0 {
				fmt.Fprintf(&sb, "%s: (none)\n", title)
				return
			}
			fmt.Fprintf(&sb, "%s:\n", title)
			for _, item := range list {
				fmt.Fprintf(&sb, "  - %s\n", item)
			}
			return
		}
		fmt.Fprintf(&sb, "%s: %s\n", title, metadataString(raw))
	}
	seen := map[string]bool{}
	for _, k := range signoffSummaryKeys {
		if raw, ok := obj[k.key]; ok {
			write(k.title, raw)
			seen[k.key] = true
		}
	}
	for _, k := range sortedKeys(obj) {
		if !seen[k] {
			write(k, obj[k])
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// signoffPatch builds the metadata and label edit for a state change.
func signoffPatch(state, note, followup string, now time.Time) issueops.IssuePatch {
	patch := issueops.IssuePatch{}
	patch.Metadata.Set = map[string]json.RawMessage{
		metaSignoff:   storageString(state),
		metaSignoffAt: storageString(now.UTC().Format(time.RFC3339)),
		metaSignoffBy: storageString(currentActor()),
	}
	setOrUnset := func(key, value string) {
		if value != "" {
			patch.Metadata.Set[key] = storageString(value)
		} else {
			patch.Metadata.Unset = append(patch.Metadata.Unset, key)
		}
	}
	setOrUnset(metaSignoffNote, note)
	setOrUnset(metaSignoffFollowup, followup)
	if state == signoffPending {
		patch.Labels.Add = []string{labelNeedsSignoff}
	} else {
		patch.Labels.Remove = []string{labelNeedsSignoff}
	}
	return patch
}

// formatSignoffBlock renders the SIGN-OFF section bd show prints near the top
// of a closed issue: the state and the latest summary. Empty when there is
// neither.
func formatSignoffBlock(issue *types.Issue, comments []*types.Comment) string {
	state := signoffState(issue.Metadata)
	c, body := latestSignoffSummary(comments)
	if state == "" && c == nil {
		return ""
	}
	if state == "" {
		state = "no state"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n%s %s\n", ui.RenderBold("SIGN-OFF:"), state)
	if c != nil {
		for _, line := range strings.Split(renderSignoffSummary(body), "\n") {
			fmt.Fprintf(&sb, "  %s\n", line)
		}
	}
	return sb.String()
}

// primePendingSignoffCount counts closed issues awaiting sign-off for bd
// prime, best effort: any failure to reach the store reads as zero, so prime
// never fails or stalls a session start over it.
func primePendingSignoffCount() int {
	ctx := context.Background()
	if timeout := primeStoreTimeout(); timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if !usesProxiedServer() && store == nil {
		if err := ensureStoreActiveForPrime(ctx); err != nil || store == nil {
			return 0
		}
	}
	rd, err := openIssueReader()
	if err != nil {
		return 0
	}
	limit := 0
	page, err := rd.List(ctx, issueops.ListRequest{
		Status:         string(types.StatusClosed),
		MetadataFields: map[string]string{metaSignoff: signoffPending},
		Limit:          &limit,
		SkipCounts:     true,
	})
	if err != nil {
		return 0
	}
	return len(page.Items)
}

func storageString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func applySignoffState(ctx context.Context, id, state, note, followup string) error {
	lifecycle, err := openIssueLifecycle()
	if err != nil {
		return err
	}
	opsCtx, err := issueOpsContext(ctx)
	if err != nil {
		return err
	}
	_, err = lifecycle.Update(opsCtx, issueops.UpdateRequest{
		Actor:   currentActor(),
		IssueID: id,
		Patch:   signoffPatch(state, note, followup, time.Now()),
	})
	if err != nil {
		return err
	}
	commandDidWrite.Store(true)
	return nil
}

// addCommentAnyRoute appends one comment through the Commenter role on
// whichever route this invocation is on.
func addCommentAnyRoute(ctx context.Context, id, author, text string) error {
	if usesProxiedServer() {
		commenter, err := proxiedCommenter()
		if err != nil {
			return err
		}
		_, err = commenter.AddComment(ctx, issueops.AddCommentRequest{Author: author, IssueID: id, Text: text})
		return err
	}
	if _, err := addCommentDirect(ctx, store, id, author, text); err != nil {
		return err
	}
	commandDidWrite.Store(true)
	return commitPendingIfEmbedded(ctx, store, currentActor(), doltAutoCommitParams{
		Command:  "signoff",
		IssueIDs: []string{id},
	})
}

// readSignoffSummaryFile reads and validates a summary (- for stdin).
func readSignoffSummaryFile(path string) (string, error) {
	raw, err := readBodyFile(path)
	if err != nil {
		return "", fmt.Errorf("reading --signoff-file: %w", err)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("--signoff-file %s is empty", path)
	}
	if strings.HasPrefix(raw, "{") && !json.Valid([]byte(raw)) {
		return "", fmt.Errorf("--signoff-file %s is not valid JSON", path)
	}
	return raw, nil
}

// signoffRequiredByConfig reports whether config signoff.types or
// signoff.labels name this bead.
func signoffRequiredByConfig(d *types.IssueDetails) bool {
	if containsString(configList(cfgSignoffTypes, nil), string(d.IssueType)) {
		return true
	}
	wanted := configList(cfgSignoffLabels, nil)
	for _, l := range d.Labels {
		if containsString(wanted, l) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// bd close integration

type closeSignoffItem struct {
	id    string
	state string
	note  string
}

type closeSignoffPlan struct {
	items   []closeSignoffItem
	summary string // --signoff-file body, added before the close
}

func registerCloseSignoffFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("signoff-needed", false, "Mark the closed issue as awaiting the user's sign-off (needs a 'bd-kind: signoff' summary comment, or --signoff-file)")
	cmd.Flags().String("signoff-file", "", "Add this sign-off summary (JSON with what_changed, calls_made, removed_or_renamed, where_to_see; - for stdin) and mark the issue awaiting sign-off")
	cmd.Flags().String("no-signoff", "", "Close without a sign-off where config signoff.types/signoff.labels require one; records this reason (state waived)")
}

// closeSignoffIDs mirrors resolveCloseReasons' one argument rewrite: `bd done
// <id> <message>` treats the last argument as the reason.
func closeSignoffIDs(cmd *cobra.Command, args []string) []string {
	if cmd.CalledAs() == "done" && len(args) >= 2 {
		reasonFlag := false
		for _, name := range []string{"reason", "resolution", "message", "comment", "reason-file"} {
			reasonFlag = reasonFlag || cmd.Flags().Changed(name)
		}
		if !reasonFlag {
			return args[:len(args)-1]
		}
	}
	return args
}

// planCloseSignoff decides, before anything is closed, which beads leave the
// close awaiting sign-off, and refuses a close that config or a flag says
// needs a summary when none exists. It returns nil when sign-off is not in play.
func planCloseSignoff(ctx context.Context, cmd *cobra.Command, args []string) (*closeSignoffPlan, error) {
	needed, _ := cmd.Flags().GetBool("signoff-needed")
	file, _ := cmd.Flags().GetString("signoff-file")
	waiveReason, _ := cmd.Flags().GetString("no-signoff")
	waived := cmd.Flags().Changed("no-signoff")
	configured := len(configList(cfgSignoffTypes, nil)) > 0 || len(configList(cfgSignoffLabels, nil)) > 0
	if !needed && file == "" && !waived && !configured {
		return nil, nil
	}
	if waived && (needed || file != "") {
		return nil, fmt.Errorf("--no-signoff cannot be combined with --signoff-needed or --signoff-file")
	}
	if waived && strings.TrimSpace(waiveReason) == "" {
		return nil, fmt.Errorf("--no-signoff needs a reason")
	}
	ids := closeSignoffIDs(cmd, args)
	if len(ids) == 0 {
		return nil, nil
	}
	plan := &closeSignoffPlan{}
	if file != "" {
		if len(ids) != 1 {
			return nil, fmt.Errorf("--signoff-file takes exactly one issue ID")
		}
		if reasonFile, _ := cmd.Flags().GetString("reason-file"); file == "-" && reasonFile == "-" {
			return nil, fmt.Errorf("--signoff-file and --reason-file cannot both read stdin")
		}
		summary, err := readSignoffSummaryFile(file)
		if err != nil {
			return nil, err
		}
		plan.summary = summary
	}
	rd, err := openIssueReader()
	if err != nil {
		return nil, err
	}
	for _, raw := range ids {
		id, err := resolveLabelTarget(ctx, raw)
		if err != nil {
			// The close reports unresolvable ids its own way.
			continue
		}
		d, err := rd.Get(ctx, issueops.GetRequest{ID: id, IncludeComments: true, BriefDeps: true})
		if err != nil {
			continue
		}
		required := needed || file != "" || signoffRequiredByConfig(d)
		switch {
		case waived:
			plan.items = append(plan.items, closeSignoffItem{id: id, state: signoffWaived, note: strings.TrimSpace(waiveReason)})
		case required:
			if c, _ := latestSignoffSummary(d.Comments); c == nil && plan.summary == "" {
				// The ID is the store's (a resolved partial ID): one shell word in the
				// command, or a placeholder named escaped after the message.
				idArg, idNote := remedy.CommandArg(runtime.GOOS, id, "<issue-id>")
				return nil, fmt.Errorf("%s needs a sign-off summary before it closes: pass --signoff-file <file|-> with %s, add one with 'bd comments add %s --kind signoff --json-file <file>', or pass --no-signoff \"<reason>\"%s",
					id, signoffSummaryExample, idArg, idNote)
			}
			plan.items = append(plan.items, closeSignoffItem{id: id, state: signoffPending})
		}
	}
	if plan.summary != "" && len(plan.items) == 1 {
		if err := addCommentAnyRoute(ctx, plan.items[0].id, currentActor(), joinCommentKind(commentKindSignoff, plan.summary)); err != nil {
			return nil, fmt.Errorf("adding sign-off summary to %s: %w", plan.items[0].id, err)
		}
	}
	return plan, nil
}

// finishCloseSignoff records the planned state on each bead the close
// actually closed. Notices go to stderr so --json output stays one document.
func finishCloseSignoff(ctx context.Context, plan *closeSignoffPlan) error {
	if plan == nil || len(plan.items) == 0 {
		return nil
	}
	rd, err := openIssueReader()
	if err != nil {
		return err
	}
	for _, item := range plan.items {
		d, err := rd.Get(ctx, issueops.GetRequest{ID: item.id, BriefDeps: true})
		if err != nil || d.Status != types.StatusClosed {
			continue
		}
		if err := applySignoffState(ctx, item.id, item.state, item.note, ""); err != nil {
			return fmt.Errorf("recording sign-off on %s: %w", item.id, err)
		}
		if !isQuiet() {
			if item.state == signoffPending {
				fmt.Fprintf(os.Stderr, "%s %s awaits the user's sign-off (bd signoff %s)\n", ui.RenderAccent("•"), item.id, item.id)
			} else {
				fmt.Fprintf(os.Stderr, "%s %s sign-off waived: %s\n", ui.RenderAccent("•"), item.id, item.note)
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// bd signoff

var signoffCmd = &cobra.Command{
	Use:     "signoff <id> [--approve | --dispute <text> | --request]",
	GroupID: "issues",
	Short:   "Review, approve or dispute a closed issue's sign-off summary",
	Long: `Review, approve or dispute a closed issue's sign-off summary.

An agent that closes an issue may have made calls on the user's behalf. It
leaves a summary (bd close --signoff-file, or a 'bd-kind: signoff' comment) and
the issue awaits the user's sign-off. With no flag, signoff prints the state
and the latest summary.

  --approve           sign it off
  --dispute "text"    open a follow-up issue (discovered-from this one, same
                      priority) carrying the text; this issue stays closed
  --request           mark an already-closed issue as awaiting sign-off

State lives in metadata (signoff, signoff_at, signoff_by, signoff_note,
signoff_followup); the needs-signoff label marks the pending ones.

Examples:
  bd list --signoff pending
  bd signoff bd-123
  bd signoff bd-123 --approve
  bd signoff bd-123 --dispute "stun and stagger must share one formula"`,
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		approve, _ := cmd.Flags().GetBool("approve")
		request, _ := cmd.Flags().GetBool("request")
		dispute, _ := cmd.Flags().GetString("dispute")
		note, _ := cmd.Flags().GetString("note")
		disputing := cmd.Flags().Changed("dispute")
		actions := 0
		for _, b := range []bool{approve, request, disputing} {
			if b {
				actions++
			}
		}
		if actions > 1 {
			return HandleErrorRespectJSON("pass only one of --approve, --dispute, --request")
		}
		if disputing && strings.TrimSpace(dispute) == "" {
			return HandleErrorRespectJSON("--dispute needs the text of the dispute")
		}
		if actions > 0 {
			CheckReadonly("signoff")
		}
		ctx := rootCtx
		id, err := resolveLabelTarget(ctx, args[0])
		if err != nil {
			return HandleErrorRespectJSON("%v", err)
		}
		rd, err := openIssueReader()
		if err != nil {
			return HandleErrorRespectJSON("%v", err)
		}
		d, err := rd.Get(ctx, issueops.GetRequest{ID: id, IncludeComments: true, BriefDeps: true})
		if err != nil {
			return HandleErrorRespectJSON("%v", err)
		}
		SetLastTouchedID(id)
		switch {
		case approve:
			if err := applySignoffState(ctx, id, signoffApproved, strings.TrimSpace(note), ""); err != nil {
				return HandleErrorRespectJSON("signoff: %s: %v", id, err)
			}
			return reportSignoff(id, signoffApproved, "", strings.TrimSpace(note))
		case request:
			if d.Status != types.StatusClosed {
				return HandleErrorRespectJSON("%s is %s; sign-off applies to closed issues", id, d.Status)
			}
			if c, _ := latestSignoffSummary(d.Comments); c == nil {
				idArg, idNote := remedy.CommandArg(runtime.GOOS, id, "<issue-id>")
				fmt.Fprintf(os.Stderr, "%s %s has no sign-off summary yet; add one with 'bd comments add %s --kind signoff --json-file <file>'%s\n", ui.RenderWarn("!"), id, idArg, idNote)
			}
			if err := applySignoffState(ctx, id, signoffPending, "", ""); err != nil {
				return HandleErrorRespectJSON("signoff: %s: %v", id, err)
			}
			return reportSignoff(id, signoffPending, "", "")
		case disputing:
			followup, err := createDisputeFollowup(ctx, d, strings.TrimSpace(dispute))
			if err != nil {
				return HandleErrorRespectJSON("signoff: creating the follow-up for %s: %v", id, err)
			}
			text := fmt.Sprintf("Sign-off disputed: %s (follow-up %s)", strings.TrimSpace(dispute), followup)
			if err := addCommentAnyRoute(ctx, id, currentActor(), text); err != nil {
				return HandleErrorRespectJSON("signoff: commenting on %s: %v", id, err)
			}
			if err := applySignoffState(ctx, id, signoffDisputed, strings.TrimSpace(dispute), followup); err != nil {
				return HandleErrorRespectJSON("signoff: %s: %v", id, err)
			}
			return reportSignoff(id, signoffDisputed, followup, strings.TrimSpace(dispute))
		}
		return showSignoff(d)
	},
}

func createDisputeFollowup(ctx context.Context, d *types.IssueDetails, text string) (string, error) {
	lifecycle, err := openIssueLifecycle()
	if err != nil {
		return "", err
	}
	opsCtx, err := issueOpsContext(ctx)
	if err != nil {
		return "", err
	}
	issueType := types.TypeTask
	if d.IssueType == types.TypeBug {
		issueType = types.TypeBug
	}
	result, err := lifecycle.Create(opsCtx, issueops.CreateRequest{
		Actor: currentActor(),
		Issue: &types.Issue{
			Title:       "Dispute: " + d.Title,
			Description: fmt.Sprintf("The user disputed the sign-off of %s (%s):\n\n%s", d.ID, d.Title, text),
			Status:      types.StatusOpen,
			Priority:    d.Priority,
			IssueType:   issueType,
		},
		Dependencies: []issueops.CreateDependency{{TargetID: d.ID, Type: types.DepDiscoveredFrom}},
		IDPrefix:     createIDPrefixOverride(),
	})
	if err != nil {
		return "", err
	}
	commandDidWrite.Store(true)
	if result.Issue == nil {
		return "", fmt.Errorf("the create reported no issue")
	}
	return result.Issue.ID, nil
}

func reportSignoff(id, state, followup, note string) error {
	if jsonOutput {
		out := map[string]interface{}{"issue_id": id, "signoff": state}
		if followup != "" {
			out["followup"] = followup
		}
		if note != "" {
			out["note"] = note
		}
		return outputJSON(out)
	}
	switch state {
	case signoffApproved:
		fmt.Printf("%s Signed off %s\n", ui.RenderPass("✓"), id)
	case signoffDisputed:
		fmt.Printf("%s Disputed %s; follow-up %s\n", ui.RenderPass("✓"), id, followup)
	default:
		fmt.Printf("%s %s awaits sign-off\n", ui.RenderPass("✓"), id)
	}
	return nil
}

type signoffView struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Status   string          `json:"status"`
	Signoff  string          `json:"signoff,omitempty"`
	At       string          `json:"signoff_at,omitempty"`
	By       string          `json:"signoff_by,omitempty"`
	Note     string          `json:"signoff_note,omitempty"`
	Followup string          `json:"signoff_followup,omitempty"`
	Author   string          `json:"summary_author,omitempty"`
	Summary  json.RawMessage `json:"summary,omitempty"`
}

func showSignoff(d *types.IssueDetails) error {
	m := metadataMap(d.Metadata)
	get := func(k string) string {
		if v, ok := m[k]; ok {
			return metadataString(v)
		}
		return ""
	}
	v := signoffView{
		ID: d.ID, Title: d.Title, Status: string(d.Status),
		Signoff: get(metaSignoff), At: get(metaSignoffAt), By: get(metaSignoffBy),
		Note: get(metaSignoffNote), Followup: get(metaSignoffFollowup),
	}
	c, body := latestSignoffSummary(d.Comments)
	if c != nil {
		v.Author = c.Author
		v.Summary = commentData(body)
	}
	if jsonOutput {
		return outputJSON(v)
	}
	state := v.Signoff
	if state == "" {
		state = "none"
	}
	fmt.Printf("%s %s [%s] sign-off: %s", d.ID, d.Title, d.Status, state)
	if v.At != "" {
		fmt.Printf(" (%s by %s)", v.At, v.By)
	}
	fmt.Println()
	if v.Note != "" {
		fmt.Printf("note: %s\n", v.Note)
	}
	if v.Followup != "" {
		fmt.Printf("follow-up: %s\n", v.Followup)
	}
	if c == nil {
		fmt.Println("No sign-off summary.")
		return nil
	}
	fmt.Printf("\nSummary by %s, %s:\n%s\n", c.Author, c.CreatedAt.UTC().Format("2006-01-02 15:04"), renderSignoffSummary(body))
	return nil
}

func init() {
	signoffCmd.Flags().Bool("approve", false, "Sign off the closed issue")
	signoffCmd.Flags().String("dispute", "", "Dispute the sign-off: opens a follow-up issue carrying this text")
	signoffCmd.Flags().Bool("request", false, "Mark a closed issue as awaiting sign-off")
	signoffCmd.Flags().String("note", "", "With --approve: a note recorded as signoff_note")
	signoffCmd.ValidArgsFunction = issueIDCompletion
	rootCmd.AddCommand(signoffCmd)

	// bd close: plan the sign-off before the close (so a missing summary
	// refuses before anything closes) and record it after, on both routes.
	registerCloseSignoffFlags(closeCmd)
	closeRunE := closeCmd.RunE
	closeCmd.RunE = func(cmd *cobra.Command, args []string) error {
		CheckReadonly("close")
		plan, err := planCloseSignoff(rootCtx, cmd, args)
		if err != nil {
			return HandleErrorRespectJSON("%v", err)
		}
		closeErr := closeRunE(cmd, args)
		if err := finishCloseSignoff(rootCtx, plan); err != nil && closeErr == nil {
			return HandleErrorRespectJSON("%v", err)
		}
		return closeErr
	}
}
