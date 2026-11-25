package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// Windows discovery is the Linux walk (discover.go) with the platform facts
// Git for Windows uses:
//
//   - Git prints paths with forward slashes ("C:/Users/x/repo"); gitSpelling
//     writes that, and gitContextFromRevParse (NormalizePath) turns it back.
//   - Git's st_dev is not used on Windows (compat/mingw.c fills it with 0), so
//     discovery never stops at a "filesystem boundary": sameFilesystem is true.
//   - Ownership is the owner SID against the process token's user SID
//     (is_path_owned_by_current_sid). Only an exact match is answered; Git's
//     allowances for the Administrators group are left to git, as the root/sudo
//     allowance is on Linux.
//   - Paths are resolved by resolvePath (GetFinalPathNameByHandle, as Git's
//     mingw_getcwd and mingw_strbuf_realpath do), which follows symlinks AND
//     junctions and returns the true case, so a case-insensitive spelling of the
//     working directory cannot change the walk.
//   - A working directory of MAX_PATH (260) UTF-16 units or more is left to git:
//     mingw_getcwd fails with ENAMETOOLONG there, so git reports no repository,
//     and the probe must not answer where git does not.
//   - A UNC or device path (\\server\share, \\?\...) is left to git: ownership and
//     safe.directory are evaluated differently for network paths.
//
// What stays with git, as on Linux: bare repositories, GIT_DIR and friends,
// core.worktree beyond the submodule shape, extensions, include directives, a
// symlinked or reparse-point .git, a .git directory with a commondir file.
//
// HasRemoteInProcess is not answered on Windows: Git for Windows reads system
// config from its install prefix and global config from %USERPROFILE%, locations
// this package does not model.

func init() { configDoubledBackslashes = true }

// processCwd is the process working directory.
func processCwd() (string, error) {
	cwd, err := os.Getwd()
	if err != nil || isNetworkOrDevicePath(cwd) {
		return "", errUnmodeledPath
	}
	return cwd, nil
}

// errUnmodeledPath makes discovery hand a path it does not model to git.
var errUnmodeledPath = errors.New("path form is left to git")

// isNetworkOrDevicePath reports a UNC path or a \\?\ / \\.\ device path.
func isNetworkOrDevicePath(p string) bool {
	return strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, `//`)
}

// resolvePath is the physical path of p as Windows itself reports it: symlinks AND
// junctions followed (filepath.EvalSymlinks follows only the former), the true case,
// the drive-letter form. This is the path Git for Windows prints, which resolves the
// working directory through the same final-path query. A UNC result is an error: it
// is not modeled.
func resolvePath(p string) (string, error) {
	// Git's getcwd buffers are MAX_PATH long (see the top of this file).
	if len(utf16.Encode([]rune(p))) >= windows.MAX_PATH {
		return "", errUnmodeledPath
	}
	h, err := windows.CreateFile(windows.StringToUTF16Ptr(p), 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return "", err
		}
		if int(n) >= windows.MAX_PATH {
			// n counts the \\?\ prefix, as Git's own check does.
			return "", errUnmodeledPath
		}
		final := windows.UTF16ToString(buf[:n])
		if strings.HasPrefix(final, `\\?\UNC\`) {
			return "", errUnmodeledPath
		}
		return strings.TrimPrefix(final, `\\?\`), nil
	}
}

// deviceOf is 0 for every path on Windows (see above); ok is false for a path that
// cannot be stat'ed or is a network or device path.
func deviceOf(path string) (dev uint64, ok bool) {
	if isNetworkOrDevicePath(path) {
		return 0, false
	}
	if _, err := os.Stat(path); err != nil {
		return 0, false
	}
	return 0, true
}

// gitSpelling is the path as Git for Windows prints it.
func gitSpelling(p string) string { return filepath.ToSlash(p) }

// ownedByCurrentUser mirrors Git for Windows' is_path_owned_by_current_sid for
// the exact-match case: the path's owner SID is the process token's user SID.
func ownedByCurrentUser(path string) bool {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return false
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false
	}
	return windows.EqualSid(owner, user.User.Sid)
}

// HasRemoteInProcess is not answered on Windows; see the top of this file.
func HasRemoteInProcess(string) (has, ok bool) {
	return false, false
}
