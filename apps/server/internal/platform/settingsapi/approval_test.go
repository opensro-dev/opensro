/*
===========================================================================

approval_test.go - the two-person approval flow and its audit

Each test runs the flow over memStore, a Store that keeps everything in
maps; the database store is checked against the same contract.

===========================================================================
*/
package settingsapi

import (
	"errors"
	"fmt"
	"testing"

	"opensro.online/server/internal/game/tunables"
)

/*
================
memStore
================
*/
type memStore struct {
	overrides map[string]string
	pending   map[string]Proposal
	audit     []AuditEntry
}

func newMemStore() *memStore {
	return &memStore{overrides: map[string]string{}, pending: map[string]Proposal{}}
}

func (m *memStore) Overrides() (map[string]string, error) { return m.overrides, nil }

func (m *memStore) Pending() ([]Proposal, error) {
	var out []Proposal
	for _, p := range m.pending {
		out = append(out, p)
	}
	return out, nil
}

func (m *memStore) Propose(p Proposal, entry AuditEntry) error {
	for _, other := range m.pending {
		if other.Key == p.Key {
			return ErrConflict
		}
	}
	m.pending[p.ID] = p
	m.append(entry)
	return nil
}

func (m *memStore) Decide(id string, check func(Proposal) (AuditEntry, bool, error)) (Proposal, error) {
	p, ok := m.pending[id]
	if !ok {
		return Proposal{}, ErrNotFound
	}
	entry, override, err := check(p)
	if err != nil {
		return p, err
	}
	delete(m.pending, id)
	if override {
		m.overrides[p.Key] = p.To
	}
	m.append(entry)
	return p, nil
}

func (m *memStore) Refuse(entry AuditEntry) error { m.append(entry); return nil }

func (m *memStore) Audit(limit int, before int64) ([]AuditEntry, error) { return m.audit, nil }

func (m *memStore) append(entry AuditEntry) {
	entry.Seq = int64(len(m.audit) + 1)
	m.audit = append(m.audit, entry)
}

/*
================
newService

A Service over a fresh memStore with a default snapshot installed.
================
*/
func newService(t *testing.T) (*Service, *memStore) {
	t.Helper()
	base, err := tunables.Parse(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tunables.Swap(base))
	store, now, ids := newMemStore(), int64(1000), 0
	return &Service{Store: store, Now: func() int64 { now++; return now },
		NewID: func() string { ids++; return fmt.Sprintf("p%d", ids) }}, store
}

var (
	alice = Staff{ID: "discord:1", Name: "Alice"}
	bob   = Staff{ID: "discord:2", Name: "Bob"}
)

/*
================
TestApprovedLiveChangeAppliesAtOnce

Alice proposes a live cap of 3, Bob approves: the running snapshot reads
3, the override is stored, and the audit holds the proposal and the
approval in order.
================
*/
func TestApprovedLiveChangeAppliesAtOnce(t *testing.T) {
	service, store := newService(t)
	p, err := service.Propose(alice, tunables.JobKillNoticesPerMinute, "3", "farmers flood chat")
	if err != nil || p.From != "6" || p.To != "3" {
		t.Fatalf("propose %+v %v", p, err)
	}
	if tunables.Current().Int(tunables.JobKillNoticesPerMinute) != 6 {
		t.Fatal("a proposal changed the running value")
	}
	result, err := service.Approve(bob, p.ID)
	if err != nil || result != ResultApplied {
		t.Fatalf("approve %q %v", result, err)
	}
	if tunables.Current().Int(tunables.JobKillNoticesPerMinute) != 3 || store.overrides[tunables.JobKillNoticesPerMinute] != "3" {
		t.Fatal("the approved live value is not running and stored")
	}
	if len(store.audit) != 2 || store.audit[0].Action != ActionPropose || store.audit[1].Action != ActionApprove ||
		store.audit[1].Who != bob.ID || store.audit[1].Result != ResultApplied {
		t.Fatalf("audit %+v", store.audit)
	}
}

/*
================
TestApprovedRestartChangeWaitsForTheNextBoot
================
*/
func TestApprovedRestartChangeWaitsForTheNextBoot(t *testing.T) {
	service, store := newService(t)
	p, err := service.Propose(alice, tunables.MostWantedTestKills, "2", "faster test")
	if err != nil {
		t.Fatal(err)
	}
	if result, err := service.Approve(bob, p.ID); err != nil || result != ResultQueued {
		t.Fatalf("approve %q %v", result, err)
	}
	if tunables.Current().Int(tunables.MostWantedTestKills) != 5 || store.overrides[tunables.MostWantedTestKills] != "2" {
		t.Fatal("a restart value changed the running shard, or was not stored")
	}
}

/*
================
TestRefusalsChangeNothingAndAreAudited

A value out of bounds, an empty reason, a second proposal for one key, a
self-approval and a withdrawal by someone else are refused; nothing is
stored or swapped, and each refusal is in the audit.
================
*/
func TestRefusalsChangeNothingAndAreAudited(t *testing.T) {
	service, store := newService(t)
	if _, err := service.Propose(alice, tunables.JobKillNoticesPerMinute, "99", "too many"); err == nil {
		t.Fatal("an out-of-bounds value was proposed")
	}
	if _, err := service.Propose(alice, tunables.JobKillNoticesPerMinute, "3", "  "); err == nil {
		t.Fatal("an empty reason was accepted")
	}
	p, err := service.Propose(alice, tunables.JobKillNoticesPerMinute, "3", "quieter")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Propose(bob, tunables.JobKillNoticesPerMinute, "4", "mine"); !errors.Is(err, ErrConflict) {
		t.Fatalf("a second pending proposal: %v", err)
	}
	if _, err := service.Approve(alice, p.ID); !errors.Is(err, errSelfApproval) {
		t.Fatalf("self approval: %v", err)
	}
	if err := service.Reject(alice, p.ID, "changed my mind"); !errors.Is(err, errSelfApproval) {
		t.Fatalf("self rejection: %v", err)
	}
	if err := service.Withdraw(bob, p.ID); !errors.Is(err, errNotProposer) {
		t.Fatalf("a withdrawal by another: %v", err)
	}
	if _, err := service.Approve(bob, "p999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	if len(store.overrides) != 0 || tunables.Current().Int(tunables.JobKillNoticesPerMinute) != 6 || len(store.pending) != 1 {
		t.Fatal("a refusal changed something")
	}
	refused := 0
	for _, entry := range store.audit {
		if entry.Result == ResultRefused {
			refused++
			if entry.Error == "" || entry.Who == "" {
				t.Fatalf("refusal without a reason or a who: %+v", entry)
			}
		}
	}
	if refused != 6 {
		t.Fatalf("%d refusals audited, want 6: %+v", refused, store.audit)
	}
}

/*
================
TestRejectAndWithdrawEndAProposal
================
*/
func TestRejectAndWithdrawEndAProposal(t *testing.T) {
	service, store := newService(t)
	p, _ := service.Propose(alice, tunables.JobKillRepeatMinutes, "30", "longer quiet")
	if err := service.Reject(bob, p.ID, "keep ten"); err != nil {
		t.Fatal(err)
	}
	q, _ := service.Propose(alice, tunables.JobKillRepeatMinutes, "20", "again")
	if err := service.Withdraw(alice, q.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.pending) != 0 || len(store.overrides) != 0 || tunables.Current().Int(tunables.JobKillRepeatMinutes) != 10 {
		t.Fatal("a rejected or withdrawn proposal changed something")
	}
	last := store.audit[len(store.audit)-1]
	if last.Action != ActionWithdraw || last.Who != alice.ID {
		t.Fatalf("last audit %+v", last)
	}
}
