/*
===========================================================================

combattarget.go - the object an attack command pursues and strikes

The command actor keeps a target object ID; the object behind it may
be a monster or, once PvP admits it (the reference server's attack-target check), a
player or a player's COS (a transport, horse or pet), which the attack validation judges
through its owner (cosTargetRefusal). The attack intents and the prepared-cast release resolve their
target here so the walk into reach, the range test and the life test read
one rule for every kind. The cast itself splits by kind: monsters take
acceptSkillStagePhaseAt's monster commit, players pvpstrike.go, a COS
playerstrike_cos.go.

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
combatTarget

A live target: exactly one of monster and player is set. player is the
authoritative record; snapshot is its detached copy for reads. For a COS
target (cos set) player and snapshot are its owner, and gid and at are the
COS's own.
================
*/
type combatTarget struct {
	gid      uint32
	monster  *monster.Instance
	player   *enterworld.Character
	snapshot *enterworld.Character
	at       simulation.Spawn
	cos      *enterworld.CharacterCOS
	cosRef   *enterworld.CharacterRef
}

/*
================
resolveCombatTarget

The living target gid names for the attacker, in the attacker's world
instance. A monster is looked up first: its population owns the gid.
================
*/
func (rt *Runtime) resolveCombatTarget(division string, attacker *enterworld.Character, gid uint32, now int64) (combatTarget, bool) {
	target, handled, ok := rt.resolveMonsterOrPlayerTarget(division, attacker, gid, now)
	if handled {
		return target, ok
	}
	return rt.resolveCOSTarget(division, attacker, gid, now)
}

/*
================
resolvePulseTarget

resolveCombatTarget for the periodic pulse lane (skillperiodic.go):
the same arms in the same order, but a COS recipient is judged on
the live record (resolveCOSTargetJudged) instead of detached into
its owner's pet-owner view. The strike and area paths keep the
copy: cosTargetRefusal judges the PvP chain on it.
================
*/
func (rt *Runtime) resolvePulseTarget(division string, attacker *enterworld.Character, gid uint32, now int64) (combatTarget, bool) {
	target, handled, ok := rt.resolveMonsterOrPlayerTarget(division, attacker, gid, now)
	if handled {
		return target, ok
	}
	return rt.resolveCOSTargetJudged(division, attacker, gid, now)
}

/*
================
resolveMonsterOrPlayerTarget

The monster and player arms both resolutions open with, in their
order: a monster is looked up first, its population owns the gid.
handled reports whether the gid named either kind at all, resolved
or refused, so the caller runs its COS arm only for a gid neither
population owns.
================
*/
func (rt *Runtime) resolveMonsterOrPlayerTarget(division string, attacker *enterworld.Character, gid uint32, now int64) (target combatTarget, handled, ok bool) {
	if attacker == nil || gid == 0 {
		return combatTarget{}, true, false
	}
	if instance, found := rt.characterMonster(division, attacker, gid); found {
		if instance.CurrentHP == 0 || rt.Monsters == nil {
			return combatTarget{}, true, false
		}
		mover, moverOK := rt.Monsters.Mover(division, instance.Gid)
		if !moverOK {
			return combatTarget{}, true, false
		}
		pose := mover.LivePoseAt(now, nil)
		return combatTarget{gid: gid, monster: &instance,
			at: simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}}, true, true
	}
	player := rt.findCharacterByGid(division, gid)
	if player == nil {
		return combatTarget{}, false, false
	}
	if player.ID == attacker.ID {
		return combatTarget{}, true, false
	}
	snapshot := rt.characterSnapshotAdmitted(division, player, func(live *enterworld.Character) bool {
		return !live.DeletePending && enterworld.CharacterAlive(live) &&
			domain.CharacterWorldInstance(live) == domain.CharacterWorldInstance(attacker)
	})
	if snapshot == nil {
		return combatTarget{}, true, false
	}
	at := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	return combatTarget{gid: gid, player: player, snapshot: snapshot, at: at}, true, true
}

/*
================
combatTargetExists

resolveCombatTarget's verdict alone, for the readers that ask only
whether the target still stands (the waiting cast's per-tick verdict,
#2139): the same gates in the same order, but no record is detached
and no pose is sampled. The player and COS gates judge the live
record under the one read door (characterAdmitted) - the discarded
copies they used to detach cost a whole record per question (the
player victim's clone alone measures 20 allocs, ~16 KB on a
production-shaped record). The COS reference check reads only the
pet's codename, object id and brood stage plus static item data, so
it weighs the same inputs on the live pet as on the detached one.
================
*/
func (rt *Runtime) combatTargetExists(division string, attacker *enterworld.Character, gid uint32) bool {
	if attacker == nil || gid == 0 {
		return false
	}
	return rt.combatTargetExistsFor(division, attacker.ID, attacker.Name, domain.CharacterWorldInstance(attacker), gid)
}

/*
================
combatTargetExistsFor

combatTargetExists on the attacker's door-read values (id, name and
world instance), for the waiting cast's copy-free verdict (#2139):
the same gates in the same order, judged without an attacker record
in hand.
================
*/
func (rt *Runtime) combatTargetExistsFor(division string, attackerID int64, attackerName string, attackerWorld uint32, gid uint32) bool {
	if gid == 0 {
		return false
	}
	if target, ok := rt.characterMonsterFor(division, attackerName, attackerWorld, gid); ok {
		if target.CurrentHP == 0 || rt.Monsters == nil {
			return false
		}
		_, ok := rt.Monsters.Mover(division, target.Gid)
		return ok
	}
	if player := rt.findCharacterByGid(division, gid); player != nil {
		if player.ID == attackerID {
			return false
		}
		return rt.characterAdmitted(division, player, func(live *enterworld.Character) bool {
			return !live.DeletePending && enterworld.CharacterAlive(live) &&
				domain.CharacterWorldInstance(live) == attackerWorld
		})
	}
	owner := rt.characterByCosGID(division, gid)
	var pet *enterworld.CharacterCOS
	if !rt.characterAdmitted(division, owner, func(live *enterworld.Character) bool {
		if live.ID == attackerID || live.DeletePending ||
			domain.CharacterWorldInstance(live) != attackerWorld {
			return false
		}
		pet = live.CompanionByGID(gid)
		return pet != nil && pet.Summoned && pet.CurrentHP != 0 &&
			(pet.NativeBodyStatus < mercenaryBodyHiddenFirst || pet.NativeBodyStatus > mercenaryBodyHiddenLast)
	}) {
		return false
	}
	ref, found := rt.cosReference(pet)
	return found && ref.TidWord>>11 != domain.PickupPetBand
}

/*
================
resolveCOSTarget

A living, summoned COS of another character in the attacker's world
instance, with its owner. The reference server refuses a dead COS, a hidden COS (body
modes 6..7) and a pickup pet (+0x43C) whoever attacks it; a protected COS
(body modes 2..4) still resolves here so cosTargetRefusal can answer
the reference server's 0x3020, and a ridden COS stays a target (the mounted trader's
transport). The owner need not be alive: the attack validation judges the owner's
relations, not its life.

The owner's copy is the pet-owner view (SnapshotCompanion, #2030): an
area query resolves every pet of every player standing in it. Its
readers are audited: cosTargetRefusal and the pet lanes'
worldPlayerEnemy read the owner's ID, name, world, body status, level,
PK, aggressions, guild, the job-suit slot and the ridden transport's
cargo, and the strike itself takes the live owner (target.player).
================
*/
func (rt *Runtime) resolveCOSTarget(division string, attacker *enterworld.Character, gid uint32, now int64) (combatTarget, bool) {
	owner := rt.characterByCosGID(division, gid)
	snapshot := rt.characterSnapshotCompanionAdmitted(division, owner, func(live *enterworld.Character) bool {
		if live.ID == attacker.ID || live.DeletePending ||
			domain.CharacterWorldInstance(live) != domain.CharacterWorldInstance(attacker) {
			return false
		}
		pet := live.CompanionByGID(gid)
		return pet != nil && pet.Summoned && pet.CurrentHP != 0 &&
			(pet.NativeBodyStatus < mercenaryBodyHiddenFirst || pet.NativeBodyStatus > mercenaryBodyHiddenLast)
	})
	if snapshot == nil {
		return combatTarget{}, false
	}
	// Admission judged the companion's summons, life and form on the
	// live record in the same door pass that detached this copy, so
	// the copy's companion carries the same verdict.
	pet := snapshot.CompanionByGID(gid)
	if pet == nil {
		return combatTarget{}, false
	}
	ref, found := rt.cosReference(pet)
	if !found || ref.TidWord>>11 == domain.PickupPetBand {
		return combatTarget{}, false
	}
	at := rt.companionLiveSpawn(division, snapshot, pet, now)
	return combatTarget{gid: gid, player: owner, snapshot: snapshot, at: at, cos: pet, cosRef: ref}, true
}

/*
================
resolveCOSTargetJudged

resolveCOSTarget judged on the live record, for the periodic pulse
lane: the same gates in the same order - the owner is not the
caster, not deletion-pending, in the caster's world instance; the
companion is summoned, alive and not hidden - weighed under the
one read door, as combatTargetExists weighs them (#2139), so a
pulse pays for no pet-owner view. What the lane reads from the
resolution comes out as values: the one companion record by value
(its identity is all the hit planner compares, off its own live
re-read), the ref cosReference reads from that same record, and
the pose companionLiveSpawn samples. The detached copy stays with
the strike and area paths: cosTargetRefusal judges the PvP chain
on the owner the copy carries, and its readers are audited there,
not here (#2033).
================
*/
func (rt *Runtime) resolveCOSTargetJudged(division string, attacker *enterworld.Character, gid uint32, now int64) (combatTarget, bool) {
	owner := rt.characterByCosGID(division, gid)
	var pet enterworld.CharacterCOS
	if !rt.characterAdmitted(division, owner, func(live *enterworld.Character) bool {
		if live.ID == attacker.ID || live.DeletePending ||
			domain.CharacterWorldInstance(live) != domain.CharacterWorldInstance(attacker) {
			return false
		}
		found := live.CompanionByGID(gid)
		if found == nil || !found.Summoned || found.CurrentHP == 0 ||
			found.NativeBodyStatus >= mercenaryBodyHiddenFirst && found.NativeBodyStatus <= mercenaryBodyHiddenLast {
			return false
		}
		pet = *found
		return true
	}) {
		return combatTarget{}, false
	}
	ref, found := rt.cosReference(&pet)
	if !found || ref.TidWord>>11 == domain.PickupPetBand {
		return combatTarget{}, false
	}
	at := rt.companionLiveSpawn(division, owner, &pet, now)
	return combatTarget{gid: gid, player: owner, at: at, cos: &pet, cosRef: ref}, true
}

/*
================
combatTargetSpacing

Both body radii plus the action reach, for any kind of target. A COS's
body is its own reference's (BodyRadius), never its owner's.
================
*/
func (rt *Runtime) combatTargetSpacing(attacker *enterworld.Character, target combatTarget, reach simulation.ActionReach) (simulation.CombatSpacing, bool) {
	if target.monster != nil {
		return rt.playerToMonsterCombatSpacing(attacker, *target.monster, reach)
	}
	if target.cos != nil {
		radius, ok := rt.deps.CharacterBodyRadius(attacker)
		if !ok {
			return simulation.CombatSpacing{}, false
		}
		spacing := simulation.CombatSpacing{ActorBodyRadius: simulation.BodyRadius(radius),
			TargetBodyRadius: simulation.BodyRadius(target.cosRef.Parameters.BodyRadius), ActionReach: reach}
		return spacing, spacing.Valid()
	}
	return rt.playerToPlayerCombatSpacing(attacker, target.snapshot, reach)
}

/*
================
monsterSpawn

A monster's live position, or the zero spawn when it has no mover.
================
*/
func (rt *Runtime) monsterSpawn(division string, gid uint32, now int64) simulation.Spawn {
	if rt.Monsters == nil {
		return simulation.Spawn{}
	}
	mover, ok := rt.Monsters.Mover(division, gid)
	if !ok {
		return simulation.Spawn{}
	}
	pose := mover.LivePoseAt(now, nil)
	return simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
}
