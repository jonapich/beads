//go:build cgo

package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestEmbeddedLeaseTTLConfig drives the lease.ttl setting end to end: a claim
// (bd update --claim, bd ready --claim) is granted for the configured length, a
// heartbeat renews for the length configured at that moment, and a value that
// is not a positive duration is refused when it is set.
func TestEmbeddedLeaseTTLConfig(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "lt")

	// leaseLeft is how long the issue's stored lease has left, from now.
	leaseLeft := func(t *testing.T, id string) time.Duration {
		t.Helper()
		got := bdShow(t, bd, dir, id)
		if got.LeaseExpiresAt == nil {
			t.Fatalf("%s has no lease_expires_at after its claim", id)
		}
		return time.Until(*got.LeaseExpiresAt)
	}
	// near passes when got is within a minute of want (the lease columns round to
	// whole seconds and the command takes a moment to run).
	near := func(got, want time.Duration) bool {
		d := got - want
		return d > -time.Minute && d < time.Minute
	}

	t.Run("unset_uses_the_sixty_minute_default", func(t *testing.T) {
		issue := bdCreate(t, bd, dir, "Default lease", "--type", "task")
		bdUpdate(t, bd, dir, issue.ID, "--claim", "--actor", "worker")
		if left := leaseLeft(t, issue.ID); !near(left, 60*time.Minute) {
			t.Errorf("lease has %v left, want about the 60m default", left)
		}
	})

	t.Run("invalid_values_are_refused_naming_the_key", func(t *testing.T) {
		for _, bad := range []string{"soon", "0", "0s", "60"} {
			out, _ := bdRunFailCode(t, bd, dir, "config", "set", "lease.ttl", bad)
			if !strings.Contains(out, "lease.ttl") {
				t.Errorf("config set lease.ttl %q: refusal does not name the key:\n%s", bad, out)
			}
		}
		out, _ := bdRunFailCode(t, bd, dir, "config", "set-many", "lease.ttl=nonsense")
		if !strings.Contains(out, "lease.ttl") {
			t.Errorf("config set-many lease.ttl=nonsense: refusal does not name the key:\n%s", out)
		}
		// Nothing was stored by the refusals, so claims still get the default.
		issue := bdCreate(t, bd, dir, "After refusals", "--type", "task")
		bdUpdate(t, bd, dir, issue.ID, "--claim", "--actor", "worker")
		if left := leaseLeft(t, issue.ID); !near(left, 60*time.Minute) {
			t.Errorf("lease has %v left after refused writes, want the 60m default", left)
		}
	})

	t.Run("configured_ttl_applies_to_claim_and_heartbeat", func(t *testing.T) {
		out := bdRunOK(t, bd, dir, "config", "set", "lease.ttl", "30m")
		if strings.Contains(out, "not a recognized config key") {
			t.Fatalf("config set lease.ttl warned about an unrecognized key:\n%s", out)
		}

		claimed := bdCreate(t, bd, dir, "Claimed for half an hour", "--type", "task")
		bdUpdate(t, bd, dir, claimed.ID, "--claim", "--actor", "worker")
		if left := leaseLeft(t, claimed.ID); !near(left, 30*time.Minute) {
			t.Errorf("update --claim: lease has %v left, want about 30m", left)
		}

		ready := bdCreate(t, bd, dir, "Ready-claimed for half an hour", "--type", "task", "--priority", "0")
		bdRunOK(t, bd, dir, "ready", "--claim", "--actor", "scout", "--json")
		if left := leaseLeft(t, ready.ID); !near(left, 30*time.Minute) {
			t.Errorf("ready --claim: lease has %v left, want about 30m", left)
		}

		// A heartbeat renews for the length configured when it runs.
		bdRunOK(t, bd, dir, "config", "set", "lease.ttl", "2h")
		bdRunOK(t, bd, dir, "heartbeat", claimed.ID, "--actor", "worker")
		if left := leaseLeft(t, claimed.ID); !near(left, 2*time.Hour) {
			t.Errorf("heartbeat: lease has %v left, want about 2h", left)
		}
	})
}
