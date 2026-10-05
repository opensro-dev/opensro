/*
===========================================================================

skilltrap.go - complete quest-trap programs from the native skill table

The indirect item path creates a stationary CGSkillObject, not a resident
character buff. Keep its authored targets and lifetime together so admission,
world scans and quest dispatch cannot disagree about the same skill.

===========================================================================
*/
package enterworld

import "opensro.online/server/internal/game/abnormal"

const (
	questTrapTag     = 0x74726170
	questTrapQuest   = 0x71657374
	questTrapLink    = 0x6c6e6b73
	questTrapDura    = 0x64757261
	questTrapScanMs  = 300
	questTrapMobMask = 16
)

/*
================
SkillQuestTrap

48D690 stops matching at the first zero target. Preserve all three words,
including empty lists used by the promotion item rows in v1.150 media.
================
*/
type SkillQuestTrap struct {
	Present    bool
	DurationMs uint32
	ScanMs     uint32
	Radius     uint32
	Targets    [3]uint32
}

/*
================
compileQuestTrap

48CCC0 and 48CEA0 own qest mode 1, stationary monster scans and expiry.
Require the complete shipped program. Unknown instructions cannot be ignored
just because a familiar trap tag appears elsewhere in the row.
================
*/
func compileQuestTrap(fields []string) SkillQuestTrap {
	program, err := CompileSkillProgram(fields)
	if err != nil || program.Len() != 5 {
		return SkillQuestTrap{}
	}
	var trap SkillQuestTrap
	seen := make(map[uint32]bool)
	for index := 0; index < program.Len(); index++ {
		instruction := program.Instruction(index)
		if seen[instruction.Tag] {
			return SkillQuestTrap{}
		}
		seen[instruction.Tag] = true
		words := instruction.Arguments
		switch instruction.Tag {
		case questTrapQuest:
			if words[0] != 1 {
				return SkillQuestTrap{}
			}
			trap.Targets = [3]uint32{words[1], words[2], words[3]}
		case questTrapDura:
			if words[0] == 0 {
				return SkillQuestTrap{}
			}
			trap.DurationMs = words[0]
		case tagEfr:
			if words[0] != 3 || words[1] != 1 || words[2] == 0 || words[3] != 1 ||
				words[4] != 0 || words[5] != questTrapMobMask {
				return SkillQuestTrap{}
			}
			trap.Radius = words[2]
		case questTrapLink:
			if words[0] != 0 || words[1] != questTrapScanMs || words[2] != 1 || words[3] != 0 {
				return SkillQuestTrap{}
			}
			trap.ScanMs = words[1]
		case questTrapTag:
		default:
			return SkillQuestTrap{}
		}
	}
	trap.Present = true
	return trap
}

//============================================================================

const (
	combatTrapHide = 0x68696465
	// combatTrapScanMs reuses the quest trap's stationary update cadence
	// (48CEA0); the combat trap's lnks second word is its owner distance.
	CombatTrapScanMs = questTrapScanMs
	// att flag lanes (combat/formula.go): 0x04 physical, 0x08 magical.
	physicalAttackFlagBit = 0x04
	magicalAttackFlagBit  = 0x08
)

/*
================
SkillCombatTrap

A planted CGSkillObject that strikes the first hostile monster to enter its
trigger radius (efr kind 3), then retires. Attack and Area are the authored
att and kind-1 efr of the explosion; statuses come from the row's blocks.
lnks is read as the timed-effect link: group, owner distance, live traps
per owner, board. The description ("When the planter goes beyond a certain
distance from the traps, they cease to exist") names the distance word.
================
*/
type SkillCombatTrap struct {
	Pinned           bool
	DurationMs       uint32
	TriggerRadius    uint32
	LinkGroup        uint32
	OwnerDistance    uint32
	MaxLive          uint32
	Attack           SkillAttack
	Area             SkillOffensiveArea
	Hidden           bool
	HideMask, HideLv uint32
}

/*
================
compileCombatTrap

Admit the Wizard's Fire Trap shape: an untargeted prepared cast with dura,
lnks, trap, a kind-3 trigger area, one att, one kind-1 explosion area,
status blocks, hide and caster getv modifiers. Anything else refuses.
================
*/
func compileCombatTrap(fields []string, row SkillRow) SkillCombatTrap {
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "3" ||
		row.TargetRequired || row.ChainSub || row.ChainNext != 0 || !row.TimingPinned ||
		!row.Consumption.Pinned || row.ActionCastingTimeMs == 0 || row.Consumption.HP != 0 || row.Consumption.HPPercent != 0 {
		return SkillCombatTrap{}
	}
	for _, column := range []int{15, 16, 17, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return SkillCombatTrap{}
		}
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return SkillCombatTrap{}
	}
	var trap SkillCombatTrap
	seen := make(map[uint32]bool)
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		a := op.Arguments
		if seen[op.Tag] && op.Tag != tagGetv && op.Tag != tagEfr {
			return SkillCombatTrap{}
		}
		seen[op.Tag] = true
		if _, found := abnormal.SourceIndex(op.Tag); found {
			continue
		}
		switch op.Tag {
		case questTrapDura:
			trap.DurationMs = a[0]
		case questTrapLink:
			trap.LinkGroup, trap.OwnerDistance, trap.MaxLive = a[0], a[1], a[2]
		case questTrapTag:
		case tagEfr:
			switch {
			case a[0] == 3 && a[1] == 1 && a[2] != 0 && a[3] == 1 && a[4] == 0 && a[5] == statusCastSelect && trap.TriggerRadius == 0:
				trap.TriggerRadius = a[2]
			case a[0] == 1 && a[1] == 1 && a[2] != 0 && a[2] <= 0xffff && a[3] != 0 && a[3] <= 255 && a[4] <= 100 &&
				a[5] == statusCastSelect && trap.Area.Radius == 0:
				trap.Area = SkillOffensiveArea{Shape: 1, Radius: a[2], MaxTargets: uint8(a[3]), ReductionPercent: uint8(a[4]), Select: statusCastSelect}
			default:
				return SkillCombatTrap{}
			}
		case uint32(skillAttackTag):
			if a[0]&(physicalAttackFlagBit|magicalAttackFlagBit) == 0 || a[1] == 0 || a[2] > a[3] || a[4] != 0 {
				return SkillCombatTrap{}
			}
			trap.Attack = SkillAttack{Present: true, Flags: a[0], Percent: int64(a[1]), Min: int64(a[2]), Max: int64(a[3]),
				ImpactCount: 1, Parameters: encodedAttackParameters(fields)}
		case combatTrapHide:
			trap.Hidden, trap.HideMask, trap.HideLv = true, a[0], a[1]
		case tagGetv:
			if _, known := SkillParameterFromKey(a[0]); !known {
				return SkillCombatTrap{}
			}
		default:
			return SkillCombatTrap{}
		}
	}
	if trap.DurationMs == 0 || trap.TriggerRadius == 0 || trap.Area.Radius == 0 || !trap.Attack.Present ||
		trap.OwnerDistance == 0 || trap.MaxLive == 0 || !seen[questTrapTag] {
		return SkillCombatTrap{}
	}
	trap.Pinned = true
	return trap
}
