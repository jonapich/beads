package httpapi

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/domain"
	storeops "github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/storage/uow"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/issueops"
)

// These tests pin the lease length a claim, a ready-claim, a claiming update and
// a heartbeat are granted when they run INSIDE `bd serve`: the workspace's
// lease.ttl, read per request, not the built-in default a client's context could
// never override over the wire. The roles are fakes that record the length the
// context they were called with carries (issueops.LeaseTTLFromContext), which is
// what the storage layer reads when it stamps lease_expires_at.

// ttlSettings is a settings role whose lease.ttl can change between requests,
// the way `bd config set lease.ttl` changes it under a running server.
type ttlSettings struct {
	*roleSettings

	mu    sync.Mutex
	value string
	reads int
}

func (s *ttlSettings) set(value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = value
}

func (s *ttlSettings) readCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

func (s *ttlSettings) GetSetting(_ context.Context, req issueops.GetSettingRequest) (issueops.SettingResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	return issueops.SettingResult{Key: req.Key, Value: s.value}, nil
}

// ttlLog records the lease length each role call carried.
type ttlLog struct {
	mu   sync.Mutex
	seen []time.Duration
}

func (l *ttlLog) record(ctx context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, storeops.LeaseTTLFromContext(ctx))
}

func (l *ttlLog) last(t *testing.T) time.Duration {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.seen) == 0 {
		t.Fatal("the role was never called")
	}
	return l.seen[len(l.seen)-1]
}

func (l *ttlLog) calls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.seen)
}

type ttlClaimer struct {
	*roleClaimer
	ttl ttlLog
}

func (c *ttlClaimer) Claim(ctx context.Context, req issueops.ClaimRequest) (issueops.ClaimResult, error) {
	c.ttl.record(ctx)
	return c.roleClaimer.Claim(ctx, req)
}

type ttlReadyClaimer struct {
	*roleReadyClaimer
	ttl ttlLog
}

func (c *ttlReadyClaimer) ClaimNext(ctx context.Context, req issueops.ClaimNextRequest) (issueops.ClaimNextResult, error) {
	c.ttl.record(ctx)
	return c.roleReadyClaimer.ClaimNext(ctx, req)
}

type ttlHeartbeater struct {
	*roleHeartbeater
	ttl ttlLog
}

func (h *ttlHeartbeater) Heartbeat(ctx context.Context, req issueops.HeartbeatRequest) (issueops.HeartbeatResult, error) {
	h.ttl.record(ctx)
	return h.roleHeartbeater.Heartbeat(ctx, req)
}

type ttlLifecycle struct {
	*roleLifecycle
	ttl ttlLog
}

func (l *ttlLifecycle) Update(ctx context.Context, req issueops.UpdateRequest) (issueops.UpdateResult, error) {
	l.ttl.record(ctx)
	return l.roleLifecycle.Update(ctx, req)
}

type leaseServer struct {
	*testServer
	settings    *ttlSettings
	claimer     *ttlClaimer
	readyClaims *ttlReadyClaimer
	heartbeats  *ttlHeartbeater
	lifecycle   *ttlLifecycle
}

func newLeaseServer(t *testing.T, fromSettings bool, storedTTL string) *leaseServer {
	t.Helper()
	s := &leaseServer{
		settings: &ttlSettings{roleSettings: &roleSettings{}, value: storedTTL},
		claimer: &ttlClaimer{roleClaimer: &roleClaimer{result: issueops.ClaimResult{
			Issue: seededIssue("bd-1", "alice", types.StatusInProgress), Changed: true,
		}}},
		readyClaims: &ttlReadyClaimer{roleReadyClaimer: &roleReadyClaimer{
			result: issueops.ClaimNextResult{Claimed: readyRow("bd-1")},
		}},
		heartbeats: &ttlHeartbeater{roleHeartbeater: &roleHeartbeater{result: issueops.HeartbeatResult{
			IssueID: "bd-1", Holder: "alice",
			HeartbeatAt: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC), LeaseExpiresAt: time.Date(2026, 10, 16, 13, 0, 0, 0, time.UTC),
		}}},
		lifecycle: &ttlLifecycle{roleLifecycle: &roleLifecycle{
			updateResult: issueops.UpdateResult{Issue: updatedIssue("bd-1"), Changed: true},
		}},
	}
	s.testServer = newTestServer(t, rolesConfig(Config{
		Claimer:              s.claimer,
		ReadyClaimer:         s.readyClaims,
		Heartbeater:          s.heartbeats,
		Lifecycle:            s.lifecycle,
		Settings:             s.settings,
		LeaseTTLFromSettings: fromSettings,
	}))
	return s
}

// grants drives each of the four lease-granting operations once and returns the
// lease length each role was called with, by operation.
func (s *leaseServer) grants(t *testing.T) map[string]time.Duration {
	t.Helper()
	for name, do := range s.operations(t) {
		if resp := do(); resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200: %s", name, resp.StatusCode, readAll(t, resp))
		}
	}
	return map[string]time.Duration{
		"claim":     s.claimer.ttl.last(t),
		"claimNext": s.readyClaims.ttl.last(t),
		"heartbeat": s.heartbeats.ttl.last(t),
		"update":    s.lifecycle.ttl.last(t),
	}
}

func (s *leaseServer) operations(t *testing.T) map[string]func() *http.Response {
	return map[string]func() *http.Response{
		"claim":     func() *http.Response { return s.claim(t, claimPath, `{"actor":"alice"}`) },
		"claimNext": func() *http.Response { return s.claimNext(t, claimNextPath, `{"actor":"alice"}`) },
		"heartbeat": func() *http.Response { return s.claim(t, heartbeatPath, `{"actor":"alice"}`) },
		"update": func() *http.Response {
			return s.updateIssue(t, updatePath, `{"actor":"alice","claim":true,"patch":{}}`)
		},
	}
}

func assertTTLs(t *testing.T, got map[string]time.Duration, want time.Duration) {
	t.Helper()
	for name, ttl := range got {
		if ttl != want {
			t.Errorf("%s was granted a %v lease, want %v", name, ttl, want)
		}
	}
}

// TestServedClaimsAndHeartbeatsGrantTheConfiguredLeaseTTL is the point of the
// setting: a claim, a ready-claim, a claiming update and a heartbeat executed by
// the server are as long as lease.ttl says, not the 60m default.
func TestServedClaimsAndHeartbeatsGrantTheConfiguredLeaseTTL(t *testing.T) {
	s := newLeaseServer(t, true, "90s")
	assertTTLs(t, s.grants(t), 90*time.Second)
}

// TestServedLeaseTTLFollowsTheSettingWithoutARestart: the value is read per
// request, so a `bd config set lease.ttl` made while the server runs is the next
// request's length, and unsetting it returns the default.
func TestServedLeaseTTLFollowsTheSettingWithoutARestart(t *testing.T) {
	s := newLeaseServer(t, true, "90s")
	assertTTLs(t, s.grants(t), 90*time.Second)

	s.settings.set("2h")
	assertTTLs(t, s.grants(t), 2*time.Hour)

	s.settings.set("")
	assertTTLs(t, s.grants(t), storeops.DefaultLeaseTTL)
}

// TestServedLeaseTTLIsTheDefaultWhenTheSettingIsUnset: no lease.ttl means the
// built-in 60m, which is what the CLI's own routes grant too.
func TestServedLeaseTTLIsTheDefaultWhenTheSettingIsUnset(t *testing.T) {
	s := newLeaseServer(t, true, "")
	assertTTLs(t, s.grants(t), storeops.DefaultLeaseTTL)
}

// TestServedLeaseTTLIgnoresTheSettingUnlessAsked: a server built without
// LeaseTTLFromSettings grants the default and never reads the setting, so every
// existing deployment of the library behaves as it did.
func TestServedLeaseTTLIgnoresTheSettingUnlessAsked(t *testing.T) {
	s := newLeaseServer(t, false, "90s")
	assertTTLs(t, s.grants(t), storeops.DefaultLeaseTTL)
	if n := s.settings.readCount(); n != 0 {
		t.Errorf("the setting was read %d times with LeaseTTLFromSettings off, want 0", n)
	}
}

// TestServedUpdateWithoutAClaimReadsNoSetting: only a claiming update grants a
// lease, so any other update pays for no settings read.
func TestServedUpdateWithoutAClaimReadsNoSetting(t *testing.T) {
	s := newLeaseServer(t, true, "90s")
	resp := s.updateIssue(t, updatePath, `{"actor":"alice","patch":{"title":"new"}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, readAll(t, resp))
	}
	if n := s.settings.readCount(); n != 0 {
		t.Errorf("a non-claiming update read the setting %d times, want 0", n)
	}
	if n := s.lifecycle.ttl.calls(); n != 1 {
		t.Errorf("the lifecycle role was called %d times, want 1", n)
	}
}

// TestServedLeaseWithAnUnusableSettingRefusesBeforeTheRole: a stored lease.ttl
// that does not parse is the workspace's fault, so the request fails with a 500
// and nothing reaches the role; the role is not run with a shorter lease than
// the operator asked for.
func TestServedLeaseWithAnUnusableSettingRefusesBeforeTheRole(t *testing.T) {
	s := newLeaseServer(t, true, "not-a-duration")
	for name, do := range s.operations(t) {
		if resp := do(); resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want 500: %s", name, resp.StatusCode, readAll(t, resp))
		}
	}
	for name, n := range map[string]int{
		"claim": s.claimer.ttl.calls(), "claimNext": s.readyClaims.ttl.calls(),
		"heartbeat": s.heartbeats.ttl.calls(), "update": s.lifecycle.ttl.calls(),
	} {
		if n != 0 {
			t.Errorf("%s reached its role %d times despite an unusable lease.ttl", name, n)
		}
	}
}

// ttlProvider is a unit-of-work provider that records the lease length each unit
// of work was opened with, which is the context the claim's use case then runs
// under. The settings read opens one too, before the claim's, so the claim's is
// the last.
type ttlProvider struct {
	fakeProvider

	tmu  sync.Mutex
	seen []time.Duration
}

func (p *ttlProvider) NewUOW(ctx context.Context) (uow.UnitOfWork, error) {
	p.tmu.Lock()
	p.seen = append(p.seen, storeops.LeaseTTLFromContext(ctx))
	p.tmu.Unlock()
	return p.fakeProvider.NewUOW(ctx)
}

func (p *ttlProvider) lastTTL(t *testing.T) time.Duration {
	t.Helper()
	p.tmu.Lock()
	defer p.tmu.Unlock()
	if len(p.seen) == 0 {
		t.Fatal("no unit of work was opened")
	}
	return p.seen[len(p.seen)-1]
}

// ttlConfig answers the settings read the provider source makes.
type ttlConfig struct {
	domain.ConfigUseCase
	value string
}

func (c ttlConfig) GetConfig(context.Context, string) (string, error) { return c.value, nil }

// TestServedClaimOnTheProviderSourceGrantsTheConfiguredLeaseTTL is the same
// proof for the other source `bd serve` answers from: the setting is read
// through the provider's own settings role, and the claim's unit of work is
// opened with that length.
func TestServedClaimOnTheProviderSourceGrantsTheConfiguredLeaseTTL(t *testing.T) {
	provider := &ttlProvider{fakeProvider: fakeProvider{
		issues:     &fakeIssues{issue: seededIssue("bd-1", "alice", types.StatusInProgress)},
		readConfig: ttlConfig{value: "45m"},
	}}
	ts := newTestServer(t, Config{Provider: provider, LeaseTTLFromSettings: true})

	resp := ts.claim(t, claimPath, `{"actor":"alice"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, readAll(t, resp))
	}
	if got := provider.lastTTL(t); got != 45*time.Minute {
		t.Errorf("the claim's unit of work was opened with a %v lease, want 45m", got)
	}
}
