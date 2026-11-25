package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// linkedWorktree adds a linked worktree of repo (beside it, on its own branch)
// and returns its path. The fixture repo has one commit and a tracked .beads,
// so the worktree has a .beads of its own.
func linkedWorktree(t *testing.T, repo string) string {
	t.Helper()
	wt := filepath.Join(filepath.Dir(repo), "linked-wt")
	add := gitCommand("worktree", "add", "-q", "-b", "wt-branch", wt)
	add.Dir = repo
	if out, err := add.CombinedOutput(); err != nil {
		t.Skipf("git worktree add is not available here: %v\n%s", err, out)
	}
	return wt
}

// TestCommonDirFromProcessContextInALinkedWorktree: from a linked worktree git's
// common directory is the MAIN repository's .git, not the worktree's own
// .git/worktrees/<name>. The in-process answer must be the one git gives, for
// the worktree root, its .beads and a subdirectory.
func TestCommonDirFromProcessContextInALinkedWorktree(t *testing.T) {
	repo, _ := setupTestRepo(t)
	wt := linkedWorktree(t, repo)
	t.Chdir(wt)
	ResetCaches()
	t.Cleanup(ResetCaches)

	sub := filepath.Join(wt, "a", "b")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	mainGitDir := resolvedForCompare(t, filepath.Join(repo, ".git"))

	for _, dir := range []string{wt, filepath.Join(wt, ".beads"), sub} {
		t.Run(strings.TrimPrefix(dir, wt), func(t *testing.T) {
			got, ok := CommonDirFromProcessContext(dir)
			if !ok {
				t.Fatalf("CommonDirFromProcessContext(%s) declined, want an answer", dir)
			}
			want := subprocessCommonDir(t, dir)
			if resolvedForCompare(t, got) != want {
				t.Fatalf("CommonDirFromProcessContext(%s) = %q, git says %q", dir, got, want)
			}
			if want != mainGitDir {
				t.Fatalf("test setup: git's common dir from the worktree is %q, want the main repository's %q", want, mainGitDir)
			}
		})
	}

	// The main checkout is a different work tree from the one this process is in:
	// its common dir is the same directory, but the in-process context describes
	// the worktree, so a path there is not answered from it.
	if got, ok := CommonDirFromProcessContext(repo); ok {
		t.Errorf("CommonDirFromProcessContext(%s) = %q from inside the linked worktree, want a decline for the other work tree", repo, got)
	}
}

// TestCommonDirFromProcessContextInTheMainCheckoutOfAWorktreeRepo: with a linked
// worktree present, the main checkout still answers for itself, and declines for
// the worktree (another work tree with its own .git file).
func TestCommonDirFromProcessContextInTheMainCheckoutOfAWorktreeRepo(t *testing.T) {
	repo, _ := setupTestRepo(t)
	wt := linkedWorktree(t, repo)
	t.Chdir(repo)
	ResetCaches()
	t.Cleanup(ResetCaches)

	got, ok := CommonDirFromProcessContext(filepath.Join(repo, ".beads"))
	if !ok {
		t.Fatal("CommonDirFromProcessContext(main .beads) declined, want an answer")
	}
	if want := subprocessCommonDir(t, filepath.Join(repo, ".beads")); resolvedForCompare(t, got) != want {
		t.Errorf("main checkout: got %q, git says %q", got, want)
	}
	if got, ok := CommonDirFromProcessContext(wt); ok {
		t.Errorf("CommonDirFromProcessContext(%s) = %q from the main checkout, want a decline for the other work tree", wt, got)
	}
}
