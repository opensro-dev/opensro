/*
===========================================================================

combattarget.go - the object an attack command pursues and strikes

CGCharAutoCommandActor keeps a target object ID; the object behind it may
be a monster or, once PvP admits it (CGObjPC_CanAttackTarget 52BF90), a
player. The attack intents and the prepared-cast release resolve their
target here so the walk into reach, the range test and the life test read
one rule for both kinds. The cast itself splits by kind: monsters take
acceptSkillStagePhaseAt's monster commit, players pvpstrike.go.

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
authoritative record; snapshot is its detached copy for reads.
================
*/
type combatTarget struct {
	gid      uint32
	monster  *monster.Instance
	player   *enterworld.Character
	snapshot *enterworld.Character
	at       simulation.Spawn
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
	if player == nil || player.ID == attacker.ID {
		return combatTarget{}, false
	}
	snapshot := rt.characterSnapshot(division, player)
	if snapshot == nil || snapshot.DeletePending || !enterworld.CharacterAlive(snapshot) ||
		domain.CharacterWorldInstance(snapshot) != domain.CharacterWorldInstance(attacker) {
		return combatTarget{}, false
	}
	at := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	return combatTarget{gid: gid, player: player, snapshot: snapshot, at: at}, true
}

/*
================
combatTargetSpacing

Both body radii plus the action reach, for either kind of target.
================
*/
func (rt *Runtime) combatTargetSpacing(attacker *enterworld.Character, target combatTarget, reach simulation.ActionReach) (simulation.CombatSpacing, bool) {
	if target.monster != nil {
		return rt.playerToMonsterCombatSpacing(attacker, *target.monster, reach)
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
