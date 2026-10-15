package bot_test

import (
	"testing"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

func TestParseMonsterRowRoundTrips(t *testing.T) {
	def := simulation.MonsterDef{
		RefObjID:      1949,
		Name:          "Bandit",
		WalkSpeed:     20,
		RunSpeed:      50,
		ScaleDenom:    100,
		LinkedEffects: &monster.EffectSnapshot{},
	}
	spawn := simulation.Spawn{RegionID: 23197, X: 4315, Y: -214, Z: 388, Angle: 0}
	row := simulation.BuildMonsterCreateRow(def, 404417, spawn)

	actor, err := bot.ParseMonsterRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if actor.RefObjID != 1949 || actor.GID != 404417 || actor.Region != 23197 {
		t.Fatalf("actor = %+v", actor)
	}
	if actor.X != 4315 || actor.Y != -214 || actor.Z != 388 {
		t.Fatalf("position = %v,%v,%v", actor.X, actor.Y, actor.Z)
	}
	if actor.Name != "Bandit" {
		t.Fatalf("name = %q", actor.Name)
	}
}

func TestActorFromRowFiltersUnknownRefs(t *testing.T) {
	refs := bot.MonsterRefs{1949: {}}
	def := simulation.MonsterDef{RefObjID: 1949, Name: "Bandit", LinkedEffects: &monster.EffectSnapshot{}}
	row := simulation.BuildMonsterCreateRow(def, 42, simulation.Spawn{RegionID: 1})

	if _, ok := bot.ActorFromRow(row, refs); !ok {
		t.Fatal("a known monster ref was skipped")
	}
	other := simulation.MonsterDef{RefObjID: 9999, Name: "Npc", LinkedEffects: &monster.EffectSnapshot{}}
	rowOther := simulation.BuildMonsterCreateRow(other, 43, simulation.Spawn{RegionID: 1})
	if _, ok := bot.ActorFromRow(rowOther, refs); ok {
		t.Fatal("an unknown ref was accepted")
	}
}

func TestParseRefs(t *testing.T) {
	refs, err := bot.ParseRefs("1949, 1956 ,2322")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 {
		t.Fatalf("refs = %v", refs)
	}
	if _, ok := refs[1956]; !ok {
		t.Fatal("1956 missing")
	}
	if _, err := bot.ParseRefs("x"); err == nil {
		t.Fatal("a bad ref was accepted")
	}
}

// TestDespawnGIDsParsesTheBulkChunk guards issue #158: the 0x3417 chunk inside
// a 0x30CB byListSub=2 bracket is a raw list of 4-byte gids, not spawn rows.
func TestDespawnGIDsParsesTheBulkChunk(t *testing.T) {
	payload := []byte{
		0x01, 0x00, 0x00, 0x00,
		0x02, 0x00, 0x00, 0x00,
		0x03, 0x00, 0x00, 0x00,
	}
	got := bot.DespawnGIDs(payload)
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("gids = %v, want [1 2 3]", got)
	}
	// A trailing partial gid is ignored.
	if got := bot.DespawnGIDs([]byte{0x01, 0x00, 0x00}); len(got) != 0 {
		t.Fatalf("a partial chunk was parsed: %v", got)
	}
}

// monsterRowForTest builds one object-list row with the given ref and gid.
func monsterRowForTest(ref, gid uint32) []byte {
	def := simulation.MonsterDef{RefObjID: ref, Name: "M", LinkedEffects: &monster.EffectSnapshot{}}
	spawn := simulation.Spawn{RegionID: 23197, X: 100, Y: 10, Z: 100}
	return simulation.BuildMonsterCreateRow(def, gid, spawn)
}

// TestActorFromRowAllMonstersByBand guards issue #163: an empty ref set (the
// "all" selection) accepts any row whose gid sits in the monster band and
// rejects the NPC band. A ref list still filters.
func TestActorFromRowAllMonstersByBand(t *testing.T) {
	all, err := bot.ParseRefs("all")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("the all selection is not empty: %v", all)
	}
	// A monster-band gid (400000 and above) is accepted.
	if _, ok := bot.ActorFromRow(monsterRowForTest(1933, 452883), all); !ok {
		t.Fatal("a monster-band row was rejected with the all selection")
	}
	// An NPC-band gid (200000+) is rejected.
	if _, ok := bot.ActorFromRow(monsterRowForTest(1933, 250000), all); ok {
		t.Fatal("an NPC-band row was accepted with the all selection")
	}
	// A ref list still filters.
	refs, err := bot.ParseRefs("1933")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bot.ActorFromRow(monsterRowForTest(1949, 452883), refs); ok {
		t.Fatal("a ref list accepted a row with another ref")
	}
	if _, ok := bot.ActorFromRow(monsterRowForTest(1933, 452883), refs); !ok {
		t.Fatal("a ref list rejected its own ref")
	}
}
