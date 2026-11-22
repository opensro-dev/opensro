/*
===========================================================================

petrecovery.go - a pet that cannot get where it is going is resolved, and counted

Port-only, not native; shipped as the default by the owner (2026-11-22,
"Ship it as the default": the AAA rule set for pets that cannot reach).
Native BATTLE never gives up a target: on its duration's expiry the
reference server's state returns without leaving it, and its stuck answer
keeps the fight (monster.NativeStuckResponse). A native pet
seldom met a wall it could not pass; the port's collision owner refuses
more, so a pet could stand at a wall for as long as its target lived.

The rules, one owner for all of them:

  - No way, no stare. The follower records whether its last move can
    reach its goal (simulation.PetFollower.Reach: the straight segment
    was cut short and no detour leads past). A battle approach that is
    blocked short of strike range ends the fight on that tick; a follow
    that is blocked short of the follow distance relocates the pet onto
    its owner.
  - The owner hears why. A fight the owner ordered (orderPetAttack) that
    ends this way is answered with the client's own refusal for an attack
    command: 0xB69E [2][selector 2][code 16][cosGid], which the client
    turns into "Cannot attack due to an obstacle." (client 0x74FDD9 dispatches
    selector 2 as category 4, client 0x74FDEB; category 4 code 16 is
    UIIT_SKILL_USE_FAIL_PATH_INTERRUPTED) and which clears its assist
    target.
  - A watchdog as the last net. A pet that has made no progress toward its
    goal for petProgressStallMs is resolved the same way, in battle and in
    follow alike (petProgress), whatever the reason.
  - Every resolution is counted by kind and region on the metrics'
    pet_combat block (PetCombatCounters), so the places where the port's
    collision or navmesh disagree with the map show up in numbers.

The native stuck event (petstuck.go) is untouched: it still answers its
tenth stop as native does. This file only adds the ways out native lacks.

===========================================================================
*/

package action

import (
	"strconv"
	"sync"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// petProgressStallMs is how long a pet may make no progress toward
	// its goal before it is resolved (and, in battle, logged).
	petProgressStallMs = 3000
	// petProgressDistance is the distance that counts as progress: less is
	// a pet standing where its step was refused. Destination coordinates
	// are integers; one unit sits far above that noise and far below one
	// tick of running.
	petProgressDistance = 1.0
	// petStallCooldownMs is how long a target a fight yielded on is not
	// re-acquired by sight (the mercenary acquisition), so the pet gets
	// home before it would turn back. An owner's admitted order on that
	// target clears the pause (orderPetAttack): the pet obeys its owner,
	// and a pursuit that cannot reach yields again.
	petStallCooldownMs = 10_000
	// petObstacleRefusal is the attack command's refusal code the client
	// shows as "Cannot attack due to an obstacle." (category 4, code 16).
	petObstacleRefusal uint8 = 0x10
	// petRecoveryMaxRegions bounds each kind's region table; later regions
	// are counted under "other".
	petRecoveryMaxRegions = 512
)

// petRecoveryKind names why a pet was resolved.
type petRecoveryKind uint8

const (
	petRecoveryBattleNoRoute petRecoveryKind = iota
	petRecoveryBattleStall
	petRecoveryFollowNoRoute
	petRecoveryFollowStall
	petRecoveryKinds
)

var petRecoveryNames = [petRecoveryKinds]string{"battle_no_route", "battle_stall", "follow_no_route", "follow_stall"}

/*
================
petRecoveryCounters

Every resolution since start, by kind, and by kind and the region the pet
stood in. A region is about 192 units square: fine enough to find the wall.
================
*/
type petRecoveryCounters struct {
	mu       sync.Mutex
	total    [petRecoveryKinds]uint64
	other    [petRecoveryKinds]uint64
	byRegion [petRecoveryKinds]map[uint16]uint64
}

/*
================
petRecoveryCounters.count
================
*/
func (c *petRecoveryCounters) count(kind petRecoveryKind, region uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total[kind]++
	regions := c.byRegion[kind]
	if regions == nil {
		regions = make(map[uint16]uint64)
		c.byRegion[kind] = regions
	}
	if _, seen := regions[region]; !seen && len(regions) >= petRecoveryMaxRegions {
		c.other[kind]++
		return
	}
	regions[region]++
}

/*
================
petRecoveryCounters.into

Writes "recovery.<kind>" and "recovery.<kind>.region.<id>" (and
".region.other") into the metrics block.
================
*/
func (c *petRecoveryCounters) into(out map[string]uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for kind, name := range petRecoveryNames {
		prefix := "recovery." + name
		out[prefix] = c.total[kind]
		for region, n := range c.byRegion[kind] {
			out[prefix+".region."+strconv.Itoa(int(region))] = n
		}
		if c.other[kind] != 0 {
			out[prefix+".region.other"] = c.other[kind]
		}
	}
}

/*
================
petProgress

The watchdog on one pet's way to one goal: when it last made progress.
Progress is closing the distance by petProgressDistance past the best it
reached, or, where the goal itself moves (follow), moving that far. A
chase of minutes is never stalled; a pet standing at a wall is, after
petProgressStallMs.
================
*/
type petProgress struct {
	armed   bool
	best    float64
	last    simulation.Spawn
	sinceMs int64
}

/*
================
petProgress.reset
================
*/
func (w *petProgress) reset() { *w = petProgress{} }

/*
================
petProgress.stalled

Notes the pet at `at`, distance from its goal; countMoves also counts the
pet's own movement as progress. True once no progress was made for
petProgressStallMs.
================
*/
func (w *petProgress) stalled(distance float64, at simulation.Spawn, countMoves bool, nowMs int64) bool {
	moved := countMoves && simulation.WorldDistance2D(at, w.last) >= petProgressDistance
	if !w.armed || distance <= w.best-petProgressDistance || moved {
		w.armed, w.best, w.last, w.sinceMs = true, distance, at, nowMs
		return false
	}
	return nowMs-w.sinceMs >= petProgressStallMs
}

/*
================
petObstacleFrame

The client's refusal of an attack command for an obstacle (file header).
================
*/
func petObstacleFrame(cosGID uint32) simulation.Frame {
	w := wire.NewWriter(7)
	w.U8(2).U8(wire.CosCommandAttackTag).U8(petObstacleRefusal).U32(cosGID)
	return simulation.Frame{Opcode: wire.OpCosCommandResult, Payload: w.Payload()}
}

// petYield is why and where a fight ends for want of a way, with the
// approach's numbers for the fight log.
type petYield struct {
	kind   petRecoveryKind
	at     simulation.Spawn
	fields log.Fields
}

/*
================
yieldPetBattle

Ends a fight the pet cannot carry out: BATTLE is left through its one exit
(cancelPetCombat) and the pet follows again; the target sits out
petStallCooldownMs against re-acquisition by sight; the yield is logged
(throttled) and counted; an ordered fight tells its owner why (queued on
the session, drained by advancePetSession).
================
*/
func (rt *Runtime) yieldPetBattle(step petCombatStep, yield petYield) {
	intent := step.state.combat
	target, ordered := intent.target, intent.ordered
	yield.fields["kind"] = petRecoveryNames[yield.kind]
	rt.petDiagnostic(step.key.division, step.snapshot.Name, step.pet.GID, "approach not reaching strike range", yield.fields)
	rt.cancelPetCombat(step.key, step.state, step.nowMs)
	step.state.stallTarget, step.state.stallUntilMs = target, step.nowMs+petStallCooldownMs
	rt.petRecovery.count(yield.kind, yield.at.RegionID)
	if ordered {
		step.state.notices = append(step.state.notices, petObstacleFrame(step.pet.GID))
	}
}

/*
================
petFollowStuck

The follow lane's judgement, before its step: a pet native's stuck handler
would warp (an attack or pickup pet or a fellow COS) is stuck when its last
move toward its owner ended blocked short of the follow distance, or when
it has made no progress for the watchdog window. A pet within the follow
distance is home and its watchdog rests. Another plane (a dungeon below
its owner's field) is the owner-follow warp's case, not a wall. Judging the
same tick twice (the judged path abstains to the copy path) answers the
same.
================
*/
func (rt *Runtime) petFollowStuck(state *petSession, tid uint16, owner simulation.Spawn, nowMs int64) (petRecoveryKind, bool) {
	follower := state.mover.AnyFollower()
	if follower == nil || monster.NativeStuckResponse(false, petStuckTraceSelector, tid) != monster.StuckWarpToControllerAndIdle {
		return 0, false
	}
	at := follower.Position(nowMs)
	distance := simulation.WorldDistance2D(at, owner)
	if distance <= simulation.PetFollowDistance || !monster.FollowLocationCompatible(at.RegionID, owner.RegionID) {
		state.followProgress.reset()
		return 0, false
	}
	// Only a verdict on a follow move counts: leaving BATTLE or PICKITEM
	// clears the move's verdict (resetPetStuck -> ResetStuck), and a follow
	// move's goal stands within the follow distance of the owner.
	if reach := follower.Reach(); reach.Blocked && simulation.WorldDistance2D(reach.Goal, owner) <= simulation.PetFollowDistance &&
		simulation.WorldDistance2D(reach.Reached, owner) > simulation.PetFollowDistance {
		return petRecoveryFollowNoRoute, true
	}
	return petRecoveryFollowStall, state.followProgress.stalled(distance, at, true, nowMs)
}

/*
================
resolvePetFollow

Relocates a stuck follower (petFollowStuck) onto its owner, counted; done
is false when the pet is not stuck and the follow step runs as before.
================
*/
func (rt *Runtime) resolvePetFollow(key petOwnerKey, state *petSession, snapshot *enterworld.Character, pet *enterworld.CharacterCOS, tid uint16, owner simulation.Spawn, nowMs int64) (frames []simulation.Frame, done bool) {
	kind, stuck := rt.petFollowStuck(state, tid, owner, nowMs)
	if !stuck {
		return nil, false
	}
	state.followProgress.reset()
	rt.petRecovery.count(kind, state.mover.AnyFollower().Position(nowMs).RegionID)
	return rt.relocateStuckPet(key, state, snapshot, pet, nowMs), true
}
