/*
===========================================================================

petai.go - session-owned summoned-pet movement, pickup and peer presentation

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/item/grounditem"
	"sort"
	"strings"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
petOwnerKey
================
*/
type petOwnerKey struct{ division, name string }

/*
================
petSession
================
*/
type petSession struct {
	session   uint64
	character *enterworld.Character
	refObjID  uint32
	follower  *simulation.PetFollower
	// Transport COS do not run follower AI. Their admission anchor belongs to
	// this same presentation owner; mounted motion comes from the rider owner.
	transportCOS   *enterworld.CharacterCOS
	transportWorld simulation.WorldState
	generation     uint64
	pickup         *wire.ItemMoveRequest
	pickupDeadline int64
	public         []wire.Frame
}

// BindPetSession admits a logical authenticated owner, not every character in
// storage. Duplicate EnterWorld/resume preserves the live pet movement plane.
/*
================
BindPetSession
================
*/
func (rt *Runtime) BindPetSession(division string, c *enterworld.Character, session uint64) {
	if c == nil || session == 0 {
		return
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	key := petOwnerKey{division, strings.ToLower(c.Name)}
	rt.petMu.Lock()
	old := rt.petSessions[key]
	if old != nil && old.session == session && old.character == c {
		rt.petMu.Unlock()
		return
	}
	if rt.petSessions == nil {
		rt.petSessions = make(map[petOwnerKey]*petSession)
	}
	state := &petSession{session: session, character: c}
	rt.petSessions[key] = state
	rt.petMu.Unlock()
	rt.deps.Read(division, func() {
		if cos := c.ActiveCOS; cos != nil && cos.Summoned {
			state.transportCOS = cos
			state.transportWorld = simulation.WorldState{Spawn: rt.liveSpawn(simulation.WorldKey(division, c.Name), c, rt.Now().UnixMilli())}
			if old != nil && old.character == c && old.transportCOS == cos {
				state.transportWorld = old.transportWorld
			}
		}
	})
}

// Called with the division lock held; the map lock never spans a character
// authority door, movement constraint, or packet publication.
/*
================
forgetPetSession
================
*/
func (rt *Runtime) forgetPetSession(division, name string) {
	rt.petMu.Lock()
	defer rt.petMu.Unlock()
	delete(rt.petSessions, petOwnerKey{division, strings.ToLower(name)})
}

/*
================
advancePets
================
*/
func (rt *Runtime) advancePets(nowMs int64) []simulation.DivisionFrames {
	rt.petMu.Lock()
	keys := make([]petOwnerKey, 0, len(rt.petSessions))
	for key := range rt.petSessions {
		keys = append(keys, key)
	}
	rt.petMu.Unlock()
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].division != keys[j].division {
			return keys[i].division < keys[j].division
		}
		return keys[i].name < keys[j].name
	})
	var out []simulation.DivisionFrames
	for _, key := range keys {
		unlock := rt.lockDivision(key.division)
		frames := rt.advancePet(key, nowMs)
		rt.petMu.Lock()
		state := rt.petSessions[key]
		rt.petMu.Unlock()
		characterID := int64(0)
		if state != nil {
			characterID = state.character.ID
		}
		var public []wire.Frame
		if state != nil {
			public = state.public
			state.public = nil
		}
		unlock()
		if len(public) > 0 && rt.PushDivisionPeerFrames != nil {
			rt.PushDivisionPeerFrames(key.division, key.name, public)
		}
		if len(frames) != 0 && characterID != 0 {
			out = append(out, simulation.DivisionFrames{DivisionID: key.division, OnlyCharacterID: characterID, Frames: frames})
		}
	}
	return out
}

/*
================
advancePet
================
*/
func (rt *Runtime) advancePet(key petOwnerKey, nowMs int64) (output []simulation.Frame) {
	rt.petMu.Lock()
	state := rt.petSessions[key]
	rt.petMu.Unlock()
	if state == nil {
		return nil
	}
	generation := state.generation
	defer func() {
		if state.pickup != nil && (state.follower == nil || generation != state.generation) {
			state.pickup = nil
			for _, f := range failureResult(wire.ErrCodeInvalidRequest).Frames {
				output = append(output, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
			}
		}
	}()
	// The session binds the canonical character pointer. Avoid searching the
	// division's entire saved-character population on every pet tick.
	var snapshot *enterworld.Character
	rt.deps.Read(key.division, func() {
		if state.character.ActiveCOS != nil {
			snapshot = state.character.Snapshot()
		}
	})
	if snapshot == nil {
		state.follower = nil
		return nil
	}
	cos := snapshot.ActiveCOS
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok || snapshot.DeletePending || enterworld.CurrentHP(snapshot) == 0 || cos == nil || !cos.Summoned || cos.Mounted || cos.CurrentHP == 0 {
		if state.follower == nil {
			return nil
		}
		frames := state.follower.Stop(nowMs)
		state.follower = nil
		return frames
	}
	ref, found := refs.CharacterRefByCodename(cos.Codename)
	gid, valid := enterworld.CosObjectIDForCharacter(snapshot)
	if !found || ref == nil || !valid || gid != cos.GID || ref.RefObjID != cos.RefObjID || ref.TidWord&0x7fe != 0x1c6 || ref.TidWord>>11 < 3 || ref.TidWord>>11 > 4 {
		state.follower = nil
		return nil
	}
	owner := rt.liveSpawn(simulation.WorldKey(key.division, snapshot.Name), snapshot, nowMs)
	if state.follower == nil || state.follower.GID() != cos.GID || state.refObjID != cos.RefObjID {
		state.follower = simulation.NewPetFollower(cos.GID, owner)
		state.generation++
		state.refObjID = cos.RefObjID
	}
	if rt.cosMovementBlocked(key.division, snapshot) {
		return state.follower.Stop(nowMs)
	}
	block := rt.cosAbnormal(key.division, snapshot.Name, cos.GID)
	walk, run := cosParameter(ref, block, movementWalkParameter), cosParameter(ref, block, movementRunParameter)
	state.follower.SetMovementSpeeds(walk, run, nowMs)
	var constraint func(simulation.Spawn, simulation.Spawn) (simulation.Spawn, *simulation.MoveError)
	if rt.ConstrainMovement != nil {
		constraint = func(from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
			return rt.ConstrainMovement(snapshot.Name, from, to)
		}
	}
	if state.pickup != nil {
		q := *state.pickup
		item, found := rt.characterGround(key.division, snapshot, q.GroundGID)
		if !found || nowMs >= state.pickupDeadline {
			state.pickup = nil
			frames := state.follower.Stop(nowMs)
			for _, f := range failureResult(wire.ErrCodeInvalidRequest).Frames {
				frames = append(frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
			}
			return frames
		}
		pose := state.follower.Position(nowMs)
		target := simulation.Spawn{RegionID: item.Position.RegionID, X: float64(item.Position.X), Y: float64(item.Y), Z: float64(item.Position.Z)}
		if simulation.WorldDistance2D(pose, target) > grounditem.ExecuteRange {
			return state.follower.Approach(target, float64(run), nowMs, grounditem.ExecuteRange, constraint)
		}
		state.pickup = nil
		frames := state.follower.Stop(nowMs)
		result := rt.applyCosGroundAt(key.division, state.character, q, time.UnixMilli(nowMs), false)
		state.public = append(state.public, result.Broadcast...)
		for _, f := range result.Frames {
			frames = append(frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
		}
		return frames
	}
	return state.follower.Advance(owner, float64(run), nowMs, constraint)
}

// PetPresentation is the sole read boundary for peer pet motion. It holds the
// same division lock as advancement and returns only detached public fields.
/*
================
PetPresentation
================
*/
func (rt *Runtime) PetPresentation(division, name string) *simulation.PeerCOS {
	unlock := rt.lockDivision(division)
	defer unlock()
	rt.petMu.Lock()
	state := rt.petSessions[petOwnerKey{division, strings.ToLower(name)}]
	rt.petMu.Unlock()
	if state == nil {
		return nil
	}
	var result *simulation.PeerCOS
	rt.deps.Read(division, func() {
		c := state.character
		cos := c.ActiveCOS
		if cos == nil || !cos.Summoned {
			return
		}
		refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
		if !ok {
			return
		}
		ref, ok := refs.CharacterRefByCodename(cos.Codename)
		gid, valid := enterworld.CosObjectIDForCharacter(c)
		if !ok || ref == nil || !valid || gid != cos.GID || ref.RefObjID != cos.RefObjID || ref.TidWord&0x7fe != 0x1c6 || ref.TidWord>>11 < 1 || ref.TidWord>>11 > 4 {
			return
		}
		var world simulation.WorldState
		var revision uint64
		// A rideable COS stands where it was summoned until ridden, then moves
		// with its rider; pets (3, 4) follow their own AI.
		if rideableCOSBand(ref.TidWord>>11) || cos.Mounted {
			if state.transportCOS != cos {
				state.transportCOS = cos
				state.transportWorld = simulation.WorldState{Spawn: rt.liveSpawn(simulation.WorldKey(division, c.Name), c, rt.Now().UnixMilli())}
				state.generation++
			}
			world = state.transportWorld
			if cos.Mounted {
				world = rt.Worlds.Snapshot(simulation.WorldKey(division, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) })
			}
		} else {
			if state.follower == nil || cos.GID != state.follower.GID() || cos.RefObjID != state.refObjID {
				return
			}
			world, revision = state.follower.Presentation()
		}
		name := cos.Name
		if name == "" {
			name = ref.Name
		}
		block := rt.cosAbnormal(division, c.Name, cos.GID)
		result = &simulation.PeerCOS{Mounted: cos.Mounted, NativeBodyStatus: cos.NativeBodyStatus, World: world, Revision: revision, Session: state.session, Generation: state.generation,
			Row: wire.CosSpawnBand2{Band: uint8(ref.TidWord >> 11), RefObjID: cos.RefObjID, Gid: cos.GID,
				Walk: cosParameter(ref, block, movementWalkParameter), Run: cosParameter(ref, block, movementRunParameter),
				Scale: ref.Scale, Name: name, OwnerName: c.Name, OwnerGid: enterworld.ObjectIDForCharacter(c)}}
		if block != nil && block.Mask != 0 {
			result.AbnormalVitals = abnormalVitalsPayload(cos.GID, block)
		}
		if cos.CurrentHP == 0 {
			result.LifeState = wire.LifeStateDead
		}
	})
	return result
}

// Called inside the summon transaction, with the division operation lock held.
/*
================
rememberTransportCOS
================
*/
func (rt *Runtime) rememberTransportCOS(division string, c *enterworld.Character, pose simulation.Spawn) {
	rt.petMu.Lock()
	defer rt.petMu.Unlock()
	if state := rt.petSessions[petOwnerKey{division, strings.ToLower(c.Name)}]; state != nil {
		state.transportCOS = c.ActiveCOS
		state.transportWorld = simulation.WorldState{Spawn: pose}
		state.generation++
	}
}

// Successful owner re-entry retires motion/pickup from the departed world.
// The persisted pet identity and inventory survive; the new projection starts
// at the same admitted destination as its owner, before peer publication.
/*
================
relocateReturningPet
================
*/
func (rt *Runtime) relocateReturningPet(division string, c *enterworld.Character, destination simulation.Spawn) {
	rt.petMu.Lock()
	defer rt.petMu.Unlock()
	state := rt.petSessions[petOwnerKey{division, strings.ToLower(c.Name)}]
	if state == nil || state.character != c {
		return
	}
	state.pickup = nil
	state.public = nil
	state.generation++
	state.follower = nil
	if cos := c.ActiveCOS; cos != nil && cos.Summoned {
		state.follower = simulation.NewPetFollower(cos.GID, destination)
		state.refObjID = cos.RefObjID
	}
}
