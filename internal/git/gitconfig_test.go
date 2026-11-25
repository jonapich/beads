//go:build linux || windows

package git

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseGitConfigValue pins the value grammar, with the doubled-backslash
// option on (Windows) and off (Linux, where the grammar must stay as it was).
func TestParseGitConfigValue(t *testing.T) {
	type tc struct {
		raw  string
		want string
		ok   bool
	}
	withDoubled := []tc{
		{`C:\\x`, `C:\x`, true},
		{`"C:\\x y"`, `C:\x y`, true},
		{`a\\\\b`, `a\\b`, true},
		{`C:\\x # c:\y`, `C:\x`, true},
		{`C:\\`, `C:\`, true},
		{`plain value`, `plain value`, true},
		{`  spaced  `, `spaced`, true},
		{`"quoted # not a comment"`, `quoted # not a comment`, true},
		{`C:\x`, ``, false},
		{`a\"b`, ``, false},
		{`x\`, ``, false},
		{`a\\\"`, ``, false},
		{`"a\\"b"`, ``, false},
		{`a\tb`, ``, false},
		{`"unterminated`, ``, false},
	}
	withoutDoubled := []tc{
		{`plain value`, `plain value`, true},
		{`C:\\x`, ``, false},
		{`a\tb`, ``, false},
		{`x\`, ``, false},
	}
	run := func(t *testing.T, doubled bool, cases []tc) {
		old := configDoubledBackslashes
		configDoubledBackslashes = doubled
		t.Cleanup(func() { configDoubledBackslashes = old })
		for _, c := range cases {
			got, ok := parseGitConfigValue(c.raw)
			if ok != c.ok || (ok && got != c.want) {
				t.Errorf("doubled=%v parseGitConfigValue(%q) = %q, %v; want %q, %v", doubled, c.raw, got, ok, c.want, c.ok)
			}
		}
	}
	t.Run("doubled backslashes understood", func(t *testing.T) { run(t, true, withDoubled) })
	t.Run("doubled backslashes not understood", func(t *testing.T) { run(t, false, withoutDoubled) })
}

// TestRepoConfigWithBackslashPathsAllowsDiscovery: Git for Windows writes the
// remote and submodule urls of a checkout with doubled backslashes; with the
// option on, such a config no longer sends discovery to git, and without it it
// still does.
func TestRepoConfigWithBackslashPathsAllowsDiscovery(t *testing.T) {
	dir := t.TempDir()
	config := "[core]\n\trepositoryformatversion = 0\n\tbare = false\n" +
		"[remote \"origin\"]\n\turl = C:\\\\Users\\\\x\\\\repo\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	old := configDoubledBackslashes
	t.Cleanup(func() { configDoubledBackslashes = old })

	configDoubledBackslashes = true
	if worktree, ok := repoConfigAllowsDiscovery(dir); !ok || worktree != "" {
		t.Errorf("with doubled backslashes understood: repoConfigAllowsDiscovery = %q, %v; want \"\", true", worktree, ok)
	}
	configDoubledBackslashes = false
	if _, ok := repoConfigAllowsDiscovery(dir); ok {
		t.Error("without doubled backslashes understood the config was accepted; the Linux grammar must be unchanged")
	}
}
