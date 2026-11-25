package main

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
)

// TestPersistentPostRunDirectArmSkipsAutoBackupForReads pins that the direct
// (non-proxied) post-run arm runs auto-backup after a write command and not
// after a read one. The docs say backup runs after writes; before this gate a
// read paid for the `git remote` probe plus an engine open on every call. The
// proxied arm is a separate branch that still runs it for reads
// (backup_auto_proxied_test.go pins that), so it is not touched here.
func TestPersistentPostRunDirectArmSkipsAutoBackupForReads(t *testing.T) {
	originalStore := store
	originalReadonly := readonlyMode
	originalProxied := proxiedServerMode
	originalRootCtx := rootCtx
	originalRootCancel := rootCancel
	originalCommandSpan := commandSpan
	originalProfileFile := profileFile
	originalTraceFile := traceFile
	storeMutex.Lock()
	originalStoreActive := storeActive
	storeMutex.Unlock()
	originalDidWrite := commandDidWrite.Load()
	originalDidExplicitCommit := commandDidExplicitDoltCommit
	originalDidWriteTipMetadata := commandDidWriteTipMetadata
	originalCommit := runPostRunAutoCommit
	originalBackup := runPostRunAutoBackup
	originalExport := runPostRunAutoExport
	originalPush := runPostRunAutoPush
	t.Cleanup(func() {
		store = originalStore
		readonlyMode = originalReadonly
		proxiedServerMode = originalProxied
		rootCtx = originalRootCtx
		rootCancel = originalRootCancel
		commandSpan = originalCommandSpan
		profileFile = originalProfileFile
		traceFile = originalTraceFile
		storeMutex.Lock()
		storeActive = originalStoreActive
		storeMutex.Unlock()
		commandDidWrite.Store(originalDidWrite)
		commandDidExplicitDoltCommit = originalDidExplicitCommit
		commandDidWriteTipMetadata = originalDidWriteTipMetadata
		runPostRunAutoCommit = originalCommit
		runPostRunAutoBackup = originalBackup
		runPostRunAutoExport = originalExport
		runPostRunAutoPush = originalPush
	})

	tests := []struct {
		command     string
		wantBackups int
	}{
		{command: "show", wantBackups: 0},
		{command: "list", wantBackups: 0},
		{command: "create", wantBackups: 1},
	}
	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			backupCalls := 0
			runPostRunAutoCommit = func(context.Context, doltAutoCommitParams) error { return nil }
			runPostRunAutoBackup = func(context.Context) { backupCalls++ }
			runPostRunAutoExport = func(context.Context, bool) error { return nil }
			runPostRunAutoPush = func(context.Context) {}

			store = &strictReadonlyPostRunStore{}
			readonlyMode = false
			proxiedServerMode = false
			rootCtx = context.Background()
			rootCancel = nil
			commandSpan = nil
			profileFile = nil
			traceFile = nil
			commandDidWrite.Store(false)
			commandDidExplicitDoltCommit = false
			commandDidWriteTipMetadata = false

			if err := rootCmd.PersistentPostRunE(&cobra.Command{Use: tc.command}, nil); err != nil {
				t.Fatalf("PersistentPostRunE(%s): %v", tc.command, err)
			}
			if backupCalls != tc.wantBackups {
				t.Fatalf("%s ran auto-backup %d time(s), want %d", tc.command, backupCalls, tc.wantBackups)
			}
		})
	}
}
