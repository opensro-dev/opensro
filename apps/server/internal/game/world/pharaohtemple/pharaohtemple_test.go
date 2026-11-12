/*
===========================================================================

pharaohtemple_test.go - the temple identity table and its switch.

===========================================================================
*/
package pharaohtemple

import "testing"

/*
================
TestDungeonsAreTheSevenRegions

Dungeoninfo ids 10-16 are regions 0x800A-0x8010 in order, the Temple is the
last, and the table keeps clear of what v1.150 uses (ids 1-9: the Donwhang
cave, the Qin-Shi floors, the flag-war arena 0x8009).
================
*/
func TestDungeonsAreTheSevenRegions(t *testing.T) {
	dungeons := Dungeons()
	if len(dungeons) != 7 {
		t.Fatalf("table has %d dungeons, want 7", len(dungeons))
	}
	for index, dungeon := range dungeons {
		id := FirstDungeonID + index
		if dungeon.ID != id || dungeon.Region != 0x8000|uint16(id) {
			t.Fatalf("dungeon %d = %+v, want id %d region 0x%04X", index, dungeon, id, 0x8000|id)
		}
		if dungeon.ID <= 9 || dungeon.Name == "" || dungeon.SourceWorld == 0 {
			t.Fatalf("dungeon %+v collides with a native id or is unnamed", dungeon)
		}
	}
	last := dungeons[len(dungeons)-1]
	if last.ID != TempleDungeonID || last.Region != TempleRegionID || TempleRegionID != 0x8010 || last.SourceWorld != 60 {
		t.Fatalf("the Temple is %+v", last)
	}
	for _, region := range []uint16{0, 0x8001, 0x8007, 0x8009, 0x8011, 0x0010, 0x6A93} {
		if IsRegion(region) {
			t.Fatalf("region 0x%04X reads as a temple region", region)
		}
	}
}

/*
================
TestGatesAreTheFour
================
*/
func TestGatesAreTheFour(t *testing.T) {
	want := []string{
		"GATE_DESERT_MERCHANT_HUNTER_GATE", "GATE_DESERT_THIEF_GATE",
		"GATE_TEMPLE_MERCHANT_HUNTER_GATE", "GATE_TEMPLE_THIEF_GATE",
	}
	got := GateCodes()
	if len(got) != len(want) {
		t.Fatalf("gates %v, want %v", got, want)
	}
	for index, code := range want {
		if got[index] != code || !IsGate(code) {
			t.Fatalf("gate %d = %q, want %q", index, got[index], code)
		}
	}
	if !IsGate("gate_temple_thief_gate") {
		t.Fatal("a zone name in the meshes' lower case is not a gate")
	}
	for _, other := range []string{"", "GATE_DUNGEON_DH_IN", "GATE_TOMB_GATE_IN", "GATE_STORMDESERT_GATE"} {
		if IsGate(other) {
			t.Fatalf("%q reads as a temple gate", other)
		}
	}
	// The caller's slice is a copy.
	got[0] = "x"
	if GateCodes()[0] != want[0] {
		t.Fatal("GateCodes shares its table")
	}
}

/*
================
TestFromEnv

The on values are the importer's; the off values are native; anything else
is refused, and Enabled reads a refused value as off.
================
*/
func TestFromEnv(t *testing.T) {
	cases := []struct {
		value   string
		on      bool
		refused bool
	}{
		{"", false, false}, {"0", false, false}, {"off", false, false}, {"FALSE", false, false},
		{"1", true, false}, {"on", true, false}, {" True ", true, false},
		{"yes", false, true}, {"2", false, true},
	}
	for _, c := range cases {
		t.Setenv(EnvPharaohTemple, c.value)
		on, err := FromEnv()
		if on != c.on || (err != nil) != c.refused {
			t.Fatalf("%q: on=%v err=%v, want on=%v refused=%v", c.value, on, err, c.on, c.refused)
		}
		if Enabled() != c.on {
			t.Fatalf("%q: Enabled() = %v", c.value, Enabled())
		}
	}
}

/*
================
TestHidesFollowTheSwitch

Off hides the four gates by name, anything landing in a temple region, and
the two desert objects; on hides nothing. A teleport the temple does not own
is never hidden either way.
================
*/
func TestHidesFollowTheSwitch(t *testing.T) {
	t.Setenv(EnvPharaohTemple, "off")
	for _, code := range GateCodes() {
		if !Hides(code, 0x1234) {
			t.Fatalf("off showed gate %s", code)
		}
	}
	if !Hides("GATE_OTHER", TempleRegionID) {
		t.Fatal("off showed a teleport into the Temple")
	}
	if Hides("GATE_STORMDESERT_GATE", 21569) || Hides("GATE_DUNGEON_DH_OUT", 0x8001) {
		t.Fatal("off hid a teleport the temple does not own")
	}
	if !HidesBuilding(DesertMerchantHunterBuilding) || !HidesBuilding(DesertThiefBuilding) || HidesBuilding("STORE_CH_GATE") {
		t.Fatal("off: building hiding is wrong")
	}
	t.Setenv(EnvPharaohTemple, "on")
	for _, code := range GateCodes() {
		if Hides(code, TempleRegionID) {
			t.Fatalf("on hid gate %s", code)
		}
	}
	if HidesBuilding(DesertMerchantHunterBuilding) || HidesBuilding(DesertThiefBuilding) {
		t.Fatal("on hid a desert gate object")
	}
}
