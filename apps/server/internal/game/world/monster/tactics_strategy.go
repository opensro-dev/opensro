/*
===========================================================================

tactics_strategy.go - the per-tactics strategy choices of 53FC00

CAITactics_InstallActionCallbacks (53FC00) fills a CTactics' callback table
from its tactics row. Most rows get the ordinary callbacks; the row's flags
and three hard-coded tactics IDs replace the acquisition and target-check
slots. This file names those choices so the AI legs can ask for them.

===========================================================================
*/

package monster

const (
	// tacticsFlagVehicleRedirect (+0x90 & 4) installs 5481A0 at +0x100 and,
	// with tacticsFlagJobRedirect, forces every SetCombatTarget (53FFE0).
	tacticsFlagVehicleRedirect = 0x4
	// tacticsFlagJobRedirect is the low byte's sign bit (+0x90 & 0x80),
	// which installs 548270 at +0x100.
	tacticsFlagJobRedirect = 0x80

	// fixedQueryTacticsTiger, -Uruchi and -Isyutaru are the RefTactics IDs
	// 53FC00 compares (0x21, 0x3F, 0x71): MOB_CH_TIGERWOMAN, MOB_OA_URUCHI
	// and MOB_KK_ISYUTARU.
	fixedQueryTacticsTiger    = 0x21
	fixedQueryTacticsUruchi   = 0x3F
	fixedQueryTacticsIsyutaru = 0x71

	// SquadMemberLimit is how many actors may hold one target without
	// forcing it: CGameWorld vf+0x74 (5EB2E0) returns 6 for every world
	// class v1.150 has (the data-driven worlds that read +0x60 are later).
	SquadMemberLimit = 6

	// monsterAttackRangeParam is parameter 0x21 as CGObjMob_InitializeParameters
	// seeds it (4C39B7). 53F780 adds it to the body radius at +0x160.
	monsterAttackRangeParam = 15
)

/*
==================
FixedQuery

The three uniques whose tactics 53FC00 gives CAITactics_AcquireTargetFixedQuery
(547AD0) and CAITactics_SwitchToSecondaryOpponentBeyondReach (548340). The
flag branches are tested first, so a redirecting row keeps its own callbacks.
==================
*/
func (c TacticsControls) FixedQuery() bool {
	if c.Flags&(tacticsFlagVehicleRedirect|tacticsFlagJobRedirect) != 0 {
		return false
	}
	return c.ID == fixedQueryTacticsTiger || c.ID == fixedQueryTacticsUruchi || c.ID == fixedQueryTacticsIsyutaru
}

/*
==================
ForcesCombatTarget

53FFE0 forces the squad registration of a row with flag 4 or 0x80, whatever
its caller asked for.
==================
*/
func (c TacticsControls) ForcesCombatTarget() bool {
	return c.Flags&(tacticsFlagVehicleRedirect|tacticsFlagJobRedirect) != 0
}

/*
==================
AcquisitionForced

Whether a sight acquisition registers its target past the squad limit:
547AD0 passes force 1 and 5478F0 passes 0, and 53FFE0 forces the flag rows.
==================
*/
func (i Instance) AcquisitionForced() bool {
	if !i.Nest.HasControls {
		return false
	}
	return i.Nest.Controls.FixedQuery() || i.Nest.Controls.ForcesCombatTarget()
}

/*
==================
AttackReachRadius

CTactics +0x160: the body radius (+0x158, a float) plus the truncated attack
range parameter, stored as a float (53F902..53F92A).
==================
*/
func (i Instance) AttackReachRadius() float32 {
	return float32(float64(float32(i.BodyRadius())) + float64(monsterAttackRangeParam))
}
