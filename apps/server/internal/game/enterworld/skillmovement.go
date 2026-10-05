/*
===========================================================================

skillmovement.go - movement-speed skill descriptors

Compiles the hste / hst2 / hst3 movement programs and decides which of them
run as an instant self effect (non-attack activity 1). The slot rules the
three kinds follow at run time live in item/statuseffect/movement.go.

===========================================================================
*/

package enterworld

import "opensro.online/server/internal/game/item/statuseffect"

/*
================
SkillMovementModifier

An executable descriptor admission, independent of item/skill names.
Compound programs remain closed until all their operations and persistence
rules are implemented. cbuf uses the native timed-job branch.
================
*/
type SkillMovementModifier struct {
	Present, Supported bool
	Percent            uint32
	Kind               statuseffect.MovementKind
	Persistent         bool
}

/*
================
encodedMovementModifier

Compiles the hste/hst2/hst3 movement program of a skill row's encoded tail.
The modifier is Present when any of the three tags appears, and Supported
only when every parameter in the tail is one this owner understands, a
positive dura is authored and the winning percent is nonzero.
================
*/
func encodedMovementModifier(fields []string) SkillMovementModifier {
	result := SkillMovementModifier{Present: encodedTailContainsTag(fields, 0x68737465) || encodedTailContainsTag(fields, 0x68737432) || encodedTailContainsTag(fields, 0x68737433)}
	if !result.Present {
		return result
	}
	supported, duration := true, false
	var percentages [3]uint32
	var seen [3]bool
	for i := skilldataColEncodedTail; i < len(fields); {
		n, ok := textdataInt(fields[i])
		if !ok {
			return result
		}
		if n == 0 {
			i++
			continue
		}
		if n == 0x73736f75 {
			break
		}
		arity := spawnParamArity(uint32(n))
		if i+arity >= len(fields) {
			return result
		}
		switch n {
		case 0x68737465, 0x68737432, 0x68737433:
			kind := statuseffect.MovementHaste
			if n == 0x68737432 {
				kind = statuseffect.MovementOverride
			}
			if n == 0x68737433 {
				kind = statuseffect.MovementIndependent
			}
			value, valid := textdataInt(fields[i+1])
			if seen[kind] || !valid || value < 0 || value > 0xffffffff {
				supported = false
			} else {
				percentages[kind] = uint32(value)
				seen[kind] = true
			}
		case 0x64757261:
			value, valid := textdataInt(fields[i+1])
			duration = valid && value > 0 && value <= 0xffffffff
		case 0x63627566:
			result.Persistent = true
		case 0x65667461, 0x6e627566, 0x62627566:
		default:
			supported = false
		}
		i += 1 + arity
	}
	// 59642C: nonzero hste takes precedence over hst2, then hst3.
	for kind, percent := range percentages {
		if percent != 0 {
			result.Kind = statuseffect.MovementKind(kind)
			result.Percent = percent
			break
		}
	}
	result.Supported = supported && duration && result.Percent != 0
	return result
}

/*
================
instantMovementSkill

Non-attack activity 1 runs events 0 and 2 directly (4AD890..4AD8D1).
Only the complete self movement/dura program is admitted here; timed item
jobs and target/area/delayed programs keep their separate activation
contracts.

Column 18 is not pinned: it is the replacement descriptor's PackedStates
word (skillreplacement.go), which the replacement owner consumes for any
value. Columns 50/51 are not pinned either: they are the weapon kinds the
action owner checks through 58D480 before charging, so a dagger-only row
(the Rogue's Scud, 13/255) is admitted like an unrestricted one. They must
still be bytes, or the row would keep the compiler's 0xFF/0xFF default and
admit any weapon.
================
*/
func instantMovementSkill(fields []string, row SkillRow) bool {
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "1" || fields[68] != "3" ||
		!row.MovementModifier.Supported || row.MovementModifier.Persistent || !row.Consumption.Pinned || !row.TimingPinned || row.Consumption.HP != 0 || row.Consumption.HPPercent != 0 {
		return false
	}
	for _, i := range []int{9, 12, 13, 15, 16, 17, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 56} {
		if fields[i] != "0" {
			return false
		}
	}
	_, weapon1 := textdataByte(fields[skilldataColWeaponKind1])
	_, weapon2 := textdataByte(fields[skilldataColWeaponKind2])
	return weapon1 && weapon2
}
