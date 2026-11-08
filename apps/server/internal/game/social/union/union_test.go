/*
===========================================================================

union_test.go - union slots, removal and dissolution

===========================================================================
*/
package union

import (
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
memoryStore
================
*/
type memoryStore struct {
	rows map[int64]domain.AllianceRecord
}

func (s *memoryStore) Alliances(string) ([]domain.AllianceRecord, error) {
	var out []domain.AllianceRecord
	for _, row := range s.rows {
		out = append(out, row)
	}
	return out, nil
}

func (s *memoryStore) SaveAlliance(_ string, record domain.AllianceRecord, present bool) error {
	if present {
		s.rows[record.AllianceID] = record
	} else {
		delete(s.rows, record.AllianceID)
	}
	return nil
}

/*
================
TestUnionFillsEightSlots

Natively the founder takes slot 0, joiners the next free slots, and a
ninth guild is refused.
================
*/
func TestUnionFillsEightSlots(t *testing.T) {
	store := &memoryStore{rows: map[int64]domain.AllianceRecord{}}
	a := New()
	if err := a.Restore("d", store); err != nil {
		t.Fatal(err)
	}
	for guild := int64(2); guild <= domain.AllianceSlots; guild++ {
		if _, err := a.Join("d", 1, guild); err != nil {
			t.Fatalf("guild %d: %v", guild, err)
		}
	}
	if _, err := a.Join("d", 1, 99); !errors.Is(err, ErrFull) {
		t.Fatalf("a ninth guild joined (%v)", err)
	}
	if _, err := a.Join("d", 1, 3); !errors.Is(err, ErrMember) {
		t.Fatalf("a member joined twice (%v)", err)
	}
	record, _ := a.Of("d", 5)
	if record.Guilds != [domain.AllianceSlots]int64{1, 2, 3, 4, 5, 6, 7, 8} || store.rows[record.AllianceID] != record {
		t.Fatalf("union %+v stored %+v", record, store.rows)
	}
	if !a.Allied("d", 2, 8) || a.Allied("d", 2, 2) || a.Allied("other", 2, 8) {
		t.Fatal("allied answers wrong")
	}
}

/*
================
TestUnionRemovalAndDissolution

A member's leaving frees its slot; the union of one guild left, or of a
departed leader, is gone from memory and the store.
================
*/
func TestUnionRemovalAndDissolution(t *testing.T) {
	store := &memoryStore{rows: map[int64]domain.AllianceRecord{}}
	a := New()
	_ = a.Restore("d", store)
	_, _ = a.Join("d", 1, 2)
	_, _ = a.Join("d", 1, 3)
	if _, dissolved, err := a.Remove("d", 2); err != nil || dissolved {
		t.Fatalf("a member's leaving dissolved (%v)", err)
	}
	if record, _ := a.Of("d", 1); record.Guilds[1] != 0 || record.Guilds[2] != 3 {
		t.Fatalf("slots after leaving %+v", record.Guilds)
	}
	if _, dissolved, _ := a.Remove("d", 3); !dissolved || len(store.rows) != 0 {
		t.Fatal("a one-guild union survived")
	}
	_, _ = a.Join("d", 4, 5)
	_, _ = a.Join("d", 4, 6)
	if _, dissolved, _ := a.Remove("d", 4); !dissolved || a.Allied("d", 5, 6) {
		t.Fatal("the leader's leaving kept the union")
	}
	if record, err := a.Join("d", 7, 8); err != nil || record.AllianceID != 7 {
		t.Fatalf("a union founded by guild 7 is %d (%v)", record.AllianceID, err)
	}
}

/*
================
TestUnionCrestSetAndRemove

The register lane's contract (#2282): a set lands only on the union
the guild leads and only over the probed revision; the staff remove
clears whichever guild stands in the union; and every change saves.
================
*/
func TestUnionCrestSetAndRemove(t *testing.T) {
	store := &memoryStore{rows: map[int64]domain.AllianceRecord{}}
	a := New()
	_ = a.Restore("d", store)
	_, _ = a.Join("d", 1, 2)

	if _, refusal := a.CrestState("d", 99); refusal != CrestRefusalNoUnion {
		t.Fatalf("no-union state = %v", refusal)
	}
	if _, refusal := a.CrestState("d", 2); refusal != CrestRefusalNotMasterGuild {
		t.Fatalf("member-guild state = %v", refusal)
	}
	if record, refusal := a.CrestState("d", 1); refusal != CrestRefusalNone || record.Crest != 0 {
		t.Fatalf("leader state = %+v, %v", record, refusal)
	}
	if _, refusal, err := a.SetCrest("d", 2, 0, 5); err != nil || refusal != CrestRefusalNotMasterGuild {
		t.Fatalf("member-guild set = %v, %v", refusal, err)
	}
	record, refusal, err := a.SetCrest("d", 1, 0, 5)
	if err != nil || refusal != CrestRefusalNone || record.Crest != 5 {
		t.Fatalf("leader set = %+v, %v, %v", record, refusal, err)
	}
	if store.rows[record.AllianceID].Crest != 5 {
		t.Fatal("the set did not save")
	}
	// A second set over a stale probe refuses: the crest changed.
	if _, refusal, err := a.SetCrest("d", 1, 0, 6); err != nil || refusal != CrestRefusalHasCrest {
		t.Fatalf("stale set = %v, %v", refusal, err)
	}
	if record, refusal, err := a.SetCrest("d", 1, 5, 6); err != nil || refusal != CrestRefusalNone || record.Crest != 6 {
		t.Fatalf("probed set = %+v, %v, %v", record, refusal, err)
	}
	// The staff remove clears from any member guild's standing.
	if record, refusal, err := a.RemoveCrest("d", 2); err != nil || refusal != CrestRefusalNone || record.Crest != 0 {
		t.Fatalf("staff remove = %+v, %v, %v", record, refusal, err)
	}
	if store.rows[record.AllianceID].Crest != 0 {
		t.Fatal("the remove did not save")
	}
	if _, refusal, err := a.RemoveCrest("d", 1); err != nil || refusal != CrestRefusalNoCrest {
		t.Fatalf("a second remove = %v, %v", refusal, err)
	}
}
