package main

// comments_kind.go: kinded (structured) comments.
//
//	bd comments add <id> --kind handoff --author planner --json-file plan.json
//	bd comments add <id> --kind handoff --json-file -          (stdin)
//	bd comments <id> --kind handoff [--last] [--json]
//
// The comments table has no kind column, so the kind is a header line at the
// start of the stored text ("bd-kind: handoff"), followed by the body. See
// agent_surface.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/issueops"
)

// kindedComment is the JSON shape of a kinded comment.
type kindedComment struct {
	ID        string          `json:"id"`
	IssueID   string          `json:"issue_id"`
	Author    string          `json:"author"`
	CreatedAt string          `json:"created_at"`
	Kind      string          `json:"kind"`
	Data      json.RawMessage `json:"data"`
}

func registerCommentKindFlags() {
	commentsAddCmd.Flags().String("kind", "", "Store a kinded comment (e.g. handoff); read back with bd comments <id> --kind <kind>")
	commentsAddCmd.Flags().String("json-file", "", "Read a JSON comment body from a file (- for stdin); requires --kind")
	commentsCmd.Flags().String("kind", "", "List only comments of this kind, as structured records")
	commentsCmd.Flags().Bool("last", false, "With --kind: return only the most recent one")
}

// kindedCommentText builds the stored text for `bd comments add --kind`. It
// returns ok=false when the invocation is a plain comment.
func kindedCommentText(cmd *cobra.Command, positional []string) (string, bool, error) {
	kind, _ := cmd.Flags().GetString("kind")
	jsonFile, _ := cmd.Flags().GetString("json-file")
	if kind == "" {
		if jsonFile != "" {
			return "", false, fmt.Errorf("--json-file requires --kind")
		}
		return "", false, nil
	}
	kind = strings.TrimSpace(kind)
	if !agentNameRe.MatchString(kind) {
		return "", false, fmt.Errorf("invalid --kind %q: must match %s", kind, agentNameRe.String())
	}
	var body string
	if jsonFile != "" {
		if len(positional) > 0 || cmd.Flags().Changed("file") {
			return "", false, fmt.Errorf("--json-file cannot be combined with comment text or --file")
		}
		raw, err := readBodyFile(jsonFile)
		if err != nil {
			return "", false, fmt.Errorf("reading --json-file: %w", err)
		}
		raw = strings.TrimSpace(raw)
		if !json.Valid([]byte(raw)) {
			return "", false, fmt.Errorf("--json-file %s is not valid JSON", jsonFile)
		}
		body = raw
	} else {
		text, err := requireTextFromSources("comment text", "use positional args, -f, or --json-file",
			cmdTextSources(cmd, positional))
		if err != nil {
			return "", false, err
		}
		body = text
	}
	return joinCommentKind(kind, body), true, nil
}

func runCommentsKind(ctx context.Context, cmd *cobra.Command, rawID, kind string) error {
	kind = strings.TrimSpace(kind)
	if !agentNameRe.MatchString(kind) {
		return HandleErrorRespectJSON("invalid --kind %q: must match %s", kind, agentNameRe.String())
	}
	last, _ := cmd.Flags().GetBool("last")
	id, err := resolveLabelTarget(ctx, rawID)
	if err != nil {
		return HandleErrorRespectJSON("%v", err)
	}
	rd, err := openIssueReader()
	if err != nil {
		return HandleErrorRespectJSON("%v", err)
	}
	details, err := rd.Get(ctx, issueops.GetRequest{ID: id, IncludeComments: true, BriefDeps: true})
	if err != nil {
		return HandleErrorRespectJSON("%v", err)
	}
	out := make([]kindedComment, 0)
	for _, c := range details.Comments {
		k, body := splitCommentKind(c.Text)
		if k != kind {
			continue
		}
		out = append(out, kindedComment{
			ID:        c.ID,
			IssueID:   c.IssueID,
			Author:    c.Author,
			CreatedAt: c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			Kind:      k,
			Data:      commentData(body),
		})
	}
	if last {
		if len(out) == 0 {
			return HandleErrorRespectJSON("no %s comments on %s", kind, id)
		}
		latest := out[len(out)-1]
		if jsonOutput {
			return outputJSON(latest)
		}
		printKindedComment(latest)
		return nil
	}
	if jsonOutput {
		return outputJSON(out)
	}
	if len(out) == 0 {
		fmt.Printf("No %s comments on %s\n", kind, id)
		return nil
	}
	for _, c := range out {
		printKindedComment(c)
	}
	return nil
}

func printKindedComment(c kindedComment) {
	fmt.Printf("[%s] %s at %s\n", c.Kind, c.Author, c.CreatedAt)
	var pretty strings.Builder
	var v interface{}
	if err := json.Unmarshal(c.Data, &v); err == nil {
		if s, ok := v.(string); ok {
			pretty.WriteString(s)
		} else if b, err := json.MarshalIndent(v, "", "  "); err == nil {
			pretty.Write(b)
		}
	}
	fmt.Println(strings.TrimRight(pretty.String(), "\n"))
	fmt.Println()
}
