package main

// link_typed.go: typed links on an issue (mocks, artifacts, captures, commits,
// docs), stored as metadata link.<kind> arrays (see agent_surface.go).
//
//	bd link add  <id> --kind mock --url <u> [--note "..."]
//	bd link list <id> [--kind mock]
//	bd link rm   <id> --url <u> [--kind mock]
//
// The plain `bd link <id1> <id2>` dependency shorthand is unchanged: cobra
// picks a subcommand only when the first argument names one.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/issueops"
)

var linkAddCmd = &cobra.Command{
	Use:   "add <id> --kind <kind> --url <url> [--note <text>]",
	Short: "Attach a typed link (mock, artifact, capture, commit, doc) to an issue",
	Long: `Attach a typed link to an issue.

Links are stored in the issue's metadata as link.<kind> arrays. Allowed kinds
come from config link.kinds (default: mock, artifact, capture, commit, doc).
Adding a URL that is already linked under the same kind updates its note.

Examples:
  bd link add bd-123 --kind mock --url https://example.com/mock.png --note "v2, dark theme"
  bd link add bd-123 --kind commit --url abc1234
  bd list --has-link mock`,
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		CheckReadonly("link add")
		kind, _ := cmd.Flags().GetString("kind")
		url, _ := cmd.Flags().GetString("url")
		note, _ := cmd.Flags().GetString("note")
		kind, url = strings.TrimSpace(kind), strings.TrimSpace(url)
		if err := validateLinkKind(kind); err != nil {
			return HandleErrorRespectJSON("%v", err)
		}
		if url == "" {
			return HandleErrorRespectJSON("--url is required")
		}
		return runLinkEdit(rootCtx, args[0], kind, func(links []typedLink) ([]typedLink, typedLink, string) {
			entry := typedLink{URL: url, Note: strings.TrimSpace(note), At: time.Now().UTC().Format(time.RFC3339), By: currentActor()}
			for i, l := range links {
				if l.URL == url {
					if !cmd.Flags().Changed("note") {
						entry.Note = l.Note
					}
					links[i] = entry
					return links, entry, "updated"
				}
			}
			return append(links, entry), entry, "added"
		})
	},
}

var linkRmCmd = &cobra.Command{
	Use:           "rm <id> --url <url> [--kind <kind>]",
	Aliases:       []string{"remove"},
	Short:         "Remove a typed link from an issue",
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		CheckReadonly("link rm")
		kind, _ := cmd.Flags().GetString("kind")
		url, _ := cmd.Flags().GetString("url")
		kind, url = strings.TrimSpace(kind), strings.TrimSpace(url)
		if url == "" {
			return HandleErrorRespectJSON("--url is required")
		}
		if kind != "" && !agentNameRe.MatchString(kind) {
			return HandleErrorRespectJSON("invalid --kind %q: must match %s", kind, agentNameRe.String())
		}
		return runLinkRemove(rootCtx, args[0], kind, url)
	},
}

var linkListCmd = &cobra.Command{
	Use:           "list <id> [--kind <kind>]",
	Short:         "List an issue's typed links",
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		kind, _ := cmd.Flags().GetString("kind")
		ctx := rootCtx
		id, err := resolveLabelTarget(ctx, args[0])
		if err != nil {
			return HandleErrorRespectJSON("%v", err)
		}
		rd, err := openIssueReader()
		if err != nil {
			return HandleErrorRespectJSON("%v", err)
		}
		details, err := rd.Get(ctx, issueops.GetRequest{ID: id, BriefDeps: true})
		if err != nil {
			return HandleErrorRespectJSON("%v", err)
		}
		links := make([]typedLink, 0)
		for _, l := range issueLinks(details.Metadata) {
			if kind == "" || l.Kind == kind {
				links = append(links, l)
			}
		}
		if jsonOutput {
			return outputJSON(links)
		}
		if len(links) == 0 {
			fmt.Printf("No links on %s\n", id)
			return nil
		}
		for _, l := range links {
			printTypedLink(l)
		}
		return nil
	},
}

func printTypedLink(l typedLink) {
	if l.Note != "" {
		fmt.Printf("%s %s — %s\n", l.Kind, l.URL, l.Note)
	} else {
		fmt.Printf("%s %s\n", l.Kind, l.URL)
	}
}

// writeLinkKinds replaces the named link.<kind> arrays in one metadata patch,
// dropping a key whose array is empty. The patch is applied against the row
// re-read inside the write transaction, so it can only race another edit of
// the same link.<kind> key.
func writeLinkKinds(ctx context.Context, id string, byKind map[string][]typedLink) error {
	lifecycle, err := openIssueLifecycle()
	if err != nil {
		return err
	}
	ctx, err = issueOpsContext(ctx)
	if err != nil {
		return err
	}
	patch := issueops.IssuePatch{}
	patch.Metadata.Set = map[string]json.RawMessage{}
	for kind, links := range byKind {
		key := metaLinkPrefix + kind
		if len(links) == 0 {
			patch.Metadata.Unset = append(patch.Metadata.Unset, key)
			continue
		}
		raw, err := json.Marshal(links)
		if err != nil {
			return err
		}
		patch.Metadata.Set[key] = raw
	}
	if len(patch.Metadata.Set) == 0 {
		patch.Metadata.Set = nil
	}
	if _, err := lifecycle.Update(ctx, issueops.UpdateRequest{Actor: currentActor(), IssueID: id, Patch: patch}); err != nil {
		return err
	}
	commandDidWrite.Store(true)
	return nil
}

func readIssueMetadataForLinks(ctx context.Context, rawID string) (string, map[string]json.RawMessage, error) {
	id, err := resolveLabelTarget(ctx, rawID)
	if err != nil {
		return "", nil, err
	}
	rd, err := openIssueReader()
	if err != nil {
		return "", nil, err
	}
	details, err := rd.Get(ctx, issueops.GetRequest{ID: id, BriefDeps: true})
	if err != nil {
		return "", nil, err
	}
	return id, metadataMap(details.Metadata), nil
}

func runLinkEdit(ctx context.Context, rawID, kind string, edit func([]typedLink) ([]typedLink, typedLink, string)) error {
	id, meta, err := readIssueMetadataForLinks(ctx, rawID)
	if err != nil {
		return HandleErrorRespectJSON("%v", err)
	}
	links, entry, status := edit(linksOfKind(meta, kind))
	if err := writeLinkKinds(ctx, id, map[string][]typedLink{kind: links}); err != nil {
		return HandleErrorRespectJSON("link add: %s: %v", id, err)
	}
	SetLastTouchedID(id)
	last := entry
	last.Kind = kind
	if jsonOutput {
		return outputJSON(map[string]interface{}{"status": status, "issue_id": id, "link": last})
	}
	fmt.Printf("%s Link %s on %s: ", ui.RenderPass("✓"), status, id)
	printTypedLink(last)
	return nil
}

func runLinkRemove(ctx context.Context, rawID, kind, url string) error {
	id, meta, err := readIssueMetadataForLinks(ctx, rawID)
	if err != nil {
		return HandleErrorRespectJSON("%v", err)
	}
	changed := map[string][]typedLink{}
	removed := make([]typedLink, 0)
	for key := range meta {
		k, ok := strings.CutPrefix(key, metaLinkPrefix)
		if !ok || (kind != "" && k != kind) {
			continue
		}
		links := linksOfKind(meta, k)
		kept := links[:0:0]
		for _, l := range links {
			if l.URL == url {
				l.Kind = k
				removed = append(removed, l)
				continue
			}
			kept = append(kept, l)
		}
		if len(kept) != len(links) {
			changed[k] = kept
		}
	}
	if len(removed) == 0 {
		return HandleErrorRespectJSON("no link with url %q on %s", url, id)
	}
	if err := writeLinkKinds(ctx, id, changed); err != nil {
		return HandleErrorRespectJSON("link rm: %s: %v", id, err)
	}
	SetLastTouchedID(id)
	if jsonOutput {
		return outputJSON(map[string]interface{}{"status": "removed", "issue_id": id, "removed": removed})
	}
	for _, l := range removed {
		fmt.Printf("%s Link removed from %s: ", ui.RenderPass("✓"), id)
		printTypedLink(l)
	}
	return nil
}

func init() {
	linkAddCmd.Flags().String("kind", "", "Link kind (config link.kinds; default mock, artifact, capture, commit, doc)")
	linkAddCmd.Flags().String("url", "", "Link target (URL, path, or commit hash)")
	linkAddCmd.Flags().String("note", "", "Short note shown with the link")
	linkRmCmd.Flags().String("kind", "", "Only remove the link under this kind")
	linkRmCmd.Flags().String("url", "", "URL of the link to remove")
	linkListCmd.Flags().String("kind", "", "Only list links of this kind")
	for _, c := range []*cobra.Command{linkAddCmd, linkRmCmd, linkListCmd} {
		c.ValidArgsFunction = issueIDCompletion
		linkCmd.AddCommand(c)
	}
}
