package enterworld

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/world/simulation"
)

func TestGroundObjectListRows(t *testing.T) {
	registry := grounditem.NewRegistry()
	registry.Add("global-official", grounditem.Item{
		Gid: 300001, RefObjID: 107, Codename: "ITEM_CH_BLADE_01_A", TypeFlags: 0x132c,
		Position: grounditem.Point{RegionID: 24222, X: 100, Z: 200}, Y: 20,
	})
	registry.Add("global-official", grounditem.Item{
		Gid: 300002, RefObjID: 1, TypeFlags: 0x2ec, GoldAmount: 500,
		Position: grounditem.Point{RegionID: 24222, X: 110, Z: 210}, Y: 20,
	})
	registry.Add("other-division", grounditem.Item{
		Gid: 300003, RefObjID: 107, Codename: "ITEM_CH_BLADE_01_A", TypeFlags: 0x132c,
		Position: grounditem.Point{RegionID: 24222, X: 120, Z: 220}, Y: 20,
	})

	rows := GroundObjectListRows(registry.All("global-official"))
	if len(rows) != 2 {
		t.Fatalf("division rows = %d, want 2 (division-scoped)", len(rows))
	}
	for _, row := range rows {
		if row.NativeOpcode != OpcodeObjectListChunk {
			t.Errorf("row opcode = %#x, want 0x3417", row.NativeOpcode)
		}
		if len(row.Payload) == 0 {
			t.Error("row payload empty")
		}
		// The list form must NOT carry the 0x30D7 appear tail: re-encoding
		// the registry row with the tail must be exactly one byte longer.
	}
	items := registry.All("global-official")
	for index, item := range items {
		withTail := item.SpawnRow(true).Encode()
		if len(rows[index].Payload) != len(withTail)-1 {
			t.Errorf("row %d length %d, want %d (no appear tail)", index, len(rows[index].Payload), len(withTail)-1)
		}
	}
}

func TestNpcObjectListRowsGate(t *testing.T) {
	entry := LocalPlayerEntry{StartProfile: StartProfile{RegionID: 0x62a8, X: 960, Y: 20, Z: 458}}

	disabled := NpcSpawnConfig{}
	if rows := disabled.NpcObjectListRows(&entry); len(rows) != 0 {
		t.Fatalf("disabled config produced %d rows", len(rows))
	}
	if snapshot := disabled.NpcRefObjSnapshot(); len(snapshot) != 0 {
		t.Fatalf("disabled config produced refObj rows %v", snapshot)
	}

	roster := simulation.DefaultNpcRoster()
	enabled := NpcSpawnConfig{Enabled: true, Roster: roster}
	rows := enabled.NpcObjectListRows(&entry)
	if len(rows) != len(roster) {
		t.Fatalf("enabled rows = %d, want %d", len(rows), len(roster))
	}
	// Anchor defaults to the Constantinople shop, not the player.
	want := simulation.BuildNpcCreateRow(roster[0], simulation.NpcShopSpawn())
	if !reflect.DeepEqual(rows[0].Payload, NewPacket(OpcodeObjectListChunk, want).Payload) {
		t.Error("shop-anchored row bytes diverge from simulation.BuildNpcCreateRow")
	}

	atPlayer := NpcSpawnConfig{Enabled: true, AtPlayer: true, Roster: roster}
	playerRows := atPlayer.NpcObjectListRows(&entry)
	wantAtPlayer := simulation.BuildNpcCreateRow(roster[0], simulation.Spawn{RegionID: 0x62a8, X: 960, Y: 20, Z: 458})
	if !reflect.DeepEqual(playerRows[0].Payload, NewPacket(OpcodeObjectListChunk, wantAtPlayer).Payload) {
		t.Error("player-anchored row bytes diverge")
	}

	snapshot := enabled.NpcRefObjSnapshot()
	if len(snapshot) != len(roster) || snapshot[0].RefObjID != 7495 || snapshot[0].TidWord != 0x0146 ||
		snapshot[0].Codename != "NPC_EU_SMITH" || snapshot[0].Kind != "npc" {
		t.Fatalf("refObj snapshot = %+v", snapshot)
	}
	if snapshot[1].RefObjID != 9251 || snapshot[1].TidWord != 0x0146 ||
		snapshot[1].Codename != "NPC_CH_GACHA_MACHINE" || snapshot[1].Kind != "npc" {
		t.Fatalf("Gacha refObj snapshot = %+v", snapshot[1])
	}
}

// TestBuildObjectListRowsDivisionScoped drives the seam through Build: the
// resolved division reaches the rows provider, and the rows ride between the
// 0x30CB start (count byte) and the 0x330A finalize.
func TestBuildObjectListRowsDivisionScoped(t *testing.T) {
	registry := grounditem.NewRegistry()
	registry.Add(DefaultDivisionID, grounditem.Item{
		Gid: 300010, RefObjID: 107, Codename: "ITEM_CH_BLADE_01_A", TypeFlags: 0x132c,
		Position: grounditem.Point{RegionID: 24222, X: 100, Z: 200}, Y: 20,
	})
	registry.Add("elsewhere", grounditem.Item{
		Gid: 300011, RefObjID: 107, Codename: "ITEM_CH_BLADE_01_A", TypeFlags: 0x132c,
		Position: grounditem.Point{RegionID: 24222, X: 100, Z: 200}, Y: 20,
	})

	character := chinaSpearman()
	deps := testDeps(character)
	deps.ResolveDivisionID = DevResolveDivisionIDFromCatalog(deps.Characters)
	var seenDivision string
	deps.ObjectListRows = func(divisionID string, _ *Character, _ *LocalPlayerEntry) []Packet {
		seenDivision = divisionID
		return GroundObjectListRows(registry.All(divisionID))
	}

	result := Build(deps, BootstrapRequest{CharacterName: "asd2", DivisionID: "0"})
	if result.NativeResult != 1 {
		t.Fatalf("bootstrap failed: %+v", result)
	}
	if seenDivision != DefaultDivisionID {
		t.Fatalf("rows provider saw division %q, want the RESOLVED %q", seenDivision, DefaultDivisionID)
	}

	packets := result.Packets
	if len(packets) != 8 {
		t.Fatalf("packet count = %d, want 8 (one ground row)", len(packets))
	}
	if packets[5].NativeOpcode != OpcodeObjectListStart || !reflect.DeepEqual(packets[5].Payload, []byte{0x01, 0x01, 0x00}) {
		t.Errorf("object list start = %+v, want count 1", packets[5])
	}
	if packets[6].NativeOpcode != OpcodeObjectListChunk {
		t.Errorf("row opcode = %#x", packets[6].NativeOpcode)
	}
	if packets[7].NativeOpcode != OpcodeObjectListFinalize {
		t.Errorf("finalize opcode = %#x", packets[7].NativeOpcode)
	}
}
