//go:build !linux && !windows

package git

import "path/filepath"

// resolvePath is the physical path of p, symlinks resolved.
func resolvePath(p string) (string, error) { return filepath.EvalSymlinks(p) }

// discoverGitInProcess is not answered on this platform: path case-folding
// (darwin) and ownership semantics make an exact in-process answer harder to
// prove, so discovery always runs git. Linux and Windows have their own.
func discoverGitInProcess() (revParseResult, bool) {
	return revParseResult{}, false
}

// CommonDirInProcess is not answered on this platform; see discoverGitInProcess.
func CommonDirInProcess(string) (commonDir string, isRepo, ok bool) {
	return "", false, false
}

// HasRemoteInProcess is not answered on this platform; see discoverGitInProcess.
func HasRemoteInProcess(string) (has, ok bool) {
	return false, false
}
