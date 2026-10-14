/*
===========================================================================

denreturn_test.go - the Bandit Den scroll's countdown and the pet summon
it leaves free

The scroll counts down in teleport mode 2. Natively a pet may be summoned
then (493100 refuses only mode 1), and so may a transport (49B9F0) unless
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

/*
================
TestPetSummonRefusesOnlyTeleportModeOne

CGItemCOSSummoner_Use (493100) answers 0x1869 only when the owner's
teleport mode is 1 (493140..49314C). The Bandit Den scroll's countdown is
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

Natively 49B9F0 summons a transport during the Den countdown (teleport
mode 2). With SRO_DEN_NO_TRANSPORT_SUMMON (port-only, not native) it is
refused 0x69 and nothing changes; mode 1 is refused either way.
================
*/
func TestDenCountdownTransportSummonFollowsTheFlag(t *testing.T) {
	for _, tc := range []struct {
		rule    bool
		mode    uint8
		summons bool
	}{{false, teleportModeFree, true}, {true, teleportModeFree, false}, {false, teleportModeBlocking, false}, {true, teleportModeBlocking, false}} {
		c := testCharacter()
		dressTrader(c)
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 22, RefObjID: 3905,
			Codename: "ITEM_COS_T_DHORSE3", TypeFlags: wire.PackTypeFlags(3, 3, 3, 2), StackCount: 1})
		rt, _ := newTestRuntime(c, testCosSource(testItems()))
		rt.BindPetSession(testDivision, c, 101)
		rt.DenNoTransportSummon = tc.rule
		c.NativeTeleportMode = tc.mode
		before := c.Snapshot()
		result := rt.HandleItemUse(testDivision, c, []byte{22, 0xec, 0x11})
		summoned := len(result.Frames) > 0 && result.Frames[0].Payload[0] == 1 && c.ActiveCOS != nil
		if summoned != tc.summons {
			t.Fatalf("rule=%v mode=%d: summoned %v, want %v (%+v)", tc.rule, tc.mode, summoned, tc.summons, result.Frames)
		}
		if !tc.summons && (len(result.Frames) == 0 || result.Frames[0].Payload[1] != 0x69 || !reflect.DeepEqual(before, c.Snapshot())) {
			t.Fatalf("rule=%v mode=%d: refusal %+v changed state or code", tc.rule, tc.mode, result.Frames)
		}
	}
	if denTransportRefused(teleportModeFree, false, true) {
		t.Fatal("the rule refused a riding horse")
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
