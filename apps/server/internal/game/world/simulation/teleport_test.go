package simulation

import (
	"encoding/binary"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
)

func TestTeleportGateRosterAndWire(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	rows, err := AppendTeleportGates(dir, LoadNpcWorldRoster(dir))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[uint32]bool{}
	for _, row := range rows {
		if row.Teleport == nil {
			continue
		}
		seen[row.RefObjID] = true
		p := BuildNpcCreateRow(row, Spawn{})
		if len(p) != 24 || binary.LittleEndian.Uint32(p) != row.RefObjID || binary.LittleEndian.Uint32(p[4:]) != row.ObjectID || binary.LittleEndian.Uint16(p[8:]) != row.Spawn.RegionID {
			t.Fatalf("gate wire %s %x", row.Codename, p)
		}
		if row.RefObjID == 2094 && (row.Spawn.RegionID != 25000 || row.Spawn.X != 1254 || row.Teleport.Radius != 10 || row.Teleport.Height != 25) {
			t.Fatalf("Jangan gate placement/bounds: %+v", row)
		}
	}
	for _, id := range []uint32{2094, 2095, 2096, 2197, 19074, 19075} {
		if !seen[id] {
			t.Fatalf("missing city gate %d", id)
		}
	}
	if seen[19076] {
		t.Fatal("dynamic instance gate invented at region zero")
	}
}
