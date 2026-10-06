/*
===========================================================================

battle_test.go - native fortress score, rank, checkpoint and release boundaries

===========================================================================
*/
package fortress

import (
	"math"
	"opensro.online/server/internal/domain"
	"testing"
)

/*
================
battleFixture
================
*/
func battleFixture(t *testing.T) (*Authority, *memoryFortressStore) {
	t.Helper()
	store := &memoryFortressStore{records: map[uint32]domain.FortressRecord{}, requests: map[int64]domain.FortressRequestRecord{}}
	a := New([]Catalog{{ID: 1}})
	if err := a.Restore("a", store); err != nil {
		t.Fatal(err)
	}
	a.SetPeriod("a", PeriodWar, true)
	return a, store
}

/*
================
TestBattleRankTransitionsAndIndependentCheckpoints
================
*/
func TestBattleRankTransitionsAndIndependentCheckpoints(t *testing.T) {
	a, store := battleFixture(t)
	change := BattleChange{FortressID: 1, CharacterID: 7, Kill: true}
	for count := uint32(1); count <= 161; count++ {
		change.NowMs = int64(count) * 1000
		row, next, err := a.RecordBattle("a", change)
		if err != nil || row.Kills != count || row.Deaths != 0 {
			t.Fatalf("count %d: %+v %v", count, row, err)
		}
		threshold, _, valid := BattleRank(row.Rank + 1)
		if valid && count == threshold {
			if next != row.Rank+1 {
				t.Fatalf("no promotion at %d", count)
			}
			if err := a.CommitBattleRank("a", change, next); err != nil {
				t.Fatal(err)
			}
		} else if next != 0 {
			t.Fatalf("early promotion at %d", count)
		}
		expected := uint32(1)
		for rank := uint8(1); rank <= MaxBattleRank; rank++ {
			n, _, _ := BattleRank(rank)
			if count >= n {
				expected = n
			}
		}
		if count >= 151 {
			expected = 151
		}
		if count >= 161 {
			expected = 161
		}
		if got := store.records[1].BattleRecords[0].Kills; got != expected {
			t.Fatalf("checkpoint at %d = %d want %d", count, got, expected)
		}
	}
	// One character's first death must not flush another's unsaved kill.
	change.NowMs++
	a.RecordBattle("a", change)
	if _, _, err := a.RecordBattle("a", BattleChange{FortressID: 1, CharacterID: 9}); err != nil {
		t.Fatal(err)
	}
	if got := store.records[1].BattleRecords[0].Kills; got != 161 {
		t.Fatalf("another character flushed %d kills", got)
	}
	a.ReleaseBattleRecords("a", 1)
	if _, ok := a.BattleRecord("a", 1, 7); ok {
		t.Fatal("live score survived release")
	}
	restarted := New([]Catalog{{ID: 1}})
	if err := restarted.Restore("a", store); err != nil {
		t.Fatal(err)
	}
	if row, ok := restarted.BattleRecord("a", 1, 7); !ok || row.Kills != 161 || row.Rank != 6 {
		t.Fatalf("restart %+v %v", row, ok)
	}
}

/*
================
TestBattleAdmissionFailureAndUnsignedCounts
================
*/
func TestBattleAdmissionFailureAndUnsignedCounts(t *testing.T) {
	a, store := battleFixture(t)
	a.SetPeriod("a", PeriodWar, false)
	change := BattleChange{FortressID: 1, CharacterID: 7, Kill: true}
	if _, _, err := a.RecordBattle("a", change); err == nil {
		t.Fatal("scored outside war")
	}
	a.SetPeriod("a", PeriodWar, true)
	store.fail = true
	if row, _, err := a.RecordBattle("a", change); err == nil || row.Kills != 1 {
		t.Fatalf("failed write discarded live score %+v %v", row, err)
	}
	store.fail = false
	a.mu.Lock()
	r := a.divisionLocked("a").records[1]
	r.battles[7] = domain.FortressBattleRecord{CharacterID: 7, Kills: math.MaxUint32, Deaths: math.MaxUint32, Rank: 6}
	a.mu.Unlock()
	row, next, err := a.RecordBattle("a", change)
	if err != nil || row.Kills != 0 || next != 0 {
		t.Fatalf("unsigned kill %+v %d %v", row, next, err)
	}
	change.Kill = false
	row, _, err = a.RecordBattle("a", change)
	if err != nil || row.Deaths != 0 {
		t.Fatalf("unsigned death %+v %v", row, err)
	}
}
