/*
===========================================================================

petai.go - session-owned summoned-pet movement, pickup and peer presentation

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/grounditem"
	"sort"
	"strings"
	"time"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
petOwnerKey
================
*/
type petOwnerKey struct {
	division, name string
	gid            uint32
}

/*
================
petSession
================
*/
type petSession struct {
	formationBattle  bool
	formationActive  bool
	relocatedAtMs    int64
	formationSlots   monster.ApproachSlots
	formationSlot    int
	formationTimers  *monster.AITimeManager
	displacement     *playerDisplacement
	mercenaryPenalty mercenaryPenaltyClock
	satiety          companion.SatietyClock
	session          uint64
	ready            bool
	character        *enterworld.Character
	refObjID         uint32
	follower         *simulation.PetFollower
	// Transport COS do not run follower AI. Their admission anchor belongs to
	// this same presentation owner; mounted motion comes from the rider owner.
	transportCOS   *enterworld.CharacterCOS
	transportWorld simulation.WorldState
	// tether is the parked trade transport's hold on its owner this tick
	// (simulation/tether.go); nil for every other companion state.
	tether     *simulation.Tether
	generation uint64
	// summonedAtMs marks a pet just called out of its item (not restored at
	// entry): its first publication carries spawn sub-state 1.
	summonedAtMs   int64
	pickup         *wire.ItemMoveRequest
	pickupDeadline int64
	pickupCommand  bool
	public         []wire.Frame
	// combat is the attack pet's BATTLE state (petcombat.go); nil follows.
	combat *petCombatIntent
	// others holds kill-settlement frames for other characters (a party's
	// shared experience), drained with public by advancePets.
	others []RecipientFrames
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
	rt.bindPetSession(division, c, session, true)
}

/*
================
bindPetSession

Admission already owns the division lock. Bind before bootstrap so its
object list and subsequent peer publication use the same actor positions.
================
*/
func (rt *Runtime) bindPetSession(division string, c *enterworld.Character, session uint64, ready bool) {
	key := petOwnerKey{division: division, name: strings.ToLower(c.Name)}
	rt.petMu.Lock()
	old := rt.petSessions[key]
	if old != nil && old.session == session && old.character == c {
		if ready {
			for childKey, child := range rt.petSessions {
				if childKey.division == division && childKey.name == key.name {
					child.ready = true
				}
			}
		}
		rt.petMu.Unlock()
		return
	}
	if rt.petSessions == nil {
		rt.petSessions = make(map[petOwnerKey]*petSession)
	}
	state := &petSession{session: session, character: c, ready: ready, mercenaryPenalty: mercenaryPenaltyClock{lastMs: rt.Now().UnixMilli()}}
	rt.petSessions[key] = state
	rt.petMu.Unlock()
	var companions []*enterworld.CharacterCOS
	rt.deps.Read(division, func() { companions = c.Companions() })
	for _, cos := range companions {
		if cos.Summoned {
			rt.bindCompanionSession(division, state, cos)
		}
	}
}

// Called with the division lock held; the map lock never spans a character
// authority door, movement constraint, or packet publication.
/*
================
forgetPetSession
================
*/
func (rt *Runtime) forgetPetSession(division, name string) {
	retired := make(map[petOwnerKey]*petSession)
	rt.petMu.Lock()
	for key, state := range rt.petSessions {
		if key.division == division && key.name == strings.ToLower(name) {
			retired[key] = state
			delete(rt.petSessions, key)
		}
	}
	rt.petMu.Unlock()
	for key, state := range retired {
		rt.cancelPetCombat(key, state, rt.Now().UnixMilli())
	}
}

/*
================
advancePets
================
*/
func (rt *Runtime) advancePets(nowMs int64) []simulation.DivisionFrames {
	rt.petMu.Lock()
	keys := make([]petOwnerKey, 0, len(rt.petSessions))
	for key, state := range rt.petSessions {
		if key.gid != 0 && state.ready {
			keys = append(keys, key)
		}
	}
	rt.petMu.Unlock()
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].division != keys[j].division {
			return keys[i].division < keys[j].division
		}
		if keys[i].name != keys[j].name {
			return keys[i].name < keys[j].name
		}
		return keys[i].gid < keys[j].gid
	})
	var out []simulation.DivisionFrames
	var tethers map[string]simulation.Tether
	for _, key := range keys {
		unlock := rt.lockDivision(key.division)
		frames := rt.advancePetSatiety(key, nowMs)
		frames = append(frames, rt.advancePet(key, nowMs)...)
		rt.petMu.Lock()
		state := rt.petSessions[key]
		rt.petMu.Unlock()
		characterID := int64(0)
		if state != nil {
			characterID = state.character.ID
		}
		if state != nil && state.tether != nil {
			if tethers == nil {
				tethers = make(map[string]simulation.Tether)
			}
			tethers[simulation.WorldKey(key.division, state.character.Name)] = *state.tether
		}
		var public []wire.Frame
		var others []RecipientFrames
		if state != nil {
			public, others = state.public, state.others
			state.public, state.others = nil, nil
		}
		unlock()
		for _, other := range others {
			out = append(out, simulation.DivisionFrames{DivisionID: key.division, OnlyCharacterID: other.CharacterID, Frames: simFrames(other.Frames)})
		}
		if len(public) > 0 && rt.PushDivisionPeerFrames != nil {
			rt.PushDivisionPeerFrames(key.division, key.name, public)
		}
		if len(frames) != 0 && characterID != 0 {
			out = append(out, simulation.DivisionFrames{DivisionID: key.division, OnlyCharacterID: characterID, Frames: frames})
		}
	}
	if rt.Worlds != nil {
		rt.Worlds.ReplaceTethers(tethers)
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
	state.tether = nil
	following := false
	defer func() {
		if !following {
			rt.releasePetFormation(key, state)
		}
	}()
	generation := state.generation
	defer func() {
		if state.pickup != nil && (state.follower == nil || generation != state.generation || nowMs >= state.pickupDeadline) {
			result := finishPendingCosPickup(state, failureResult(wire.ErrCodeInvalidRequest))
			for _, f := range result.Frames {
				output = append(output, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
			}
		}
	}()
	// The session binds the canonical character pointer. Avoid searching the
	// division's entire saved-character population on every pet tick.
	var snapshot *enterworld.Character
	rt.deps.Read(key.division, func() {
		if state.character.CompanionByGID(key.gid) != nil {
			snapshot = state.character.Snapshot()
		}
	})
	if snapshot == nil {
		rt.cancelPetCombat(key, state, nowMs)
		state.follower = nil
		return nil
	}
	cos := snapshot.CompanionByGID(key.gid)
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok || snapshot.DeletePending || enterworld.CurrentHP(snapshot) == 0 || cos == nil || !cos.Summoned || cos.Mounted || cos.CurrentHP == 0 {
		// An unsummoned, dead or mounted pet leaves BATTLE; a later summon
		// must not resume an old fight.
		rt.cancelPetCombat(key, state, nowMs)
		if state.follower == nil {
			return nil
		}
		frames := state.follower.Stop(nowMs)
		state.follower = nil
		return frames
	}
	ref, found := refs.CharacterRefByCodename(cos.Codename)
	if !found || ref == nil || ref.RefObjID != cos.RefObjID || ref.TidWord&0x7fe != 0x1c6 || !followingCOSBand(ref.TidWord>>11) {
		state.follower = nil
		if found && ref != nil && ref.RefObjID == cos.RefObjID && isVehicleCOS(ref.TidWord) {
			// 4FD720: the first primary actor is a vehicle (CGObj_IsVehicleCOS
			// 4827F0, the band-2 trade transport), so it holds its owner.
			state.tether = &simulation.Tether{Anchor: rt.companionLiveSpawn(key.division, snapshot, cos, nowMs),
				Range: simulation.TradeTransportTetherRange, Reason: simulation.TetherTradeTransport}
		}
		return nil
	}
	owner := rt.liveSpawn(simulation.WorldKey(key.division, snapshot.Name), snapshot, nowMs)
	if state.follower == nil || state.follower.GID() != cos.GID || state.refObjID != cos.RefObjID {
		state.follower = simulation.NewPetFollower(cos.GID, owner)
		state.generation++
		state.refObjID = cos.RefObjID
	}
	// Relocation and summons build new followers; every tick installs the one
	// planner they all route with.
	state.follower.SetRoutePlanner(rt.PlanCompanionRoute)
	if state.displacement != nil {
		if nowMs < state.displacement.untilMs {
			return state.follower.Stop(nowMs)
		}
		state.displacement = nil
	}
	if rt.companionMovementBlocked(key.division, snapshot, cos) {
		rt.cancelPetCombat(key, state, nowMs)
		return state.follower.Stop(nowMs)
	}
	block := rt.cosAbnormal(key.division, snapshot.Name, cos.GID)
	// The step keeps the native run: 548A30 compares it with the authored
	// speed. Only the follower moves at the paced one.
	walk, run := cosParameter(ref, cos, block, movementWalkParameter), cosParameter(ref, cos, block, movementRunParameter)
	state.follower.SetMovementSpeeds(walk, rt.cosPacedRun(ref, run), nowMs)
	var constraint func(simulation.Spawn, simulation.Spawn) (simulation.Spawn, *simulation.MoveError)
	if rt.ConstrainMovement != nil {
		constraint = func(from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
			return rt.ConstrainMovement(snapshot.Name, from, to)
		}
	}
	if frames, handled := rt.advancePetCombat(petCombatStep{key: key, state: state, snapshot: snapshot,
		pet: cos, ref: ref, run: run, constraint: constraint, nowMs: nowMs}); handled {
		return frames
	}
	if state.pickup != nil {
		q := *state.pickup
		item, found := rt.characterGround(key.division, snapshot, q.GroundGID)
		if !found || nowMs >= state.pickupDeadline {
			result := finishPendingCosPickup(state, failureResult(wire.ErrCodeInvalidRequest))
			frames := state.follower.Stop(nowMs)
			for _, f := range result.Frames {
				frames = append(frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
			}
			return frames
		}
		pose := state.follower.Position(nowMs)
		target := simulation.Spawn{RegionID: item.Position.RegionID, X: float64(item.Position.X), Y: float64(item.Y), Z: float64(item.Position.Z)}
		if simulation.WorldDistance2D(pose, target) > grounditem.ExecuteRange {
			return state.follower.Approach(target, float64(rt.cosPacedRun(ref, run)), nowMs, grounditem.ExecuteRange, constraint)
		}
		frames := state.follower.Stop(nowMs)
		result := rt.applyCosGroundAt(key.division, state.character, q, cosGroundAttempt{now: time.UnixMilli(nowMs)})
		result = finishPendingCosPickup(state, result)
		state.public = append(state.public, result.Broadcast...)
		for _, f := range result.Frames {
			frames = append(frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
		}
		return frames
	}
	if ref.TidWord>>11 >= 3 && ref.TidWord>>11 <= domain.MercenaryBand {
		following = true
		return rt.advanceOwnerFormation(petCombatStep{key: key, state: state, snapshot: snapshot, pet: cos, ref: ref, run: run, constraint: constraint, nowMs: nowMs})
	}
	return state.follower.Advance(owner, float64(rt.cosPacedRun(ref, run)), nowMs, constraint)
}

// PetPresentation is the sole read boundary for peer pet motion. It holds the
// same division lock as advancement and returns only detached public fields.
/*
================
PetPresentation
================
*/
func (rt *Runtime) PetPresentation(division, name string) *simulation.PeerCOS {
	pets := rt.CompanionPresentations(division, name)
	if len(pets) == 0 {
		return nil
	}
	return pets[0]
}

/*
================
CompanionPresentations

Copy each companion under the division and character read doors. Every pet
supplies its own world position to peer visibility, including parked mounts.
================
*/
func (rt *Runtime) CompanionPresentations(division, name string) []*simulation.PeerCOS {
	unlock := rt.lockDivision(division)
	defer unlock()
	return rt.companionPresentations(division, name)
}

/*
================
companionPresentations

CompanionPresentations for a caller that already holds the division lock:
it takes the character read door once.
================
*/
func (rt *Runtime) companionPresentations(division, name string) []*simulation.PeerCOS {
	var result []*simulation.PeerCOS
	var guildID int64
	rt.deps.Read(division, func() {
		result, guildID = rt.companionPresentationsInDoor(division, name)
	})
	// Guild() takes the same store read lock as Read. Resolve its detached
	// name only after releasing the character door, including peer snapshots.
	guildName := ""
	if guildID != 0 {
		if store := rt.deps.GuildAuthority(); store != nil {
			if guild, _, found := store.Guild(division, guildID); found {
				guildName = guild.Name
			}
		}
	}
	for _, pet := range result {
		if pet.Row.Band == domain.MercenaryBand {
			pet.Row.OwnerName = guildName
		}
	}
	return result
}

/*
================
companionPresentationsInDoor

The presentations for a caller that already holds the character read door
(HandleObjectSelect's resolveLiveObject). It must take no door itself: the
store's read lock is a sync.RWMutex, and a second RLock queued behind a
waiting writer never returns while the first is held (the 2026-10-10
GameWorld deadlock: an object select with a summoned pet against the tick's
UpdateCharacter). The copied guild id lets publication resolve the guild
name outside this door; selection needs only the pose and life state.
================
*/
func (rt *Runtime) companionPresentationsInDoor(division, name string) ([]*simulation.PeerCOS, int64) {
	rt.petMu.Lock()
	owner := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(name)}]
	rt.petMu.Unlock()
	if owner == nil || !owner.ready {
		return nil, 0
	}
	var guildID int64
	if owner.character.GuildID != nil {
		guildID = *owner.character.GuildID
	}
	var result []*simulation.PeerCOS
	for _, pet := range owner.character.Companions() {
		if !pet.Summoned {
			continue
		}
		rt.petMu.Lock()
		state := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(name), gid: pet.GID}]
		rt.petMu.Unlock()
		if state == nil {
			continue
		}
		if projection := rt.companionPresentation(division, state, pet); projection != nil {
			if projection.Row.Band == domain.MercenaryBand {
				projection.Row.OwnerName = ""
				projection.Row.HoldType = enterworld.DressedJob(owner.character)
				projection.Row.PvpState = owner.character.PVPState()
			}
			result = append(result, projection)
		}
	}
	return result, guildID
}

/*
================
companionPresentation
================
*/
func (rt *Runtime) companionPresentation(division string, state *petSession, cos *enterworld.CharacterCOS) *simulation.PeerCOS {
	c := state.character
	now := rt.Now().UnixMilli()
	var result *simulation.PeerCOS
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return nil
	}
	ref, ok := refs.CharacterRefByCodename(cos.Codename)
	if !ok || ref == nil || ref.RefObjID != cos.RefObjID || ref.TidWord&0x7fe != 0x1c6 || ref.TidWord>>11 < 1 || ref.TidWord>>11 > 5 && ref.TidWord>>11 != domain.CapturedCOSBand {
		return nil
	}
	var world simulation.WorldState
	var revision uint64
	if ref.TidWord>>11 <= 2 || cos.Mounted {
		if state.transportCOS != cos {
			state.transportCOS = cos
			state.transportWorld = simulation.WorldState{Spawn: rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)}
			state.generation++
		}
		world = state.transportWorld
		if cos.Mounted {
			world = rt.Worlds.Snapshot(simulation.WorldKey(division, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) })
		}
	} else {
		if state.follower == nil || cos.GID != state.follower.GID() || cos.RefObjID != state.refObjID {
			return nil
		}
		world, revision = state.follower.Presentation()
	}
	name := cos.Name
	if name == "" {
		name = ref.Name
	}
	block := rt.cosAbnormal(division, c.Name, cos.GID)
	walk, run := rt.cosMovementSpeeds(ref, cos, block)
	result = &simulation.PeerCOS{Mounted: cos.Mounted, NativeBodyStatus: cos.NativeBodyStatus, World: world, Revision: revision, Session: state.session, Generation: state.generation,
		Fresh: state.summonedAtMs != 0 && now-state.summonedAtMs <= petAppearWindowMs,
		Row: wire.CosSpawnBand2{Band: uint8(ref.TidWord >> 11), RefObjID: cos.RefObjID, Gid: cos.GID,
			Walk: walk, Run: run,
			Scale: cosParameter(ref, cos, block, actionSpeedParameter), Name: name, OwnerName: c.Name, OwnerModelRef: enterworld.CharacterModelRef(c, nil), OwnerGid: enterworld.ObjectIDForCharacter(c)}}
	if state.relocatedAtMs != 0 && now-state.relocatedAtMs <= petAppearWindowMs {
		result.Row.State = 7
	}
	if block != nil && block.Mask != 0 {
		result.AbnormalVitals = abnormalVitalsPayload(cos.GID, block)
	}
	if cos.CurrentHP == 0 {
		result.LifeState = wire.LifeStateDead
	}
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
	owner := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(c.Name)}]
	rt.petMu.Unlock()
	if owner == nil || c.ActiveCOS == nil {
		return
	}
	state := rt.bindCompanionSession(division, owner, c.ActiveCOS)
	state.transportCOS = c.ActiveCOS
	state.transportWorld = simulation.WorldState{Spawn: pose}
	state.generation++
	// 4FA861: a transport summoned with goods aboard is a caravan.
	rt.registerCaravan(division, c)
}

/*
================
bindCompanionSession

The division lock serializes admission. Transport replacement retires its
own state without touching the owner's independent attack and pickup pets.
================
*/
func (rt *Runtime) bindCompanionSession(division string, owner *petSession, pet *enterworld.CharacterCOS) *petSession {
	key := petOwnerKey{division: division, name: strings.ToLower(owner.character.Name), gid: pet.GID}
	rt.petMu.Lock()
	old := rt.petSessions[key]
	if old != nil && old.character == owner.character && old.transportCOS == pet {
		old.session = owner.session
		old.ready = owner.ready
		rt.petMu.Unlock()
		return old
	}
	owner.formationSlots.Release(pet.GID)
	state := &petSession{formationSlot: -1, session: owner.session, ready: owner.ready, character: owner.character, transportCOS: pet, refObjID: pet.RefObjID}
	if old != nil {
		state.generation = old.generation + 1
	}
	rt.petSessions[key] = state
	rt.petMu.Unlock()
	pose := rt.liveSpawn(simulation.WorldKey(division, owner.character.Name), owner.character, rt.Now().UnixMilli())
	pose = rt.companionAdmissionSpawn(pet, pose)
	state.transportWorld = simulation.WorldState{Spawn: pose}
	state.follower = simulation.NewPetFollower(pet.GID, pose)
	return state
}

// Successful owner re-entry retires motion/pickup from the departed world.
// The persisted pet identity and inventory survive; the new projection starts
// around its owner's admitted destination, before bootstrap publication.
/*
================
relocateReturningPet
================
*/
func (rt *Runtime) relocateReturningPet(division string, c *enterworld.Character, destination simulation.Spawn) map[petOwnerKey]petSession {
	rt.petMu.Lock()
	previous := make(map[petOwnerKey]petSession)
	ownerKey := petOwnerKey{division: division, name: strings.ToLower(c.Name)}
	if owner := rt.petSessions[ownerKey]; owner != nil {
		previous[ownerKey] = *owner
	}
	keys := make([]petOwnerKey, 0)
	for key, state := range rt.petSessions {
		if key.division != division || key.name != strings.ToLower(c.Name) || key.gid == 0 || state.character != c {
			continue
		}
		previous[key] = *state
		keys = append(keys, key)
	}
	rt.petMu.Unlock()
	sort.Slice(keys, func(i, j int) bool { return keys[i].gid < keys[j].gid })
	for _, key := range keys {
		rt.petMu.Lock()
		state := rt.petSessions[key]
		rt.petMu.Unlock()
		rt.cancelPetCombat(key, state, rt.Now().UnixMilli())
		rt.releasePetFormation(key, state)
		expireCosPickup(state)
		state.public = nil
		state.generation++
		state.follower = nil
		if cos := c.CompanionByGID(key.gid); cos != nil {
			pose := rt.companionAdmissionSpawn(cos, destination)
			state.follower = simulation.NewPetFollower(cos.GID, pose)
			state.refObjID = cos.RefObjID
			state.transportWorld = simulation.WorldState{Spawn: pose}
		}
	}
	return previous
}

/*
================
restoreCompanionRelocation

Projection can refuse after movement preparation. The division lock keeps
the old followers quiescent, so restoring them also restores pending pickup
and generation identity without replaying or cancelling an unrelated job.
================
*/
func (rt *Runtime) restoreCompanionRelocation(previous map[petOwnerKey]petSession) {
	rt.petMu.Lock()
	defer rt.petMu.Unlock()
	for key, state := range previous {
		if current := rt.petSessions[key]; current != nil {
			*current = state
		}
	}
}

/*
================
followingCOSBand

Pets (3, 4) and a captured quest monster (6) follow their owner on foot;
transports (1, 2) move only with their rider. A parked trade transport
instead holds its trader within range (advancePet's tether).
================
*/
func followingCOSBand(band uint16) bool {
	return band == 3 || band == 4 || band == domain.MercenaryBand || band == domain.CapturedCOSBand
}

/*
================
CompanionTargets

The monster tick's companion targets for one owner: every summoned,
living, unmounted companion in container order (5464E0), at its presented
pose. A mounted ride is struck through its rider (monsterAttackStage).
================
*/
func (rt *Runtime) CompanionTargets(division string, ownerGID uint32, nowMs int64) []simulation.CompanionTarget {
	unlock := rt.lockDivision(division)
	defer unlock()
	return rt.companionTargets(division, ownerGID, nowMs)
}

/*
================
companionTargets

CompanionTargets for a caller that already holds the division lock (a
monster area gathering its candidates at release).
================
*/
func (rt *Runtime) companionTargets(division string, ownerGID uint32, nowMs int64) []simulation.CompanionTarget {
	owner := rt.findCharacterByGid(division, ownerGID)
	if owner == nil {
		return nil
	}
	var out []simulation.CompanionTarget
	for _, pet := range rt.companionPresentations(division, owner.Name) {
		// 5299E0 is never hostile to a grab pet (CGObj_IsPickPetCOS, slot 0x43C).
		if pet.Mounted || pet.LifeState == wire.LifeStateDead || pet.Row.Band == domain.PickupPetBand {
			continue
		}
		var record enterworld.CharacterCOS
		rt.deps.Read(division, func() {
			if live := owner.CompanionByGID(pet.Row.Gid); live != nil {
				record = *live
			}
		})
		ref, found := rt.cosReference(&record)
		if !found {
			continue
		}
		out = append(out, simulation.CompanionTarget{Gid: pet.Row.Gid, Pose: pet.World.LiveSpawnAt(nowMs),
			BodyRadius: simulation.BodyRadius(ref.Parameters.BodyRadius), NativeBodyStatus: pet.NativeBodyStatus, Band: pet.Row.Band})
	}
	return out
}
