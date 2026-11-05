/*
===========================================================================

questgate_test.go - the quest instance exit gate: data, placement,
select answer and 0x7495 type 3

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// questGateTestRecall is the recall point a gate sends the fixture to.
var questGateTestRecall = simulation.Spawn{RegionID: 24744, X: 900, Y: 0, Z: 1200}

/*
================
questGateTestWorldPoint
================
*/
func questGateTestWorldPoint(s simulation.Spawn) *domain.WorldPoint {
	region, angle := int64(s.RegionID), int64(s.Angle)
	x, y, z := s.X, s.Y, s.Z
	return &domain.WorldPoint{WorldSpawn: domain.WorldSpawn{RegionID: &region, X: &x, Y: &y, Z: &z, Angle: &angle}}
}

/*
================
questGateFixture

A party-quest layer holding the fixture character, a recall point on
file, and an exit gate raised beside the entry with a fixed roll (every
offset 0.5 * 80 + 20 = 60), selected.
================
*/
func questGateFixture(t *testing.T) (*Runtime, *enterworld.Character, instance.Lease, uint32) {
	t.Helper()
	c := instanceWorldTestCharacter(21, "Gatekeeper")
	rt, _ := newInstanceWorldRuntime(t, c)
	rt.NpcSpawn.Enabled = true
	rt.portals = &portalCatalog{questGate: &questGateBuilding{ref: 19076, tid: 0x130, height: 16, radius: 10}}
	rt.questGates.roll = func() float64 { return 0.5 }
	admitInstanceWorldCharacter(t, rt, c, 1, instanceWorldTestStart)
	lease := openInstanceWorldOrFail(t, rt, instanceWorldTestPartyDefinition, 0)
	if status := rt.TransferCharacterToInstance(testDivision, c, lease, instanceWorldTestEntry); status != instance.Success {
		t.Fatalf("transfer: status %d", status)
	}
	c.World.LastRecallPoint = questGateTestWorldPoint(questGateTestRecall)
	gid, ok := rt.SpawnQuestExitGate(lease.ID, instanceWorldTestEntry)
	if !ok {
		t.Fatal("the gate was not raised")
	}
	rt.Selected.Set(testDivision, c.Name, gid)
	return rt, c, lease, gid
}

/*
================
questGateTravel
================
*/
func questGateTravel(rt *Runtime, c *enterworld.Character, gid uint32, choice uint8) OpResult {
	return rt.HandlePortal(testDivision, c, wire.NewWriter(6).U32(gid).U8(gateInstanceTravel).U8(choice).Payload())
}

/*
================
TestQuestGateBuildingRow

Shipped data: the INS_QUEST_TELEPORT row loads as a teleport-gate
building (type word & 0x1e == 0x10) with positive bounds.
================
*/
func TestQuestGateBuildingRow(t *testing.T) {
	clientdata.RequireGameData(t)
	catalog, err := loadPortalCatalog(gamedatatest.TextdataDir(t))
	if err != nil {
		t.Fatal(err)
	}
	gate := catalog.questGate
	if gate == nil || gate.ref != 19076 || gate.tid&0x1e != 0x10 || gate.height <= 0 || gate.radius <= 0 {
		t.Fatalf("quest gate row = %+v", gate)
	}
}

/*
================
TestQuestGateStandsInItsWorldOnly

The gate rises 20-100 from the fall on each horizontal axis, keeps the
fall's height, is a teleport gate holding 0x8000000, joins only its own
world's scope pass, and goes when the layer's gates are removed.
================
*/
func TestQuestGateStandsInItsWorldOnly(t *testing.T) {
	rt, _, lease, gid := questGateFixture(t)
	gates := rt.QuestGateNpcs(uint32(lease.ID))
	if len(gates) != 1 || gates[0].ObjectID != gid {
		t.Fatalf("gates in the layer = %+v", gates)
	}
	gate := gates[0]
	want := simulation.Spawn{RegionID: instanceWorldTestEntry.RegionID, X: instanceWorldTestEntry.X + 60,
		Y: instanceWorldTestEntry.Y, Z: instanceWorldTestEntry.Z + 60, Angle: 32768}
	if gate.Spawn != want {
		t.Fatalf("gate stands at %+v, want %+v", gate.Spawn, want)
	}
	if gate.Teleport == nil || gate.TalkFlags != simulation.NpcTalkFlagTeleportGate ||
		!gate.Services.Has(simulation.NpcServiceTeleportGate) || gate.RefObjID != 19076 {
		t.Fatalf("gate def = %+v", gate)
	}
	if gid < questGateGIDBase || gid >= questGateGIDBase+questGateGIDSpan {
		t.Fatalf("gate gid %d outside its band", gid)
	}
	if other := rt.QuestGateNpcs(domain.DefaultWorldInstance); len(other) != 0 {
		t.Fatalf("the gate leaked into the field: %+v", other)
	}
	rt.RemoveQuestExitGates(lease.ID)
	if left := rt.QuestGateNpcs(uint32(lease.ID)); len(left) != 0 {
		t.Fatalf("gates after removal = %+v", left)
	}
}

/*
================
TestQuestGateSelectAnswer

Selecting the gate answers [1][gid][0x8000000][1 row][kind 0], the shape
the v1.150 client reads; a gate in another world is not found, and a
gate beyond its range answers 4.
================
*/
func TestQuestGateSelectAnswer(t *testing.T) {
	rt, c, _, gid := questGateFixture(t)
	rt.Selected.Clear(testDivision, c.Name)
	out := rt.HandleObjectSelect(testDivision, c, wire.NewWriter(4).U32(gid).Payload())
	want := wire.NewWriter(11).U8(1).U32(gid).U32(0x8000000).U8(1).U8(0).Payload()
	if out.Refusal != "" || len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, want) {
		t.Fatalf("select answer = %+v, want %x", out, want)
	}
	if selected, ok := rt.Selected.Get(testDivision, c.Name); !ok || selected != gid {
		t.Fatal("the gate was not recorded as the selection")
	}

	far, ok := rt.SpawnQuestExitGate(instance.ID(domain.CharacterWorldInstance(c)),
		simulation.Spawn{RegionID: instanceWorldTestEntry.RegionID, X: instanceWorldTestEntry.X + 3000, Z: instanceWorldTestEntry.Z})
	if !ok {
		t.Fatal("the far gate was not raised")
	}
	out = rt.HandleObjectSelect(testDivision, c, wire.NewWriter(4).U32(far).Payload())
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, wire.EncodeObjectSelectRefusal(hitRangeTooFar)) {
		t.Fatalf("far gate select = %+v", out)
	}

	elsewhere, ok := rt.SpawnQuestExitGate(instance.ID(domain.DefaultWorldInstance), instanceWorldTestEntry)
	if !ok {
		t.Fatal("the field gate was not raised")
	}
	out = rt.HandleObjectSelect(testDivision, c, wire.NewWriter(4).U32(elsewhere).Payload())
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, wire.EncodeObjectSelectRefusal(selectObjectNotFound)) {
		t.Fatalf("other-world gate select = %+v", out)
	}
}

/*
================
TestQuestGateTravelRefusals

The reference case 3 order: the shared prologue (teleport under way,
hit range), then the quest bit, a summoned transport, the choice, and a
point that cannot be entered.
================
*/
func TestQuestGateTravelRefusals(t *testing.T) {
	refused := func(t *testing.T, out OpResult, code byte) {
		t.Helper()
		if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{2, code}) {
			t.Fatalf("got %+v, want refusal %#x", out.Frames, code)
		}
	}
	t.Run("unselected", func(t *testing.T) {
		rt, c, _, gid := questGateFixture(t)
		rt.Selected.Clear(testDivision, c.Name)
		refused(t, questGateTravel(rt, c, gid, 0), 2)
	})
	t.Run("trailing bytes", func(t *testing.T) {
		rt, c, _, gid := questGateFixture(t)
		refused(t, rt.HandlePortal(testDivision, c, wire.NewWriter(7).U32(gid).U8(gateInstanceTravel).U8(0).U8(0).Payload()), 2)
	})
	t.Run("teleporting", func(t *testing.T) {
		rt, c, _, gid := questGateFixture(t)
		c.NativeTeleportMode = 1
		refused(t, questGateTravel(rt, c, gid, 0), errCodeTeleportState)
	})
	t.Run("out of range before the quest bit", func(t *testing.T) {
		rt, c, lease, _ := questGateFixture(t)
		far, _ := rt.SpawnQuestExitGate(lease.ID, simulation.Spawn{RegionID: instanceWorldTestEntry.RegionID, X: instanceWorldTestEntry.X + 3000})
		rt.Selected.Set(testDivision, c.Name, far)
		rt.QuestTravelBlocks = func(*enterworld.Character) uint32 { return operationMaskQuestTravel }
		refused(t, questGateTravel(rt, c, far, 0), hitRangeTooFar)
	})
	t.Run("quest bit", func(t *testing.T) {
		rt, c, _, gid := questGateFixture(t)
		rt.QuestTravelBlocks = func(*enterworld.Character) uint32 { return operationMaskQuestTravel }
		refused(t, questGateTravel(rt, c, gid, 0), errCodeGuideQuestBlock)
	})
	t.Run("transport out", func(t *testing.T) {
		rt, c, _, gid := questGateFixture(t)
		c.ActiveCOS = &enterworld.CharacterCOS{GID: 0x700001, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true}
		refused(t, questGateTravel(rt, c, gid, 0), errCodeGuideTransport)
	})
	t.Run("choice out of range", func(t *testing.T) {
		rt, c, _, gid := questGateFixture(t)
		refused(t, questGateTravel(rt, c, gid, 1), 2)
	})
	t.Run("no recall point", func(t *testing.T) {
		rt, c, _, gid := questGateFixture(t)
		c.World.LastRecallPoint = nil
		refused(t, questGateTravel(rt, c, gid, 0), errCodeGateMoveFailed)
	})
}

/*
================
TestQuestGateTravelLeavesTheInstance

Choice 0 takes the player out of the layer to the last recall point, in
the field, and clears the transfer anchor like the layer's own return.
================
*/
func TestQuestGateTravelLeavesTheInstance(t *testing.T) {
	rt, c, _, gid := questGateFixture(t)
	out := questGateTravel(rt, c, gid, 0)
	if len(out.Frames) != 0 {
		t.Fatalf("a passing gate answered %+v", out.Frames)
	}
	if got := domain.CharacterWorldInstance(c); got != domain.DefaultWorldInstance {
		t.Fatalf("world after the gate = %d, want the field", got)
	}
	if c.World.InstanceReturn != nil {
		t.Fatal("the gate kept the transfer anchor")
	}
	pose := rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) }).Spawn
	if pose.RegionID != questGateTestRecall.RegionID || pose.X != questGateTestRecall.X || pose.Z != questGateTestRecall.Z {
		t.Fatalf("pose after the gate = %+v, want the recall point %+v", pose, questGateTestRecall)
	}
}
