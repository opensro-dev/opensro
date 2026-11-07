package store

import (
	"opensro.online/server/internal/game/item/grounditem"
	"testing"
)

func TestGroundPopulationPersistenceThroughSQLite(t *testing.T) {
	t.Parallel()

	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	r := grounditem.NewRegistry()
	s.AttachGround(r)
	main := r.Add(testDivision, grounditem.Item{RefObjID: 3117, MagicOptions: []uint64{0xffffffffffffffff}})
	foreign := r.Add(testDivision, grounditem.Item{RefObjID: 3117, Population: grounditem.Population{World: 0x1000a, Generation: 2}})
	s.Mutate("ground-population-test", nil)
	s.Close()
	s = openTest(t, dir, clock)
	snapshot := s.GroundSnapshotForRestore()
	snapshot.Divisions[testDivision][0].MagicOptions[0] = 0
	snapshot = s.GroundSnapshotForRestore()
	fresh := grounditem.NewRegistry()
	fresh.Restore(snapshot)
	got, ok := fresh.Get(testDivision, main.Gid)
	if !ok || len(got.MagicOptions) != 1 || got.MagicOptions[0] != 0xffffffffffffffff {
		t.Fatal("SQLite or snapshot copy lost magic options", got)
	}
	if _, ok := fresh.Get(testDivision, foreign.Gid); ok {
		t.Fatal("reboot adopted old population")
	}
	if got := fresh.Add(testDivision, grounditem.Item{RefObjID: 3117}); got.Gid <= foreign.Gid {
		t.Fatal("reused retired GID")
	}
}
