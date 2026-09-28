/*
===========================================================================

goldbonus.go - reward-owner gold modifiers at ground publication

The drop generator owns probability, randomness and heap identity. This final
quantity adjustment reads the winner's live keeper and runs before persistence,
so neither the killing blow nor the eventual picker can steal the bonus.

===========================================================================
*/
package action

import (
	"math"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
)

/*
================
applyMonsterGoldBonus

4C40EA..4C41AD divides B6 by 100, spills the fraction, and multiplies gold
stock by 1+bonus before publishing. Item stacks and drop probability are
unchanged. The prepared slice belongs exclusively to this kill transaction.
================
*/
func (rt *Runtime) applyMonsterGoldBonus(division string, owner *enterworld.Character, planned []grounditem.Item) {
	if len(planned) == 0 {
		return
	}
	stats, _, err := rt.playerCombatStats(division, owner)
	if err != nil {
		return
	}
	percent, present := stats.Param(itemParamGoldDrop)
	if !present || percent <= 0 {
		return
	}
	fraction := float32(float64(percent) / 100)
	for i := range planned {
		if !planned[i].IsGold() {
			continue
		}
		amount := float64(planned[i].GoldAmount) * (1 + float64(fraction))
		// Port inference: cap exceptional combined bonuses at the wire's
		// unsigned quantity ceiling rather than permitting integer wrap.
		planned[i].GoldAmount = uint32(min(amount, math.MaxUint32))
	}
}
