/*
===========================================================================

returnscroll.go - authoritative travel and scene re-entry

===========================================================================
*/
package action

import (
	"math"
	"opensro.online/server/internal/domain"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// The channel-11 teleport modes a return cast sets (4A0380).
const (
	teleportModeBlocking uint8 = 1
	teleportModeFree     uint8 = 2
)

// errCodeThievesOnly is 4A0380's 0x186A (UIIT_MSG_STRGERR_ONLY_ROBBER_CAN_USE_THIS_ITEM).
const errCodeThievesOnly uint8 = 0x6a

/*
================
pendingReturn
================
*/
type pendingReturn struct {
	division, name string
	character      *enterworld.Character
	due            int64
	generation     uint64
	// destination is a reverse return's chosen point; nil returns to the
	// appointed rebirth point.
	destination *simulation.Spawn
}

/*
================
teleportState
================
*/
func teleportState(c *enterworld.Character, mode uint8) wire.Frame {
	return wire.Frame{Opcode: 0x3122, Payload: wire.NewWriter(6).U32(enterworld.ObjectIDForCharacter(c)).U8(11).U8(mode).Payload()}
}

/*
================
returnScrollDuration

v1.188 4A0380: Param1 is the cast duration (milliseconds in v1.150 data;
v1.188 timers use seconds) and Param2 must be blocking mode 1. A zero
duration takes 4E0B50's minimum timer interval.
================
*/
func returnScrollDuration(ref *enterworld.ItemRef) (int64, bool) {
	duration, mode, ok := returnScrollTiming(ref)
	return duration, ok && mode == teleportModeBlocking
}

/*
================
returnScrollTiming

Param1's duration and the teleport mode Param2 selects: 4A0380 passes
(Param2 == 0) + 1, so a blocking scroll (1) casts in mode 1 and the Thief
Den scroll (0) in mode 2, which leaves the player free to move.
================
*/
func returnScrollTiming(ref *enterworld.ItemRef) (int64, uint8, bool) {
	duration, present := ref.NativeFields.Lookup("itemParam1_29c")
	blocking, hasBlocking := ref.NativeFields.Lookup("itemParam2_2a0")
	if !present || !hasBlocking || math.IsNaN(duration) || math.IsInf(duration, 0) || duration < 0 ||
		duration > math.MaxUint32 || math.Trunc(duration) != duration || blocking != 0 && blocking != 1 {
		return 0, 0, false
	}
	mode := teleportModeBlocking
	if blocking == 0 {
		mode = teleportModeFree
	}
	if duration == 0 {
		return 100, mode, true
	}
	return int64(duration), mode, true
}

/*
================
teleportBlocks

True for the teleport modes that hold the player still: a return's 1 and
the skin change's in-place reload 3. Mode 2 (the Thief Den scroll's long
cast) lets the player move until the timer ends.
================
*/
func teleportBlocks(mode uint8) bool {
	return mode == teleportModeBlocking || mode == skinTeleportMode
}

/*
================
returnScrollAdmission

The player checks every return cast shares: the ordinary scroll (4A0380)
and the reverse return (4A00C0). A native refusal sets *result; a silent
one leaves it. v1.188 error low bytes agree with v1.150 689420 category 1.
================
*/
func (rt *Runtime) returnScrollAdmission(division string, c *enterworld.Character, result *OpResult) bool {
	if !rt.returnCastAdmission(division, c, result) {
		return false
	}
	if rt.hasSummonedTransportCOS(c) {
		*result = itemUseFailure(0x5e)
		return false
	}
	return true
}

/*
================
returnCastAdmission

The checks every return cast shares before its destination's own: the
Thief Den scroll skips RESURRECT's transport refusal (4A0380 tests it only
on that branch).
================
*/
func (rt *Runtime) returnCastAdmission(division string, c *enterworld.Character, result *OpResult) bool {
	// Native 4a0399 checks the quest mask before the active-cast test.
	if rt.QuestTravelBlocks != nil && rt.QuestTravelBlocks(c)&0x20000 != 0 {
		*result = itemUseFailure(0x5f)
		return false
	}
	if c.NativeTeleportMode != 0 {
		*result = itemUseFailure(0x5d)
		return false
	}
	// 4E2940 writes actor core +0x0c from the criminal-state owner. Only
	// state 2 blocks return; transient aggression (state 1) does not.
	if c.PVPState() == 2 {
		*result = itemUseFailure(0x75)
		return false
	}
	if rt.hasOpenSkillCast(division, c.Name) {
		return false
	}
	_, exists := rt.returnCasts.Load(simulation.WorldKey(division, c.Name))
	return !exists
}

/*
================
beginReturnScroll

Inside the item-use character mutation door. v1.188 49B9F0 -> 4A0380
selects Param1 duration, Param2 blocking mode and Param3 destination. The
ordinary RESURRECT family has no destination payload; other return
families must never silently teleport to the race start. The location
check records where the scroll was used (4E0250, slot 0x264): the reverse
return's "last recall point".
================
*/
func (rt *Runtime) beginReturnScroll(division string, c *enterworld.Character, ref *enterworld.ItemRef, row int, request wire.ItemUseRequest, now int64, result *OpResult) bool {
	duration, mode, ok := returnScrollTiming(ref)
	if !ok {
		return false
	}
	var destination *simulation.Spawn
	switch ref.ReturnDestination {
	case "RESURRECT":
		if mode != teleportModeBlocking || !rt.returnScrollAdmission(division, c, result) {
			return false
		}
	case "THIEFDEN":
		// 4A0380: only a thief in job mode (job state 2) returns to the den,
		// at the gate Param4 names.
		if !rt.returnCastAdmission(division, c, result) {
			return false
		}
		if enterworld.DressedJob(c) != domain.JobThief {
			*result = itemUseFailure(errCodeThievesOnly)
			return false
		}
		gate, found := rt.buildingGateSpawn(ref.ReturnTeleport)
		if !found {
			return false
		}
		destination = &gate
	default:
		return false
	}
	at := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	if !rt.startReturnCast(returnCast{division: division, character: c, row: row, slot: request.Slot,
		typeWord: request.TypeWord, duration: duration, destination: destination, now: now, mode: mode}, result) {
		return false
	}
	if point, ok := recordedPoint(c, at); ok {
		c.World.LastRecallPoint = point
	}
	return true
}

/*
================
returnCast

One channel-11 return cast to start: the scroll row it consumes, its use
reply identity, its duration and, for a reverse return, its destination.
================
*/
type returnCast struct {
	division    string
	character   *enterworld.Character
	row         int
	slot        uint8
	typeWord    uint16
	duration    int64
	destination *simulation.Spawn
	now         int64
	// mode is the channel-11 teleport mode; zero is a return's 1. The skin
	// change reloads in place under mode 3 (4EFFC0).
	mode uint8
}

/*
================
startReturnCast

Commits an admitted return cast: stops a walker, enters teleport mode 1,
consumes the scroll and schedules completion (advanceReturnScrolls).
================
*/
func (rt *Runtime) startReturnCast(cast returnCast, result *OpResult) bool {
	c, division, now := cast.character, cast.division, cast.now
	if now > math.MaxInt64-cast.duration {
		return false
	}
	key := simulation.WorldKey(division, c.Name)
	moving := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }).MoveSegment.Valid()
	spawn := rt.liveSpawn(key, c, now)
	state := rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.Spawn = spawn
		w.MoveSegment = nil
		w.SpawnSet = true
		w.MovementSourceSeeded = true
	})
	writeBackWorld(c, state)
	c.World.MoveSegment = nil
	mode := cast.mode
	if mode == 0 {
		mode = 1
	}
	c.NativeTeleportMode = mode
	rt.ClearCombatIntent(division, c.Name)
	rt.Pending.Clear(grounditem.PendingKey(division, c.Name))
	rt.returnCasts.Store(key, pendingReturn{division: division, name: c.Name, character: c, due: now + cast.duration,
		generation: rt.returnGeneration.Add(1), destination: cast.destination})
	remaining := rt.consumeItemUseRow(c, cast.row)
	status := teleportState(c, mode)
	stop := wire.Frame{Opcode: wire.OpObjectSourceCorrection, Payload: wire.ObjectSourceCorrection{Gid: enterworld.ObjectIDForCharacter(c), Position: wire.Position{RegionID: spawn.RegionID, X: float32(spawn.X), Y: float32(spawn.Y), Z: float32(spawn.Z), Heading: spawn.Angle}}.Encode()}
	// HandleItemUse publishes the item's visual after the success.
	*result = OpResult{Frames: []wire.Frame{status, {Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(cast.slot, remaining, cast.typeWord)}}, Broadcast: []wire.Frame{status}}
	// 466F90 is a log record, not a state publication. 4A9430 emits
	// the moving-only correction before 4E0B50 publishes channel 11.
	if moving {
		result.Frames = append([]wire.Frame{stop}, result.Frames...)
		result.Broadcast = append([]wire.Frame{stop}, result.Broadcast...)
	}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}

// v1.150 6FFE50 sends an empty 72DD. Success is the public channel-11
// transition, not a fabricated acknowledgement or a refunded consumable.
/*
================
OpResult
================
*/
func (rt *Runtime) HandleReturnCancel(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil || len(payload) != 0 {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	key := simulation.WorldKey(division, c.Name)
	if _, ok := rt.returnCasts.Load(key); !ok {
		return OpResult{Frames: []wire.Frame{{Opcode: 0xb2dd, Payload: []byte{2, 6}}}}
	}
	if !rt.deps.Update(c, "return-scroll-cancel", func() bool {
		// INFERENCE: a return cast cancels; a skin change's mode-3 reload
		// carries a model already committed.
		if c.NativeTeleportMode == 0 || c.NativeTeleportMode == skinTeleportMode {
			return false
		}
		c.NativeTeleportMode = 0
		rt.returnCasts.Delete(key)
		return true
	}) {
		return OpResult{}
	}
	f := teleportState(c, 0)
	return OpResult{Frames: []wire.Frame{f}, Broadcast: []wire.Frame{f}}
}

// The existing simulation action tick advances the native channel-11 timer.
// A dead actor postpones completion by 1s (405F70/40614E), rather than
// teleporting a corpse or inventing a refund/reset on death.
/*
================
advanceReturnScrolls
================
*/
func (rt *Runtime) advanceReturnScrolls(now int64) {
	var jobs []pendingReturn
	rt.returnCasts.Range(func(_, value any) bool {
		job := value.(pendingReturn)
		if job.due <= now {
			jobs = append(jobs, job)
		}
		return true
	})
	for _, job := range jobs {
		unlock := rt.lockDivision(job.division)
		frames, peers := rt.completeReturnScroll(job, now)
		unlock()
		if len(frames) > 0 && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(job.division, job.name, frames)
		}
		if len(peers) > 0 && rt.PushDivisionPeerFrames != nil {
			rt.PushDivisionPeerFrames(job.division, job.name, peers)
		}
	}
}

/*
================
completeReturnScroll
================
*/
func (rt *Runtime) completeReturnScroll(job pendingReturn, now int64) ([]wire.Frame, []wire.Frame) {
	key := simulation.WorldKey(job.division, job.name)
	current, ok := rt.returnCasts.Load(key)
	if !ok || current.(pendingReturn) != job {
		return nil, nil
	}
	c := rt.findCharacter(job.division, job.name)
	if c == nil || c != job.character {
		rt.returnCasts.Delete(key)
		return nil, nil
	}
	var previous simulation.WorldState
	var previousWorld *enterworld.CharacterWorld
	var destination simulation.Spawn
	rt.bindResidentRegion(key, now)
	if !rt.deps.Update(c, "return-scroll-complete", func() bool {
		if c.DeletePending || c.NativeTeleportMode == 0 {
			rt.returnCasts.Delete(key)
			return false
		}
		if !enterworld.CharacterAlive(c) {
			job.due = now + 1000
			rt.returnCasts.Store(key, job)
			return false
		}
		destination = rt.appointedRebirthPoint(c)
		if job.destination != nil {
			destination = *job.destination
		}
		previousWorld = c.World
		state := rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
			previous = *w
			w.Spawn = destination
			w.MoveSegment = nil
			w.Sitting = false
			w.PostureTransitionUntilMs = 0
			w.SpawnSet = true
			w.MovementSourceSeeded = true
		})
		writeBackWorld(c, state)
		c.World.MoveSegment = nil
		c.NativeTeleportMode = 0
		return true
	}) {
		return nil, nil
	}
	rt.returnCasts.Delete(key)
	rt.endTransformForLoading(job.division, c)
	rt.endPartyAurasForLoading(job.division, c)
	previousPets := rt.relocateReturningPet(job.division, c, destination)
	packets, accepted := rt.deps.ReentryPackets(job.division, job.name)
	clear := teleportState(c, 0)
	if !accepted || len(packets) == 0 || packets[0].NativeOpcode != enterworld.OpcodeResetClient {
		rt.restoreCompanionRelocation(previousPets)
		rt.deps.Update(c, "return-scroll-entry-rollback", func() bool {
			rt.Worlds.Update(key, func() simulation.WorldState { return previous }, func(w *simulation.WorldState) { *w = previous })
			c.World = previousWorld
			return true
		})
		return []wire.Frame{clear}, []wire.Frame{clear}
	}
	rt.bindResidentRegion(key, now)
	rt.Selected.Clear(job.division, job.name)
	rt.NpcDialogs.Clear(job.division, job.name)
	rt.ClearCombatIntent(job.division, job.name)
	rt.clearSkillFinalizes(job.division, job.name)
	rt.clearCompoundJob(compoundKey{job.division, job.name})
	corpses, corpseDespawns := rt.retireCompanionCorpses(job.division, c)
	frames := append(missionReentryFrames(packets), corpses...)
	if snapshot := rt.characterSnapshot(job.division, c); snapshot != nil && snapshot.NativeBodyStatus != 0 {
		frames = append(frames, bodyStatusFrame(enterworld.ObjectIDForCharacter(c), snapshot.NativeBodyStatus))
	}
	return frames, append(corpseDespawns, clear, wire.Frame{Opcode: wire.OpObjectSourceCorrection, Payload: wire.ObjectSourceCorrection{Gid: enterworld.ObjectIDForCharacter(c), Position: wire.Position{RegionID: destination.RegionID, X: float32(destination.X), Y: float32(destination.Y), Z: float32(destination.Z), Heading: destination.Angle}}.Encode()})
}

// 4EC8A0 -> 4FD720 -> COS virtual +30 / 4827F0. Transport COS
// (1/2/3/2) block ordinary returns; pets are a separate migration branch.
/*
================
bool
================
*/
func (rt *Runtime) hasSummonedTransportCOS(c *enterworld.Character) bool {
	cos := c.ActiveCOS
	if cos == nil || !cos.Summoned {
		return false
	}
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return true
	}
	ref, ok := refs.CharacterRefByCodename(cos.Codename)
	return !ok || ref == nil || ref.RefObjID != cos.RefObjID || ref.TidWord&0xfffe == 0x11c6
}

// A successful world reconstruction retires the departing actor's timer. Call
// with the division operation lock held; failed entry keeps the old lifetime.
/*
================
retireReturnForReentry
================
*/
func (rt *Runtime) retireReturnForReentry(division string, c *enterworld.Character) {
	rt.jobDresses.Delete(simulation.WorldKey(division, c.Name))
	rt.deps.Update(c, "return-scroll-retire-reentry", func() bool {
		rt.returnCasts.Delete(simulation.WorldKey(division, c.Name))
		if c.NativeTeleportMode == 0 {
			return false
		}
		c.NativeTeleportMode = 0
		return true
	})
}
