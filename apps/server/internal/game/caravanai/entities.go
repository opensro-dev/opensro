/*
===========================================================================

entities.go - the agents as world entities (port-only, #3704 slice 3)

Slice 1/2 agents were records the brain stepped; slice 3 puts them in
the world as server-owned characters. This file is the runtime's half
of that contract: a stable synthetic character identity per agent and
a per-tick snapshot (identity, job, appearance codenames, pose and
march target) that the shard's wiring turns into peer spawn, movement
and despawn frames. Nothing here imports the wire or the world: the
wiring owns presentation, the runtime owns who the agents are and
where they stand.

===========================================================================
*/
package caravanai

import (
	"opensro.online/server/internal/domain"
)

// AgentCharacterIDBase is where the agents' synthetic character ids
// start. Real character ids are store rowids minted from 1, and
// PlayerObjectID clamps ids to [0, 0x7fffffff] before offsetting them
// into the player object band, so a base at 0x70000000 plus a roster
// slot can never collide with a minted character on any shard that
// has not created two billion characters. The ids exist only so the
// agents ride the same PlayerObjectID space as real peers: clients
// correlate a character-grade object by that gid (targeting, party
// and gauge lanes all key on it).
const AgentCharacterIDBase int64 = 0x70000000

/*
================
AgentEntity

One agent as a world entity, snapshotted for the presentation leg:
who it is (synthetic character id, name, job), what it wears (the
shipped codenames the wiring resolves to reference rows), where it
stands and, while travelling, the anchor it is marching to (the
client walks a peer toward the acked goal, so the goal is part of
the entity, not a wiring guess).
================
*/
type AgentEntity struct {
	CharacterID int64
	Name        string
	JobType     uint8
	ModelCode   string
	SuitCode    string
	WeaponCode  string
	Pose        Pose
	Target      Pose
	Moving      bool
	// HP and MaxHP are the live vitals (gauge and combat snapshots);
	// Level and LastAttacker feed the combat directory (slice 3c):
	// the defender level, and who the agent is already fighting.
	HP           int
	MaxHP        int
	Level        uint8
	LastAttacker string
	// SkillID is the agent's strike skill and CombatTarget the
	// player it is fighting (slice 3e): the directory's attacker
	// snapshot and the fight-back admission.
	SkillID      uint32
	CombatTarget string
}

/*
================
jobTypeForRole

The wire job byte of an agent role: the same domain job codes a
dressed player's spawn row carries.
================
*/
func jobTypeForRole(role AgentRole) uint8 {
	switch role {
	case RoleTrader:
		return domain.JobTrader
	case RoleThief:
		return domain.JobThief
	case RoleHunter:
		return domain.JobHunter
	}
	return 0
}

/*
================
entityLocked

The entity snapshot of one agent. Callers hold r.mu (AgentEntities).
================
*/
func (u *AgentUnit) entityLocked() AgentEntity {
	entity := AgentEntity{
		CharacterID:  u.CharacterID,
		Name:         u.Name,
		JobType:      jobTypeForRole(u.Role),
		ModelCode:    u.ModelCode,
		SuitCode:     u.JobSuitCode,
		WeaponCode:   u.WeaponCode,
		Pose:         Pose{RegionID: u.RegionID, X: u.X, Y: u.Y, Z: u.Z},
		HP:           u.HP,
		MaxHP:        u.MaxHP,
		Level:        u.Level,
		LastAttacker: u.LastAttacker,
		SkillID:      u.PrimarySkillID,
		CombatTarget: u.CombatTarget,
	}
	entity.Target = entity.Pose
	if u.State == StateTraveling && u.WaypointIndex >= 0 && u.WaypointIndex < len(u.Route) {
		anchor := u.Route[u.WaypointIndex]
		entity.Target = Pose{RegionID: anchor.RegionID, X: anchor.X, Y: anchor.Y, Z: anchor.Z}
		entity.Moving = true
	}
	return entity
}

/*
================
AgentEntities

The division's agents as world entities, in roster order. The wiring
diffs consecutive snapshots (through its own shown sets) into peer
spawn, movement and despawn frames; the runtime neither knows nor
tracks who is looking.
================
*/
func (r *Runtime) AgentEntities(division string) []AgentEntity {
	units := r.EnsureDivisionRoster(division)
	r.mu.Lock()
	defer r.mu.Unlock()
	entities := make([]AgentEntity, 0, len(units))
	for _, unit := range units {
		// The dead are not in the world: omitting them is what
		// despawns the corpse for every viewer, and respawn
		// re-enters the slice, which re-shows the agent.
		if unit.State == StateDead {
			continue
		}
		entities = append(entities, unit.entityLocked())
	}
	return entities
}
