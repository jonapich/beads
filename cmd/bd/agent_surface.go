package main

// agent_surface.go holds the shared vocabulary of the agent-memory surface:
// stages, typed links, kinded comments and claim records. None of it adds a
// schema column. Stages, links and claim records live in issue metadata under
// the keys below, and a comment's kind rides in a header line of its text, so
// every one of them survives export, import and federation unchanged.
//
// Metadata layout (keys must match storage.ValidateMetadataKey, so no hyphens):
//
//	stage               current stage name, e.g. "code"
//	stage_changed_at    RFC3339 UTC time of the last stage change
//	stage_at.<stage>    RFC3339 UTC time the issue last entered <stage>
//	link.<kind>         [{"url","note","at","by"}], dropped when empty
//	claim.session       session that made the last --claim
//	claim.at            RFC3339 UTC time of the last --claim

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
)

const (
	metaStage          = "stage"
	metaStageChangedAt = "stage_changed_at"
	metaStageAtPrefix  = "stage_at."
	metaLinkPrefix     = "link."
	metaClaimSession   = "claim.session"
	metaClaimAt        = "claim.at"

	// commentKindHeader starts the first line of a kinded comment's text.
	commentKindHeader = "bd-kind: "
)

// Config keys (yaml-only, see config.YamlOnlyKeys).
const (
	cfgStageValues        = "stage.values"
	cfgLinkKinds          = "link.kinds"
	cfgAwaitingUserLabels = "awaiting_user.labels"
	cfgReadyExcludeLabels = "ready.exclude_labels"
)

var (
	defaultLinkKinds          = []string{"mock", "artifact", "capture", "commit", "doc"}
	defaultAwaitingUserLabels = []string{"needs-decision", "needs-design"}
	// readyToJudgeLinkKinds are the link kinds that make an awaiting-user
	// issue "ready to judge": there is something for the human to look at.
	readyToJudgeLinkKinds = []string{"mock", "artifact"}

	agentNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// configList reads a list-valued config key in either YAML-list or
// comma-separated form, falling back to def when unset.
func configList(key string, def []string) []string {
	var out []string
	for _, entry := range config.GetStringSlice(key) {
		for _, part := range strings.Split(entry, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// validateStage checks a stage name against stage.values when it is set, and
// against the metadata key alphabet otherwise.
func validateStage(stage string) error {
	if !agentNameRe.MatchString(stage) {
		return fmt.Errorf("invalid stage %q: must match %s", stage, agentNameRe.String())
	}
	if allowed := configList(cfgStageValues, nil); len(allowed) > 0 && !containsString(allowed, stage) {
		return fmt.Errorf("invalid stage %q: %s allows %s", stage, cfgStageValues, strings.Join(allowed, ", "))
	}
	return nil
}

// stageMetadataEdits returns the --set-metadata pairs a stage change writes.
func stageMetadataEdits(stage string, now time.Time) []string {
	ts := now.UTC().Format(time.RFC3339)
	return []string{
		metaStage + "=" + stage,
		metaStageChangedAt + "=" + ts,
		metaStageAtPrefix + stage + "=" + ts,
	}
}

// claimMetadataEdits returns the --set-metadata pairs a --claim by session
// writes.
func claimMetadataEdits(session string, now time.Time) []string {
	return []string{
		metaClaimSession + "=" + session,
		metaClaimAt + "=" + now.UTC().Format(time.RFC3339),
	}
}

// agentUpdateMetadataEdits turns bd update's --stage, and --claim with a
// known session, into extra --set-metadata pairs. Both routes of bd update
// append them to the user's own pairs, so they land in the same patch.
func agentUpdateMetadataEdits(cmd *cobra.Command) ([]string, error) {
	var edits []string
	now := time.Now()
	if cmd.Flags().Changed("stage") {
		stage, _ := cmd.Flags().GetString("stage")
		stage = strings.TrimSpace(stage)
		if err := validateStage(stage); err != nil {
			return nil, err
		}
		edits = append(edits, stageMetadataEdits(stage, now)...)
	}
	if claim, _ := cmd.Flags().GetBool("claim"); claim {
		session, _ := cmd.Flags().GetString("session")
		if session == "" {
			session = os.Getenv("CLAUDE_SESSION_ID")
		}
		if session = strings.TrimSpace(session); session != "" {
			edits = append(edits, claimMetadataEdits(session, now)...)
		}
	}
	return edits, nil
}

// metadataMap decodes issue metadata into raw top-level values. A missing or
// non-object value reads as empty.
func metadataMap(meta json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	if len(meta) == 0 {
		return out
	}
	_ = json.Unmarshal(meta, &out)
	return out
}

// metadataString returns a metadata value as a plain string: a JSON string is
// unquoted, anything else is returned as compact JSON.
func metadataString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}

// issueStage returns the issue's stage and when it changed, both possibly empty.
func issueStage(meta json.RawMessage) (stage, changedAt string) {
	m := metadataMap(meta)
	if v, ok := m[metaStage]; ok {
		stage = metadataString(v)
	}
	if v, ok := m[metaStageChangedAt]; ok {
		changedAt = metadataString(v)
	}
	return stage, changedAt
}

// typedLink is one entry of a link.<kind> metadata array. Kind is not stored
// in the entry (the key names it) but is filled in on the way out.
type typedLink struct {
	Kind string `json:"kind,omitempty"`
	URL  string `json:"url"`
	Note string `json:"note,omitempty"`
	At   string `json:"at,omitempty"`
	By   string `json:"by,omitempty"`
}

func validateLinkKind(kind string) error {
	if !agentNameRe.MatchString(kind) {
		return fmt.Errorf("invalid link kind %q: must match %s", kind, agentNameRe.String())
	}
	if allowed := configList(cfgLinkKinds, defaultLinkKinds); !containsString(allowed, kind) {
		return fmt.Errorf("invalid link kind %q: %s allows %s", kind, cfgLinkKinds, strings.Join(allowed, ", "))
	}
	return nil
}

// issueLinks returns every typed link on the issue, ordered by kind and then
// by insertion order within a kind.
func issueLinks(meta json.RawMessage) []typedLink {
	m := metadataMap(meta)
	kinds := make([]string, 0)
	for key := range m {
		if strings.HasPrefix(key, metaLinkPrefix) {
			kinds = append(kinds, strings.TrimPrefix(key, metaLinkPrefix))
		}
	}
	sort.Strings(kinds)
	var out []typedLink
	for _, kind := range kinds {
		for _, l := range linksOfKind(m, kind) {
			l.Kind = kind
			out = append(out, l)
		}
	}
	return out
}

func linksOfKind(m map[string]json.RawMessage, kind string) []typedLink {
	raw, ok := m[metaLinkPrefix+kind]
	if !ok {
		return nil
	}
	var links []typedLink
	if err := json.Unmarshal(raw, &links); err != nil {
		return nil
	}
	for i := range links {
		links[i].Kind = ""
	}
	return links
}

func hasLinkOfKind(meta json.RawMessage, kinds ...string) bool {
	m := metadataMap(meta)
	for _, kind := range kinds {
		if len(linksOfKind(m, kind)) > 0 {
			return true
		}
	}
	return false
}

// splitCommentKind separates a kinded comment's header from its body. A
// comment without the header has kind "" and its whole text as body.
func splitCommentKind(text string) (kind, body string) {
	if !strings.HasPrefix(text, commentKindHeader) {
		return "", text
	}
	rest := text[len(commentKindHeader):]
	line, body, _ := strings.Cut(rest, "\n")
	kind = strings.TrimSpace(line)
	if !agentNameRe.MatchString(kind) {
		return "", text
	}
	return kind, body
}

// joinCommentKind builds the stored text of a kinded comment.
func joinCommentKind(kind, body string) string {
	return commentKindHeader + kind + "\n" + body
}

// commentData returns a kinded comment body as JSON: parsed when it is valid
// JSON, otherwise as a JSON string.
func commentData(body string) json.RawMessage {
	trimmed := strings.TrimSpace(body)
	if trimmed != "" && json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	b, _ := json.Marshal(body)
	return b
}
