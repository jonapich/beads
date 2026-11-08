//go:build cgo

package main

import (
	"os/exec"
	"strings"
	"testing"
)

const doltInfoBranchRef = "refs/heads/__dolt_remote_info__"

// Runs the real command against real git remotes, because only that shows
// whether bd dolt push consults the check and whether Dolt honours the variable
// bd sets. Not gated on BEADS_TEST_EMBEDDED_DOLT, so `docker buildx bake test`
// always runs it.
func TestEmbeddedDoltPushToGitRemote(t *testing.T) {
	t.Parallel()
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "ps")

	origin := setupBareGitRemote(t)
	bdDolt(t, bd, dir, "remote", "add", "origin", "git+file://"+origin)

	push := func(t *testing.T, env []string, args ...string) string {
		t.Helper()
		cmd := exec.Command(bd, append([]string{"dolt", "push"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(envWithout(bdEnv(dir), doltRemoteInfoBranchEnv), env...)
		stdout, stderr, err := runCommandBuffers(t, cmd)
		if err != nil {
			t.Fatalf("bd dolt push %s failed: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
		}
		return stdout.String()
	}
	wantPushed := func(t *testing.T, env []string, args ...string) {
		t.Helper()
		if out := push(t, env, args...); strings.Contains(out, pushUpToDateMessage) || !strings.Contains(out, "Pushing to Dolt remote") {
			t.Fatalf("bd dolt push %v should have pushed, got:\n%s", args, out)
		}
	}
	wantSkipped := func(t *testing.T, args ...string) {
		t.Helper()
		if out := push(t, nil, args...); !strings.Contains(out, pushUpToDateMessage) {
			t.Fatalf("bd dolt push %v should have been skipped, got:\n%s", args, out)
		}
	}

	t.Run("default remote", func(t *testing.T) {
		wantPushed(t, nil)
		first := lsRemoteRef(t, origin, gitDoltDataRef)
		if first == "" {
			t.Fatalf("push did not create %s", gitDoltDataRef)
		}
		if got := lsRemoteRef(t, origin, doltInfoBranchRef); got != "" {
			t.Fatalf("push created the info branch at %s, want it off by default", got)
		}

		wantSkipped(t)
		if got := lsRemoteRef(t, origin, gitDoltDataRef); got != first {
			t.Fatalf("a skipped push moved %s from %s to %s", gitDoltDataRef, first, got)
		}
		wantPushed(t, nil, "--force")

		bdCreate(t, bd, dir, "needs pushing", "--type", "task")
		wantPushed(t, nil)
		if got := lsRemoteRef(t, origin, gitDoltDataRef); got == first {
			t.Fatalf("a push with new data left %s at %s", gitDoltDataRef, got)
		}
		wantSkipped(t)
	})

	t.Run("named remote", func(t *testing.T) {
		second := setupBareGitRemote(t)
		bdDolt(t, bd, dir, "remote", "add", "second", "git+file://"+second)

		// The default remote being current says nothing about this one.
		wantPushed(t, []string{doltRemoteInfoBranchEnv + "=custom-info"}, "second")
		if got := lsRemoteRef(t, second, "refs/heads/custom-info"); got == "" {
			t.Fatal("a value set in DOLT_REMOTE_INFO_BRANCH was not honoured")
		}

		wantSkipped(t, "second")
		wantPushed(t, nil, "second", "--force")
	})
}
