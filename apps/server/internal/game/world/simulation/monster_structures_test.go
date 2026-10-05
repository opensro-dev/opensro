package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
structureFortressState

The Jangan fortress world holding one fort stone and one guard tower.
================
*/
func structureFortressState(t *testing.T) (*MonsterState, instance.Lease) {
	t.Helper()
	s := NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			19553: {RefObjID: 19553, MaxHP: 900, ScaleDenom: 100, Structure: true, TypeID4: 1},
			19536: {RefObjID: 19536, MaxHP: 500, ScaleDenom: 100, Structure: true, TypeID4: 2},
		},
		[]monster.NestRow{
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19553, RegionID: 0x62aa, X: 100, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1},
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19536, RegionID: 0x62aa, X: 140, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1},
		},
	))
	now := time.UnixMilli(1)
	s.SetTimeSource(func() time.Time { return now })
	s.StartDivision("fort")
	lease, ok := s.PopulationLease("fort", instance.Pack(2, 1))
	if !ok {
		t.Fatal("INS_FORT_JA is not open")
	}
	// The nests' first hive callback runs one NestHiveTickMs after start.
	s.AdvancePopulation(now.UnixMilli() + monster.NestHiveTickMs)
	if n := len(s.PopulationInstances("fort", lease, []uint16{0x62aa})); n != 2 {
		t.Fatalf("fortress world holds %d structures, want 2", n)
	}
	return s, lease
}

/*
================
TestStructuresFallAndStandAgain

52D2B0 marks a dead structure destroyed rather than removing it; the
standing count follows, and a reinstall puts every structure back at full
hit points under new objects.
================
*/
func TestStructuresFallAndStandAgain(t *testing.T) {
	s, lease := structureFortressState(t)
	world := instance.Pack(2, 1)
	var tower monster.Instance
	for _, row := range s.PopulationInstances("fort", lease, []uint16{0x62aa}) {
		if row.Ref.TypeID4 == 2 {
			tower = row
		}
	}
	if s.StandingStructures("fort", world, 2) != 1 {
		t.Fatal("the guard tower is not standing")
	}
	if _, ok := s.MarkStructureDestroyed("fort", tower.Gid); ok {
		t.Fatal("a living structure was marked destroyed")
	}
	if hit, ok := s.ApplyDamage("fort", tower.Gid, tower.CurrentHP); !ok || !hit.Fatal {
		t.Fatal("the tower did not die")
	}
	row, ok := s.MarkStructureDestroyed("fort", tower.Gid)
	if !ok || row.StructureState != structureStateDestroyed {
		t.Fatalf("destroyed state = %+v", row)
	}
	if s.StandingStructures("fort", world, 2) != 0 {
		t.Fatal("a destroyed tower still stands")
	}
	if _, still := s.Get("fort", tower.Gid); !still {
		t.Fatal("the destroyed tower left the world")
	}
	if installed := s.ReinstallStructures("fort", world, 2); installed != 2 {
		t.Fatalf("reinstalled %d structures, want 2", installed)
	}
	if _, old := s.Get("fort", tower.Gid); old {
		t.Fatal("the destroyed tower survived the reinstall")
	}
	if s.StandingStructures("fort", world, 2) != 1 {
		t.Fatal("the reinstalled tower does not stand")
	}
	for _, row := range s.PopulationInstances("fort", lease, []uint16{0x62aa}) {
		if row.CurrentHP != row.Ref.MaxHP {
			t.Fatalf("reinstalled %d at %d hit points", row.Ref.RefObjID, row.CurrentHP)
		}
	}
}
