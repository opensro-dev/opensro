/*
===========================================================================

returnscroll.go - authoritative travel and scene re-entry

===========================================================================
*/
package action

import (
	"math"
	"sort"

	"opensro.online/server/internal/domain"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

// The channel-11 teleport modes a return cast sets (reference server).
const (
	teleportModeBlocking uint8 = 1
	teleportModeFree     uint8 = 2
)

// errCodeThievesOnly is the reference server's 0x186A (UIIT_MSG_STRGERR_ONLY_ROBBER_CAN_USE_THIS_ITEM).
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
	destination *travelPoint
	// thiefDen marks the Bandit Den scroll's cast (denreturn.go keys its
	// port-only transport rule on it).
	thiefDen bool
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

v1.188 return scroll: Param1 is the cast duration (milliseconds in v1.150 data;
v1.188 timers use seconds) and Param2 must be blocking mode 1. A zero
duration takes the native minimum timer interval.
================
*/
func returnScrollDuration(ref *enterworld.ItemRef) (int64, bool) {
	duration, mode, ok := returnScrollTiming(ref)
	return duration, ok && mode == teleportModeBlocking
}

/*
================
returnScrollTiming

Param1's duration and the teleport mode Param2 selects: the reference server passes
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

The player checks every return cast shares: the ordinary scroll
and the reverse return. A native refusal sets *result; a silent
one leaves it. v1.188 error low bytes agree with v1.150 689420 category 1.
================
*/
func (rt *Runtime) returnScrollAdmission(division string, c *enterworld.Character, result *OpResult) bool {
	if !rt.returnCastAdmission(division, c, result) {
		return false
	}
	if rt.townTravelTransportRefused(c) {
		*result = itemUseFailure(0x5e)
		return false
	}
	return true
}

/*
================
returnCastAdmission

The checks every return cast shares before its destination's own: the
Thief Den scroll skips RESURRECT's transport refusal (the reference server tests it only
on that branch).
================
*/
func (rt *Runtime) returnCastAdmission(division string, c *enterworld.Character, result *OpResult) bool {
	// The reference server checks the quest mask before the active-cast test.
	if rt.QuestTravelBlocks != nil && rt.QuestTravelBlocks(c)&0x20000 != 0 {
		*result = itemUseFailure(0x5f)
		return false
	}
	if c.NativeTeleportMode != 0 {
		*result = itemUseFailure(0x5d)
		return false
	}
	// The reference server writes actor core +0x0c from the criminal-state owner. Only
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

Inside the item-use character mutation door. The v1.188 item use
selects Param1 duration, Param2 blocking mode and Param3 destination. The
ordinary RESURRECT family has no destination payload; other return
families must never silently teleport to the race start. The location
check records where the scroll was used (slot 0x264): the reverse
return's "last recall point".
================
*/
func (rt *Runtime) beginReturnScroll(division string, c *enterworld.Character, ref *enterworld.ItemRef, row int, request wire.ItemUseRequest, now int64, result *OpResult) bool {
	duration, mode, ok := returnScrollTiming(ref)
	if !ok {
		return false
	}
	var destination *travelPoint
	switch ref.ReturnDestination {
	case "RESURRECT":
		if mode != teleportModeBlocking || !rt.returnScrollAdmission(division, c, result) {
			return false
		}
	case "THIEFDEN":
		// Reference server: only a thief in job mode (job state 2) returns to the den,
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
		duration = denReturnDuration(duration, rt.FastDenReturn)
	default:
		return false
	}
	at := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	if !rt.startReturnCast(returnCast{division: division, character: c, row: row, slot: request.Slot,
		typeWord: request.TypeWord, duration: duration, destination: destination, now: now, mode: mode,
		thiefDen: ref.ReturnDestination == "THIEFDEN"}, result) {
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
A premium limited use (countjob.go) casts with no row: nothing is consumed
and the item-use reply is the caller's.
================
*/
type returnCast struct {
	division    string
	character   *enterworld.Character
	row         int // -1: no scroll row (a premium limited use)
	slot        uint8
	typeWord    uint16
	duration    int64
	destination *travelPoint
	now         int64
	// mode is the channel-11 teleport mode; zero is a return's 1. The skin
	// change reloads in place under mode 3.
	mode uint8
	// thiefDen marks the Bandit Den scroll's cast.
	thiefDen bool
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
	rt.notePlacement(c, "return-scroll-start")
	c.World.MoveSegment = nil
	mode := cast.mode
	if mode == 0 {
		mode = 1
	}
	c.NativeTeleportMode = mode
	rt.ClearCombatIntent(division, c.Name)
	rt.Pending.Clear(grounditem.PendingKey(division, c.Name))
	rt.returnCasts.Store(key, pendingReturn{division: division, name: c.Name, character: c, due: now + cast.duration,
		generation: rt.returnGeneration.Add(1), destination: cast.destination, thiefDen: cast.thiefDen})
	status := teleportState(c, mode)
	stop := wire.Frame{Opcode: wire.OpObjectSourceCorrection, Payload: wire.ObjectSourceCorrection{Gid: enterworld.ObjectIDForCharacter(c), Position: wire.Position{RegionID: spawn.RegionID, X: float32(spawn.X), Y: float32(spawn.Y), Z: float32(spawn.Z), Heading: spawn.Angle}}.Encode()}
	// HandleItemUse publishes the item's visual after the success.
	*result = OpResult{Frames: []wire.Frame{status}, Broadcast: []wire.Frame{status}}
	if cast.row >= 0 {
		remaining := rt.consumeItemUseRow(c, cast.row)
		result.Frames = append(result.Frames, wire.Frame{Opcode: wire.OpItemUseResponse,
			Payload: wire.EncodeItemUseSuccess(cast.slot, remaining, cast.typeWord)})
	}
	// The native login log record is not a state publication. The stop emits
	// the moving-only correction before the teleport mode publishes channel 11.
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

/*
================
cancelReturnOnDeathInDoor

The reference server's normal death refreshes the job state job
while the teleport mode is set: it removes the queued channel-11
job and resets the mode to 0, the same cancel a 0x705B request runs. Only a
job of destination kind 6 survives, and no scroll queues one (the return
scroll 0 or 1, the reverse return 2, 3 or 7), so a death during a return or
reverse-return cast cancels it and the scroll stays spent. Returns the
mode-0 frame every observer sees, or nil when nothing was pending. The
caller holds the character's door and the division lock.
================
*/
func (rt *Runtime) cancelReturnOnDeathInDoor(division string, c *enterworld.Character) []wire.Frame {
	key := simulation.WorldKey(division, c.Name)
	if _, ok := rt.returnCasts.Load(key); !ok {
		return nil
	}
	rt.returnCasts.Delete(key)
	if c.NativeTeleportMode == 0 || c.NativeTeleportMode == skinTeleportMode {
		return nil
	}
	c.NativeTeleportMode = 0
	return []wire.Frame{teleportState(c, 0)}
}

// maxReturnScrollCompletionsPerTick bounds the arrivals one tick pays
// for (#2143): a completion is a full world transfer (store door,
// membership move, pet relocation, entry-packet rebuild), and a wave
// of casts landing together used to run every one of them inline -
// the hook's measured worst is 556 ms over 492 slow calls. Leftovers
// stay due in returnCasts and drain at this rate on later ticks; a
// cast waited seconds already, so the added ticks are invisible, and
// one completion always runs so the queue can never stall.
const maxReturnScrollCompletionsPerTick = 2

/*
================
advanceReturnScrolls

Complete the due return casts, oldest first, at most
maxReturnScrollCompletionsPerTick of them; the rest wait for later
ticks (#2143). The map's own order is random, so the due sort is also
what makes a waiting wave fair.
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
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].due != jobs[j].due {
			return jobs[i].due < jobs[j].due
		}
		if jobs[i].division != jobs[j].division {
			return jobs[i].division < jobs[j].division
		}
		return jobs[i].name < jobs[j].name
	})
	if len(jobs) > maxReturnScrollCompletionsPerTick {
		jobs = jobs[:maxReturnScrollCompletionsPerTick]
	}
	for _, job := range jobs {
		frames, peers := rt.completeReturnScrollInDivision(job, now)
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
completeReturnScrollInDivision
================
*/
func (rt *Runtime) completeReturnScrollInDivision(job pendingReturn, now int64) ([]wire.Frame, []wire.Frame) {
	// The wait for the division lane is timed as its own step
	// (returnScrollDivisionWait), outside the completion's work: a job
	// queued behind a long lane holder used to charge that wait to
	// whatever named it (#1820 E, the tickPublicationGate rule).
	var unlock func()
	rt.Steps.Time("returnScrollDivisionWait", func() {
		unlock = rt.lockDivision(job.division)
	})
	defer unlock()
	return rt.completeReturnScroll(job, now)
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
	var arrival travelPoint
	cancelled := false
	currentWorld := instance.ID(domain.CharacterWorldInstance(c))
	rt.bindResidentRegionDeferred(key, now)
	if !rt.deps.UpdateDeferred(c, "return-scroll-complete", func() bool {
		if c.DeletePending || c.NativeTeleportMode == 0 {
			rt.returnCasts.Delete(key)
			return false
		}
		// Death cancels the job (cancelReturnOnDeathInDoor); a corpse that
		// reached here by another path is never teleported.
		if !enterworld.CharacterAlive(c) {
			rt.returnCasts.Delete(key)
			c.NativeTeleportMode, cancelled = 0, true
			return true
		}
		// Body mode does not hold this teleport: berserk (body mode 1) is
		// not the state the native completion waits on, so the cast
		// completes on its timer even mid-berserk.
		arrival = rt.appointedRebirth(job.division, c)
		if job.destination != nil {
			arrival = *job.destination
		}
		destination = arrival.spawn
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
		rt.notePlacement(c, "return-scroll")
		c.World.MoveSegment = nil
		setCharacterWorld(c, arrival.world)
		c.NativeTeleportMode = 0
		return true
	}) {
		return nil, nil
	}
	if cancelled {
		clear := teleportState(c, 0)
		return []wire.Frame{clear}, []wire.Frame{clear}
	}
	rt.returnCasts.Delete(key)
	clearMode := teleportState(c, 0)
	// The town teleport head: hostility and abnormal states settle
	// before the move, and stay settled if the move is rolled back.
	headActor, headPublic := rt.applyTownTeleportHead(job.division, c)
	membership, moved := rt.moveWorldMembership(job.division, c, currentWorld, arrival.world)
	if !moved {
		rt.deps.UpdateDeferred(c, "return-scroll-world-rollback", func() bool {
			rt.Worlds.Update(key, func() simulation.WorldState { return previous }, func(w *simulation.WorldState) { *w = previous })
			c.World = previousWorld
			return true
		})
		return append([]wire.Frame{clearMode}, append(headActor, headPublic...)...), append([]wire.Frame{clearMode}, headPublic...)
	}
	rt.endTransformForLoadingDeferred(job.division, c)
	rt.endPartyAurasForLoadingDeferred(job.division, c)
	previousPets := rt.relocateReturningPet(job.division, c, destination)
	packets, accepted := rt.deps.ReentryPackets(job.division, job.name)
	clear := teleportState(c, 0)
	if !accepted || len(packets) == 0 || packets[0].NativeOpcode != enterworld.OpcodeResetClient {
		if arrival.world != currentWorld {
			rt.restorePopulationSession(membership)
		}
		rt.restoreCompanionRelocation(previousPets)
		rt.deps.UpdateDeferred(c, "return-scroll-entry-rollback", func() bool {
			rt.Worlds.Update(key, func() simulation.WorldState { return previous }, func(w *simulation.WorldState) { *w = previous })
			c.World = previousWorld
			return true
		})
		return append([]wire.Frame{clear}, append(headActor, headPublic...)...), append([]wire.Frame{clear}, headPublic...)
	}
	rt.noteTeleportArrival(job.division, c)
	rt.bindResidentRegionDeferred(key, now)
	rt.Selected.Clear(job.division, job.name)
	rt.closeStorageSession(job.division, job.name)
	rt.NpcDialogs.Clear(job.division, job.name)
	rt.ClearCombatIntent(job.division, job.name)
	rt.clearSkillFinalizes(job.division, job.name)
	rt.clearCompoundJob(compoundKey{job.division, job.name})
	corpses, corpseDespawns := rt.retireCompanionCorpsesDeferred(job.division, c)
	frames := append(missionReentryFrames(packets), corpses...)
	frames = append(frames, headActor...)
	frames = append(frames, headPublic...)
	// Only the body status is replayed here; read it under the door
	// instead of detaching the whole record (#1820).
	if _, _, bodyStatus := rt.characterLifeState(job.division, c); bodyStatus != 0 {
		frames = append(frames, bodyStatusFrame(enterworld.ObjectIDForCharacter(c), bodyStatus))
	}
	return frames, append(append(corpseDespawns, clear, wire.Frame{Opcode: wire.OpObjectSourceCorrection, Payload: wire.ObjectSourceCorrection{Gid: enterworld.ObjectIDForCharacter(c), Position: wire.Position{RegionID: destination.RegionID, X: float32(destination.X), Y: float32(destination.Y), Z: float32(destination.Z), Heading: destination.Angle}}.Encode()}), headPublic...)
}

// The reference server's first-COS transport test (COS virtual +30). Transport COS
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
	// A mode-3 reload re-enters the same actor in place (the reference server keeps the
	// object and its state timers), so a suit's activation wait survives
	// the reload that follows its dress (reloadJobDress).
	if c.NativeTeleportMode != skinTeleportMode {
		rt.endJobActivation(division, c)
	}
	rt.deps.Update(c, "return-scroll-retire-reentry", func() bool {
		rt.returnCasts.Delete(simulation.WorldKey(division, c.Name))
		if c.NativeTeleportMode == 0 {
			return false
		}
		c.NativeTeleportMode = 0
		return true
	})
}
