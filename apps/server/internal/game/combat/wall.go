/*
===========================================================================

wall.go - an impact against a defender standing behind a Force wall

58E5F0 resolves the wall first. 40EBE0 / 40EEF0 are the ordinary lane
formulas with the defender's defense and parry replaced by the wall's pw
words, and they return 0 for a lane the wall does not cover. Then the
defender's own lanes are resolved and every lane the wall covers is
zeroed (58F02A..58F03D), whatever the wall absorbed. Both use the one
critical decision already rolled for the hit.

===========================================================================
*/

package combat

import "opensro.online/server/internal/game/enterworld"

// WallOutcome is one impact's split: Defender is what the defender takes,
// Absorbed what the wall's lanes computed, Covered whether every lane of
// the attack was a wall lane (58F06B..58F0A6; otherwise the absorb record
// is type 8).
/*
================
WallOutcome
================
*/
type WallOutcome struct {
	Defender Result
	Absorbed uint32
	Covered  bool
}

/*
==================
ResolveAgainstWall
==================
*/
func ResolveAgainstWall(attacker, defender Stats, attack enterworld.SkillAttack, wall enterworld.SkillWall, roll Roll32767, player, critical bool) (WallOutcome, error) {
	return ResolveCalculationAgainstWall(attacker, defender, WallCalculation{
		Attack: AttackCalculation{Attack: attack, OriginalFlags: attack.Flags, Lanes: attack.Flags & (physicalAttackFlag | magicalAttackFlag), Player: player, Critical: critical},
		Wall:   wall,
	}, roll)
}

/*
================
WallCalculation
================
*/
type WallCalculation struct {
	Attack AttackCalculation
	Wall   enterworld.SkillWall
}

/*
================
ResolveCalculationAgainstWall
================
*/
func ResolveCalculationAgainstWall(attacker, defender Stats, input WallCalculation, roll Roll32767) (WallOutcome, error) {
	calculation, wall := input.Attack, input.Wall
	attack, critical := calculation.Attack, calculation.Critical
	lanes := calculation.Lanes
	walled := lanes & wall.Mask
	var out WallOutcome
	out.Covered = lanes&^wall.Mask == 0
	if walled != 0 {
		shield := defender
		shield.PhysicalDefense, shield.MagicalDefense = float64(wall.Defense), float64(wall.Defense)
		shield.ParryRate, shield.MagicalParry = float64(wall.Parry), float64(wall.Parry)
		wallAttack := calculation
		wallAttack.Lanes = walled
		wallAttack.wall = true
		// atca (58F52F) and da scale the defender's record, not the wall's.
		wallAttack.Attack.Atca = false
		wallAttack.Attack.DownAttack = enterworld.SkillDownAttack{}
		absorbed, err := ResolveCalculation(attacker, shield, wallAttack, roll)
		if err != nil {
			return WallOutcome{}, err
		}
		out.Absorbed = absorbed.Damage
	}
	if lanes&^walled == 0 {
		// Every lane zeroed: the record keeps its roll flags with no damage.
		flags := normalResultFlag
		if critical && attack.Flags&physicalAttackFlag != 0 {
			flags = 2
		}
		if attacker.Berserk {
			flags |= 4
		}
		out.Defender = Result{ResultFlags: flags}
		return out, nil
	}
	own := calculation
	own.Lanes = lanes &^ walled
	result, err := ResolveCalculation(attacker, defender, own, roll)
	if err != nil {
		return WallOutcome{}, err
	}
	// The record's flag byte is the roll's (58EF0C), not the surviving lane's.
	if critical && attack.Flags&physicalAttackFlag != 0 {
		result.ResultFlags = result.ResultFlags&^normalResultFlag | 2
	}
	out.Defender = result
	return out, nil
}
