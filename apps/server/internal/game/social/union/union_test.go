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

5B8C70: the founder takes slot 0, joiners the next free slots, and a
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
