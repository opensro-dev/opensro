/*
===========================================================================

jobsuitreload_test.go - a suit going on or off re-enters the wearer in place

The v1.150 client reads the job class and alias only at spawn (868D00), so
a completed suit move is followed by the skin change's mode-3 in-place
reload (reloadJobDress, an inference recorded there). A refused move does
not reload, and the suit's activation wait survives the reload.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
reloadFrameIn

Whether frames carry the channel-11 mode-3 teleport state for the wearer.
================
*/
func reloadFrameIn(frames []wire.Frame, c *enterworld.Character) bool {
	want := teleportState(c, skinTeleportMode)
	for _, frame := range frames {
		if frame.Opcode == want.Opcode && string(frame.Payload) == string(want.Payload) {
			return true
		}
	}
	return false
}

/*
================
TestJobSuitWearReloadsInPlace
================
*/
func TestJobSuitWearReloadsInPlace(t *testing.T) {
	rt, clock, c := jobFixture(t)
	c.Job = domain.CharacterJob{Type: domain.JobTrader, Grade: 1, Alias: "Merchant"}
	wear := encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: jobTestSuitSlot, DestSlot: jobSuitSlot, Quantity: 1})
	rt.HandleItemMove(testDivision, c, wear)
	var pushed, broadcast []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { pushed = append(pushed, frames...) }
	rt.PushDivisionPeerFrames = func(_, _ string, frames []wire.Frame) { broadcast = append(broadcast, frames...) }
	clock.now = clock.now.Add(jobDressSeconds * time.Second)
	rt.advanceJobDresses(clock.NowMs())
	if enterworld.DressedJob(c) != domain.JobTrader || c.NativeTeleportMode != skinTeleportMode {
		t.Fatalf("after the dress: job %d, teleport mode %d, want trader and mode 3", enterworld.DressedJob(c), c.NativeTeleportMode)
	}
	if !reloadFrameIn(pushed, c) || !reloadFrameIn(broadcast, c) {
		t.Fatalf("no mode-3 reload published: actor %+v peers %+v", pushed, broadcast)
	}
	if _, pending := rt.returnCasts.Load(simulation.WorldKey(testDivision, c.Name)); !pending {
		t.Fatal("no in-place reload was scheduled")
	}
	// The reload re-enters the same actor: the activation wait survives.
	rt.retireReturnForReentry(testDivision, c)
	if !rt.jobActivationPending(testDivision, c, clock.NowMs()) {
		t.Fatal("the reload ended the suit's activation wait")
	}
}

/*
================
TestJobSuitStripReloadsInPlace

Taking the suit off in a town reloads too, so the alias clears.
================
*/
func TestJobSuitStripReloadsInPlace(t *testing.T) {
	const town = uint16(0x61A8)
	rt, clock, c := jobFixture(t)
	c.Job = domain.CharacterJob{Type: domain.JobTrader, Grade: 1, Alias: "Merchant"}
	for i := range c.MissionInventory {
		if c.MissionInventory[i].Slot == int64(jobTestSuitSlot) {
			c.MissionInventory[i].Slot = int64(jobSuitSlot)
		}
	}
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(world *simulation.WorldState) {
		world.Spawn = simulation.Spawn{RegionID: town, X: 960, Y: 20, Z: 960}
	})
	strip := encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: jobSuitSlot, DestSlot: jobTestSuitSlot, Quantity: 1})
	if out := rt.HandleItemMove(testDivision, c, strip); len(out.Frames) != 1 || out.Frames[0].Opcode != opJobDressBar {
		t.Fatalf("strip began with %+v", out.Frames)
	}
	var pushed []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { pushed = append(pushed, frames...) }
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {}
	clock.now = clock.now.Add(jobDressSeconds * time.Second)
	rt.advanceJobDresses(clock.NowMs())
	if enterworld.DressedJob(c) != 0 || c.NativeTeleportMode != skinTeleportMode || !reloadFrameIn(pushed, c) {
		t.Fatalf("after the strip: job %d, mode %d, frames %+v", enterworld.DressedJob(c), c.NativeTeleportMode, pushed)
	}
}

/*
================
TestJobSuitRefusedAtTheTimerDoesNotReload

A suit move refused when its timer ends (the wearer left the job) changes
nothing and does not reload.
================
*/
func TestJobSuitRefusedAtTheTimerDoesNotReload(t *testing.T) {
	rt, clock, c := jobFixture(t)
	c.Job = domain.CharacterJob{Type: domain.JobTrader, Grade: 1, Alias: "Merchant"}
	wear := encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: jobTestSuitSlot, DestSlot: jobSuitSlot, Quantity: 1})
	rt.HandleItemMove(testDivision, c, wear)
	c.Job = domain.CharacterJob{}
	var pushed []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { pushed = append(pushed, frames...) }
	clock.now = clock.now.Add(jobDressSeconds * time.Second)
	rt.advanceJobDresses(clock.NowMs())
	if enterworld.DressedJob(c) != 0 || c.NativeTeleportMode != 0 || reloadFrameIn(pushed, c) {
		t.Fatalf("a refused dress reloaded: job %d mode %d frames %+v", enterworld.DressedJob(c), c.NativeTeleportMode, pushed)
	}
}
