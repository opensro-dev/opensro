/*
===========================================================================

jobscrolls_test.go - the job-suit scroll bans, both settings, live

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
suitedReverseReturnFixture

A suited player holding two reverse return scrolls (the returned row),
last recalled at a field point of the permanent world.
================
*/
func suitedReverseReturnFixture(t *testing.T) (*Runtime, *enterworld.Character, int) {
	t.Helper()
	rt, c, _ := reverseReturnFixture(t)
	scroll := len(c.MissionInventory) - 1
	packed := uint32(instance.Pack(1, 1))
	c.World.PackedInstance = &packed
	point := &domain.WorldPoint{WorldSpawn: *worldSpawnFromMission(simulation.Spawn{RegionID: 25000, X: 812, Y: 30, Z: 1204}), World: 1}
	c.World.LastRecallPoint, c.World.LastDeathPoint = point, point
	dressTrader(c)
	return rt, c, scroll
}

/*
================
TestJobSuitResurrectionScrollFollowsItsTunable

0 (native) a dead suited player revives with the scroll; 1 refuses it
with 0x3E and changes nothing. The reverse return ban alone leaves the
resurrection scroll alone.
================
*/
func TestJobSuitResurrectionScrollFollowsItsTunable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		env     map[string]string
		refused bool
	}{
		{"native", nil, false},
		{"other-ban-only", map[string]string{"SRO_JOB_BLOCK_REVERSE_RETURN_SCROLL": "1"}, false},
		{"banned", map[string]string{"SRO_JOB_BLOCK_RESURRECTION_SCROLL": "1"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			swapTunables(t, tc.env)
			c, items, body := resurrectionScrollFixture(0)
			dressTrader(c)
			rt, _ := newTestRuntime(c, items)
			rt.RefundExperience = func(*enterworld.Character, int64) ([]wire.Frame, bool) { return nil, true }
			if tc.refused {
				assertItemUseRefusedUnchanged(t, rt, c, body, errCodeItemCannotBeUsed)
				return
			}
			rt.HandleItemUse(testDivision, c, body)
			if !enterworld.CharacterAlive(c) {
				t.Fatal("a suited player must revive with the scroll")
			}
		})
	}
}

/*
================
TestJobSuitReverseReturnScrollFollowsItsTunable

0 (native) a suited player's reverse return starts its cast; 1 refuses
it with 0x3E before it is consumed. The resurrection ban alone leaves the
reverse return alone.
================
*/
func TestJobSuitReverseReturnScrollFollowsItsTunable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		env     map[string]string
		refused bool
	}{
		{"native", nil, false},
		{"other-ban-only", map[string]string{"SRO_JOB_BLOCK_RESURRECTION_SCROLL": "1"}, false},
		{"banned", map[string]string{"SRO_JOB_BLOCK_REVERSE_RETURN_SCROLL": "1"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			swapTunables(t, tc.env)
			rt, c, scroll := suitedReverseReturnFixture(t)
			out := useReverseScroll(rt, c, reverseReturnLastRecall)
			if tc.refused {
				if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, wire.EncodeItemUseError(errCodeItemCannotBeUsed)) {
					t.Fatalf("%+v; want refusal %x", out.Frames, errCodeItemCannotBeUsed)
				}
				if c.MissionInventory[scroll].StackCount != 2 || c.NativeTeleportMode != 0 {
					t.Fatal("the refused scroll was consumed or cast")
				}
				return
			}
			if c.MissionInventory[scroll].StackCount != 1 || c.NativeTeleportMode != 1 {
				t.Fatalf("the suited return did not start: %+v", out.Frames)
			}
		})
	}
}

/*
================
TestJobSuitScrollBanTurnsOffLive

A snapshot swapped between two uses reaches the second one: with the ban
on the scroll is refused, and once the ban is lifted (no restart) the
same player's next use starts.
================
*/
func TestJobSuitScrollBanTurnsOffLive(t *testing.T) {
	swapTunables(t, map[string]string{"SRO_JOB_BLOCK_REVERSE_RETURN_SCROLL": "1"})
	rt, c, _ := suitedReverseReturnFixture(t)
	if out := useReverseScroll(rt, c, reverseReturnLastRecall); c.NativeTeleportMode != 0 {
		t.Fatalf("ban on: the return started: %+v", out.Frames)
	}
	off, err := tunables.Parse(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tunables.Swap(off))
	if out := useReverseScroll(rt, c, reverseReturnLastRecall); c.NativeTeleportMode != 1 {
		t.Fatalf("ban lifted: the return did not start: %+v", out.Frames)
	}
}
