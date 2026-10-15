/*
===========================================================================

mallride.go - Item Mall vehicles and pets move 30% faster

Port-only, not native (owner decision 2026-10-15, at the balance advisor's
request). With SRO_MALL_RIDE_SPEED on, the riding vehicles and the pets
the Item Mall sells walk and run 1.3 times their characterdata speeds, or
the factor the variable names (game/tunables: off, on or 1..3).
Only move speed changes. As with the mall transports (malltransport.go),
the scale lands on the loaded reference rows before any owner reads them,
so the summon, the entry spawn, riding, the pet AI and the speeds
published to clients all agree. Off, the default here, is native; the
GameWorld's Nomad job turns it on.

The families are the COS rows the mall's MALL_PET_A_GROWTH (growth pets),
MALL_PET_B_SILK (pick pets) and MALL_PET_C_VEHICLE (vehicles) shop tabs
summon (refshopgoods -> refpackageitem -> itemdata's COS link). The
transports of MALL_PET_D_CART keep their own rule.

===========================================================================
*/
package enterworld

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/game/tunables"
)

// EnvMallRideSpeed sets the faster mall vehicles and pets (tunables.MallRideSpeed).
const EnvMallRideSpeed = "SRO_MALL_RIDE_SPEED"

// mallRideFamilies are the COS codename families the mall's vehicle and pet
// tabs sell; a family matches its bare codename and its _<digits> level
// rows only, so COS_P_WOLF_WHITE never takes COS_P_WOLF_WHITE_SMALL_*.
var mallRideFamilies = []string{
	// MALL_PET_C_VEHICLE
	"COS_C_OSTRICH", "COS_C_SCARABAEUS", "COS_C_TIGER",
	// MALL_PET_A_GROWTH
	"COS_P_BEAR", "COS_P_WOLF_WHITE", "COS_P_PENGUIN", "COS_P_RAVEN",
	// MALL_PET_B_SILK
	"COS_P_BROWNIE", "COS_P_CAT", "COS_P_GOLDPIG", "COS_P_MYOWON",
	"COS_P_PINKPIG", "COS_P_RABBIT", "COS_P_SEOWON", "COS_P_SPOT_RABBIT",
}

/*
================
MallRideSpeedFromEnv

The speed factor and whether the rule is on. A value the registry refuses
is an error, so the GameWorld's boot stops on it.
================
*/
func MallRideSpeedFromEnv() (float64, bool, error) {
	v, err := tunables.FromEnv(tunables.MallRideSpeed)
	if err != nil {
		return 1, false, err
	}
	return v.Number, v.Enabled, nil
}

/*
================
isMallRide

A COS row (never a transport) of one of the mall's vehicle or pet families.
================
*/
func isMallRide(ref *CharacterRef) bool {
	if ref == nil || ref.TidWord&tidKindMask != tidKindCOS || ref.TidWord>>11 == transportBand {
		return false
	}
	for _, family := range mallRideFamilies {
		if mallRideFamilyRow(ref.Codename, family) {
			return true
		}
	}
	return false
}

/*
================
mallRideFamilyRow

Whether codename is family itself or one of its _<digits> level rows.
================
*/
func mallRideFamilyRow(codename, family string) bool {
	if codename == family {
		return true
	}
	level, ok := strings.CutPrefix(codename, family+"_")
	if !ok || level == "" {
		return false
	}
	for _, r := range level {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

/*
================
ApplyMallRideSpeed

Scales every mall vehicle and pet reference's walk and run speeds by
factor, before any owner reads them; the count is the rows changed.
================
*/
func (t *TextdataItems) ApplyMallRideSpeed(factor float64) (int, error) {
	t.once.Do(t.load)
	if t.archive != nil {
		return 0, fmt.Errorf("%s: references are already archived", EnvMallRideSpeed)
	}
	// The speeds are float32 rows; the old constant scaled them as float32 too.
	scale, scaled := float32(factor), 0
	for _, ref := range t.charactersByCodename {
		if isMallRide(ref) {
			ref.WalkSpeed *= scale
			ref.RunSpeed *= scale
			scaled++
		}
	}
	// The summonable list holds copies of the same rows.
	for i := range t.summonableCharacters {
		if ref := &t.summonableCharacters[i]; isMallRide(ref) {
			ref.WalkSpeed *= scale
			ref.RunSpeed *= scale
		}
	}
	return scaled, nil
}
