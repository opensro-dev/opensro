/*
===========================================================================

petjudged.go - the pet follow tick on door-read facts

advancePet's fast path for an admitted follow tick (#2139): the follower
is already bound and nothing must commit through the owner record, so
the tick finishes on facts read inside the character door
(readPetTickFacts) and the owner world WorldStore.SnapshotExistingInto
refills in the session's retained slot, never a copy of the owner.
Every other tick abstains to advancePet's copy path, which owns the
bind, battles, roars, pickups and refusals.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
petSession.judgedCosAbnormalKey

The session's folded COS abnormal map key for one (division, name,
gid) binding, folded on first use and again only when the binding
changes: the judged tick weighs this identity twice a tick and it is
fixed for the summoned pet's life (#2033).
================
*/
func (state *petSession) judgedCosAbnormalKey(division, name string, gid uint32) cosAbnormalKey {
	binding := petOwnerKey{division: division, name: name, gid: gid}
	if !state.judgedCosKeySet || state.judgedCosKeyFrom != binding {
		state.judgedCosKey = cosAbnormalKeyFor(division, name, gid)
		state.judgedCosKeyFrom = binding
		state.judgedCosKeySet = true
	}
	return state.judgedCosKey
}

/*
================
advancePetJudged

advancePet on door-read facts (readPetTickFacts), for the admitted
ticks that finish without the owner record (#2139): the follower is
already bound, no battle, roar or pickup is live, and the tick is the
follow - its early stops included - in advancePet's own order. The
owner's world comes from WorldStore.SnapshotExistingInto, refilled
into the session's retained slot and consumed inside this tick -
seeding it would read the live record outside the character door,
so a tick whose owner has no world entry yet abstains with everything else
that must commit through the record - the bind, a battle, a roar, a
pickup, a mercenary, a relocation, the refusals - and advancePet
judges it on the companion copy exactly as before. The stuck answer stays lazy the
same way: a stuck event raised by the judged step takes the copy
only to answer it. The answer itself is built once per session and
reads the tick's cells, so installing it allocates nothing.
================
*/
func (rt *Runtime) advancePetJudged(key petOwnerKey, state *petSession, facts *petTickFacts, nowMs int64, following *bool, answerStuck *func() []simulation.Frame) ([]simulation.Frame, bool) {
	// The forced-copy seam and any live battle state judge on the copy.
	if rt.companionCopy != nil || state.combat != nil || state.roar != nil || state.pickup != nil {
		return nil, false
	}
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return nil, false
	}
	// The pet view is the session's scratch record rebuilt whole from
	// the door-read facts: only the eight carried values are set, every
	// other field reads zero, and the abstain boundary above is what
	// keeps a tick that needs more than these on the copy. The view is
	// consumed inside this call - the formation step, mirror write
	// included - and rebuilt on the next judged tick.
	pet := &state.judgedPet
	*pet = enterworld.CharacterCOS{
		GID: facts.petGID, RefObjID: facts.petRefObjID, Codename: facts.petCodename,
		BroodStage: facts.petBroodStage, Satiety: facts.petSatiety,
		MercenaryAttributes: facts.petMercenaryAttributes,
		FollowRunFactor:     facts.petFollowRunFactor, FollowRunSet: facts.petFollowRunSet,
	}
	ref, found := refs.CharacterRefByCodename(pet.Codename)
	if !found || ref == nil || ref.RefObjID != pet.RefObjID || ref.TidWord&0x7fe != 0x1c6 || !rt.companionFollows(ref.TidWord) {
		return nil, false
	}
	if ref.TidWord>>11 == domain.MercenaryBand {
		return nil, false
	}
	if state.mover.AnyFollower() == nil || state.mover.AnyFollower().GID() != key.gid || state.refObjID != pet.RefObjID {
		return nil, false
	}
	ownerWorld := &state.judgedOwnerWorld
	if !rt.Worlds.SnapshotExistingInto(simulation.WorldKey(key.division, state.character.Name), ownerWorld) {
		return nil, false
	}
	// Relocation and summons build new followers; every tick installs the one
	// planner they all route with.
	state.mover.AnyFollower().SetRoutePlanner(rt.PlanCompanionRoute)
	if pet.BroodStage == 1 {
		rt.cancelPetCombat(key, state, nowMs)
		return state.mover.AnyFollower().Stop(nowMs), true
	}
	if rt.TransportFollowTether && isVehicleCOS(ref.TidWord) {
		// Port-only (transportfollow.go): a following transport holds
		// its trader as a parked one does, from where it walks now.
		state.tether = &simulation.Tether{Anchor: state.mover.AnyFollower().Position(nowMs),
			Range: simulation.TradeTransportTetherRange, Reason: simulation.TetherTradeTransport}
	}
	// The stuck answer is the session's own, built once: refresh the
	// cells it reads, then install it. advancePet's deferred call runs
	// it after this tick returns, exactly as the per-tick closure ran.
	state.judgedStuckNowMs, state.judgedStuckTidWord = nowMs, ref.TidWord
	if state.judgedStuckAnswer == nil {
		state.judgedStuckAnswer = func() []simulation.Frame {
			if state.mover.AnyFollower() == nil || !state.mover.AnyFollower().StuckPending() {
				return nil
			}
			snapshot := rt.characterSnapshotCompanionAdmitted(key.division, state.character, nil)
			if snapshot == nil {
				return nil
			}
			cos := snapshot.CompanionByGID(key.gid)
			if cos == nil {
				return nil
			}
			return rt.answerPetStuck(key, state, snapshot, cos, state.judgedStuckTidWord, state.judgedStuckNowMs)
		}
	}
	*answerStuck = state.judgedStuckAnswer
	if state.displacement != nil {
		state.roar = nil
		if nowMs < state.displacement.untilMs {
			return state.mover.AnyFollower().Stop(nowMs), true
		}
		state.displacement = nil
	}
	if rt.cosMovementBlockedKeyed(state.judgedCosAbnormalKey(key.division, state.character.Name, key.gid)) {
		state.roar = nil
		rt.cancelPetCombat(key, state, nowMs)
		return state.mover.AnyFollower().Stop(nowMs), true
	}
	block := rt.cosAbnormalKeyed(state.judgedCosAbnormalKey(key.division, state.character.Name, pet.GID))
	// The step keeps the native run: natively it is compared with the authored
	// speed. Only the follower moves at the paced one.
	walk, run := cosParameter(ref, pet, block, movementWalkParameter), cosParameter(ref, pet, block, movementRunParameter)
	state.mover.AnyFollower().SetMovementSpeeds(walk, rt.cosPacedRun(ref, run), nowMs)
	var constraint func(simulation.Spawn, simulation.Spawn) (simulation.Spawn, *simulation.MoveError)
	if rt.ConstrainMovement != nil {
		if state.judgedConstraint == nil {
			state.judgedConstraint = func(from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
				return rt.ConstrainMovement(state.character.Name, from, to)
			}
		}
		constraint = state.judgedConstraint
	}
	if nowMs < state.wolf.howlHoldUntil {
		return state.mover.AnyFollower().Stop(nowMs), true
	}
	// A follower whose region the owner's live region cannot be walked
	// to relocates inside the formation step, and the step publishes
	// the respawn from the pet record (publishPetRelocation: name,
	// body status, life, level). The facts carry none of those, so the
	// tick abstains on exactly the gate the step relocates on and the
	// copy publishes the respawn as before.
	if !monster.FollowLocationCompatible(state.mover.AnyFollower().Position(nowMs).RegionID, ownerWorld.LiveSpawnAt(nowMs).RegionID) {
		return nil, false
	}
	if ref.TidWord>>11 >= 3 && ref.TidWord>>11 <= domain.MercenaryBand {
		*following = true
		return rt.advanceOwnerFormation(petCombatStep{key: key, state: state, snapshot: state.character, ownerWorld: ownerWorld, pet: pet, ref: ref, run: run, constraint: constraint, nowMs: nowMs}), true
	}
	return state.mover.AnyFollower().Advance(ownerWorld.LiveSpawnAt(nowMs), float64(rt.cosPacedRun(ref, run)), nowMs, constraint), true
}
