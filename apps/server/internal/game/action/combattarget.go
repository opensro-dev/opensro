/*
===========================================================================

combattarget.go - the object an attack command pursues and strikes

CGCharAutoCommandActor keeps a target object ID; the object behind it may
be a monster or, once PvP admits it (CGObjPC_CanAttackTarget 52BF90), a
player or a player's COS (a transport, horse or pet), which 5293A0 judges
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
	if attacker == nil || gid == 0 {
		return combatTarget{}, false
	}
	if target, ok := rt.characterMonster(division, attacker, gid); ok {
		if target.CurrentHP == 0 || rt.Monsters == nil {
			return combatTarget{}, false
		}
		mover, ok := rt.Monsters.Mover(division, target.Gid)
		if !ok {
			return combatTarget{}, false
		}
		pose := mover.LivePoseAt(now, nil)
		return combatTarget{gid: gid, monster: &target,
			at: simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}}, true
	}
	player := rt.findCharacterByGid(division, gid)
	if player == nil {
		return rt.resolveCOSTarget(division, attacker, gid, now)
	}
	if player.ID == attacker.ID {
		return combatTarget{}, false
	}
	snapshot := rt.characterSnapshotAdmitted(division, player, func(live *enterworld.Character) bool {
		return !live.DeletePending && enterworld.CharacterAlive(live) &&
			domain.CharacterWorldInstance(live) == domain.CharacterWorldInstance(attacker)
	})
	if snapshot == nil {
		return combatTarget{}, false
	}
	at := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	return combatTarget{gid: gid, player: player, snapshot: snapshot, at: at}, true
}

/*
================
resolveCOSTarget

A living, summoned COS of another character in the attacker's world
instance, with its owner. 5291D0 refuses a dead COS, a hidden COS (body
modes 6..7) and a pickup pet (+0x43C) whoever attacks it; a protected COS
(body modes 2..4) still resolves here so cosTargetRefusal can answer
5291D0's 0x3020, and a ridden COS stays a target (the mounted trader's
transport). The owner need not be alive: 5293A0 judges the owner's
relations, not its life.
================
*/
func (rt *Runtime) resolveCOSTarget(division string, attacker *enterworld.Character, gid uint32, now int64) (combatTarget, bool) {
	owner := rt.characterByCosGID(division, gid)
	snapshot := rt.characterSnapshotAdmitted(division, owner, func(live *enterworld.Character) bool {
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
