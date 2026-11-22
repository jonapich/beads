package main

// list_agent.go: the agent-memory filters of `bd list`.
//
//	--stage <name>         metadata stage equals name
//	--has-link <kind>      the issue carries at least one link of that kind
//	--awaiting-user        open issues with an awaiting_user.labels label,
//	                       the ones with a mock/artifact link ("ready to judge") first
//	--stale-claims <dur>   in_progress issues whose claim looks dead: the lease
//	                       expired more than <dur> ago, or, with no lease, no
//	                       heartbeat or update for <dur>
//
// --stage, --has-link and --awaiting-user become plain request filters (a
// metadata equality, a metadata key presence, a label-any set), so both routes
// answer them in SQL. Ordering and the stale test run on the returned page.

import (
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/issueops"
)

type listAgentFilter struct {
	awaitingUser bool
	stale        bool // --stale-claims was given (0s is a valid window)
	staleClaims  time.Duration
}

func registerListAgentFlags(cmd *cobra.Command) {
	cmd.Flags().String("stage", "", "Filter by workflow stage (metadata stage; see bd update --stage)")
	cmd.Flags().String("has-link", "", "Filter to issues with at least one typed link of this kind (see bd link add)")
	cmd.Flags().Bool("awaiting-user", false, "Open issues labelled with any of config awaiting_user.labels (default needs-decision, needs-design); issues with a mock or artifact link sort first")
	cmd.Flags().String("stale-claims", "", "In-progress issues whose claim looks abandoned: lease expired more than this long ago, or no lease and no heartbeat/update for this long (e.g. 30m, 2h)")
}

// gatherListAgentFlags folds the agent filters into the request. It runs
// before label normalization and the limit decision.
func gatherListAgentFlags(cmd *cobra.Command, in *listInput) error {
	if stage, _ := cmd.Flags().GetString("stage"); stage != "" {
		stage = strings.TrimSpace(stage)
		if !agentNameRe.MatchString(stage) {
			return HandleErrorRespectJSON("invalid --stage %q: must match %s", stage, agentNameRe.String())
		}
		if in.MetadataFields == nil {
			in.MetadataFields = map[string]string{}
		}
		if prev, ok := in.MetadataFields[metaStage]; ok && prev != stage {
			return HandleErrorRespectJSON("--stage %s conflicts with --metadata-field stage=%s", stage, prev)
		}
		in.MetadataFields[metaStage] = stage
	}
	if kind, _ := cmd.Flags().GetString("has-link"); kind != "" {
		kind = strings.TrimSpace(kind)
		if !agentNameRe.MatchString(kind) {
			return HandleErrorRespectJSON("invalid --has-link %q: must match %s", kind, agentNameRe.String())
		}
		if in.HasMetadataKey != "" {
			return HandleErrorRespectJSON("--has-link cannot be combined with --has-metadata-key")
		}
		in.HasMetadataKey = metaLinkPrefix + kind
	}
	in.agent.awaitingUser, _ = cmd.Flags().GetBool("awaiting-user")
	if in.agent.awaitingUser {
		if len(in.LabelsAny) > 0 {
			return HandleErrorRespectJSON("--awaiting-user cannot be combined with --label-any")
		}
		in.LabelsAny = configList(cfgAwaitingUserLabels, defaultAwaitingUserLabels)
	}
	if raw, _ := cmd.Flags().GetString("stale-claims"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < 0 {
			return HandleErrorRespectJSON("invalid --stale-claims %q: expected a duration such as 30m or 2h", raw)
		}
		if in.Status != "" && in.Status != string(types.StatusInProgress) {
			return HandleErrorRespectJSON("--stale-claims lists in_progress issues; it cannot be combined with --status %s", in.Status)
		}
		in.Status = string(types.StatusInProgress)
		in.agent.stale = true
		in.agent.staleClaims = d
	}
	return nil
}

// needsAllRows reports whether the page has to be fetched whole and
// trimmed here, because a filter runs after the query.
func (f listAgentFilter) needsAllRows() bool { return f.stale }

// claimIsStale applies the --stale-claims test to one row.
func claimIsStale(is *types.Issue, d time.Duration, now time.Time) bool {
	cutoff := now.Add(-d)
	if is.LeaseExpiresAt != nil {
		return is.LeaseExpiresAt.Before(cutoff)
	}
	last := is.UpdatedAt
	if is.HeartbeatAt != nil && is.HeartbeatAt.After(last) {
		last = *is.HeartbeatAt
	}
	return last.Before(cutoff)
}

// applyListAgentPage runs the post-query half of the agent filters on a page:
// the stale-claim test (then the caller's limit), and the awaiting-user order.
func applyListAgentPage(in listInput, page issueops.IssuePage) issueops.IssuePage {
	f := in.agent
	if f.stale {
		now := time.Now()
		kept := make([]*types.IssueWithCounts, 0, len(page.Items))
		for _, row := range page.Items {
			if row != nil && row.Issue != nil && claimIsStale(row.Issue, f.staleClaims, now) {
				kept = append(kept, row)
			}
		}
		page.Items = kept
		page.HasMore = false
		if in.effectiveLimit > 0 && len(page.Items) > in.effectiveLimit {
			page.Items = page.Items[:in.effectiveLimit]
			page.HasMore = true
		}
	}
	if f.awaitingUser {
		sort.SliceStable(page.Items, func(a, b int) bool {
			return readyToJudge(page.Items[a]) && !readyToJudge(page.Items[b])
		})
	}
	return page
}

func readyToJudge(row *types.IssueWithCounts) bool {
	return row != nil && row.Issue != nil && hasLinkOfKind(row.Issue.Metadata, readyToJudgeLinkKinds...)
}
