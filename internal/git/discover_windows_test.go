package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests run on a Windows host (`go test ./internal/git/...`): the bake
// pipeline builds and tests on Linux only, so this is the lane that proves the
// Windows discovery (discover_windows.go) against real Git for Windows. Every
// case compares the in-process answer with what `git rev-parse` prints.

func winFixtureEnv(t *testing.T) []string {
	t.Helper()
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			continue
		}
		env = append(env, kv)
	}
	empty := filepath.Join(t.TempDir(), "empty.gitconfig")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+empty,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
}

func winGit(t *testing.T, env []string, dir string, args ...string) string {
	t.Helper()
	cmd := gitCommand(args...)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// winRevParse is the reference answer: what git prints, raw.
func winRevParse(t *testing.T, env []string, dir string) revParseResult {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--git-dir", "--git-common-dir", "--show-toplevel")
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.Output()
	if err != nil {
		return revParseResult{notRepo: true}
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 3 {
		t.Fatalf("git rev-parse in %s: unexpected output %q", dir, out)
	}
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return revParseResult{gitDir: lines[0], commonDir: lines[1], topLevel: lines[2]}
}

// sameRawRevParse compares two raw rev-parse answers EXACTLY, case included: both
// are in git's spelling (forward slashes, true case), and the code downstream string-compares
// these paths, so the probe must print what git prints, not the caller's spelling.
func sameRawRevParse(got, want revParseResult) bool {
	return got.notRepo == want.notRepo && got.gitDir == want.gitDir &&
		got.commonDir == want.commonDir && got.topLevel == want.topLevel
}

// winAssertSameAsGit checks the context startup derives for dir (in-process when
// discovery answers) against the one derived from the git subprocess.
func winAssertSameAsGit(t *testing.T, env []string, dir string) {
	t.Helper()
	var got gitContext
	if raw, ok := discoverGitFrom(dir); ok {
		got = gitContextFromRevParse(dir, raw)
	} else {
		got = loadGitContext(dir, env)
	}
	want := loadGitContext(dir, env)
	if (got.err != nil) != (want.err != nil) {
		t.Fatalf("in %s: startup err %v, git err %v", dir, got.err, want.err)
	}
	if got.err != nil {
		return
	}
	if got.gitDirRaw != want.gitDirRaw || got.commonDir != want.commonDir ||
		got.repoRoot != want.repoRoot || got.isWorktree != want.isWorktree {
		t.Fatalf("in %s: startup %+v, git %+v", dir, got, want)
	}
}

type winDiscoveryFixture struct {
	root, main, worktree, submodule, nested, junction string
	env                                               []string
}

func newWinDiscoveryFixture(t *testing.T) winDiscoveryFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := winFixtureEnv(t)
	f := winDiscoveryFixture{
		root:     root,
		main:     filepath.Join(root, "main"),
		worktree: filepath.Join(root, "wt"),
		junction: filepath.Join(root, "junction"),
		env:      env,
	}
	f.submodule = filepath.Join(f.main, "sub")
	f.nested = filepath.Join(f.main, "a", "inner")
	subSrc := filepath.Join(root, "subsrc")
	for _, dir := range []string{f.main, subSrc, filepath.Join(f.main, "a", "b"), f.nested, filepath.Join(root, "norepo", "x")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{f.main, subSrc} {
		winGit(t, env, dir, "init", "-q")
		winGit(t, env, dir, "commit", "-q", "--allow-empty", "-m", "base")
	}
	winGit(t, env, f.main, "worktree", "add", "-q", f.worktree)
	winGit(t, env, f.main, "-c", "protocol.file.allow=always", "submodule", "add", "-q", subSrc, "sub")
	winGit(t, env, f.nested, "init", "-q")
	for _, dir := range []string{filepath.Join(f.worktree, "w"), filepath.Join(f.submodule, "s"), filepath.Join(f.nested, "deep")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	// A directory junction needs no privilege, unlike a symlink.
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", f.junction, f.main).CombinedOutput(); err != nil {
		t.Logf("junction not available (%v: %s); the junction cases are skipped", err, out)
		f.junction = ""
	}
	return f
}

func winRepoWithConfig(t *testing.T, env []string, config string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "r")
	winGit(t, env, t.TempDir(), "init", "-q", dir)
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

const winPlainRepoConfig = "[core]\n\trepositoryformatversion = 0\n\tfilemode = false\n\tbare = false\n\tlogallrefupdates = true\n\tsymlinks = false\n\tignorecase = true\n"

// TestWindowsDiscoverGitFromMatchesRevParse is the equivalence contract on
// Windows: wherever in-process discovery answers, its answer is what `git
// rev-parse --git-dir --git-common-dir --show-toplevel` prints there (paths
// compared as Windows paths), and the context startup derives from it is the one
// derived from git.
func TestWindowsDiscoverGitFromMatchesRevParse(t *testing.T) {
	f := newWinDiscoveryFixture(t)
	cases := []struct{ name, dir string }{
		{"repo top", f.main},
		{"repo subdir", filepath.Join(f.main, "a", "b")},
		{"repo first-level subdir", filepath.Join(f.main, "a")},
		{"linked worktree top", f.worktree},
		{"linked worktree subdir", filepath.Join(f.worktree, "w")},
		{"submodule top", f.submodule},
		{"submodule subdir", filepath.Join(f.submodule, "s")},
		{"nested repo top", f.nested},
		{"nested repo subdir", filepath.Join(f.nested, "deep")},
		{"upper-case spelling of a subdir", strings.ToUpper(filepath.Join(f.main, "a", "b"))},
		{"lower-case spelling of the top", strings.ToLower(f.main)},
		{"forward slashes", filepath.ToSlash(filepath.Join(f.main, "a", "b"))},
		{"no repository", filepath.Join(f.root, "norepo", "x")},
		{"config with a UTF-8 BOM", winRepoWithConfig(t, f.env, "\xef\xbb\xbf"+winPlainRepoConfig)},
		{"config with CRLF and comments", winRepoWithConfig(t, f.env,
			"# leading comment\r\n[core] ; header comment\r\n\tbare = false # trailing\r\n[remote \"origin\"]\r\n\turl = \"/x y\"\r\n")},
	}
	if f.junction != "" {
		cases = append(cases,
			struct{ name, dir string }{"junction to the repo top", f.junction},
			struct{ name, dir string }{"junction to a repo subdir", filepath.Join(f.junction, "a", "b")},
		)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := discoverGitFrom(tc.dir)
			if !ok {
				t.Fatalf("discoverGitFrom(%s) declined; want an in-process answer", tc.dir)
			}
			if want := winRevParse(t, f.env, tc.dir); !sameRawRevParse(got, want) {
				t.Fatalf("discoverGitFrom(%s) = %+v, git rev-parse = %+v", tc.dir, got, want)
			}
			winAssertSameAsGit(t, f.env, tc.dir)
		})
	}
}

// TestWindowsDiscoverGitFromDeclinesUnmodeledLayouts: the layouts that go to git,
// as on Linux, with git's answer then used.
func TestWindowsDiscoverGitFromDeclinesUnmodeledLayouts(t *testing.T) {
	f := newWinDiscoveryFixture(t)
	repoWithConfig := func(t *testing.T, args ...string) string {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "r")
		winGit(t, f.env, t.TempDir(), "init", "-q", dir)
		winGit(t, f.env, dir, append([]string{"config"}, args...)...)
		return dir
	}
	mkdir := func(t *testing.T, parts ...string) string {
		t.Helper()
		dir := filepath.Join(parts...)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	invalidGitfile := mkdir(t, t.TempDir(), "badfile")
	if err := os.WriteFile(filepath.Join(invalidGitfile, ".git"), []byte("not a gitfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(t.TempDir(), "bare.git")
	winGit(t, f.env, t.TempDir(), "init", "-q", "--bare", bare)
	bareSub := mkdir(t, bare, "sub")
	commondirInDotGit := repoWithConfig(t, "core.filemode", "false")
	if err := os.WriteFile(filepath.Join(commondirInDotGit, ".git", "commondir"), []byte(".\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, dir string }{
		{"core.worktree", repoWithConfig(t, "core.worktree", t.TempDir())},
		{"core.bare", repoWithConfig(t, "core.bare", "true")},
		{"extensions.worktreeConfig", repoWithConfig(t, "extensions.worktreeConfig", "true")},
		{"include directive", repoWithConfig(t, "include.path", "NUL")},
		{"malformed key line", winRepoWithConfig(t, f.env, winPlainRepoConfig+"!!! = x\n")},
		{"escaped value", winRepoWithConfig(t, f.env, winPlainRepoConfig+"[alias]\n\tx = \"a\\tb\"\n")},
		{"inside the git directory", filepath.Join(f.main, ".git", "refs")},
		{"bare repository", bare},
		{"below a bare repository", bareSub},
		{"commondir inside .git", commondirInDotGit},
		{"invalid gitfile", invalidGitfile},
	}
	if f.junction != "" {
		// A reparse-point .git is left to git, as a symlinked one is on Linux.
		junctionedDotGit := mkdir(t, t.TempDir(), "junctioned")
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(junctionedDotGit, ".git"), filepath.Join(f.main, ".git")).CombinedOutput(); err == nil {
			cases = append(cases, struct{ name, dir string }{"junctioned .git", junctionedDotGit})
		} else {
			t.Logf("junctioned .git case skipped: %v: %s", err, out)
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := discoverGitFrom(tc.dir); ok {
				t.Fatalf("discoverGitFrom(%s) = %+v; want it to defer to git", tc.dir, got)
			}
			winAssertSameAsGit(t, f.env, tc.dir)
		})
	}
}

// TestWindowsGitContextInProcessMatchesGitSubprocess checks the startup path end
// to end: discoverGitInProcess (the process working directory) against the git
// subprocess.
func TestWindowsGitContextInProcessMatchesGitSubprocess(t *testing.T) {
	f := newWinDiscoveryFixture(t)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, dir := range []string{
		f.main, filepath.Join(f.main, "a", "b"), f.worktree, filepath.Join(f.worktree, "w"),
		f.submodule, filepath.Join(f.nested, "deep"),
	} {
		t.Run(strings.TrimPrefix(dir, f.root), func(t *testing.T) {
			t.Chdir(dir)
			raw, ok := discoverGitInProcess()
			if !ok {
				t.Fatalf("discoverGitInProcess declined in %s", dir)
			}
			got := gitContextFromRevParse("", raw)
			want := loadGitContext("", f.env)
			if got.err != nil || want.err != nil {
				t.Fatalf("errors: in-process %v, git %v", got.err, want.err)
			}
			if got.gitDirRaw != want.gitDirRaw || got.commonDir != want.commonDir ||
				got.repoRoot != want.repoRoot || got.isWorktree != want.isWorktree {
				t.Fatalf("in-process %+v, git %+v", got, want)
			}
		})
	}
}

// TestWindowsDiscoverDefersToGitEnvOverrides: GIT_DIR and friends redefine
// discovery, so their presence (even empty) sends it to git.
func TestWindowsDiscoverDefersToGitEnvOverrides(t *testing.T) {
	f := newWinDiscoveryFixture(t)
	t.Chdir(f.main)
	for _, name := range discoveryEnvOverrides {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "")
			if got, ok := discoverGitInProcess(); ok {
				t.Fatalf("with %s set, discoverGitInProcess() = %+v; want it to defer to git", name, got)
			}
		})
	}
}

// TestWindowsCommonDirInProcessMatchesGit: the beads-directory probe
// (getGitCommonDirForPath) answers what `git -C dir rev-parse --git-common-dir`
// does, resolved to the same directory.
func TestWindowsCommonDirInProcessMatchesGit(t *testing.T) {
	f := newWinDiscoveryFixture(t)
	for _, dir := range []string{f.main, filepath.Join(f.main, "a", "b"), f.worktree, filepath.Join(f.worktree, "w"), f.submodule} {
		t.Run(strings.TrimPrefix(dir, f.root), func(t *testing.T) {
			got, isRepo, ok := CommonDirInProcess(dir)
			if !ok || !isRepo {
				t.Fatalf("CommonDirInProcess(%s) = %q, isRepo=%v, ok=%v; want an answer", dir, got, isRepo, ok)
			}
			cmd := exec.Command("git", "-C", dir, "rev-parse", "--git-common-dir")
			cmd.Env = f.env
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("git rev-parse --git-common-dir: %v", err)
			}
			resolve := func(p string) string {
				if !filepath.IsAbs(p) {
					p = filepath.Join(dir, p)
				}
				if r, err := filepath.EvalSymlinks(filepath.Clean(p)); err == nil {
					p = r
				}
				return p
			}
			if a, b := resolve(got), resolve(strings.TrimSpace(string(out))); a != b {
				t.Fatalf("CommonDirInProcess(%s) = %q (%s), git says %q (%s)", dir, got, a, strings.TrimSpace(string(out)), b)
			}
		})
	}
	if _, isRepo, ok := CommonDirInProcess(filepath.Join(f.root, "norepo", "x")); !ok || isRepo {
		t.Errorf("a directory outside any repository: isRepo=%v ok=%v, want an answer that it is not one", isRepo, ok)
	}
}

// TestWindowsOwnership pins the owner check: a directory this test creates is
// the current user's; one Windows owns (System32 belongs to TrustedInstaller) is
// not, which sends the repository to git's safe.directory evaluation.
func TestWindowsOwnership(t *testing.T) {
	if !ownedByCurrentUser(t.TempDir()) {
		t.Error("a directory just created by this process was not reported as owned by the current user")
	}
	system32 := filepath.Join(os.Getenv("SystemRoot"), "System32")
	if _, err := os.Stat(system32); err != nil {
		t.Skip("no System32 to probe")
	}
	if ownedByCurrentUser(system32) {
		t.Errorf("%s was reported as owned by the current user", system32)
	}
	if ownedByCurrentUser(filepath.Join(t.TempDir(), "does-not-exist")) {
		t.Error("a missing path was reported as owned")
	}
}

// TestWindowsLeavesNetworkAndDevicePathsToGit: UNC and device paths are not
// modeled.
func TestWindowsLeavesNetworkAndDevicePathsToGit(t *testing.T) {
	for _, p := range []string{`\\server\share\repo`, `//server/share/repo`, `\\?\C:\repo`, `\\.\C:\repo`} {
		if !isNetworkOrDevicePath(p) {
			t.Errorf("%q was not treated as a network or device path", p)
		}
		if _, ok := deviceOf(p); ok {
			t.Errorf("deviceOf(%q) answered", p)
		}
	}
	for _, p := range []string{`C:\repo`, `d:/repo`} {
		if isNetworkOrDevicePath(p) {
			t.Errorf("%q was treated as a network or device path", p)
		}
	}
	if _, ok := HasRemoteInProcess(""); ok {
		t.Error("HasRemoteInProcess answered on Windows, which models no config scopes")
	}
}

// TestWindowsLeavesLongWorkingDirectoriesToGit: Git for Windows' getcwd buffers
// are MAX_PATH units, so from a working directory of 260 or more it fails and git
// reports no repository. The probe must not answer a repository where git does
// not: a path that long is declined, and what startup then derives is git's.
func TestWindowsLeavesLongWorkingDirectoriesToGit(t *testing.T) {
	f := newWinDiscoveryFixture(t)
	long := f.main
	for len(long) < 270 {
		long = filepath.Join(long, strings.Repeat("d", 60))
	}
	if err := os.MkdirAll(long, 0o750); err != nil {
		t.Skipf("cannot create a %d-character path here: %v", len(long), err)
	}
	if got, ok := discoverGitFrom(long); ok {
		t.Fatalf("discoverGitFrom(%d-character path) = %+v; want it to defer to git", len(long), got)
	}
	if _, isRepo, ok := CommonDirInProcess(long); ok {
		t.Errorf("CommonDirInProcess(%d-character path) answered (isRepo=%v)", len(long), isRepo)
	}
	winAssertSameAsGit(t, f.env, long)

	// Just under the limit is still answered.
	short := f.main
	for len(short)+61 < windows259 {
		short = filepath.Join(short, strings.Repeat("d", 60))
	}
	if err := os.MkdirAll(short, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, ok := discoverGitFrom(short); !ok {
		t.Errorf("discoverGitFrom(%d-character path) declined; the limit is 260 units", len(short))
	}
}

// windows259 is the longest path (in UTF-16 units) below MAX_PATH.
const windows259 = 259
