/*
===========================================================================

skillrequirements.go - what a skill demands of its caster

Parsed by noteParameterIndex, enforced by the server's prerequisite-and-cost
validation and its equipment-requirement check through
action.skillAdmission.

===========================================================================
*/

package enterworld

/*
==================
SkillReqc

The reqc flag word at RefSkill+0x39C (the server's parameter index). Shipped rows
use 1 and 32 only.

	bit 0  (1)     the target must be knocked down (motion 8), else 0x3006
	               (the server's target validation)
	bit 2  (4)     the caster must be at or below 30 % HP, else 0x3036
	bit 4  (0x10)  the command must carry flag 0x10, else 0x3034;
	               the server sets it for a command issued in body mode 6
	               (stealth)
	bit 5  (32)    skill-manager selector bit 0 must be set, else 0x3032
	               (the server's owner-condition check). Its selector install sets it
	               from an active scls skill, and its retirement clears it.

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

Up to five {kind, value} pairs at RefSkill+0x3A0 (a sixth pair is a
fatal load error) and the reqn switch at +0x3B4. The kind picks a
check through the server's byte table:

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

Tags the server's validation reads about the caster's own state:

	nmf  +0x594  may be used while frozen, asleep or stunned
	tele +0x2EC  refused while rooted
	tel2 +0x2F0
	tel3 +0x2F4  refused while rooted
	ao   +0x274  refused while seated
	pw   +0x2B4  refused while seated
	rpkt +0x2CC  refused while an rpkt buff is installed (0x3009)
	qest +0x49C  refused near a skill object or monster, measured
	             against efr kind 3 (+0x294) word 2 (0x3037/0x3038)
	msch +0x4A4  word 0 picks the transform (1) or duple (2) checks
	             (0x3031, 0x3006, 0x3008, 0x3009, 0x3039)
	hide +0x428  word 0: 1 stealth, 2 invisibility, 4 trap; berserk and
	             battle refuse (0x3031, 0x3028)
	trap +0x4A0  lifts the berserk refusal of hide

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
