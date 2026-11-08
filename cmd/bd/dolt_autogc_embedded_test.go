//go:build cgo

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Runs real commands against a real journal, because only that shows which
// commands actually collect garbage and that a pass keeps the data. Not gated
// on BEADS_TEST_EMBEDDED_DOLT, so `docker buildx bake test` always runs it.
func TestEmbeddedAutoGCRunsAfterAWrite(t *testing.T) {
	t.Parallel()
	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "ag")

	const limitBytes = 1 << 20
	journals, err := filepath.Glob(filepath.Join(beadsDir, "embeddeddolt", "*", ".dolt", "noms", strings.Repeat("v", 32)))
	if err != nil || len(journals) != 1 {
		t.Fatalf("want one chunk journal, got %v (err %v)", journals, err)
	}
	journalSize := func() int64 {
		t.Helper()
		info, err := os.Stat(journals[0])
		if errors.Is(err, os.ErrNotExist) {
			return 0 // A pass removes the journal until the next write.
		}
		if err != nil {
			t.Fatalf("stat journal: %v", err)
		}
		return info.Size()
	}
	run := func(limitMB string, args ...string) string {
		t.Helper()
		cmd := exec.Command(bd, args...)
		cmd.Dir = dir
		cmd.Env = append(bdEnv(dir), "BD_DOLT_AUTO_GC_JOURNAL_MB="+limitMB)
		stdout, stderr, err := runCommandBuffers(t, cmd)
		if err != nil {
			t.Fatalf("bd %s failed: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
		}
		return stdout.String()
	}

	// Random, so compression cannot keep the journal under the limit.
	raw := make([]byte, 3<<20)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	body := hex.EncodeToString(raw)
	bodyFile := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(bodyFile, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	big := parseIssueJSON(t, []byte(run("0", "create", "--json", "large issue", "--type", "task", "--body-file", bodyFile)))
	if got := journalSize(); got < limitBytes {
		t.Fatalf("journal is %d bytes with auto-gc off, want at least %d", got, limitBytes)
	}

	run("1", "list")
	if got := journalSize(); got < limitBytes {
		t.Fatalf("a read-only command collected garbage: journal is %d bytes", got)
	}

	run("1", "create", "small issue", "--type", "task")
	if got := journalSize(); got >= limitBytes {
		t.Fatalf("journal is still %d bytes after a write over the limit, want under %d", got, limitBytes)
	}

	if got := bdShow(t, bd, dir, big.ID); got.Description != body {
		t.Fatalf("description changed across the pass: %d bytes, want %d", len(got.Description), len(body))
	}
}
