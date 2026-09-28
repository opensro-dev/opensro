/*
===========================================================================

levelrecovery.go - level-up recovery through the installed combat graph.

Progression owns the level transaction. This adapter reads the same effect
and abnormal owners as skill healing, without opening another character
transaction or publishing packets before progression commits.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
RecoverLevelVitals

SR_GameServer 4E5065..4E50AF recovers the missing HP and MP only on an
upward level crossing, through the normal reduced-recovery path. The
supplied character is progression's detached post-level candidate; only
that candidate is changed if all keeper inputs are valid.
================
*/
func (rt *Runtime) RecoverLevelVitals(division string, character *enterworld.Character) error {
	if !enterworld.CharacterAlive(character) {
		return nil
	}
	stats, _, err := rt.playerCombatStats(division, character)
	if err != nil {
		return err
	}
	maxHP, maxMP, currentHP, currentMP := rt.playerKeeperVitals(division, character)
	hpReduction, _ := stats.Param(combat.HPRecoveryReductionParameter)
	mpReduction, _ := stats.Param(combat.MPRecoveryReductionParameter)
	hp := combat.RecoverVital(currentHP, maxHP, maxHP-currentHP, hpReduction)
	mp := combat.RecoverVital(currentMP, maxMP, maxMP-currentMP, mpReduction)
	character.CurrentHP, character.CurrentMP = &hp, &mp
	return nil
}
