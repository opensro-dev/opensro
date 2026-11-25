/*
===========================================================================

operatorskill.go - the operator skills a GM casts without learning them

The v1.150 client's /GMSKILL arm (ChatInput_HandleGmCommand 0x509CF0,
cast at @0x509E1D through CGInterface_ExecuteSelectedActionAtTarget
6FCD50) casts SKILL_OP_HARMONY_A_01 through the ordinary skill press,
with no learn and no skill slot. The row is the only SKILL_OP_ row the
v1.150 skilldata ships (id 3978, "operator: harmony therapy"), and no
mastery or trainer teaches it, so a cast can succeed only if the server
lets a GM use it unlearned.

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
the character holds GM privilege. Every cast admission asks this, not
SkillLearned, so the operator exception lives in one place.
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
