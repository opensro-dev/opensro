/*
===========================================================================

withdrawal.go - restoration shares inventory and effect lifecycle owners

Progression owns the rank/refund transaction. This adapter supplies the
division lock, existing inventory planner, and retirement of the old rank.
No separate potion inventory or modifier registry is introduced.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
)

/*
================
WithdrawalHooks
================
*/
func (rt *Runtime) WithdrawalHooks() progression.WithdrawalHooks {
	return progression.WithdrawalHooks{
		Lock:          rt.lockDivision,
		PlanInventory: rt.PlanQuestInventory,
		Finish:        rt.finishWithdrawal,
	}
}

/*
================
finishWithdrawal

59FA85 retires the old skill's attached instances before rebuilding derived
parameters. Retirement uses the common owner, including durable jobs,
movement, body state and linked effects. The caller holds the character
door and division lock after all fallible planning has succeeded.
================
*/
func (rt *Runtime) finishWithdrawal(division string, c *enterworld.Character, previousSkill uint32) []wire.Frame {
	now := rt.Now().UnixMilli()
	if previousSkill != 0 && rt.effects != nil {
		var tokens []uint32
		for _, effect := range rt.effects.Snapshot(division, c.Name) {
			if effect.SkillID == previousSkill {
				tokens = append(tokens, effect.InstanceToken)
			}
		}
		rt.publishEndedEffects(division, c, rt.effects.RetireInstances(division, c.Name, tokens), now)
	}
	rt.clampStoredGaugeToKeeper(division, c)
	frames := rt.gaugeDropFrames(division, c, true, true, true)
	return append(frames, rt.updateQuestInventory(c)...)
}
