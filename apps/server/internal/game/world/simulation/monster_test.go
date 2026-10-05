package simulation

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// The monster create row is the shared bionic spawn tail (byte-identical
// in SHAPE to the NPC row: pos block, standing movement block, speeds,
// per-monster f32 scale denom, mastery 0, name mask 1 + name) plus exactly one
// trailing rarity byte (sub_861b00 @0x861b74) - and NOTHING else: no HP,
// no level (those are post-spawn 0x33A6). Field offsets follow the
// prior-board SCOUT-M2 byte map; the layout re-pin is tracked on the
// monster-live board.
func TestBuildMonsterCreateRowShape(t *testing.T) {
	def := MonsterDef{
		RefObjID:   1933,
		TidWord:    0x00C6,
		Codename:   "MOB_CH_MANGNYANG",
		Name:       "MOB_CH_MANGNYANG",
		WalkSpeed:  8,
		RunSpeed:   22,
		ScaleDenom: 100,
		Rarity:     4,
	}
	spawn := Spawn{RegionID: 25258, X: 812.68, Y: 75.08, Z: 392.90, Angle: 0x1234}
	gid := uint32(400001)

	row := BuildMonsterCreateRow(def, gid, spawn)

	// Fixed prefix: u32 refObjID, u32 gid, u16 region, f32 x/y/z, u16
	// heading.
	if got := binary.LittleEndian.Uint32(row[0:4]); got != 1933 {
		t.Fatalf("refObjID = %d, want 1933", got)
	}
	if got := binary.LittleEndian.Uint32(row[4:8]); got != gid {
		t.Fatalf("gid = %d, want %d", got, gid)
	}
	if got := binary.LittleEndian.Uint16(row[8:10]); got != 25258 {
		t.Fatalf("region = %d, want 25258", got)
	}
	if got := math.Float32frombits(binary.LittleEndian.Uint32(row[10:14])); got != float32(812.68) {
		t.Fatalf("x = %v, want 812.68", got)
	}
	if got := binary.LittleEndian.Uint16(row[22:24]); got != 0x1234 {
		t.Fatalf("heading word = %#04x, want 0x1234", got)
	}
	// Standing movement block: moveMode 0, speedIndex 2 (the WALK
	// channel - BUG-7 speed-half fix; the prior-wave 1/run made the
	// client integrate walk-timed goals at run speed), rotation flag 0,
	// standing heading word.
	if row[24] != 0 || row[25] != MonsterSpawnSpeedChannel || row[26] != 0 {
		t.Fatalf("movement block = % X, want 00 02 00", row[24:27])
	}
	if got := binary.LittleEndian.Uint16(row[27:29]); got != 0x1234 {
		t.Fatalf("standing heading = %#04x, want 0x1234", got)
	}
	// Scalars: motion/movement/life 0, then the PER-MONSTER speeds (not
	// the 20/50 NPC fixture pair) and the f32 scale denom 100.
	if row[29] != 0 || row[30] != 0 || row[31] != 0 {
		t.Fatalf("motion/movement/life = % X, want 00 00 00", row[29:32])
	}
	if got := math.Float32frombits(binary.LittleEndian.Uint32(row[32:36])); got != 8 {
		t.Fatalf("walk speed = %v, want 8", got)
	}
	if got := math.Float32frombits(binary.LittleEndian.Uint32(row[36:40])); got != 22 {
		t.Fatalf("run speed = %v, want 22", got)
	}
	if got := math.Float32frombits(binary.LittleEndian.Uint32(row[40:44])); got != 100 {
		t.Fatalf("scale denom = %v, want f32 100 (an integer write loads as a denormal)", got)
	}
	// Mastery count 0, name mask 1, u16 name length + bytes.
	if row[44] != 0 || row[45] != 1 {
		t.Fatalf("mastery/nameMask = % X, want 00 01", row[44:46])
	}
	name := []byte(def.Name)
	if got := binary.LittleEndian.Uint16(row[46:48]); got != uint16(len(name)) {
		t.Fatalf("name length = %d, want %d", got, len(name))
	}
	if !bytes.Equal(row[48:48+len(name)], name) {
		t.Fatalf("name bytes = %q, want %q", row[48:48+len(name)], name)
	}
	// The monster tail: ONE rarity byte, then end-of-row.
	rarityAt := 48 + len(name)
	if row[rarityAt] != def.Rarity {
		t.Fatalf("rarity = %d, want authored %d", row[rarityAt], def.Rarity)
	}
	if len(row) != rarityAt+1 {
		t.Fatalf("row length = %d, want %d (no HP/level bytes after rarity)", len(row), rarityAt+1)
	}
}

// The monster row differs from the NPC row ONLY in its inputs and the
// rarity tail: same builder shape, so a shared-tail regression in one is
// caught by the other's parity vectors.
func TestMonsterRowMatchesNpcRowThroughSharedTail(t *testing.T) {
	def := MonsterDef{RefObjID: 7495, Name: "Weapon Trader Balbardo", WalkSpeed: WalkSpeed, RunSpeed: RunSpeed, ScaleDenom: 100}
	npc := NpcDef{ObjectID: 200001, RefObjID: 7495, TidWord: 0x0146, Codename: "NPC_EU_SMITH", Name: "Weapon Trader Balbardo"}
	anchor := Spawn{RegionID: 25511, X: 941.5, Y: 7.2, Z: 1417.2}

	npcRow := BuildNpcCreateRow(npc, anchor)
	// The NPC builder derives gid + offsets position by (+8,+5); feed the
	// monster builder the SAME derived values so the shared tail must be
	// byte-identical EXCEPT the two intentional divergences: the +25
	// speed-channel byte (monster ships walk=2, the NPC fixture ships 1)
	// and the rarity tail.
	monsterRow := BuildMonsterCreateRow(def, npc.ObjectID, Spawn{
		RegionID: anchor.RegionID,
		X:        anchor.X + 8,
		Y:        anchor.Y,
		Z:        anchor.Z + 5,
	})

	expected := append([]byte(nil), npcRow...)
	expected[25] = MonsterSpawnSpeedChannel
	if !bytes.Equal(monsterRow[:len(monsterRow)-1], expected) {
		t.Fatal("monster row (minus rarity tail) diverges from the NPC shared bionic tail beyond the speed-channel byte")
	}
	if monsterRow[len(monsterRow)-1] != MonsterRarityNormal {
		t.Fatal("monster row does not end with the rarity byte")
	}
}

func TestBuildMonsterCreateRowUsesPerMonsterScale(t *testing.T) {
	row := BuildMonsterCreateRow(MonsterDef{
		RefObjID:   7550,
		Name:       "MOB_SCALE_CANARY",
		WalkSpeed:  8,
		RunSpeed:   22,
		ScaleDenom: 135,
	}, 400001, Spawn{})

	if got := math.Float32frombits(binary.LittleEndian.Uint32(row[40:44])); got != 135 {
		t.Fatalf("scale denom = %v, want the monster's characterdata value 135", got)
	}
}

/*
================
TestStructureRowFollowsCICATStructOrder

4FA0B0: RefObjID, HP, RefEventStructID, state, then the shared object
block, the name, and a headquarters' guild.
================
*/
func TestStructureRowFollowsCICATStructOrder(t *testing.T) {
	def := MonsterDef{RefObjID: 19553, Structure: true, CurrentHP: 1170000, EventStructID: 84, StructureState: 4, Name: "Stone", ScaleDenom: 100}
	row := BuildStructureCreateRow(def, 7, Spawn{RegionID: 17991, X: 849, Z: 1065})
	head := []byte{0x61, 0x4c, 0, 0, 0x50, 0xda, 0x11, 0, 84, 0, 0, 0, 4, 0, 7, 0, 0, 0}
	if !bytes.Equal(row[:len(head)], head) {
		t.Fatalf("row head %x", row[:len(head)])
	}
	if !bytes.HasSuffix(row, append([]byte{0, 1, 5, 0}, "Stone"...)) {
		t.Fatalf("row tail %x", row[len(row)-10:])
	}
	def.TypeID4 = 5
	if headquarters := BuildStructureCreateRow(def, 7, Spawn{RegionID: 17991}); !bytes.HasSuffix(headquarters, []byte{0, 0, 0, 0}) {
		t.Fatal("headquarters row lacks its guild")
	}
}
