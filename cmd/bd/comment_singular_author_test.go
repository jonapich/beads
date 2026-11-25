package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TestCommentShorthandSharesTheAuthorAndKindFlags pins that `bd comment` offers
// what `bd comments add` does for signing a comment: --author (with -a) and
// --kind/--json-file, described by the same help text, so the two cannot drift.
func TestCommentShorthandSharesTheAuthorAndKindFlags(t *testing.T) {
	for _, name := range []string{"author", "kind", "json-file"} {
		single := commentCmd.Flags().Lookup(name)
		plural := commentsAddCmd.Flags().Lookup(name)
		if single == nil {
			t.Errorf("bd comment has no --%s flag", name)
			continue
		}
		if plural == nil {
			t.Fatalf("bd comments add has no --%s flag; this test compares against it", name)
		}
		if single.Usage != plural.Usage {
			t.Errorf("--%s help differs: bd comment %q, bd comments add %q", name, single.Usage, plural.Usage)
		}
		if single.Shorthand != plural.Shorthand {
			t.Errorf("--%s shorthand differs: bd comment %q, bd comments add %q", name, single.Shorthand, plural.Shorthand)
		}
		if single.DefValue != plural.DefValue || single.Value.Type() != plural.Value.Type() {
			t.Errorf("--%s type/default differ: bd comment %s=%q, bd comments add %s=%q",
				name, single.Value.Type(), single.DefValue, plural.Value.Type(), plural.DefValue)
		}
	}
}

// TestCommentShorthandKindedText: the text the shorthand stores for --kind is
// the one `bd comments add --kind` stores (a kind header, then the body), and a
// plain call is left alone.
func TestCommentShorthandKindedText(t *testing.T) {
	cmd := newCommentShorthandFlagsCommand(t, "--kind", "handoff")
	text, kinded, err := kindedCommentText(cmd, []string{"all", "green"})
	if err != nil {
		t.Fatalf("kindedCommentText: %v", err)
	}
	if !kinded {
		t.Fatal("--kind on bd comment was not treated as a kinded comment")
	}
	if want := joinCommentKind("handoff", "all green"); text != want {
		t.Errorf("kinded text = %q, want %q", text, want)
	}

	plain := newCommentShorthandFlagsCommand(t)
	if _, kinded, err := kindedCommentText(plain, []string{"all", "green"}); err != nil || kinded {
		t.Errorf("a call without --kind: kinded=%v err=%v, want a plain comment", kinded, err)
	}
}

// newCommentShorthandFlagsCommand clones commentCmd's flag definitions onto a
// fresh command, so a test parses flags without touching the real command's
// state.
func newCommentShorthandFlagsCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "comment"}
	commentCmd.Flags().VisitAll(func(f *pflag.Flag) {
		switch f.Value.Type() {
		case "bool":
			cmd.Flags().BoolP(f.Name, f.Shorthand, f.DefValue == "true", f.Usage)
		case "string":
			cmd.Flags().StringP(f.Name, f.Shorthand, f.DefValue, f.Usage)
		default:
			t.Fatalf("--%s has unhandled flag type %q", f.Name, f.Value.Type())
		}
	})
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return cmd
}
