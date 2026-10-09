/*
===========================================================================

operator_pk_test.go - active PK recovery preserves criminal history

===========================================================================
*/
package action

import (
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestOperatorClearPKPreservesHistoryAndRepairsKeeper
================
*/
func TestOperatorClearPKPreservesHistoryAndRepairsKeeper(t *testing.T) {
	for _, record := range []*domain.PKRecord{
		nil,
		{},
		{DailyCount: 15, DailyDay: 20261010, TotalCount: 9, Penalty: 12000},
		{DailyCount: 15, DailyDay: 20261010, TotalCount: 9},
		{DailyCount: 4, DailyDay: 20261010, TotalCount: 2, Penalty: 100, TotalDecayAt: 1234567},
	} {
		c := testCharacter()
		c.PK = record
		c.Aggressions = map[uint32]uint32{123: 20}
		rt, _ := newTestRuntime(c, testItems())
		key := simulation.WorldKey(testDivision, c.Name)
		rt.aggressionActors.Store(key, playerAggressionClock{testDivision, c.Name, 1})
		before := c.Snapshot()
		if err := rt.OperatorClearPK(testDivision, c.Name); err != nil {
			t.Fatal(err)
		}
		if c.PVPState() != 0 || len(c.Aggressions) != 0 {
			t.Fatalf("active PK remains: %+v / %v", c.PK, c.Aggressions)
		}
		if _, exists := rt.aggressionActors.Load(key); exists {
			t.Fatal("aggression clock survived")
		}
		if before.PK != nil {
			before.PK.Penalty = 0
			if before.PK.TotalCount > 0 && before.PK.TotalDecayAt == 0 {
				before.PK.TotalDecayAt = rt.Now().Add(48 * time.Hour).Unix()
			}
		}
		before.Aggressions = nil
		if !reflect.DeepEqual(before, c.Snapshot()) {
			t.Fatal("clear changed history or unrelated character state")
		}
		if pk.RepairKeeper(c, rt.Now()) != 0 {
			t.Fatal("reconnect would restore a red penalty")
		}
		if err := rt.OperatorClearPK(testDivision, c.Name); err != nil || !reflect.DeepEqual(before, c.Snapshot()) {
			t.Fatal("repeated clear changed keeper or history", err)
		}
		info, err := rt.OperatorCharacter(testDivision, c.Name)
		if err != nil || !reflect.DeepEqual(info["pk"], c.PK) || info["pvpState"] != uint8(0) {
			t.Fatalf("audit projection: %v / %v", info, err)
		}
	}
}

/*
================
TestOperatorClearPKRefusesMissingAndDeleted
================
*/
func TestOperatorClearPKRefusesMissingAndDeleted(t *testing.T) {
	c := testCharacter()
	c.DeletePending = true
	c.PK = &domain.PKRecord{Penalty: 1200, DailyCount: 5, TotalCount: 2}
	c.Aggressions = map[uint32]uint32{123: 20}
	rt, _ := newTestRuntime(c, testItems())
	before := c.Snapshot()
	for _, name := range []string{"missing", c.Name} {
		if err := rt.OperatorClearPK(testDivision, name); err == nil {
			t.Fatal("invalid character accepted")
		}
		if !reflect.DeepEqual(before, c.Snapshot()) {
			t.Fatal("refusal mutated character")
		}
	}
}

/*
================
TestOperatorClearPKPersistsThroughAuthorityRestart
================
*/
func TestOperatorClearPKPersistsThroughAuthorityRestart(t *testing.T) {
	seed := testCharacter()
	seed.PK = &domain.PKRecord{Penalty: 1200, DailyCount: 15, DailyDay: 20261010, TotalCount: 5}
	d := openDoorRuntime(t, t.TempDir(), seed)
	deps := d.rt.deps.(*enterworld.Deps)
	deps.UpdateCharacter = d.authority.UpdateCharacter
	deps.ReadCharacter = func(_ string, read func()) { d.authority.ReadState(read) }
	if err := d.rt.OperatorClearPK(testDivision, d.character.Name); err != nil {
		t.Fatal(err)
	}
	want := d.character.Snapshot().PK
	if health := d.authority.Health(); health.LastError != "" {
		t.Fatalf("storage: %+v", health)
	}
	if got := d.reboot(t).character.PK; !reflect.DeepEqual(want, got) {
		t.Fatalf("PK record did not survive restart: %+v / %+v", got, want)
	}
}
