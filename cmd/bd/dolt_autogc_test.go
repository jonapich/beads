package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/config"
)

type fakeAutoGCTarget struct {
	size    int64
	sizeErr error
	gcErr   error
	gcRuns  int
	meta    map[string]string
	metaErr error
}

func (f *fakeAutoGCTarget) ChunkJournalSize() (int64, error) {
	return f.size, f.sizeErr
}

func (f *fakeAutoGCTarget) DoltGC(context.Context) error {
	f.gcRuns++
	return f.gcErr
}

func (f *fakeAutoGCTarget) GetLocalMetadata(_ context.Context, key string) (string, error) {
	return f.meta[key], nil
}

func (f *fakeAutoGCTarget) SetLocalMetadata(_ context.Context, key, value string) error {
	if f.metaErr != nil {
		return f.metaErr
	}
	if f.meta == nil {
		f.meta = map[string]string{}
	}
	f.meta[key] = value
	return nil
}

func setupAutoGCConfig(t *testing.T, journalMB string) {
	t.Helper()
	if journalMB != "" {
		t.Setenv("BD_DOLT_AUTO_GC_JOURNAL_MB", journalMB)
	}
	config.ResetForTesting()
	t.Cleanup(config.ResetForTesting)
	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}
}

func TestRunAutoGC(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	const oneMB = 1 << 20

	stamp := func(at time.Time) map[string]string {
		return map[string]string{autoGCLastAttemptKey: at.Format(time.RFC3339)}
	}

	tests := []struct {
		name      string
		limitMB   string
		fake      *fakeAutoGCTarget
		wantRuns  int
		wantError bool
		wantStamp bool
	}{
		{name: "below the limit", limitMB: "2", fake: &fakeAutoGCTarget{size: oneMB}},
		{name: "at the limit", limitMB: "1", fake: &fakeAutoGCTarget{size: oneMB}, wantRuns: 1, wantStamp: true},
		{name: "an unmeasurable journal is left alone", limitMB: "1", fake: &fakeAutoGCTarget{size: 100 * oneMB, sizeErr: errors.New("stat failed")}},
		{name: "a recent attempt throttles", limitMB: "1", fake: &fakeAutoGCTarget{size: 2 * oneMB, meta: stamp(now.Add(-time.Minute))}, wantStamp: true},
		{name: "an old attempt does not throttle", limitMB: "1", fake: &fakeAutoGCTarget{size: 2 * oneMB, meta: stamp(now.Add(-time.Hour))}, wantRuns: 1, wantStamp: true},
		{name: "a future attempt does not throttle forever", limitMB: "1", fake: &fakeAutoGCTarget{size: 2 * oneMB, meta: stamp(now.Add(time.Hour))}, wantRuns: 1, wantStamp: true},
		{name: "a failed pass is reported and still throttles", limitMB: "1", fake: &fakeAutoGCTarget{size: 2 * oneMB, gcErr: errors.New("gc failed")}, wantRuns: 1, wantError: true, wantStamp: true},
		{name: "no pass runs without recording the attempt", limitMB: "1", fake: &fakeAutoGCTarget{size: 2 * oneMB, metaErr: errors.New("read-only")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupAutoGCConfig(t, tt.limitMB)
			err := runAutoGC(ctx, tt.fake, tt.fake, now)
			if (err != nil) != tt.wantError {
				t.Fatalf("runAutoGC error = %v, wantError %v", err, tt.wantError)
			}
			if tt.fake.gcRuns != tt.wantRuns {
				t.Fatalf("GC ran %d times, want %d", tt.fake.gcRuns, tt.wantRuns)
			}
			if got := tt.fake.meta[autoGCLastAttemptKey] != ""; got != tt.wantStamp {
				t.Fatalf("attempt recorded = %v, want %v", got, tt.wantStamp)
			}
		})
	}
}

func TestRunAutoGCDefaults(t *testing.T) {
	setupAutoGCConfig(t, "")
	if got := config.GetInt(autoGCJournalMBKey); got != 32 {
		t.Fatalf("%s default = %d, want 32", autoGCJournalMBKey, got)
	}
	if got := config.GetDuration(autoGCIntervalKey); got != 10*time.Minute {
		t.Fatalf("%s default = %s, want 10m", autoGCIntervalKey, got)
	}
}
