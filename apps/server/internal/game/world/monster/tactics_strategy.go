/*
===========================================================================

tactics_strategy.go - the per-tactics strategy choices

The native tactics installer fills a tactics object's callback table
from its tactics row. Most rows get the ordinary callbacks; the row's
flags and three hard-coded tactics IDs replace the acquisition and
target-check slots. This file names those choices so the AI legs can
ask for them.

DiversionBasis, DiversionKeepBasis and KeepDistance are loaded but
no native code reads them back from a tactics row, so the port
leaves them unread too.

===========================================================================
*/

package monster

const (
	// tacticsFlagVehicleRedirect (the row flag 4) installs the vehicle
	// redirect as the target check and, with tacticsFlagJobRedirect,
	// forces every combat-target assignment.
	tacticsFlagVehicleRedirect = 0x4
	// tacticsFlagJobRedirect is the flag byte's sign bit (0x80), which
	// installs the job redirect as the target check.
	tacticsFlagJobRedirect = 0x80

	// fixedQueryTacticsTiger, -Uruchi and -Isyutaru are the RefTactics
	// IDs the native installer compares (0x21, 0x3F, 0x71):
	// MOB_CH_TIGERWOMAN, MOB_OA_URUCHI and MOB_KK_ISYUTARU.
	fixedQueryTacticsTiger    = 0x21
	fixedQueryTacticsUruchi   = 0x3F
	fixedQueryTacticsIsyutaru = 0x71

	// SquadMemberLimit is how many actors may hold one target without
	// forcing it: the native world query returns 6 for every world
	// kind v1.150 has.
	SquadMemberLimit = 6

	// monsterAttackRangeParam is parameter 0x21 as native mob parameter
	// init seeds it; tactics init adds it to the body radius.
	monsterAttackRangeParam = 15
)

/*
==================
FixedQuery

The three uniques whose tactics the native installer gives the fixed
query and the switch-to-secondary-opponent check. The flag branches
are tested first, so a redirecting row keeps its own callbacks.
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

The native target setter forces the squad registration of a row with
flag 4 or 0x80, whatever its caller asked for.
==================
*/
func (c TacticsControls) ForcesCombatTarget() bool {
	return c.Flags&(tacticsFlagVehicleRedirect|tacticsFlagJobRedirect) != 0
}

/*
==================
OrdinaryTargetCheck

Whether the target check is the ordinary flee-type check, the one
that abandons a target outside the home trace. Flag 4 installs the
vehicle redirect, flag 0x80 the job redirect and the fixed query its
own check in its place.
==================
*/
func (c TacticsControls) OrdinaryTargetCheck() bool {
	return !c.ForcesCombatTarget() && !c.FixedQuery()
}

/*
==================
VehicleRedirect

The target-check redirect a flag row runs: flag 4 takes a trader's,
a hunter's or a state-4 player's vehicle (job states 1, 3, 4); flag
0x80 a thief's or a state-4 player's (job states 2, 4). Flag 4 is
tested first.
==================
*/
func (c TacticsControls) VehicleRedirect() (redirects bool, jobs [3]uint8) {
	if c.Flags&tacticsFlagVehicleRedirect != 0 {
		return true, [3]uint8{1, 3, 4}
	}
	if c.Flags&tacticsFlagJobRedirect != 0 {
		return true, [3]uint8{2, 4, 2}
	}
	return false, jobs
}

/*
==================
AcquisitionForced

Whether a sight acquisition registers its target past the squad
limit: the fixed query passes force, the ordinary query does not,
and the native target setter forces the flag rows.
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

The tactics attack reach: the body radius plus the truncated attack
range parameter, stored as a float.
==================
*/
func (i Instance) AttackReachRadius() float32 {
	return float32(float64(float32(i.BodyRadius())) + float64(monsterAttackRangeParam))
}
