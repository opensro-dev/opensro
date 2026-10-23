/*
===========================================================================

skillrequirements.go - what a skill demands of its caster

Parsed by noteParameterIndex, enforced as the native prerequisite, cost and
equipment checks are, through action.skillAdmission.

===========================================================================
*/

package enterworld

/*
==================
SkillReqc

The reqc flag word of the skill row. Shipped rows
use 1 and 32 only.

	bit 0  (1)     the target must be knocked down (motion 8), else 0x3006
	               (the native target validation)
	bit 2  (4)     the caster must be at or below 30 % HP, else 0x3036

	bit 4  (0x10)  the command must carry flag 0x10, else 0x3034;
	               natively it is set for a command issued in body mode 6
	               (stealth)
	bit 5  (32)    skill-manager selector bit 0 must be set, else 0x3032
	               (the native owner-condition test). An active scls skill
	               sets it; its retirement clears it.

==================
*/
type SkillReqc struct {
	Present     bool
	KnockedDown bool // bit 0
	LowHP       bool // bit 2
	Flag16      bool // bit 4
	Dance       bool // bit 5
}

/*
==================
SkillReqi

Up to five {kind, value} pairs (a sixth pair is a fatal load error) and
the reqn switch. The kind picks a check through the native byte table:

	kind 1..3, 9..11  armour set      (case 0)
	kind 4            secondary slot  (case 1)
	kind 6            primary weapon  (case 2)
	kind 14           avatar socket 4 (case 3)
	anything else     never matches

Without reqn the first matching pair admits the skill; with reqn every
pair must match. Evaluated by action.reqiRefusal.
==================
*/
type SkillReqi struct {
	Present bool
	All     bool // reqn
	Count   int
	Pairs   [5]SkillReqiPair
}

/*
==================
SkillReqiPair
==================
*/
type SkillReqiPair struct {
	Kind  uint32
	Value uint32
}

/*
==================
SkillCastGate

Tags the native cast admission reads about the caster's own state:

	nmf   may be used while frozen, asleep or stunned
	tele  refused while rooted
	tel2
	tel3  refused while rooted
	ao    refused while seated
	pw    refused while seated
	rpkt  refused while an rpkt buff is installed (0x3009)
	qest  refused near a skill object or monster, measured
	      against efr kind 3 word 2 (0x3037/0x3038)
	msch  word 0 picks the transform (1) or duple (2) checks
	      (0x3031, 0x3006, 0x3008, 0x3009, 0x3039)
	hide  word 0: 1 stealth, 2 invisibility, 4 trap; berserk and
	      battle refuse (0x3031, 0x3028)
	trap  lifts the berserk refusal of hide

==================
*/
type SkillCastGate struct {
	Nmf              bool
	Tele, Tel2, Tel3 bool
	Ao, Pw           bool
	Rpkt             bool
	Qest             bool
	Efr3Present      bool
	Efr3Radius       uint32
	MschPresent      bool
	MschMode         uint32
	// MschLevel is word 1: the highest level a Duplicate may copy.
	MschLevel       uint32
	HideGatePresent bool
	HideGateMode    uint32
	TrapPresent     bool
	QuestTrap       SkillQuestTrap
}
