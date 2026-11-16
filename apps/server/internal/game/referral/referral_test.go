/*
===========================================================================

referral_test.go - the sweep pays the qualified and pages past the rest

===========================================================================
*/
package referral

import (
	"fmt"
	"sort"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
fakeStore

Pending referrals by account, each with its account's level.
================
*/
type fakeStore struct {
	levels map[string]int64
	paid   map[string]bool
}

func (f *fakeStore) PendingReferrals(after string, limit int) ([]domain.Referral, error) {
	names := []string{}
	for name := range f.levels {
		if !f.paid[name] && name > after {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > limit {
		names = names[:limit]
	}
	out := make([]domain.Referral, len(names))
	for i, name := range names {
		out[i] = domain.Referral{Referee: name}
	}
	return out, nil
}

func (f *fakeStore) QualifyReferral(referee string, level int64, _ domain.ReferralRewards) (bool, error) {
	if f.levels[referee] < level {
		return false, nil
	}
	f.paid[referee] = true
	return true, nil
}

/*
================
TestSweepPagesPastPlayersWhoNeverQualify

A full batch of players below the level must not hide the one behind
them: the cursor carries the next pass past them, then wraps.
================
*/
func TestSweepPagesPastPlayersWhoNeverQualify(t *testing.T) {
	store := &fakeStore{levels: map[string]int64{}, paid: map[string]bool{}}
	for i := range SweepBatch {
		store.levels[fmt.Sprintf("a%04d", i)] = 1
	}
	store.levels["z-qualified"] = QualifyingLevel

	paid, cursor := Sweep(store, "")
	if paid != 0 || cursor == "" {
		t.Fatalf("first pass paid %d with cursor %q, want 0 and a cursor", paid, cursor)
	}
	paid, cursor = Sweep(store, cursor)
	if paid != 1 || !store.paid["z-qualified"] || cursor != "" {
		t.Fatalf("second pass paid %d, cursor %q, want the qualified player and a wrap", paid, cursor)
	}
	if paid, _ = Sweep(store, cursor); paid != 0 {
		t.Fatalf("paid %d again", paid)
	}
}

/*
================
TestFromEnvIsOffByDefault
================
*/
func TestFromEnvIsOffByDefault(t *testing.T) {
	t.Setenv(EnvReferrals, "")
	if FromEnv() {
		t.Fatal("referrals on with the flag unset")
	}
	t.Setenv(EnvReferrals, "on")
	if !FromEnv() {
		t.Fatal("referrals off with the flag on")
	}
}
