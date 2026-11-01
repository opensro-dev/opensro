/*
===========================================================================

tradercargodrop_test.go - SRO_TRADER_CARGO_DROP, both settings and each case

Port-only, not native (tradercargodrop.go). Off, a rider's death and a
logout take the goods with the vehicle, as the native death does. On, a trader dying
on its transport, or leaving the world with a loaded transport outside a
town, leaves the goods on the ground.

===========================================================================
*/
package action

import (
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

// cargoTestUnits is the stack the loaded transport carries.
const cargoTestUnits = 7

/*
================
newCargoTestRuntime

A trader standing in region with a summoned transport of band carrying
one stack, ridden when mounted.
================
*/
func newCargoTestRuntime(t *testing.T, on bool, region, band uint16, mounted bool) (*Runtime, *enterworld.Character) {
	t.Helper()
	rt, _, character, _ := newCombatTestRuntime(t, 100)
	rt.TraderCargoDrop = on
	character.World.Spawn.RegionID = testInt64(int64(region))
	rt.Worlds.Forget(simulation.WorldKey(testDivision, character.Name))
	equipCombatTestPet(t, rt, character, band)
	pet := character.ActiveCOS
	pet.Mounted = mounted
	pet.StateFlags |= cosStateSummoned
	potion := testItems()["ITEM_ETC_HP_POTION_01"]
	pet.Container = &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{{
		Slot: 0, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: cargoTestUnits}}}
	return rt, character
}

/*
================
killRider
================
*/
func killRider(rt *Runtime, c *enterworld.Character) []wire.Frame {
	zero := int64(0)
	c.CurrentHP = &zero
	effects, _ := rt.settlePlayerDeathInDoor(testDivision, c, deathKiller{}, rt.Now().UnixMilli())
	return effects
}

/*
================
cargoOnGround

Whether exactly the transport's stack lies unowned on the ground.
================
*/
func cargoOnGround(t *testing.T, rt *Runtime) bool {
	t.Helper()
	ground := rt.Ground.All(testDivision)
	switch {
	case len(ground) == 0:
		return false
	case len(ground) == 1 && ground[0].StackCount == cargoTestUnits && ground[0].DroppedBy == "":
		return true
	}
	t.Fatalf("unexpected ground %+v", ground)
	return false
}

/*
================
TestRiderDeathCargo

Off: the native death releases the vehicle with its goods. On: the goods
drop before the release.
================
*/
func TestRiderDeathCargo(t *testing.T) {
	_, field := regionsByCombat(t)
	for _, on := range []bool{false, true} {
		rt, character := newCargoTestRuntime(t, on, field, cosBandTransport, true)
		gid := character.ActiveCOS.GID
		effects := killRider(rt, character)
		if character.ActiveCOS != nil || !despawnsGID(effects, gid) {
			t.Fatalf("on=%v: the ridden transport was not released", on)
		}
		if dropped := cargoOnGround(t, rt); dropped != on {
			t.Fatalf("on=%v: cargo dropped %v", on, dropped)
		}
	}
}

/*
================
TestParkedTransportOutlivesItsTrader

Both settings: a trader's death leaves its parked transport and goods in
the world (killing the transport drops them, cosdeath.go).
================
*/
func TestParkedTransportOutlivesItsTrader(t *testing.T) {
	_, field := regionsByCombat(t)
	for _, on := range []bool{false, true} {
		rt, character := newCargoTestRuntime(t, on, field, cosBandTransport, false)
		pet := character.ActiveCOS
		killRider(rt, character)
		if character.ActiveCOS != pet || !pet.Summoned || len(pet.Container.Rows) != 1 || cargoOnGround(t, rt) {
			t.Fatalf("on=%v: the parked transport changed with its trader's death: %+v", on, pet)
		}
	}
}

/*
================
TestFieldDisconnectCargo

A logout outside a town, ridden or parked. Off: nothing changes (the
native dismiss happens later in the close). On: the transport dies where
it stands, its goods drop and its record is deleted.
================
*/
func TestFieldDisconnectCargo(t *testing.T) {
	_, field := regionsByCombat(t)
	for _, on := range []bool{false, true} {
		for _, mounted := range []bool{false, true} {
			rt, character := newCargoTestRuntime(t, on, field, cosBandTransport, mounted)
			var published []wire.Frame
			rt.PushDivisionPeerFrames = func(_, _ string, frames []wire.Frame) { published = append(published, frames...) }
			pet := character.ActiveCOS
			rt.DropDisconnectedCargo(testDivision, character.Name, 1, errFieldDisconnect)
			if dropped := cargoOnGround(t, rt); dropped != on {
				t.Fatalf("on=%v mounted=%v: cargo dropped %v", on, mounted, dropped)
			}
			if !on {
				if character.ActiveCOS != pet || len(published) != 0 {
					t.Fatalf("mounted=%v: the rule ran while off", mounted)
				}
				continue
			}
			if character.ActiveCOS != nil || pet.Summoned || pet.Mounted || !despawnsGID(published, pet.GID) {
				t.Fatalf("mounted=%v: the transport outlived the disconnect: %+v", mounted, pet)
			}
			if _, dismounted := findFrame(published, wire.OpCosRideState); dismounted != mounted {
				t.Fatalf("mounted=%v: dismount published %v", mounted, dismounted)
			}
		}
	}
}

/*
================
TestDisconnectCargoSkips

With the rule on, a logout keeps the native dismiss for a transport in a
town, an empty transport, a loaded pet that is no transport, and a close
from a session a relog already replaced.
================
*/
func TestDisconnectCargoSkips(t *testing.T) {
	town, field := regionsByCombat(t)
	cases := []struct {
		name   string
		region uint16
		band   uint16
		empty  bool
		stale  bool
	}{
		{name: "town", region: town, band: cosBandTransport},
		{name: "empty", region: field, band: cosBandTransport, empty: true},
		{name: "pet", region: field, band: cosBandAttackPet},
		{name: "replaced session", region: field, band: cosBandTransport, stale: true},
	}
	for _, tc := range cases {
		rt, character := newCargoTestRuntime(t, true, tc.region, tc.band, false)
		pet := character.ActiveCOS
		if tc.empty {
			pet.Container.Rows = nil
		}
		if tc.stale {
			rt.characterAdmissions.Store(simulation.WorldKey(testDivision, character.Name),
				populationAdmission{session: 2, division: testDivision, name: character.Name})
		}
		rt.DropDisconnectedCargo(testDivision, character.Name, 1, errFieldDisconnect)
		if character.ActiveCOS != pet || !pet.Summoned || len(rt.Ground.All(testDivision)) != 0 {
			t.Fatalf("%s: the rule touched the transport: %+v", tc.name, pet)
		}
	}
}

// errFieldDisconnect is a socket that went away and never came back (the
// resume grace ran out): a real logout.
var errFieldDisconnect = errors.New("transport: resume grace period expired")

/*
================
TestDisconnectCargoFollowsTheCloseCause

With the rule on and a loaded transport in the field: a reload or
reconnect (the new session replaces the old one, which closes before the
new one takes the character) keeps the transport and its goods, a server
shutdown drops nothing, and a socket that never came back drops the goods
as before. BR-261023-2329-B01C: a reload in the field cost a trader the
transport and its goods.
================
*/
func TestDisconnectCargoFollowsTheCloseCause(t *testing.T) {
	_, field := regionsByCombat(t)
	cases := []struct {
		name  string
		cause error
		drops bool
	}{
		{name: "replaced by a reload", cause: transport.CloseReason{Reason: transport.ByeReasonReplaced}},
		{name: "server shutdown", cause: transport.CloseReason{Reason: transport.ByeReasonShutdown}},
		{name: "never came back", cause: errFieldDisconnect, drops: true},
	}
	for _, tc := range cases {
		rt, character := newCargoTestRuntime(t, true, field, cosBandTransport, false)
		pet := character.ActiveCOS
		rt.DropDisconnectedCargo(testDivision, character.Name, 1, tc.cause)
		if dropped := cargoOnGround(t, rt); dropped != tc.drops {
			t.Fatalf("%s: cargo dropped %v, want %v", tc.name, dropped, tc.drops)
		}
		kept := character.ActiveCOS == pet && pet.Summoned && len(pet.Container.Rows) == 1
		if kept == tc.drops {
			t.Fatalf("%s: transport kept %v with %+v", tc.name, kept, pet)
		}
	}
}
