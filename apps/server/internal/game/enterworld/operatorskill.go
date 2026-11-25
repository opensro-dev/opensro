/*
===========================================================================

operatorskill.go - the operator skills a GM casts without learning them

The v1.150 client's /GMSKILL chat command casts SKILL_OP_HARMONY_A_01
through the ordinary skill press, with no learn and no skill slot (the
client side, with its addresses, is gm-local-commands.ts). The row is
the only SKILL_OP_ row the v1.150 skilldata ships (id 3978, "operator:
harmony therapy"), and no mastery or trainer teaches it, so a cast can
succeed only if the server lets a GM use it unlearned.

Inferred, not read: no v1.188 SR_GameServer binary was available to
confirm the server rule. The inference is the one that makes the
client's own command work: a GM-privileged character may use every
SKILL_OP_ row as if learned; a player never may.

===========================================================================
*/
package enterworld

import "strings"

// operatorSkillPrefix names the operator rows in skilldata column 3.
const operatorSkillPrefix = "SKILL_OP_"

/*
================
SkillUsable

Whether the character may cast the row: it is learned (SkillLearned,
which includes a worn spirit's lent skills), or it is an operator row and
the character holds GM privilege. The admissions this change swapped
ask this, not SkillLearned, so the operator exception lives in one place.
The combat executor's admission (action/skillcombat.go) still asks
SkillLearned: it admits no operator row, and none ships that would reach
it (the one operator row is a field).
================
*/
func SkillUsable(c *Character, skill SkillRow) bool {
	if SkillLearned(c, skill.ID) {
		return true
	}
	return c != nil && c.GMPrivilege && OperatorSkill(skill)
}

/*
================
OperatorSkill

Whether the row is an operator (GM) skill.
================
*/
func OperatorSkill(skill SkillRow) bool {
	return strings.HasPrefix(skill.Codename, operatorSkillPrefix)
}
