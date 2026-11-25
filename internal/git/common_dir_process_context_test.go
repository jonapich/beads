package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// subprocessCommonDir is what `git -C dir rev-parse --git-common-dir` resolves
// to, in the spelling the caller finally compares: absolute, cleaned, symlinks
// resolved.
func subprocessCommonDir(t *testing.T, dir string) string {
	t.Helper()
	cmd := gitCommand("-C", dir, "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git -C %s rev-parse --git-common-dir: %v", dir, err)
	}
	got := strings.TrimSpace(string(out))
	if !filepath.IsAbs(got) {
		abs, err := filepath.Abs(dir)
		if err != nil {
			t.Fatal(err)
		}
		got = filepath.Join(abs, got)
	}
	return resolvedForCompare(t, got)
}

func resolvedForCompare(t *testing.T, p string) string {
	t.Helper()
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p
}

func inRepoWorkingDir(t *testing.T) string {
	t.Helper()
	repo, _ := setupTestRepo(t)
	t.Chdir(repo)
	ResetCaches()
	t.Cleanup(ResetCaches)
	return repo
}

func TestCommonDirFromProcessContextAnswersInsideTheWorkTree(t *testing.T) {
	repo := inRepoWorkingDir(t)
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{repo, filepath.Join(repo, ".beads"), sub} {
		t.Run(strings.TrimPrefix(dir, repo), func(t *testing.T) {
			got, ok := CommonDirFromProcessContext(dir)
			if !ok {
				t.Fatalf("CommonDirFromProcessContext(%s) declined, want an answer", dir)
			}
			if want := subprocessCommonDir(t, dir); resolvedForCompare(t, got) != want {
				t.Fatalf("CommonDirFromProcessContext(%s) = %q, git says %q", dir, got, want)
			}
			if !filepath.IsAbs(got) {
				t.Fatalf("CommonDirFromProcessContext(%s) = %q, want an absolute path", dir, got)
			}
		})
	}
}

func TestCommonDirFromProcessContextDeclinesWhereGitWouldAnswerDifferently(t *testing.T) {
	repo := inRepoWorkingDir(t)

	t.Run("nested repository", func(t *testing.T) {
		inner := filepath.Join(repo, "inner")
		if err := os.MkdirAll(filepath.Join(inner, ".beads"), 0o750); err != nil {
			t.Fatal(err)
		}
		initCmd := gitCommand("init")
		initCmd.Dir = inner
		if out, err := initCmd.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		dir := filepath.Join(inner, ".beads")
		if got, ok := CommonDirFromProcessContext(dir); ok {
			t.Fatalf("CommonDirFromProcessContext(%s) = %q, want a decline: a nested repository owns it", dir, got)
		}
		// And git does answer differently, which is why declining is right.
		if subprocessCommonDir(t, dir) == resolvedForCompare(t, filepath.Join(repo, ".git")) {
			t.Fatal("test setup: the nested repository did not change git's answer")
		}
	})

	t.Run("outside the work tree", func(t *testing.T) {
		elsewhere := t.TempDir()
		if got, ok := CommonDirFromProcessContext(elsewhere); ok {
			t.Fatalf("CommonDirFromProcessContext(%s) = %q, want a decline", elsewhere, got)
		}
	})

	t.Run("symlinked out of the work tree", func(t *testing.T) {
		target := t.TempDir()
		link := filepath.Join(repo, "linked")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("cannot create a symlink here: %v", err)
		}
		if got, ok := CommonDirFromProcessContext(link); ok {
			t.Fatalf("CommonDirFromProcessContext(%s) = %q, want a decline: it resolves outside the tree", link, got)
		}
	})

	t.Run("missing path", func(t *testing.T) {
		missing := filepath.Join(repo, "does", "not", "exist")
		if got, ok := CommonDirFromProcessContext(missing); ok {
			t.Fatalf("CommonDirFromProcessContext(%s) = %q, want a decline", missing, got)
		}
	})
}

func TestCommonDirFromProcessContextDefersToGitEnvOverrides(t *testing.T) {
	repo := inRepoWorkingDir(t)
	for _, name := range discoveryEnvOverrides {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "")
			if got, ok := CommonDirFromProcessContext(repo); ok {
				t.Fatalf("with %s set, CommonDirFromProcessContext = %q; want it to defer to git", name, got)
			}
		})
	}
}

func TestCommonDirFromProcessContextDeclinesOutsideAnyRepository(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	ResetCaches()
	t.Cleanup(ResetCaches)
	if got, ok := CommonDirFromProcessContext(dir); ok {
		t.Fatalf("CommonDirFromProcessContext(%s) = %q outside any repository, want a decline", dir, got)
	}
}
