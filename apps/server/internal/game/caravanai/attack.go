/*
===========================================================================

attack.go - a player or monster strike on an AI agent

Records the damage, raises distress and Hunter calls, and starts the
agent's death when the combat authority judged the strike fatal.

===========================================================================
*/
package caravanai

import (
	"opensro.online/server/internal/domain"
)

/*
==================
RecordPlayerAttack

RecordPlayerAttack applies damage from attackerPlayerName onto agentName. If
agentName was inside an active parley truce window, the attack immediately
triggers a -100 Betrayal Vendetta. If the blow defeats the agent, robbery and
route danger heat are recorded in the persistent MemoryBook.
==================
*/
func (r *Runtime) RecordPlayerAttack(
	division, agentName, attackerPlayerName string,
	damage int,
	nowMs int64,
) (betrayedParley bool, defeated bool, memRow domain.CaravanAgentMemoryRow) {
	return r.recordAttack(division, agentName, attackerPlayerName, damage, nil, nowMs)
}

/*
==================
RecordPlayerStrike

RecordPlayerAttack for a strike the combat authority judged: the agent dies
exactly when the strike was fatal. Its own HP copy can lag heals and
regeneration, so a non-fatal strike leaves it at least 1 HP rather than
starting a death the world never had.
==================
*/
func (r *Runtime) RecordPlayerStrike(
	division, agentName, attackerPlayerName string,
	damage int,
	fatal bool,
	nowMs int64,
) (betrayedParley bool, defeated bool, memRow domain.CaravanAgentMemoryRow) {
	return r.recordAttack(division, agentName, attackerPlayerName, damage, &fatal, nowMs)
}

func (r *Runtime) recordAttack(
	division, agentName, attackerPlayerName string,
	damage int,
	fatal *bool,
	nowMs int64,
) (betrayedParley bool, defeated bool, memRow domain.CaravanAgentMemoryRow) {
	r.EnsureDivisionRoster(division)
	if _, row, betrayed := r.parleys.OnDamageDuringParley(division, agentName, attackerPlayerName, nowMs, r.memories); betrayed {
		betrayedParley = true
		memRow = row
	}

	r.mu.Lock()
	unit := r.findAgentLocked(division, agentName)
	if unit == nil || unit.DeadUntilMs > 0 {
		r.mu.Unlock()
		return betrayedParley, false, memRow
	}
	unit.UnderAttack = true
	unit.LastAttacker = attackerPlayerName
	if damage > 0 {
		unit.HP -= damage
	}
	if fatal != nil {
		if *fatal {
			unit.HP = 0
		} else if unit.HP <= 0 {
			unit.HP = 1
		}
	}
	if unit.Role == RoleTrader && unit.HP > 0 {
		r.raiseDistressLocked(division, unit, attackerPlayerName, nowMs)
	}
	r.callHuntersLocked(division, unit, attackerPlayerName, nowMs)
	var stolenGold int64
	regionID := unit.RegionID
	if unit.HP <= 0 {
		defeated = true
		stolenGold = r.beginDeathLocked(unit, attackerPlayerName, nowMs)
	}
	r.mu.Unlock()

	if defeated {
		memRow = r.memories.RecordRobberyOrKill(division, agentName, attackerPlayerName, stolenGold, regionID, "", nowMs)
	}
	return betrayedParley, defeated, memRow
}
