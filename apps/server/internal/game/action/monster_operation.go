/*
===========================================================================

monster_operation.go - a monster's action as one division transaction

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
RunMonsterAction

Extends the division transaction through non-blocking packet enqueue.
Returning a committed result before enqueue allowed a later killing hit to
publish the caster's death first. The scoped attack must not escape run.
The attack capability is made once per division lane (divisionLane): built
per call it was an allocation for every monster action.
================
*/
func (rt *Runtime) RunMonsterAction(division string, run func(simulation.MonsterAttackOperation)) {
	unlock := rt.lockDivision(division)
	defer unlock()
	lane := rt.operations.lane(division, &rt.maintenance)
	if lane.monsterAttack == nil {
		lane.monsterAttack = func(d string, instance monster.Instance, target, skill uint32, now int64) simulation.MonsterAttackResult {
			if d != division {
				panic("monster attack crossed its division transaction")
			}
			return rt.monsterAttackStage(d, instance, target, skill, now, nil)
		}
	}
	run(lane.monsterAttack)
}
