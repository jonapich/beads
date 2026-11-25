package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestCommandGrantsLease pins which invocations read lease.ttl in the root
// pre-run: exactly the ones that grant or renew a claim lease.
func TestCommandGrantsLease(t *testing.T) {
	build := func(name string, flags ...string) *cobra.Command {
		cmd := &cobra.Command{Use: name}
		for _, f := range flags {
			cmd.Flags().Bool(f, false, "")
		}
		return cmd
	}
	set := func(cmd *cobra.Command, flags ...string) *cobra.Command {
		for _, f := range flags {
			if err := cmd.Flags().Set(f, "true"); err != nil {
				t.Fatalf("set --%s: %v", f, err)
			}
		}
		return cmd
	}

	tests := []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		{"heartbeat", build("heartbeat"), true},
		{"update with --claim", set(build("update", "claim"), "claim"), true},
		{"update without --claim", build("update", "claim"), false},
		{"ready with --claim", set(build("ready", "claim"), "claim"), true},
		{"ready without --claim", build("ready", "claim"), false},
		{"close --continue claims the next step", set(build("close", "continue", "no-auto"), "continue"), true},
		{"close --continue --no-auto does not claim", set(build("close", "continue", "no-auto"), "continue", "no-auto"), false},
		{"plain close", build("close", "continue", "no-auto"), false},
		{"reclaim", build("reclaim"), false},
		{"list", build("list"), false},
	}
	for _, tc := range tests {
		if got := commandGrantsLease(tc.cmd); got != tc.want {
			t.Errorf("%s: commandGrantsLease = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestApplyLeaseTTLLeavesOtherCommandsAlone pins that a command that never
// touches a lease does no settings read at all: the root context stays as it
// was, with no store open, no provider and no error.
func TestApplyLeaseTTLLeavesOtherCommandsAlone(t *testing.T) {
	oldCtx := rootCtx
	t.Cleanup(func() { rootCtx = oldCtx })
	rootCtx = t.Context()
	before := rootCtx

	if err := applyLeaseTTLToRootContext(&cobra.Command{Use: "list"}); err != nil {
		t.Fatalf("applyLeaseTTLToRootContext(list) = %v, want nil", err)
	}
	if rootCtx != before {
		t.Fatalf("applyLeaseTTLToRootContext(list) replaced the root context")
	}
}
