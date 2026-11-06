/*
===========================================================================

petjudged.go - the pet follow tick on door-read facts

advancePet's fast path for an admitted follow tick (#2139): the follower
is already bound and nothing must commit through the owner record, so
the tick finishes on facts read inside the character door
(readPetTickFacts) and the owner world WorldStore.SnapshotExisting holds,
never a copy of the owner. Every other tick abstains to advancePet's
copy path, which owns the bind, battles, roars, pickups and refusals.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
advancePetJudged

advancePet on door-read facts (readPetTickFacts), for the admitted
ticks that finish without the owner record (#2139): the follower is
already bound, no battle, roar or pickup is live, and the tick is the
follow - its early stops included - in advancePet's own order. The
owner's world comes from WorldStore.SnapshotExisting: seeding it
would read the live record outside the character door, so a tick
whose owner has no world entry yet abstains with everything else
that must commit through the record - the bind, a battle, a roar, a
pickup, a mercenary, the refusals - and advancePet judges it on the
companion copy exactly as before. The stuck answer stays lazy the
same way: a stuck event raised by the judged step takes the copy
only to answer it.
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
	pet := &facts.pet
	ref, found := refs.CharacterRefByCodename(pet.Codename)
	if !found || ref == nil || ref.RefObjID != pet.RefObjID || ref.TidWord&0x7fe != 0x1c6 || !rt.companionFollows(ref.TidWord) {
		return nil, false
	}
	if ref.TidWord>>11 == domain.MercenaryBand {
		return nil, false
	}
	if state.follower == nil || state.follower.GID() != key.gid || state.refObjID != pet.RefObjID {
		return nil, false
	}
	ownerWorld, ok := rt.Worlds.SnapshotExisting(simulation.WorldKey(key.division, state.character.Name))
	if !ok {
		return nil, false
	}
	// Relocation and summons build new followers; every tick installs the one
	// planner they all route with.
	state.follower.SetRoutePlanner(rt.PlanCompanionRoute)
	if pet.BroodStage == 1 {
		rt.cancelPetCombat(key, state, nowMs)
		return state.follower.Stop(nowMs), true
	}
	if rt.TransportFollowTether && isVehicleCOS(ref.TidWord) {
		// Port-only (transportfollow.go): a following transport holds
		// its trader as a parked one does, from where it walks now.
		state.tether = &simulation.Tether{Anchor: state.follower.Position(nowMs),
			Range: simulation.TradeTransportTetherRange, Reason: simulation.TetherTradeTransport}
	}
	*answerStuck = func() []simulation.Frame {
		if state.follower == nil || !state.follower.StuckPending() {
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
		return rt.answerPetStuck(key, state, snapshot, cos, ref.TidWord, nowMs)
	}
	if state.displacement != nil {
		state.roar = nil
		if nowMs < state.displacement.untilMs {
			return state.follower.Stop(nowMs), true
		}
		state.displacement = nil
	}
	if rt.cosMovementBlockedFor(key.division, state.character.Name, key.gid) {
		state.roar = nil
		rt.cancelPetCombat(key, state, nowMs)
		return state.follower.Stop(nowMs), true
	}
	block := rt.cosAbnormal(key.division, state.character.Name, pet.GID)
	// The step keeps the native run: natively it is compared with the authored
	// speed. Only the follower moves at the paced one.
	walk, run := cosParameter(ref, pet, block, movementWalkParameter), cosParameter(ref, pet, block, movementRunParameter)
	state.follower.SetMovementSpeeds(walk, rt.cosPacedRun(ref, run), nowMs)
	var constraint func(simulation.Spawn, simulation.Spawn) (simulation.Spawn, *simulation.MoveError)
	if rt.ConstrainMovement != nil {
		constraint = func(from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
			return rt.ConstrainMovement(state.character.Name, from, to)
		}
	}
	if nowMs < state.wolf.howlHoldUntil {
		return state.follower.Stop(nowMs), true
	}
	if ref.TidWord>>11 >= 3 && ref.TidWord>>11 <= domain.MercenaryBand {
		*following = true
		return rt.advanceOwnerFormation(petCombatStep{key: key, state: state, snapshot: state.character, ownerWorld: &ownerWorld, pet: pet, ref: ref, run: run, constraint: constraint, nowMs: nowMs}), true
	}
	return state.follower.Advance(ownerWorld.LiveSpawnAt(nowMs), float64(rt.cosPacedRun(ref, run)), nowMs, constraint), true
}
