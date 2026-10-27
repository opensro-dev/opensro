/*
===========================================================================

portalevent_test.go - the 0x7495 success tail's quest event (0x26)

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

// teleportEventMarker is the frame the recording seam answers with, so a
// test can see the event's frames land in the traveller's answer.
var teleportEventMarker = wire.Frame{Opcode: 0xbeef}

/*
================
recordTeleportEvents

Stub the quest lane's door, recording every (kind, gid) it is told.
================
*/
func recordTeleportEvents(rt *Runtime) *[][2]uint32 {
	calls := &[][2]uint32{}
	rt.UpdateQuestTeleport = func(c *enterworld.Character, kind uint8, gid uint32) []wire.Frame {
		*calls = append(*calls, [2]uint32{uint32(kind), gid})
		return []wire.Frame{teleportEventMarker}
	}
	return calls
}

/*
================
TestPortalUseRaisesQuestTeleportEvent

Every 0x7495 kind raises the used-a-teleport quest event once its move
has landed, with the use kind and the gate's gid, and the event's
frames join the traveller's answer; a refusal raises nothing.
================
*/
func TestPortalUseRaisesQuestTeleportEvent(t *testing.T) {
	t.Run("town return", func(t *testing.T) {
		rt, c := gateTownReturnFixture(t)
		calls := recordTeleportEvents(rt)
		out := sendGateTownReturn(rt, c)
		if len(*calls) != 1 || (*calls)[0] != [2]uint32{1, 2001} {
			t.Fatalf("event calls %v, want the town return at the den gate", *calls)
		}
		found := false
		for _, frame := range out.Frames {
			found = found || frame.Opcode == teleportEventMarker.Opcode
		}
		if !found {
			t.Fatalf("the event's frames are missing from the answer: %+v", out.Frames)
		}
	})
	t.Run("town return refused", func(t *testing.T) {
		rt, c := gateTownReturnFixture(t)
		calls := recordTeleportEvents(rt)
		rt.Selected.Clear(testDivision, c.Name)
		sendGateTownReturn(rt, c)
		if len(*calls) != 0 {
			t.Fatalf("a refused use raised the event: %v", *calls)
		}
	})
	t.Run("destination travel", func(t *testing.T) {
		rt, c, _, _ := returnFixture(t, 30000)
		rt.NpcRoster = []simulation.NpcDef{{ObjectID: 2001, RefObjID: 2011, Codename: "NPC_CH_FERRY", TalkFlags: simulation.NpcTalkFlagTeleport,
			Services:      simulation.NpcServices(0).With(simulation.NpcServiceTeleport),
			AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn}}
		rt.NpcSpawn.Enabled = true
		rt.portals = &portalCatalog{
			sources: map[uint32]uint32{2011: 1},
			destinations: map[uint32]portalDestination{
				1: {},
				2: {spawn: simulation.Spawn{RegionID: 25000, X: 100, Y: 30, Z: 200}, world: instance.DefinitionID(1)},
			},
			links: map[[2]uint32]portalLink{{1, 2}: {source: 1, target: 2}},
		}
		rt.Selected.Set(testDivision, c.Name, 2001)
		calls := recordTeleportEvents(rt)
		out := rt.HandlePortal(testDivision, c, wire.NewWriter(9).U32(2001).U8(2).U32(2).Payload())
		if len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
			t.Fatalf("destination travel did not re-enter: %+v", out.Frames)
		}
		if len(*calls) != 1 || (*calls)[0] != [2]uint32{2, 2001} {
			t.Fatalf("event calls %v, want the destination travel at the ferry", *calls)
		}
		found := false
		for _, frame := range out.Frames {
			found = found || frame.Opcode == teleportEventMarker.Opcode
		}
		if !found {
			t.Fatalf("the event's frames are missing from the answer: %+v", out.Frames)
		}
	})
	t.Run("quest gate", func(t *testing.T) {
		rt, c, _, gid := questGateFixture(t)
		calls := recordTeleportEvents(rt)
		out := questGateTravel(rt, c, gid, 0)
		if len(*calls) != 1 || (*calls)[0] != [2]uint32{3, gid} {
			t.Fatalf("event calls %v, want the quest gate", *calls)
		}
		found := false
		for _, frame := range out.Frames {
			found = found || frame.Opcode == teleportEventMarker.Opcode
		}
		if !found {
			t.Fatalf("the event's frames are missing from the answer: %+v", out.Frames)
		}
	})
	t.Run("guide reverse return", func(t *testing.T) {
		rt, c := guideFixture(t)
		died := simulation.Spawn{RegionID: 25000, X: 812, Y: 30, Z: 1204, Angle: 0}
		c.World.LastDeathPoint = &domain.WorldPoint{WorldSpawn: *worldSpawnFromMission(died)}
		calls := recordTeleportEvents(rt)
		out := guideReturn(rt, c, reverseReturnLastDeath)
		if len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
			t.Fatalf("the guide's return did not re-enter the world: %+v", out.Frames)
		}
		if len(*calls) != 1 || (*calls)[0] != [2]uint32{5, 2002} {
			t.Fatalf("event calls %v, want the reverse return at the guide", *calls)
		}
		found := false
		for _, frame := range out.Frames {
			found = found || frame.Opcode == teleportEventMarker.Opcode
		}
		if !found {
			t.Fatalf("the event's frames are missing from the answer: %+v", out.Frames)
		}
	})
}
