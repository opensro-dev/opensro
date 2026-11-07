/*
===========================================================================

walkin_test.go - walking into the Donwhang cave's mouth, as natively

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// caveRegion is the Donwhang cave, dungeon 1 (GATE_DUNGEON_DH_OUT's region).
const caveRegion uint16 = 0x8001

/*
================
TestWalkInGatesLoadFromTheReferenceData

v1.150's TELEPORT event zones are the cave's two gates, each with its one
link: in from level 50, both refusing a summoned transport (the server
DB's restriction, portaltransport_generated.go), both free. The tomb door's zone
has no reference row in v1.150, so it moves nobody.
================
*/
func TestWalkInGatesLoadFromTheReferenceData(t *testing.T) {
	clientdata.RequireGameData(t)
	catalog, err := loadPortalCatalog(gamedatatest.TextdataDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.walkIns) != 2 {
		t.Fatalf("walk-in census %d: %+v", len(catalog.walkIns), catalog.walkIns)
	}
	in, out := catalog.walkIns["GATE_DUNGEON_DH_IN"], catalog.walkIns["GATE_DUNGEON_DH_OUT"]
	if catalog.destinations[in.target].code != "GATE_DUNGEON_DH_OUT" || catalog.destinations[out.target].code != "GATE_DUNGEON_DH_IN" {
		t.Fatalf("cave links %+v / %+v", in, out)
	}
	transport := portalCondition{kind: portalConditionTransport}
	if len(in.conditions) != 2 || in.conditions[0] != (portalCondition{1, 50, 999}) || in.conditions[1] != transport ||
		len(out.conditions) != 1 || out.conditions[0] != transport || in.fee != 0 || out.fee != 0 {
		t.Fatalf("cave conditions %+v / %+v", in, out)
	}
	if _, ok := catalog.walkIns["GATE_JINSI_OUT"]; ok {
		t.Fatal("the tomb door is a walk-in gate in v1.150 data")
	}
}

/*
================
walkInFixture

A level-20 player admitted to the world, the shipped gates, and a zone
lookup that puts the cave mouth east of x = 500. The returned slice
collects the frames pushed to the player.
================
*/
func walkInFixture(t *testing.T) (*Runtime, *enterworld.Character, *[][]wire.Frame) {
	t.Helper()
	clientdata.RequireGameData(t)
	rt, c, _, _ := returnFixture(t, 30000)
	if err := rt.ConfigurePortals(gamedatatest.TextdataDir(t)); err != nil {
		t.Fatal(err)
	}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	rt.WalkInZoneAt = func(at simulation.Spawn) string {
		if at.RegionID == 0x6a93 && at.X > 500 {
			return "GATE_DUNGEON_DH_IN"
		}
		return ""
	}
	pushed := &[][]wire.Frame{}
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { *pushed = append(*pushed, frames) }
	placeWalker(rt, c, 100)
	if err := rt.AdmitCharacterSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	return rt, c, pushed
}

/*
================
placeWalker

Stands the player at x in region 0x6A93.
================
*/
func placeWalker(rt *Runtime, c *enterworld.Character, x float64) {
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: 0x6a93, X: x, Y: 474, Z: 48}
			w.SpawnSet = true
			w.MoveSegment = nil
		})
}

/*
================
TestWalkingIntoTheCaveMouthTeleportsInside

Entering the mouth's zone runs the native teleport command: a level-20 player is refused by
the link's level condition (the teleport answer's 0x15) and stays; standing
on in the zone does not ask again; a level-60 player who walks back in
arrives at the cave's GATE_DUNGEON_DH_OUT point with nothing charged.
================
*/
func TestWalkingIntoTheCaveMouthTeleportsInside(t *testing.T) {
	rt, c, pushed := walkInFixture(t)
	rt.advanceWalkIns(1000)
	if len(*pushed) != 0 {
		t.Fatalf("outside the zone pushed %+v", *pushed)
	}
	placeWalker(rt, c, 600)
	rt.advanceWalkIns(1100)
	if len(*pushed) != 1 || !bytes.Equal((*pushed)[0][0].Payload, []byte{2, 0x15}) {
		t.Fatalf("level-20 entry answered %+v", *pushed)
	}
	rt.advanceWalkIns(1200)
	if len(*pushed) != 1 {
		t.Fatal("standing on in the zone asked again")
	}
	level := int64(60)
	c.Level = &level
	gold := *c.Gold
	placeWalker(rt, c, 100)
	rt.advanceWalkIns(1300)
	placeWalker(rt, c, 600)
	rt.advanceWalkIns(1400)
	if len(*pushed) != 2 || (*pushed)[1][0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("level-60 entry answered %+v", *pushed)
	}
	if uint16(*c.World.Spawn.RegionID) != caveRegion {
		t.Fatalf("arrived in region %v, want the cave", *c.World.Spawn.RegionID)
	}
	if *c.Gold != gold {
		t.Fatal("the walk-in charged a fee")
	}
}

/*
================
TestAPlayerAdmittedInsideAZoneIsNotMoved

A player first sampled inside the mouth's zone (a login there) was placed,
not walked in: nothing happens until it leaves and walks back in.
================
*/
func TestAPlayerAdmittedInsideAZoneIsNotMoved(t *testing.T) {
	rt, c, pushed := walkInFixture(t)
	level := int64(60)
	c.Level = &level
	placeWalker(rt, c, 600)
	rt.advanceWalkIns(1000)
	rt.advanceWalkIns(1100)
	if len(*pushed) != 0 || uint16(*c.World.Spawn.RegionID) == caveRegion {
		t.Fatalf("a player first seen inside the mouth was moved: %+v", *pushed)
	}
	placeWalker(rt, c, 100)
	rt.advanceWalkIns(1200)
	placeWalker(rt, c, 600)
	rt.advanceWalkIns(1300)
	if uint16(*c.World.Spawn.RegionID) != caveRegion {
		t.Fatal("walking back in did not enter the cave")
	}
}

/*
================
TestWalkInRefusesTeleportingAndRiding

The native teleport command's notices: a player standing in teleport mode hears its last
teleport is still working (0x3058 kind 3), a riding one, or one with a
transport out, that a COS cannot enter (kind 4); none moves. A teleporting
player still walking is let through, as natively.
================
*/
func TestWalkInRefusesTeleportingAndRiding(t *testing.T) {
	rt, c, _ := walkInFixture(t)
	level := int64(60)
	c.Level = &level
	link := rt.portals.walkIns["GATE_DUNGEON_DH_IN"]
	c.NativeTeleportMode = 1
	out, moved := rt.enterWalkIn(testDivision, c, link, false)
	if moved || len(out.Frames) != 1 || out.Frames[0].Opcode != opEventNotice || !bytes.Equal(out.Frames[0].Payload, []byte{walkInNoticeTeleportWorking}) {
		t.Fatalf("standing teleporter: %+v", out)
	}
	c.NativeTeleportMode = 0
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true, Mounted: true}
	out, moved = rt.enterWalkIn(testDivision, c, link, false)
	if moved || len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{walkInNoticeCOSCannotEnterGates}) {
		t.Fatalf("rider: %+v", out)
	}
	// A transport out but not ridden is an active vehicle too.
	c.ActiveCOS.Mounted = false
	out, moved = rt.enterWalkIn(testDivision, c, link, false)
	if moved || len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{walkInNoticeCOSCannotEnterGates}) {
		t.Fatalf("summoned transport: %+v", out)
	}
	c.ActiveCOS = nil
	c.NativeTeleportMode = 1
	if _, moved = rt.enterWalkIn(testDivision, c, link, true); !moved {
		t.Fatal("a walking teleporter was stopped")
	}
}
