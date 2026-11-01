/*
===========================================================================

denreturn_test.go - the Bandit Den scroll's countdown and the pet summon
it leaves free

The scroll counts down in teleport mode 2. Natively a pet may be summoned
then (the reference server refuses only mode 1), and so may a transport unless
SRO_DEN_NO_TRANSPORT_SUMMON is on. SRO_FAST_DEN_RETURN halves the
countdown; off, the countdown is the scroll's Param1.

===========================================================================
*/
package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// denScrollMs is the shipped Bandit Den scroll's Param1 (itemdata 2128).
const denScrollMs = 300000

// denTestFieldRegion is a field region outside every town.
const denTestFieldRegion uint16 = 0x5F76

/*
================
TestPetSummonRefusesOnlyTeleportModeOne

The reference server's pet summoner answers 0x1869 only when the owner's
teleport mode is 1. The Bandit Den scroll's countdown is
mode 2, and a pet comes out during it.
================
*/
func TestPetSummonRefusesOnlyTeleportModeOne(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	rt.BindPetSession(testDivision, c, 101)
	ref := refs.staticItemSource["SUMMON_ATTACK"]
	c.NativeTeleportMode = teleportModeBlocking
	result := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(23).U16(ref.TypeFlags()).Payload())
	if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 2 || result.Frames[0].Payload[1] != cosSummonTeleporting {
		t.Fatalf("summon in mode 1 answered %+v, want the 0x69 refusal", result.Frames)
	}
	for _, row := range c.MissionInventory {
		if row.Slot == 23 && row.Summon != nil {
			t.Fatal("a refused summon created the pet")
		}
	}
	c.NativeTeleportMode = teleportModeFree
	useSummonerFixture(t, rt, c, 23, ref)
	if c.NativeTeleportMode != teleportModeFree {
		t.Fatal("the summon ended the Den countdown")
	}
}

/*
================
denScrollFixture

A dressed thief holding one Bandit Den scroll, and a den gate to land at.
================
*/
func denScrollFixture(t *testing.T, fast bool) (*Runtime, *fakeClock, *enterworld.Character, *enterworld.ItemRef) {
	t.Helper()
	c := testCharacter()
	c.Job = domain.CharacterJob{Type: domain.JobThief, Grade: 1, Alias: "Thief"}
	scroll := &enterworld.ItemRef{RefObjID: 2128, Codename: "ITEM_ETC_SCROLL_RETURN_THIEFDEN_01", Country: 3,
		TypeIDs: [4]int64{3, 3, 3, 1}, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, ReturnDestination: "THIEFDEN",
		ReturnTeleport: "STORE_TD_GATE", NativeFields: enterworld.NewNativeFields(map[string]float64{
			"canUse": 1, "maxStack": 10, "itemParam1_29c": denScrollMs, "itemParam2_2a0": 0})}
	items := testItems()
	items[scroll.Codename] = scroll
	c.MissionInventory = append(c.MissionInventory,
		enterworld.InventoryRow{Slot: enterworld.JobSuitSlot, RefObjID: 200, Codename: "SUIT",
			TypeFlags: wire.PackTypeFlags(3, 1, 7, domain.JobThief), StackCount: 1},
		enterworld.InventoryRow{Slot: 26, RefObjID: scroll.RefObjID, Codename: scroll.Codename,
			TypeFlags: scroll.TypeFlags(), StackCount: 1})
	rt, clock := newTestRuntime(c, items)
	rt.FastDenReturn = fast
	rt.portals = &portalCatalog{buildings: map[string]uint32{"STORE_TD_GATE": 77},
		destinations: map[uint32]portalDestination{1: {id: 1, ref: 77, building: true, world: 1,
			spawn: simulation.SeedWorldState(c).Spawn}}}
	return rt, clock, c, scroll
}

/*
================
TestDenScrollCountdownFollowsTheFlag

Off, the countdown is Param1 as native; on (port-only, not native), it is
half. Either way it runs in teleport mode 2.
================
*/
func TestDenScrollCountdownFollowsTheFlag(t *testing.T) {
	for _, tc := range []struct {
		fast bool
		want int64
	}{{false, denScrollMs}, {true, denScrollMs / 2}} {
		rt, clock, c, scroll := denScrollFixture(t, tc.fast)
		started := clock.NowMs()
		result := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(26).U16(scroll.TypeFlags()).Payload())
		pending, ok := rt.returnCasts.Load(simulation.WorldKey(testDivision, c.Name))
		if !ok {
			t.Fatalf("fast=%v: the Den scroll did not start: %+v", tc.fast, result.Frames)
		}
		if got := pending.(pendingReturn).due - started; got != tc.want || c.NativeTeleportMode != teleportModeFree {
			t.Fatalf("fast=%v: countdown %d ms in mode %d, want %d ms in mode 2", tc.fast, got, c.NativeTeleportMode, tc.want)
		}
	}
}

/*
================
TestFastDenReturnFromEnv
================
*/
func TestFastDenReturnFromEnv(t *testing.T) {
	for env, want := range map[string]bool{"": false, "off": false, "on": true, "1": true, " TRUE ": true} {
		t.Setenv(EnvFastDenReturn, env)
		if got := FastDenReturnFromEnv(); got != want {
			t.Errorf("%s=%q: %v, want %v", EnvFastDenReturn, env, got, want)
		}
	}
}

/*
================
TestDenCountdownTransportSummonFollowsTheFlag

Natively a transport is summoned during the Den countdown (teleport
mode 2). With SRO_DEN_NO_TRANSPORT_SUMMON (port-only, not native) it is
refused 0x69 and nothing changes. The rule keys on the Den scroll's cast:
another mode-2 countdown still summons, and mode 1 is refused either way.
================
*/
func TestDenCountdownTransportSummonFollowsTheFlag(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rule    bool
		den     bool
		mode    uint8
		summons bool
	}{
		{"Den countdown, native", false, true, 0, true},
		{"Den countdown, rule on", true, true, 0, false},
		{"another mode-2 countdown, rule on", true, false, teleportModeFree, true},
		{"mode 1, native", false, false, teleportModeBlocking, false},
		{"mode 1, rule on", true, false, teleportModeBlocking, false},
	} {
		rt, _, c, scroll := denScrollFixture(t, false)
		deps := rt.deps.(*enterworld.Deps)
		deps.Items = testCosSource(deps.Items.(staticItemSource))
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 22, RefObjID: 3905,
			Codename: "ITEM_COS_T_DHORSE3", TypeFlags: wire.PackTypeFlags(3, 3, 3, 2), StackCount: 1})
		rt.BindPetSession(testDivision, c, 101)
		// Natively a thief summons no transport in a town; stand in the field.
		rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(world *simulation.WorldState) {
			world.Spawn = simulation.Spawn{RegionID: denTestFieldRegion, X: 960, Y: 20, Z: 960}
		})
		rt.DenNoTransportSummon = tc.rule
		if tc.den {
			rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(26).U16(scroll.TypeFlags()).Payload())
			if c.NativeTeleportMode != teleportModeFree {
				t.Fatalf("%s: the Den scroll did not start", tc.name)
			}
		} else {
			c.NativeTeleportMode = tc.mode
		}
		before := c.Snapshot()
		result := rt.HandleItemUse(testDivision, c, []byte{22, 0xec, 0x11})
		summoned := len(result.Frames) > 0 && result.Frames[0].Payload[0] == 1 && c.ActiveCOS != nil
		if summoned != tc.summons {
			t.Fatalf("%s: summoned %v, want %v (%+v)", tc.name, summoned, tc.summons, result.Frames)
		}
		if !tc.summons && (len(result.Frames) == 0 || result.Frames[0].Payload[1] != 0x69 || !reflect.DeepEqual(before, c.Snapshot())) {
			t.Fatalf("%s: refusal %+v changed state or code", tc.name, result.Frames)
		}
	}
}

/*
================
TestDenNoTransportSummonFromEnv
================
*/
func TestDenNoTransportSummonFromEnv(t *testing.T) {
	for env, want := range map[string]bool{"": false, "off": false, "on": true, "1": true, " TRUE ": true} {
		t.Setenv(EnvDenNoTransportSummon, env)
		if got := DenNoTransportSummonFromEnv(); got != want {
			t.Errorf("%s=%q: %v, want %v", EnvDenNoTransportSummon, env, got, want)
		}
	}
}
