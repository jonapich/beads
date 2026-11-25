//go:build linux

package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// processCwd is getcwd(2), as Git uses. os.Getwd prefers $PWD whenever it
// names the same inode as ".", which under bind-mount aliases (same device
// and inode, different ancestors) would walk a different path than git.
func processCwd() (string, error) {
	return syscall.Getwd()
}

// ownedByCurrentUser mirrors Git's is_path_owned_by_current_uid (lstat, owner
// equals the effective uid). Git's sudo allowance (root with SUDO_UID) is left
// to git. A path owned by someone else engages safe.directory, which only git
// evaluates.
func ownedByCurrentUser(path string) bool {
	euid := os.Geteuid()
	if euid == 0 {
		if _, set := os.LookupEnv("SUDO_UID"); set {
			return false
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == euid
}

// statOf returns path's stat data (following symlinks, as Git's
// get_device_or_die does), or nil.
func statOf(path string) *syscall.Stat_t {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	st, _ := info.Sys().(*syscall.Stat_t)
	return st
}

// deviceOf is path's device number, as Git's get_device_or_die reads it to stop
// discovery at a mount point; ok is false when path cannot be stat'ed.
func deviceOf(path string) (dev uint64, ok bool) {
	st := statOf(path)
	if st == nil {
		return 0, false
	}
	return uint64(st.Dev), true
}

// resolvePath is the physical path of p, symlinks resolved.
func resolvePath(p string) (string, error) { return filepath.EvalSymlinks(p) }

// gitSpelling is the path as Git prints it: unchanged on Linux.
func gitSpelling(p string) string { return p }

// HasRemoteInProcess answers "does `git remote` in dir print anything" (dir
// "" is the process working directory) without running git, or ok=false.
//
// `git remote` lists every remote that any config scope defines a
// remote.<name>.* key for, and fails outside a repository. The answer is
// given only when every file git would read — system, global (XDG and
// ~/.gitconfig, or GIT_CONFIG_GLOBAL), and the repository's own config — is
// absent or within scanGitConfig's grammar and free of include directives,
// the repository is one discoverGitFrom answers for, and no environment
// variable adds config or redirects discovery. The system file's location is
// compiled into git; the candidates are /etc/gitconfig and <prefix>/etc/gitconfig
// for the git found on PATH (as invoked and with symlinks resolved), and a
// remote seen only in a system candidate declines rather than guess which one
// git reads.
func HasRemoteInProcess(dir string) (has, ok bool) {
	if discoveryEnvOverridden() {
		return false, false
	}
	if _, set := os.LookupEnv("GIT_CONFIG"); set {
		return false, false
	}
	if dir == "" {
		wd, err := processCwd()
		if err != nil {
			return false, false
		}
		dir = wd
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false, false
	}
	physical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return false, false
	}
	raw, ok := discoverGitFrom(physical)
	if !ok {
		return false, false
	}
	if raw.notRepo {
		return false, true // `git remote` exits 128 outside a repository
	}
	commonDir := raw.commonDir
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(physical, commonDir)
	}
	// Legacy remote definitions (.git/remotes/*, .git/branches/*) are not
	// listed by `git remote`, but are not modeled either: decline if present.
	for _, legacy := range []string{"remotes", "branches"} {
		if entries, err := os.ReadDir(filepath.Join(commonDir, legacy)); err == nil && len(entries) > 0 {
			return false, false
		} else if err != nil && !os.IsNotExist(err) {
			return false, false
		}
	}

	system, ok := systemConfigCandidates()
	if !ok {
		return false, false
	}
	global, ok := globalConfigFiles()
	if !ok {
		return false, false
	}
	inSystem, inOther := false, false
	for _, scope := range []struct {
		files  []string
		system bool
	}{{system, true}, {global, false}, {[]string{filepath.Join(commonDir, "config")}, false}} {
		for _, file := range scope.files {
			found, ok := configDefinesRemote(file)
			if !ok {
				return false, false
			}
			if found && scope.system {
				inSystem = true
			} else if found {
				inOther = true
			}
		}
	}
	switch {
	case inOther:
		return true, true
	case inSystem:
		return false, false
	default:
		return false, true
	}
}

// remoteName is the remote-name shape answered in-process; git ignores names
// starting with "/" and accepts many others this does not model.
var remoteName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// configDefinesRemote reports whether file defines a remote.<name>.* key.
func configDefinesRemote(file string) (found, ok bool) {
	ok = scanGitConfig(file, func(e gitConfigEntry) bool {
		if strings.HasPrefix(e.section, "include") {
			return false
		}
		if e.section != "remote" || e.subsection == nil {
			return true // remote.pushDefault and friends define no remote
		}
		if !remoteName.MatchString(*e.subsection) {
			return false
		}
		found = true
		return true
	})
	return found, ok
}

// globalConfigFiles mirrors git_global_config.
func globalConfigFiles() ([]string, bool) {
	if global, set := os.LookupEnv("GIT_CONFIG_GLOBAL"); set {
		if global == "" || !filepath.IsAbs(global) {
			return nil, false
		}
		return []string{global}, true
	}
	home := os.Getenv("HOME")
	if home == "" || !filepath.IsAbs(home) {
		return nil, false
	}
	xdg := filepath.Join(home, ".config", "git", "config")
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		if !filepath.IsAbs(x) {
			return nil, false
		}
		xdg = filepath.Join(x, "git", "config")
	}
	return []string{xdg, filepath.Join(home, ".gitconfig")}, true
}

// systemConfigCandidates mirrors git_system_config, with the compiled-in
// ETC_GITCONFIG approximated as described on HasRemoteInProcess.
func systemConfigCandidates() ([]string, bool) {
	if v, set := os.LookupEnv("GIT_CONFIG_NOSYSTEM"); set {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return nil, true
		case "", "0", "false", "no", "off":
		default:
			return nil, false
		}
	}
	if system, set := os.LookupEnv("GIT_CONFIG_SYSTEM"); set {
		if system == "" || !filepath.IsAbs(system) {
			return nil, false
		}
		return []string{system}, true
	}
	bin, err := exec.LookPath("git")
	if err != nil || !filepath.IsAbs(bin) {
		return nil, false
	}
	candidates := []string{"/etc/gitconfig"}
	paths := []string{bin}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		paths = append(paths, resolved)
	}
	for _, p := range paths {
		if filepath.Base(filepath.Dir(p)) != "bin" {
			return nil, false
		}
		if prefix := filepath.Dir(filepath.Dir(p)); prefix != "/usr" {
			candidates = append(candidates, filepath.Join(prefix, "etc", "gitconfig"))
		}
	}
	return candidates, true
}
