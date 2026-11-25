//go:build cgo

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
)

// TestEmbeddedShowAndListJSONReadInOneEngineOpen pins how many embedded engines
// the direct route of `bd show --json` and `bd list --json` opens for their
// reads. Each store call used to open its own engine (eight for show, five for
// list), and every open pays a journal stall on Windows. The id lookup, the
// config loads, the routing check and the page now share one read session.
func TestEmbeddedShowAndListJSONReadInOneEngineOpen(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	f := seedListEdgeStore(t, openEmbeddedListEdgeStore(t))

	oldStore, oldCtx, oldJSON := store, rootCtx, jsonOutput
	store, rootCtx, jsonOutput = f.store, t.Context(), true
	t.Cleanup(func() { store, rootCtx, jsonOutput = oldStore, oldCtx, oldJSON })

	alpha := f.ids["alpha"]

	t.Run("show", func(t *testing.T) {
		before := embeddeddolt.EngineOpens()
		out := captureStdout(t, func() error { return showCmd.RunE(showCmd, []string{alpha}) })
		if got := embeddeddolt.EngineOpens() - before; got != 1 {
			t.Errorf("bd show --json %s opened %d engines for its reads, want 1", alpha, got)
		}
		if !strings.Contains(out, alpha) {
			t.Errorf("bd show --json output lacks %s:\n%s", alpha, out)
		}
	})

	t.Run("list", func(t *testing.T) {
		before := embeddeddolt.EngineOpens()
		out := captureStdout(t, func() error { return runListCore(newListFlagsCommand(t), nil) })
		if got := embeddeddolt.EngineOpens() - before; got != 1 {
			t.Errorf("bd list --json opened %d engines for its reads, want 1", got)
		}
		if !strings.Contains(out, alpha) {
			t.Errorf("bd list --json output lacks %s:\n%s", alpha, out)
		}
	})
}
