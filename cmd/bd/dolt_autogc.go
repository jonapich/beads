package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/storage"
)

const (
	autoGCJournalMBKey   = "dolt.auto-gc-journal-mb"
	autoGCIntervalKey    = "dolt.auto-gc-interval"
	autoGCLastAttemptKey = "auto_gc_last_attempt"
	autoGCTimeout        = 5 * time.Minute
)

// autoGCTarget is a store whose journal can be measured locally. Only embedded
// Dolt qualifies: a Dolt server runs its own automatic GC.
type autoGCTarget interface {
	storage.GarbageCollector
	ChunkJournalSize() (int64, error)
}

type autoGCMetadata interface {
	GetLocalMetadata(ctx context.Context, key string) (string, error)
	SetLocalMetadata(ctx context.Context, key, value string) error
}

// maybeAutoGC collects Dolt garbage once the chunk journal has grown large.
// Embedded Dolt never does this on its own, and every command reads the journal
// when it opens the database, so a neglected journal slows everything down.
//
// Other bd processes are safe: each opens the engine per transaction and waits
// for its lock, so none has a session open while the pass runs. Its failures
// are warnings, because the command that got here has already succeeded.
func maybeAutoGC(ctx context.Context) {
	st := getStore()
	if st == nil {
		return
	}
	target, ok := storage.UnwrapStore(st).(autoGCTarget)
	if !ok {
		return
	}
	if err := runAutoGC(ctx, target, st, time.Now()); err != nil {
		debug.Logf("dolt auto-gc: %v\n", err)
		if !isQuiet() && !jsonOutput {
			fmt.Fprintf(os.Stderr, "Warning: dolt auto-gc failed: %v\n", err)
		}
	}
}

func runAutoGC(ctx context.Context, target autoGCTarget, meta autoGCMetadata, now time.Time) error {
	limitMB := config.GetInt(autoGCJournalMBKey)
	if limitMB <= 0 {
		return nil
	}
	size, err := target.ChunkJournalSize()
	if err != nil {
		debug.Logf("dolt auto-gc: could not measure the journal: %v\n", err)
		return nil
	}
	if size < int64(limitMB)<<20 {
		return nil
	}

	// Only a journal over the limit reaches this metadata read, so ordinary
	// writes do not pay for it.
	interval := config.GetDuration(autoGCIntervalKey)
	if last, err := meta.GetLocalMetadata(ctx, autoGCLastAttemptKey); err == nil && last != "" {
		if at, perr := time.Parse(time.RFC3339, last); perr == nil {
			if age := now.Sub(at); age >= 0 && age < interval {
				debug.Logf("dolt auto-gc: throttled (last attempt %s ago)\n", age.Round(time.Second))
				return nil
			}
		}
	}

	// Recorded before the pass, so parallel processes that crossed the limit
	// together run it once and a failing pass is not retried by every write.
	if err := meta.SetLocalMetadata(ctx, autoGCLastAttemptKey, now.UTC().Format(time.RFC3339)); err != nil {
		debug.Logf("dolt auto-gc: could not record the attempt: %v\n", err)
		return nil
	}

	gcCtx, cancel := context.WithTimeout(ctx, autoGCTimeout)
	defer cancel()
	return target.DoltGC(gcCtx)
}
