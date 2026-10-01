/*
===========================================================================

returnscroll_test.go - travel admission, cancellation and companion migration

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
returnFixture
================
*/
func returnFixture(t *testing.T, duration float64) (*Runtime, *enterworld.Character, *fakeClock, *enterworld.ItemRef) {
	t.Helper()
	c := rebirthTestCharacter(20, 100)
	items := testItems()
	ref := &enterworld.ItemRef{RefObjID: 61, Codename: "ITEM_ETC_SCROLL_RETURN_01", Country: 3, TypeIDs: [4]int64{3, 3, 3, 1}, ReturnDestination: "RESURRECT", NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": duration, "itemParam2_2a0": 1})}
	items[ref.Codename] = ref
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 21, RefObjID: 61, Codename: ref.Codename, TypeFlags: 0x9ec, StackCount: 4, VarianceBits: "0"})
	rt, clock := newTestRuntime(c, items)
	return rt, c, clock, ref
}

/*
================
useReturn
================
*/
func useReturn(rt *Runtime, c *enterworld.Character) OpResult {
	return rt.HandleItemUse(testDivision, c, []byte{21, 0xec, 9})
}

/*
================
TestReturnBlocksMovementAndSummoningUntilServerCancellation
================
*/
func TestReturnBlocksMovementAndSummoningUntilServerCancellation(t *testing.T) {
	rt, c, clock, ref := returnFixture(t, 30000)
	items := testItems()
	items[ref.Codename] = ref
	rt.deps.(*enterworld.Deps).Items = testCosSource(items)
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 22, RefObjID: 3905, Codename: "ITEM_COS_T_DHORSE3", TypeFlags: 0x11ec, StackCount: 1})
	moves := movement.NewRuntime(rt.deps.(*enterworld.Deps), rt.Worlds)
	moves.Now = func() time.Time { return time.UnixMilli(clock.NowMs()) }
	// Angular movement needs no terrain fixture and still consumes the same mode gate.
	request := []byte{0, 0, 1, 0}
	if out := moves.HandleMove(testDivision, c, request); out.Refusal != nil {
		t.Fatalf("pre-cast move refused: %+v", out.Refusal)
	}
	useReturn(rt, c)
	before := c.Snapshot()
	if out := moves.HandleMove(testDivision, c, request); out.Refusal == nil || out.Refusal.Reason != "teleportCasting" || len(out.Frames) != 0 {
		t.Fatalf("casting move accepted: %+v", out)
	}
	if out := rt.HandleItemUse(testDivision, c, []byte{22, 0xec, 0x11}); out.Frames[0].Payload[0] != 2 || c.ActiveCOS != nil || len(c.MissionInventory) != len(before.MissionInventory) {
		t.Fatal("summon bypassed active return")
	}
	rt.HandleReturnCancel(testDivision, c, nil)
	if out := moves.HandleMove(testDivision, c, request); out.Refusal != nil {
		t.Fatalf("cancel did not release movement: %+v", out.Refusal)
	}
	if out := rt.HandleItemUse(testDivision, c, []byte{22, 0xec, 0x11}); out.Frames[0].Payload[0] != 1 || c.ActiveCOS == nil {
		t.Fatal("cancel did not release summoning")
	}
}

/*
================
TestReturnScrollCommitsConsumptionAndServerTimedReentry
================
*/
func TestReturnScrollCommitsConsumptionAndServerTimedReentry(t *testing.T) {
	for _, duration := range []float64{5000, 15000, 30000} {
		t.Run(time.Duration(duration).String(), func(t *testing.T) {
			rt, c, clock, _ := returnFixture(t, duration)
			before := c.Snapshot()
			want := simulation.Spawn{RegionID: 25416, X: 703, Y: 42, Z: 1575, Angle: 12345}
			c.World.RebirthPoint = worldSpawnFromMission(want)
			if duration == 15000 {
				c.World.RebirthPoint = worldSpawnFromMission(simulation.ChinaStartProfile())
				c.World.RebirthGateRefID = 2094
				rt.portals = &portalCatalog{sources: map[uint32]uint32{2094: 1}, destinations: map[uint32]portalDestination{1: {recall: true, spawn: want}}}
			}
			out := useReturn(rt, c)
			assertOpcodes(t, out.Frames, 0x3122, wire.OpItemUseResponse, 0x3449)
			if out.Frames[1].Payload[0] != 1 || binary.LittleEndian.Uint16(out.Frames[1].Payload[2:]) != 3 || c.NativeTeleportMode != 1 || before.NativeTeleportMode != 0 {
				t.Fatal("authority/snapshot not committed")
			}
			var sent []wire.Frame
			rt.PushCharacterFrames = func(_, _ string, f []wire.Frame) { sent = append(sent, f...) }
			clock.Advance(time.Duration(duration-1) * time.Millisecond)
			rt.TickHook()(clock.NowMs())
			if len(sent) != 0 {
				t.Fatal("early return")
			}
			clock.Advance(time.Millisecond)
			rt.TickHook()(clock.NowMs())
			if len(sent) == 0 || sent[0].Opcode != enterworld.OpcodeResetClient {
				t.Fatalf("missing native reentry: %+v", sent)
			}
			if got := missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{}); got != want || c.NativeTeleportMode != 0 {
				t.Fatalf("destination %+v", got)
			}
			n := len(sent)
			clock.Advance(time.Minute)
			rt.TickHook()(clock.NowMs())
			if len(sent) != n {
				t.Fatal("completed twice")
			}
		})
	}
}

/*
================
TestReturnCancelReplacementStaleTimerAndDisconnect
================
*/
func TestReturnCancelReplacementStaleTimerAndDisconnect(t *testing.T) {
	rt, c, clock, _ := returnFixture(t, 30000)
	useReturn(rt, c)
	key := simulation.WorldKey(testDivision, c.Name)
	old, _ := rt.returnCasts.Load(key)
	if repeat := useReturn(rt, c); repeat.Frames[0].Payload[0] != 2 || repeat.Frames[0].Payload[1] != 0x5d {
		t.Fatal("repeat consumed another scroll")
	}
	out := rt.HandleReturnCancel(testDivision, c, nil)
	assertOpcodes(t, out.Frames, 0x3122)
	if c.NativeTeleportMode != 0 || out.Frames[0].Payload[5] != 0 {
		t.Fatal("cancel missing")
	}
	if c.MissionInventory[len(c.MissionInventory)-1].StackCount != 3 {
		t.Fatal("cancel refunded consumed scroll")
	}
	// Replacement at the identical clock value must still retire the old identity.
	useReturn(rt, c)
	if f, _ := rt.completeReturnScroll(old.(pendingReturn), clock.NowMs()+60000); len(f) != 0 || c.NativeTeleportMode != 1 {
		t.Fatal("stale timer cleared replacement")
	}
	rt.ForgetCharacter(testDivision, c.Name)
	if c.NativeTeleportMode != 0 {
		t.Fatal("actor timer survived disconnect")
	}
	if _, ok := rt.returnCasts.Load(key); ok {
		t.Fatal("timer leaked")
	}
}

/*
================
TestReturnDeathDefersAndEntryFailureRollsBack
================
*/
func TestReturnDeathDefersAndEntryFailureRollsBack(t *testing.T) {
	rt, c, clock, _ := returnFixture(t, 5000)
	useReturn(rt, c)
	key := simulation.WorldKey(testDivision, c.Name)
	zero := int64(0)
	c.CurrentHP = &zero
	clock.Advance(5 * time.Second)
	rt.advanceReturnScrolls(clock.NowMs())
	v, ok := rt.returnCasts.Load(key)
	if !ok || v.(pendingReturn).due != clock.NowMs()+1000 || c.NativeTeleportMode != 1 {
		t.Fatal("native dead timer not deferred")
	}
	hp := int64(100)
	c.CurrentHP = &hp
	before := missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{})
	rt.deps = &failedWarpEntry{rt.deps.(*enterworld.Deps)}
	clock.Advance(time.Second)
	rt.advanceReturnScrolls(clock.NowMs())
	if c.NativeTeleportMode != 0 || missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{}) != before {
		t.Fatal("failed entry stranded actor")
	}
}

/*
================
TestReturnInvalidReferencesNeverConsume
================
*/
func TestReturnInvalidReferencesNeverConsume(t *testing.T) {
	for _, duration := range []float64{-1, 1.5, math.NaN(), math.Inf(1), 1e20} {
		rt, c, _, _ := returnFixture(t, duration)
		out := useReturn(rt, c)
		if out.Frames[0].Payload[0] != 2 || c.NativeTeleportMode != 0 || c.MissionInventory[len(c.MissionInventory)-1].StackCount != 4 {
			t.Fatal("invalid reference admitted")
		}
	}
}

/*
================
TestReturnCriminalStateComesFromPersistedCharacterAuthority
================
*/
func TestReturnCriminalStateComesFromPersistedCharacterAuthority(t *testing.T) {
	_, seed, _, ref := returnFixture(t, 30000)
	seed.PK = &domain.PKRecord{DailyCount: 1, TotalCount: 1, Penalty: 100}
	d := openDoorRuntime(t, t.TempDir(), seed)
	deps := d.rt.deps.(*enterworld.Deps)
	deps.Items.(staticItemSource)[ref.Codename] = ref
	deps.UpdateCharacter = d.authority.UpdateCharacter
	before := d.character.Snapshot()
	out := useReturn(d.rt, d.character)
	if out.Frames[0].Payload[0] != 2 || out.Frames[0].Payload[1] != 0x75 || d.character.NativeTeleportMode != 0 || bagRowByCodename(d.character, ref.Codename).StackCount != 4 {
		t.Fatal("persisted criminal record bypassed native return restriction")
	}
	deps.Update(d.character, "test-penalty-cleared", func() bool {
		d.character.PK.Penalty = 0
		d.character.Aggressions = map[uint32]uint32{100: 1}
		return true
	})
	if before.PK.Penalty != 100 || useReturn(d.rt, d.character).Frames[0].Payload[5] != 1 {
		t.Fatal("aggression must not inherit the murderer restriction or mutate old snapshots")
	}
}

/*
================
TestReturnCompanionBranchesAndNewWorldProjection
================
*/
func TestReturnCompanionBranchesAndNewWorldProjection(t *testing.T) {
	for _, band := range []uint16{2, 3, 4} {
		rt, c, clock, ref := returnFixture(t, 5000)
		items := testItems()
		items[ref.Codename] = ref
		source := cosTestItemSource{staticItemSource: items, characters: map[string]*enterworld.CharacterRef{"COS_FIXTURE": {RefObjID: 3914, Codename: "COS_FIXTURE", TidWord: 0x1c6 | (band << 11), RunSpeed: 40, WalkSpeed: 20}}}
		rt.deps.(*enterworld.Deps).Items = source
		gid, _ := enterworld.CosObjectIDForCharacter(c)
		c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_FIXTURE", Summoned: true, CurrentHP: 100}
		rt.BindPetSession(testDivision, c, 123)
		before := c.Snapshot()
		out := useReturn(rt, c)
		if band == 2 {
			if out.Frames[0].Payload[0] != 2 || out.Frames[0].Payload[1] != 0x5e || c.NativeTeleportMode != 0 {
				t.Fatal("transport return admitted")
			}
			continue
		}
		if c.NativeTeleportMode != 1 || before.NativeTeleportMode != 0 {
			t.Fatal("pet blocked return or snapshot mutated")
		}
		clock.Advance(5 * time.Second)
		rt.advanceReturnScrolls(clock.NowMs())
		if c.NativeTeleportMode != 0 || !c.ActiveCOS.Summoned || c.ActiveCOS.GID != gid {
			t.Fatal("pet identity lost")
		}
		p := rt.PetPresentation(testDivision, c.Name)
		if p == nil || simulation.WorldDistance2D(p.World.Spawn, missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{})) > float64(companionSpawnRadius) {
			t.Fatalf("pet left behind: %+v", p)
		}
	}
}

/*
================
TestReturnMovingCorrectionPrecedesStateAndConsumption
================
*/
func TestReturnMovingCorrectionPrecedesStateAndConsumption(t *testing.T) {
	rt, c, clock, _ := returnFixture(t, 5000)
	installMidMove(rt, c, clock)
	out := useReturn(rt, c)
	assertOpcodes(t, out.Frames, wire.OpObjectSourceCorrection, 0x3122, wire.OpItemUseResponse, 0x3449)
	if rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) }).MoveSegment.Valid() {
		t.Fatal("movement remained active")
	}
}

/*
================
TestReturnUsesStoreBoundaryAndRuntimeTimerDoesNotResurrectOnReboot
================
*/
func TestReturnUsesStoreBoundaryAndRuntimeTimerDoesNotResurrectOnReboot(t *testing.T) {
	_, seed, _, ref := returnFixture(t, 30000)
	d := openDoorRuntime(t, t.TempDir(), seed)
	deps := d.rt.deps.(*enterworld.Deps)
	deps.Items.(staticItemSource)[ref.Codename] = ref
	deps.UpdateCharacter = d.authority.UpdateCharacter
	deps.ReadCharacter = func(division string, fn func()) {
		d.authority.ReadCharacters(division, func([]*enterworld.Character) { fn() })
	}
	before := d.rt.characterSnapshot(testDivision, d.character)
	useReturn(d.rt, d.character)
	after := d.rt.characterSnapshot(testDivision, d.character)
	if before.NativeTeleportMode != 0 || after.NativeTeleportMode != 1 || bagRowByCodename(before, ref.Codename).StackCount != 4 || bagRowByCodename(after, ref.Codename).StackCount != 3 {
		t.Fatal("store snapshot/commit boundary broken")
	}
	d = d.reboot(t)
	if d.character.NativeTeleportMode != 0 || bagRowByCodename(d.character, ref.Codename).StackCount != 3 {
		t.Fatal("reboot lost consumption or resurrected timer")
	}
}

/*
================
TestReturnScrollQuestBlockPrecedesConsumptionAndCasting
================
*/
func TestReturnScrollQuestBlockPrecedesConsumptionAndCasting(t *testing.T) {
	rt, c, _, _ := returnFixture(t, 30000)
	rt.QuestTravelBlocks = func(*enterworld.Character) uint32 { return 0x60000 }
	before := c.Snapshot()
	out := useReturn(rt, c)
	want := itemUseFailure(0x5f)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, want.Frames[0].Payload) || c.NativeTeleportMode != 0 || !reflect.DeepEqual(c.MissionInventory, before.MissionInventory) {
		t.Fatal("quest restriction consumed or started return")
	}
	rt.QuestTravelBlocks = func(*enterworld.Character) uint32 { return 0x40000 }
	if out := useReturn(rt, c); c.NativeTeleportMode != 1 || out.Frames[0].Opcode != 0x3122 {
		t.Fatal("gate-only mask blocked return scroll")
	}
}
