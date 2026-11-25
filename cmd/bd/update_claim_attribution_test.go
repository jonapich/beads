package main

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/httpclient/wire"
	"github.com/steveyegge/beads/internal/storage"
	storageissueops "github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/issueops"
)

// A claim with a session id carries claim.session and claim.at as metadata
// edits. Against a server that predates issues.update.claim a claim is only
// possible when it carries NO patch, so the attribution is dropped, and only
// when nothing else would be.
func TestUpdateWithoutClaimAttribution(t *testing.T) {
	attribution := claimMetadataEdits("sess-1", time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC))
	build := func(t *testing.T, updates map[string]interface{}) issueops.IssuePatch {
		t.Helper()
		patch, err := buildUpdatePatch(updates)
		if err != nil {
			t.Fatalf("buildUpdatePatch: %v", err)
		}
		return patch
	}

	t.Run("claim with only the attribution becomes a bare claim", func(t *testing.T) {
		updates := map[string]interface{}{storageissueops.OpSetMetadata: append([]string(nil), attribution...)}
		base := build(t, updates)
		if reflect.DeepEqual(base, issueops.IssuePatch{}) {
			t.Fatal("test setup: the attribution produced no patch")
		}
		bare, ok := updateWithoutClaimAttribution(updates, attribution, base, base)
		if !ok {
			t.Fatal("a claim whose only patch is its attribution was not reduced to a bare claim")
		}
		if !reflect.DeepEqual(bare, issueops.IssuePatch{}) {
			t.Errorf("bare patch = %+v, want empty", bare)
		}
	})

	t.Run("a claim that also edits a field is not reduced", func(t *testing.T) {
		updates := map[string]interface{}{
			"title":                       "new title",
			storageissueops.OpSetMetadata: append([]string(nil), attribution...),
		}
		base := build(t, updates)
		if _, ok := updateWithoutClaimAttribution(updates, attribution, base, base); ok {
			t.Error("a claim beside a title edit lost its patch: the user's edit would be dropped")
		}
	})

	t.Run("a claim that also sets other metadata is not reduced", func(t *testing.T) {
		updates := map[string]interface{}{storageissueops.OpSetMetadata: append([]string{"k=v"}, attribution...)}
		base := build(t, updates)
		if _, ok := updateWithoutClaimAttribution(updates, attribution, base, base); ok {
			t.Error("a claim beside the user's own metadata edit lost it")
		}
	})

	t.Run("a patch altered for the issue is not reduced", func(t *testing.T) {
		updates := map[string]interface{}{storageissueops.OpSetMetadata: append([]string(nil), attribution...)}
		base := build(t, updates)
		altered := base
		altered.Status = issueops.Field[issueops.Status]{Set: true, Value: "open"}
		if _, ok := updateWithoutClaimAttribution(updates, attribution, base, altered); ok {
			t.Error("a per-issue status change was dropped with the attribution")
		}
	})

	t.Run("the caller's map is left as it was", func(t *testing.T) {
		updates := map[string]interface{}{storageissueops.OpSetMetadata: append([]string(nil), attribution...)}
		base := build(t, updates)
		_, _ = updateWithoutClaimAttribution(updates, attribution, base, base)
		if got, _ := updates[storageissueops.OpSetMetadata].([]string); len(got) != len(attribution) {
			t.Errorf("updates metadata edits = %v after the call, want them untouched", got)
		}
	})
}

func TestIsMissingUpdateClaimCapability(t *testing.T) {
	skew := fmt.Errorf("updating issue: %w", &storage.ErrUnsupported{Op: "Lifecycle.Update", Backend: "http", Capability: wire.CapIssuesUpdateClaim})
	if !isMissingUpdateClaimCapability(skew) {
		t.Error("the missing issues.update.claim refusal was not recognised")
	}
	other := &storage.ErrUnsupported{Op: "Lifecycle.Update", Backend: "http", Capability: wire.CapIssuesUpdatePersistence}
	if isMissingUpdateClaimCapability(other) {
		t.Error("a refusal naming another capability was taken for the claim's")
	}
	if isMissingUpdateClaimCapability(errors.New("boom")) || isMissingUpdateClaimCapability(nil) {
		t.Error("an unrelated error was taken for the claim capability refusal")
	}
}
