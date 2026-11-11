/*
===========================================================================

jobscrolls_test.go - SRO_JOB_NO_RESCUE_SCROLLS, both settings

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
)

/*
================
TestJobSuitResurrectionScrollFollowsTheRule

Off (native) a dead suited player revives with the scroll; on, the scroll
is refused with 0x3E and nothing changes.
================
*/
func TestJobSuitResurrectionScrollFollowsTheRule(t *testing.T) {
	for _, on := range []bool{false, true} {
		c, items, body := resurrectionScrollFixture(0)
		dressTrader(c)
		rt, _ := newTestRuntime(c, items)
		rt.JobNoRescueScrolls = on
		rt.RefundExperience = func(*enterworld.Character, int64) ([]wire.Frame, bool) { return nil, true }
		if on {
			assertItemUseRefusedUnchanged(t, rt, c, body, errCodeItemCannotBeUsed)
			continue
		}
		rt.HandleItemUse(testDivision, c, body)
		if !enterworld.CharacterAlive(c) {
			t.Fatal("rule off: a suited player must revive with the scroll")
		}
	}
}

/*
================
TestJobSuitReverseReturnScrollFollowsTheRule

Off (native) a suited player's reverse return to a field point starts its
cast; on, the scroll is refused with 0x3E before it is consumed.
================
*/
func TestJobSuitReverseReturnScrollFollowsTheRule(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, c, _ := reverseReturnFixture(t)
		rt.JobNoRescueScrolls = on
		scroll := len(c.MissionInventory) - 1
		packed := uint32(instance.Pack(1, 1))
		c.World.PackedInstance = &packed
		point := &domain.WorldPoint{WorldSpawn: *worldSpawnFromMission(simulation.Spawn{RegionID: 25000, X: 812, Y: 30, Z: 1204}), World: 1}
		c.World.LastRecallPoint, c.World.LastDeathPoint = point, point
		dressTrader(c)
		out := useReverseScroll(rt, c, reverseReturnLastRecall)
		if on {
			if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, wire.EncodeItemUseError(errCodeItemCannotBeUsed)) {
				t.Fatalf("rule on: %+v; want refusal %x", out.Frames, errCodeItemCannotBeUsed)
			}
			if c.MissionInventory[scroll].StackCount != 2 || c.NativeTeleportMode != 0 {
				t.Fatal("rule on: the refused scroll was consumed or cast")
			}
			continue
		}
		if c.MissionInventory[scroll].StackCount != 1 || c.NativeTeleportMode != 1 {
			t.Fatalf("rule off: the suited return did not start: %+v", out.Frames)
		}
	}
}
