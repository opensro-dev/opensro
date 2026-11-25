/*
===========================================================================

combat.go - the damage door: player attacks land, agents die and respawn

RecordPlayerAttack is the one way damage reaches an agent (the action
package's agent strike calls it with real rolled damage, slice 3c).
This file owns what the damage does: parley betrayal, the HP debit,
death where the agent stands, the robbery memory, and the respawn
that brings the agent home once its delay has passed.

===========================================================================
*/
package caravanai

import (
	"opensro.online/server/internal/domain"
)

// agentRespawnDelayMs is how long a defeated agent stays dead before
// it reappears at its home town. Port-only tuning, not native.
const agentRespawnDelayMs int64 = 30_000

/*
==================
RecordPlayerAttack

RecordPlayerAttack applies damage from attackerPlayerName onto agentName. If
agentName was inside an active parley truce window, the attack immediately
triggers a -100 Betrayal Vendetta. If the blow defeats the agent, robbery and
route danger heat are recorded in the persistent MemoryBook, and the agent
dies where it stands: it leaves the world (AgentEntities omits the dead) and
respawns at its home town once agentRespawnDelayMs has passed.
==================
*/
func (r *Runtime) RecordPlayerAttack(
	division, agentName, attackerPlayerName string,
	damage int,
	nowMs int64,
) (betrayedParley bool, defeated bool, memRow domain.CaravanAgentMemoryRow) {
	r.EnsureDivisionRoster(division)
	if _, row, betrayed := r.parleys.OnDamageDuringParley(division, agentName, attackerPlayerName, nowMs, r.memories); betrayed {
		betrayedParley = true
		memRow = row
	}

	r.mu.Lock()
	unit := r.findAgentLocked(division, agentName)
	if unit == nil || unit.State == StateDead {
		// The dead take no damage and record no second robbery;
		// hitting a corpse must not extend its respawn.
		r.mu.Unlock()
		return betrayedParley, false, memRow
	}
	unit.UnderAttack = true
	unit.LastAttacker = attackerPlayerName
	if damage > 0 {
		unit.HP -= damage
	}
	var stolenGold int64
	regionID := unit.RegionID
	if unit.HP <= 0 {
		// The agent dies where it stands; respawnAtHomeLocked
		// brings it home once the delay has passed. Slice 5: a trader
		// who died on the road voids its escort - the run was not
		// survived, so it can never pay (contracts.go).
		if unit.Role == RoleTrader {
			r.contracts.VoidTraderEscort(division, agentName)
		}
		defeated = true
		stolenGold = int64(unit.CargoStacks) * unit.StackValueGold
		unit.CargoStacks = 0
		unit.HP = 0
		unit.UnderAttack = false
		unit.CombatTarget = ""
		unit.State = StateDead
		unit.DeadUntilMs = nowMs + agentRespawnDelayMs
	}
	r.mu.Unlock()

	if defeated {
		memRow = r.memories.RecordRobberyOrKill(division, agentName, attackerPlayerName, stolenGold, regionID, "", nowMs)
	}
	return betrayedParley, defeated, memRow
}

/*
==================
respawnAtHomeLocked

Brings a dead agent back at its home town's anchor, whole: full vitals,
no route (the next advance rebuilds one from home toward its destination),
no attacker. Callers hold r.mu.
==================
*/
func (unit *AgentUnit) respawnAtHomeLocked(nowMs int64) {
	home := TownWaypoint(unit.HomeTown)
	unit.RegionID = home.RegionID
	unit.X = home.X
	unit.Y = home.Y
	unit.Z = home.Z
	unit.HP = unit.MaxHP
	unit.MP = unit.MaxMP
	unit.State = StateIdleInTown
	unit.DeadUntilMs = 0
	unit.OriginTown = unit.HomeTown
	unit.Route = nil
	unit.WaypointIndex = 0
	unit.UnderAttack = false
	unit.LastAttacker = ""
	unit.CombatTarget = ""
	unit.lastAdvanceMs = nowMs
}
