//go:build linux || windows

package git

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// discoverGitInProcess answers `git rev-parse --git-dir --git-common-dir
// --show-toplevel` for the process working directory without running git. ok
// is false whenever the layout is outside what it reproduces exactly; the
// caller then runs git.
func discoverGitInProcess() (revParseResult, bool) {
	if discoveryEnvOverridden() {
		return revParseResult{}, false
	}
	wd, err := processCwd()
	if err != nil {
		return revParseResult{}, false
	}
	return discoverGitFrom(wd)
}

// errNotGitRepositoryMountBoundary is the in-process counterpart of Git's
// "not a git repository (or any parent up to mount point ...)".
var errNotGitRepositoryMountBoundary = errors.New("no .git found in the working directory or any parent up to the filesystem boundary")

// discoverGitFrom mirrors setup_git_directory_gently's discovery walk (Git
// 2.x) from cwd. Git walks the physical directory (getcwd), checks each
// directory's .git as a gitfile or a git directory, then the directory itself
// as a bare repository, and stops at the first filesystem boundary.
func discoverGitFrom(cwd string) (revParseResult, bool) {
	cwd, err := resolvePath(cwd)
	if err != nil || !filepath.IsAbs(cwd) {
		return revParseResult{}, false
	}
	cwd = filepath.Clean(cwd)
	cwdDev, ok := deviceOf(cwd)
	if !ok {
		return revParseResult{}, false
	}

	for dir := cwd; ; {
		dotGit := filepath.Join(dir, ".git")
		info, err := os.Lstat(dotGit)
		switch {
		case err == nil && info.Mode().IsDir():
			return discoveredGitDir(cwd, dir, dotGit)
		case err == nil && info.Mode().IsRegular():
			return discoveredGitFile(dir, dotGit)
		case err == nil:
			// Symlinked .git, sockets, ...: let git decide.
			return revParseResult{}, false
		case !os.IsNotExist(err):
			return revParseResult{}, false
		}
		// The directory itself may be a git directory (a bare repository, or
		// somewhere inside a .git directory); git answers those differently.
		if looksLikeGitDir(dir) {
			return revParseResult{}, false
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return revParseResult{notRepo: true}, true
		}
		parentDev, ok := deviceOf(parent)
		if !ok {
			return revParseResult{}, false
		}
		if parentDev != cwdDev {
			// Git stops at a mount point unless
			// GIT_DISCOVERY_ACROSS_FILESYSTEM is set (handled by the caller).
			return revParseResult{notRepo: true, notRepoErr: errNotGitRepositoryMountBoundary}, true
		}
		dir = parent
	}
}

// discoveredGitDir handles <top>/.git being a directory.
func discoveredGitDir(cwd, top, gitDir string) (revParseResult, bool) {
	// A .git directory carrying a commondir file is unusual; leave it to git.
	if exists(filepath.Join(gitDir, "commondir")) {
		return revParseResult{}, false
	}
	if !isGitDirectory(gitDir, gitDir) {
		// Git would keep walking upward past an invalid .git directory.
		return revParseResult{}, false
	}
	if !ownedByCurrentUser(top) || !ownedByCurrentUser(gitDir) {
		return revParseResult{}, false
	}
	if worktree, ok := repoConfigAllowsDiscovery(gitDir); !ok || worktree != "" {
		return revParseResult{}, false
	}
	if cwd == top {
		return revParseResult{gitDir: ".git", commonDir: ".git", topLevel: gitSpelling(top)}, true
	}
	// From a subdirectory Git prints the git directory absolute and the common
	// directory relative to the working directory.
	rel, err := filepath.Rel(top, cwd)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return revParseResult{}, false
	}
	depth := len(strings.Split(filepath.ToSlash(rel), "/"))
	return revParseResult{
		gitDir:    gitSpelling(gitDir),
		commonDir: strings.Repeat("../", depth) + ".git",
		topLevel:  gitSpelling(top),
	}, true
}

// discoveredGitFile handles <top>/.git being a gitfile ("gitdir: <path>"), as
// in linked worktrees and submodules.
func discoveredGitFile(top, gitFile string) (revParseResult, bool) {
	data, err := os.ReadFile(gitFile) // #nosec G304 -- the .git file Git itself would read during discovery
	if err != nil {
		return revParseResult{}, false
	}
	content := strings.TrimRight(string(data), "\r\n")
	target, found := strings.CutPrefix(content, "gitdir: ")
	if !found || target == "" || strings.ContainsAny(target, "\n\r\x00") {
		return revParseResult{}, false
	}
	if !filepath.IsAbs(target) {
		// Not filepath.Join: its lexical Clean would fold "link/.." before
		// symlinks resolve, where Git's realpath resolves them physically
		// (as resolvePath does for ".." after a resolved component).
		target = top + string(filepath.Separator) + filepath.FromSlash(target)
	}
	gitDir, err := resolvePath(target)
	if err != nil {
		return revParseResult{}, false
	}
	commonDir := gitDir
	if raw, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		common := strings.TrimRight(string(raw), "\r\n")
		if common == "" || strings.ContainsAny(common, "\n\r\x00") {
			return revParseResult{}, false
		}
		if !filepath.IsAbs(common) {
			common = gitDir + string(filepath.Separator) + filepath.FromSlash(common) // physical "..", as above
		}
		if commonDir, err = resolvePath(common); err != nil {
			return revParseResult{}, false
		}
	} else if !os.IsNotExist(err) {
		return revParseResult{}, false
	}
	if !isGitDirectory(gitDir, commonDir) {
		return revParseResult{}, false
	}
	if !ownedByCurrentUser(gitFile) || !ownedByCurrentUser(top) || !ownedByCurrentUser(gitDir) {
		return revParseResult{}, false
	}
	worktree, ok := repoConfigAllowsDiscovery(commonDir)
	if !ok {
		return revParseResult{}, false
	}
	if worktree != "" && !submoduleWorktreeIs(gitDir, commonDir, worktree, top) {
		return revParseResult{}, false
	}
	// Git prints both absolute from anywhere in the work tree.
	return revParseResult{gitDir: gitSpelling(gitDir), commonDir: gitSpelling(commonDir), topLevel: gitSpelling(top)}, true
}

// isGitDirectory mirrors Git's is_git_directory: a valid HEAD in the git
// directory, objects/ and refs/ in the common directory.
func isGitDirectory(gitDir, commonDir string) bool {
	if !validHeadRef(filepath.Join(gitDir, "HEAD")) {
		return false
	}
	return isDir(filepath.Join(commonDir, "objects")) && isDir(filepath.Join(commonDir, "refs"))
}

// looksLikeGitDir is a cheap superset test for "git might treat dir as a git
// directory"; a hit only means discovery is handed to git.
func looksLikeGitDir(dir string) bool {
	return exists(filepath.Join(dir, "HEAD")) &&
		(exists(filepath.Join(dir, "objects")) || exists(filepath.Join(dir, "commondir")))
}

// validHeadRef accepts the HEAD forms Git's validate_headref does for the
// files it writes itself: "ref: refs/..." or a full object id. Symlinked HEAD
// (an ancient layout) is left to git.
func validHeadRef(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	data, err := os.ReadFile(path) // #nosec G304 -- HEAD of the discovered git directory
	if err != nil {
		return false
	}
	if ref, ok := bytes.CutPrefix(data, []byte("ref:")); ok {
		return bytes.HasPrefix(bytes.TrimLeft(ref, " \t"), []byte("refs/"))
	}
	line := strings.TrimRight(string(data), "\r\n")
	if len(line) != 40 && len(line) != 64 {
		return false
	}
	for _, c := range line {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// submoduleWorktreeIs reports whether a gitfile repository's core.worktree
// names exactly the directory holding the gitfile. Git writes that for every
// submodule (core.worktree = ../../../sub in .git/modules/sub/config), and Git
// then resolves the work tree by chdir(gitdir) + chdir(core.worktree), which
// lands on top again. Only that plain shape is accepted: a separate git
// directory (no shared common directory) and a value of leading "../"
// segments followed by plain names, which resolves lexically from the
// already-physical git directory exactly as the chdirs do.
func submoduleWorktreeIs(gitDir, commonDir, worktree, top string) bool {
	if gitDir != commonDir || filepath.IsAbs(worktree) {
		return false
	}
	rest := filepath.ToSlash(filepath.Clean(worktree))
	if rest != worktree && rest+"/" != worktree {
		return false
	}
	for strings.HasPrefix(rest, "../") {
		rest = rest[len("../"):]
	}
	for _, part := range strings.Split(rest, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	resolved, err := resolvePath(filepath.Join(gitDir, worktree))
	return err == nil && resolved == top
}

// repoConfigAllowsDiscovery reports whether the repository config leaves
// Git's answer to the plain discovery result, and returns core.worktree when
// it is set (the caller decides whether it is the submodule shape). Git
// consults only the repository's own config file here
// (read_repository_format), so global and system config cannot move the work
// tree. Anything else that could — core.bare, extensions (worktreeConfig,
// unknown ones Git rejects), a format version Git refuses, include
// directives, a repeated core.worktree, or any line scanGitConfig does not
// fully understand (where Git might honor or reject it) — sends discovery to
// git.
func repoConfigAllowsDiscovery(commonDir string) (worktree string, ok bool) {
	ok = scanGitConfig(filepath.Join(commonDir, "config"), func(e gitConfigEntry) bool {
		if strings.HasPrefix(e.section, "include") || e.section == "extensions" {
			return false
		}
		if e.section != "core" || e.subsection != nil {
			return true
		}
		switch e.key {
		case "worktree":
			if worktree != "" || !e.hasValue || e.value == "" || strings.ContainsAny(e.rawValue, "\"#;") {
				return false
			}
			worktree = e.value
		case "bare":
			if !e.hasValue {
				return false // a bare "bare" key means true
			}
			switch strings.ToLower(e.value) {
			case "false", "no", "off", "0":
			default:
				return false
			}
		case "repositoryformatversion":
			if v, err := strconv.Atoi(e.value); err != nil || v < 0 || v > 1 {
				return false
			}
		}
		return true
	})
	if !ok {
		return "", false
	}
	return worktree, true
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// CommonDirInProcess answers `git -C dir rev-parse --git-common-dir` without
// running git, in git's own spelling (relative to dir's physical location
// when git prints it relative). isRepo is false where git would fail with
// "not a git repository". ok is false whenever discoverGitInProcess would
// decline; the caller then runs git.
func CommonDirInProcess(dir string) (commonDir string, isRepo, ok bool) {
	if discoveryEnvOverridden() {
		return "", false, false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false, false
	}
	raw, ok := discoverGitFrom(abs)
	if !ok {
		return "", false, false
	}
	if raw.notRepo {
		return "", false, true
	}
	return raw.commonDir, true, true
}
