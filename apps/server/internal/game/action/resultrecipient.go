/*
===========================================================================

resultrecipient.go - what a landed offensive result does to a player

SkillCombat_ApplyResultRecipients (593800) is the one owner that applies a
skill or basic-attack result to each recipient. Its offensive branch
(tagRefSkill_MatchesExecutionSelector) treats a record absorbed by a
standing wall (+0x10, drained from +0x303) separately; every other record
lands. This file is the port's single owner of the recipient side of a
landed record: every path that applies an offensive result to a player
calls offensiveResultRecipient once per landed hit, inside its division
lock and after its commit, so no hit path can forget a rule.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
==================
offensiveResultRecipient

593AE8: a landed, unabsorbed hit on a seated recipient calls
CGObjChar_RequestMotionChange(recipient, 0) and stands it. The native test
also skips a recipient whose flag +0x34D bit 0x40 is set; that flag's owner
was not identified in the v1.188 server and no port state models it. Returns
the frames every observer must see.
==================
*/
func (rt *Runtime) offensiveResultRecipient(division string, recipient *enterworld.Character, nowMs int64) []wire.Frame {
	push, outcome := rt.standUp(division, recipient, nowMs)
	if outcome != standStood {
		return nil
	}
	return []wire.Frame{push}
}

/*
==================
allWallAbsorbed

True when a standing wall absorbed every record of a hit: each record
carries an absorb block (a skipped type-8 record did not absorb).
==================
*/
func allWallAbsorbed(records []wire.SkillCastTargetImpact) bool {
	if len(records) == 0 {
		return false
	}
	for _, record := range records {
		if record.Absorb == nil {
			return false
		}
	}
	return true
}
