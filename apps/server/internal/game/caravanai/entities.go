/*
===========================================================================

entities.go - the agents as world entities (port-only, #3704 slice 3)

Slice 1/2 agents were records the brain stepped; slice 3 puts them in
the world as server-owned characters. This file is the runtime's half
of that contract: a stable synthetic character identity per agent and
a per-tick snapshot (identity, job, appearance codenames, pose and
march target) that the shard's wiring turns into peer spawn, movement
and despawn frames. Trader agents also snapshot their transport COS
(slice 3f): a second synthetic identity and the pose the beast trails
the trader at. Nothing here imports the wire or the world: the
wiring owns presentation, the runtime owns who the agents are and
where they stand.

===========================================================================
*/
package caravanai

import (
	"math"

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

// AgentTransportIDBase is where the trader agents' transport COS
// identities start (slice 3f): one reserved slot per roster agent,
// in the same synthetic band as the agents themselves and disjoint
// from both them and any minted character id, so a transport's
// PlayerObjectID gid can never collide with a peer or an agent.
const AgentTransportIDBase int64 = AgentCharacterIDBase + 0x10000

// agentTransportFollowDistance is how far behind its trader a
// transport trails, in world units: close enough to read as one
// caravan, far enough that the beast's body clears the trader's.
// Port-only flavour; native transports stand where their owner
// leaves them.
const agentTransportFollowDistance = 8.0

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
	// The transport COS (slice 3f), traders only: the shipped
	// creature codename the wiring resolves, the transport's own
	// synthetic identity, where the beast stands and, while the
	// trader marches, the trail point behind the trader it walks
	// to. TransportCharacterID is zero when the agent travels
	// without one. The transport is presentation in this slice:
	// its cargo and its own combat arrive with the trade economy
	// (slice 5).
	TransportCode        string
	TransportCharacterID int64
	TransportPose        Pose
	TransportTarget      Pose
	TransportMoving      bool
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
	entity.fillTransportLocked(u)
	return entity
}

/*
================
AgentEntity.fillTransportLocked

The transport half of the snapshot (slice 3f): the beast stands
agentTransportFollowDistance behind its trader against the march
direction, or beside it at rest, and while the trader marches its
walk goal is the trail point behind where the trader stands now -
the wiring re-acks that goal as it drifts, so the beast follows
the caravan instead of walking anchor to anchor. An agent without
a transport codename travels alone. Callers hold r.mu.
================
*/
func (e *AgentEntity) fillTransportLocked(u *AgentUnit) {
	if u.TransportCode == "" {
		return
	}
	e.TransportCode = u.TransportCode
	e.TransportCharacterID = AgentTransportIDBase + (u.CharacterID - AgentCharacterIDBase)
	// The facing the beast trails against: toward the march anchor
	// while travelling; at rest the route's next anchor if there is
	// one, else a fixed side offset.
	dirX, dirZ := 0.0, 0.0
	anchor, hasAnchor := Pose{}, false
	if u.WaypointIndex >= 0 && u.WaypointIndex < len(u.Route) {
		next := u.Route[u.WaypointIndex]
		anchor = Pose{RegionID: next.RegionID, X: next.X, Y: next.Y, Z: next.Z}
		hasAnchor = anchor.RegionID == e.Pose.RegionID
	}
	if hasAnchor {
		dirX, dirZ = float64(anchor.X-e.Pose.X), float64(anchor.Z-e.Pose.Z)
	}
	if length := math.Hypot(dirX, dirZ); length > 0.01 {
		dirX, dirZ = dirX/length, dirZ/length
	} else {
		dirX, dirZ = 1, 0
	}
	e.TransportPose = behindPose(e.Pose, dirX, dirZ)
	// The walk goal is that same trail point: it advances with the
	// trader, and the wiring re-acks it as it drifts.
	e.TransportTarget = e.TransportPose
	e.TransportMoving = e.Moving
}

/*
================
behindPose

The point distance behind pose against the facing (dirX, dirZ),
kept inside the region: region-local coordinates cannot leave
their sector, so the offset clamps at the border rather than
naming a pose in the wrong region.
================
*/
func behindPose(pose Pose, dirX, dirZ float64) Pose {
	out := pose
	out.X = float32(clampLocal(float64(pose.X) - dirX*agentTransportFollowDistance))
	out.Z = float32(clampLocal(float64(pose.Z) - dirZ*agentTransportFollowDistance))
	return out
}

// clampLocal keeps a region-local coordinate inside its sector
// (1920 world units, world.ExpandGrid's OutdoorRegionSize), with a
// small margin so a clamped pose never sits on the border itself.
func clampLocal(v float64) float64 {
	return min(max(v, 4), 1916)
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
