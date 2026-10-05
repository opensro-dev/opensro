package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
structureRowStore

A FortressStore holding structure rows only.
================
*/
type structureRowStore struct {
	rows map[uint32]domain.FortressStructureRecord
}

func (s *structureRowStore) FortressState(string) ([]domain.FortressRecord, []domain.FortressRequestRecord, error) {
	return nil, nil, nil
}

func (s *structureRowStore) SaveFortress(string, domain.FortressRecord) error { return nil }

func (s *structureRowStore) SaveFortressRequest(string, domain.FortressRequestRecord, bool) error {
	return nil
}

func (s *structureRowStore) FortressStructures(string) ([]domain.FortressStructureRecord, error) {
	var out []domain.FortressStructureRecord
	for _, row := range s.rows {
		out = append(out, row)
	}
	return out, nil
}

func (s *structureRowStore) SaveFortressStructure(_ string, row domain.FortressStructureRecord, present bool) error {
	if present {
		s.rows[row.EventStructID] = row
	} else {
		delete(s.rows, row.EventStructID)
	}
	return nil
}

/*
================
TestStructuresKeepTheirDamageAcrossARestart

A stored zone row puts its hit points and destroyed state back on the
structure the fresh population spawned there; the next save writes the
rows the structures stand with.
================
*/
func TestStructuresKeepTheirDamageAcrossARestart(t *testing.T) {
	rt, _, clock, _ := captureFixture(t)
	jangan := uint32(0)
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			jangan = record.ID
		}
	}
	store := &structureRowStore{rows: map[uint32]domain.FortressStructureRecord{
		85: {FortressID: jangan, EventStructID: 85, RefObjID: 19536, HP: 0, State: 1},
		84: {FortressID: jangan, EventStructID: 84, RefObjID: 19553, HP: 300},
	}}
	rt.FortressStore = store
	rt.advanceFortressStructures(clock.NowMs())
	if rt.Monsters.StandingStructures(testDivision, instance.Pack(2, 1), structureKindGuardTower) != 0 {
		t.Fatal("the stored destroyed tower stands again")
	}
	for _, row := range rt.Monsters.WorldStructures(testDivision, instance.Pack(2, 1)) {
		if row.Ref.TypeID4 == structureKindFortStone && row.CurrentHP != 300 {
			t.Fatalf("the stone came back at %d hit points, want 300", row.CurrentHP)
		}
	}
	rt.Monsters.ReinstallStructures(testDivision, instance.Pack(2, 1), clock.NowMs())
	rt.forceFortressSave(testDivision)
	rt.advanceFortressStructures(clock.NowMs())
	if store.rows[85].State != 0 || store.rows[85].HP != 500 || store.rows[84].HP != 900 {
		t.Fatalf("reinstalled rows %+v", store.rows)
	}
}
