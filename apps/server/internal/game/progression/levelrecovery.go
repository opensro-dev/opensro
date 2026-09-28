/*
===========================================================================

levelrecovery.go - recovery at the progression transaction boundary.

Production delegates installed-effect projection to action. Standalone
progression runtimes use the same static keeper graph as their stat packet.
Both paths mutate only the candidate that applyExperience will commit.

===========================================================================
*/

package progression

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	levelRecoverySource      uint16 = 0x80
	vitalsUpdateOpcode       uint16 = 0x33a6
	healthAndManaMask        uint8  = 3
	healthAndManaPacketBytes        = 15
)

/*
================
recoverLevelVitals

The native level owner calls reduced recovery, not an unconditional heal.
Keep the production hook inside the candidate transaction so a projection
error refuses the whole experience grant before any state or frame escapes.
================
*/
func (rt *Runtime) recoverLevelVitals(character *enterworld.Character, display wire.BaseStats) error {
	if rt.RecoverLevelVitals != nil {
		return rt.RecoverLevelVitals(character)
	}
	if !enterworld.CharacterAlive(character) {
		return nil
	}
	stats, _, err := combat.PlayerStats(character, combat.Catalogs{
		Items: rt.deps.ItemReferences(), Skills: rt.deps.SkillData(),
		MagicOptions: rt.deps.MagicOptionDefinitions(),
	})
	if err != nil {
		return err
	}
	maxHP, maxMP := int64(display.MaxHP), int64(display.MaxMP)
	hp, mp := maxHP, maxMP
	if character.CurrentHP != nil {
		hp = *character.CurrentHP
	}
	if character.CurrentMP != nil {
		mp = *character.CurrentMP
	}
	hpReduction, _ := stats.Param(combat.HPRecoveryReductionParameter)
	mpReduction, _ := stats.Param(combat.MPRecoveryReductionParameter)
	hp = combat.RecoverVital(hp, maxHP, maxHP-hp, hpReduction)
	mp = combat.RecoverVital(mp, maxMP, maxMP-mp, mpReduction)
	character.CurrentHP, character.CurrentMP = &hp, &mp
	return nil
}

/*
================
levelRecoveryFrame

The v1.150 client reads absolute HP/MP from 77A080. EXP and base-stat
packets do not change those currents. Publish this snapshot after maxima
and before EXP so the HUD observes one completed level transition.
================
*/
func levelRecoveryFrame(character *enterworld.Character) wire.Frame {
	return wire.Frame{
		Opcode: vitalsUpdateOpcode,
		Payload: wire.NewWriter(healthAndManaPacketBytes).
			U32(enterworld.ObjectIDForCharacter(character)).
			U16(levelRecoverySource).U8(healthAndManaMask).
			U32(uint32(*character.CurrentHP)).U32(uint32(*character.CurrentMP)).Payload(),
	}
}
