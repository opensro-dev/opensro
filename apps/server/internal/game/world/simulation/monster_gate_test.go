/*
===========================================================================

monster_gate_test.go - gate state transitions share the damage/death owner

Synthetic populations keep mask and death-order regressions active in CI.

===========================================================================
*/
package simulation

import (
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
gateStateFixture
================
*/
func gateStateFixture(t *testing.T) (*MonsterState, monster.Instance) {
	t.Helper()
	const ref = 19560
	s := NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			ref: {RefObjID: ref, MaxHP: 2000, ScaleDenom: 100, Structure: true, TypeID4: structureKindGate},
		},
		[]monster.NestRow{{WorldCode: "INS_FORT_JA", PolicyPinned: true, MaxCount: 1, EventStructID: 88,
			SpawnPoint: monster.SpawnPoint{RefObjID: ref, RegionID: 0x62aa, X: 100, Y: 20, Z: 100}}},
	))
	s.SetTimeSource(func() time.Time { return time.UnixMilli(1) })
	s.StartDivision("fort")
	s.AdvancePopulation(1 + monster.NestHiveTickMs)
	rows := s.WorldStructures("fort", instance.Pack(2, 1))
	if len(rows) != 1 {
		t.Fatalf("got %d gates", len(rows))
	}
	return s, rows[0]
}

/*
================
TestGateStateMasksPreserveHP

4CF8EA..4CF947: intersecting nonzero masks do nothing; disjoint masks
are ORed, and zero clears a nonzero word.
================
*/
func TestGateStateMasksPreserveHP(t *testing.T) {
	for _, tc := range []struct {
		initial, request, want uint16
		changed                bool
	}{
		{0, 0, 0, false}, {0, 2, 2, true}, {0, 1, 1, true},
		{2, 0, 0, true}, {2, 1, 3, true}, {2, 2, 2, false},
		{2, 3, 2, false}, {2, 4, 6, true}, {2, 0xffff, 2, false},
	} {
		t.Run(fmt.Sprintf("%d to %d", tc.initial, tc.request), func(t *testing.T) {
			s, gate := gateStateFixture(t)
			s.RestoreStructure("fort", gate.Gid, 1500, tc.initial)
			result, changed := s.SetGateState("fort", gate.Gid, tc.request)
			stored, _ := s.Get("fort", gate.Gid)
			if changed != tc.changed || result.Gid != gate.Gid || result.StructureState != tc.want ||
				result.CurrentHP != 1500 || stored.CurrentHP != 1500 || stored.StructureState != tc.want {
				t.Fatalf("changed=%v, result=%+v, stored=%+v", changed, result, stored)
			}
		})
	}
}

/*
================
TestGateStateCannotOverwriteDeath

Drive the two dangerous orders explicitly: a fatal hit before its queued
settlement, and settled death after a caller has retained an old snapshot.
================
*/
func TestGateStateCannotOverwriteDeath(t *testing.T) {
	for _, settled := range []bool{false, true} {
		t.Run(fmt.Sprint(settled), func(t *testing.T) {
			s, snapshot := gateStateFixture(t)
			if hit, ok := s.ApplyDamage("fort", snapshot.Gid, snapshot.CurrentHP); !ok || !hit.Fatal {
				t.Fatal("fatal hit failed")
			}
			if settled {
				if _, ok := s.MarkStructureDestroyed("fort", snapshot.Gid); !ok {
					t.Fatal("death did not settle")
				}
			}
			if row, changed := s.SetGateState("fort", snapshot.Gid, 2); changed || row.Gid != 0 {
				t.Fatalf("dead gate accepted transition: %+v", row)
			}
			stored, _ := s.Get("fort", snapshot.Gid)
			wantState := uint16(0)
			if settled {
				wantState = structureStateGateDestroyed
			}
			if stored.CurrentHP != 0 || stored.StructureState != wantState {
				t.Fatalf("death overwritten: HP=%d state=%d", stored.CurrentHP, stored.StructureState)
			}
		})
	}
}

/*
================
TestGateRestorePreservesIndependentHPAndState

Persistence restores the pair the state-only operation can produce, while
a genuine dead gate retains zero HP and the population still clamps HP.
================
*/
func TestGateRestorePreservesIndependentHPAndState(t *testing.T) {
	for _, tc := range []struct {
		hp, wantHP uint32
		state      uint16
	}{{1500, 1500, 1}, {1500, 1500, 3}, {0, 0, 3}, {3000, 2000, 2}} {
		s, gate := gateStateFixture(t)
		if !s.RestoreStructure("fort", gate.Gid, tc.hp, tc.state) {
			t.Fatal("restore refused")
		}
		row, _ := s.Get("fort", gate.Gid)
		if row.CurrentHP != tc.wantHP || row.StructureState != tc.state {
			t.Fatalf("restore HP=%d state=%d became HP=%d state=%d", tc.hp, tc.state, row.CurrentHP, row.StructureState)
		}
	}
}
