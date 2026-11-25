/*
===========================================================================

roster.go - port-only caravan agent profiles for the three-town highway

20 Hunters, 10 Traders and 10 Thieves. Traders and Hunters start idle in
town (the scheduler sends out the standing runs); four Thieves start on the
highway and the rest wait in town for hire.

===========================================================================
*/
package caravanai

import (
	"fmt"
	"strings"
)

const (
	RosterHunters = 20
	RosterTraders = 10
	RosterThieves = 10
	// rosterHighwayThieves start on ambush patrol instead of in town.
	rosterHighwayThieves = 4
)

var (
	traderNames = []string{"Jin", "Mei", "Bao", "Lin", "Hua", "Qiao", "Fen", "Zhou", "Yun", "Tao"}
	hunterNames = []string{"Kwan", "Sora", "Wei", "Lei", "Shan", "Ming", "Gang", "Rui", "Feng", "Long",
		"Hao", "Jun", "Kai", "Bo", "Yi", "Xue", "Lan", "Chen", "Ning", "Tian"}
	thiefNames   = []string{"Viper", "Kael", "Crow", "Rook", "Jackal", "Shade", "Fang", "Moth", "Wisp", "Raven"}
	rosterTowns  = []TownID{TownJangan, TownDonwhang, TownHotan}
	rosterArms   = []string{"ITEM_CH_SPEAR_07_C_RARE", "ITEM_CH_BOW_07_C_RARE", "ITEM_CH_BLADE_07_C_RARE", "ITEM_CH_SWORD_07_C_RARE", "ITEM_CH_TBLADE_07_C_RARE"}
	rosterPeople = []Personality{PersonalityCautious, PersonalityGreedy, PersonalityHonorable, PersonalityVengeful}
)

/*
================
DisplayName

"[AI]Hunter_Kwan" reads as "Kwan" in dialogs.
================
*/
func DisplayName(name string) string {
	if cut := strings.LastIndexByte(name, '_'); cut >= 0 && strings.HasPrefix(name, "[AI]") {
		return name[cut+1:]
	}
	return name
}

/*
================
defaultDivisionRoster
================
*/
func defaultDivisionRoster(division string, heat map[uint16]int) []*AgentUnit {
	var roster []*AgentUnit
	add := func(role AgentRole, name, suit string, i int) *AgentUnit {
		home := rosterTowns[i%len(rosterTowns)]
		at := TownWaypoint(home)
		gender := "M"
		if i%2 == 1 {
			gender = "F"
		}
		unit := &AgentUnit{
			Name:        fmt.Sprintf("[AI]%s_%s", role.Title(), name),
			Division:    division,
			Role:        role,
			Personality: rosterPeople[i%len(rosterPeople)],
			State:       StateIdleInTown,
			HomeTown:    home,
			OriginTown:  home,
			DestTown:    nextTown(home),
			RegionID:    at.RegionID,
			X:           at.X + float32(i%5)*8 - 16,
			Y:           at.Y,
			Z:           at.Z + float32(i/5%4)*8 - 12,
			MaxHP:       12000,
			HP:          12000,
			MaxMP:       6000,
			MP:          6000,
			WeaponCode:  rosterArms[i%len(rosterArms)],
			JobSuitCode: fmt.Sprintf("ITEM_CH_%s_TRADE_%s_02", gender, suit),
		}
		unit.Route = SelectRoute(unit.OriginTown, unit.DestTown, 3, unit.Personality, heat)
		roster = append(roster, unit)
		return unit
	}
	for i := 0; i < RosterTraders; i++ {
		unit := add(RoleTrader, traderNames[i], "TRADER", i)
		unit.CargoStars = uint8(3 + i%3)
		unit.standingCargoStars = unit.CargoStars
		unit.CargoStacks = 10
		unit.StackValueGold = int64(20_000 + 5_000*(i%3))
	}
	for i := 0; i < RosterHunters; i++ {
		unit := add(RoleHunter, hunterNames[i], "HUNTER", i)
		unit.AvailableForHire = true
	}
	for i := 0; i < RosterThieves; i++ {
		unit := add(RoleThief, thiefNames[i], "THIEF", i)
		den := TownWaypoint(TownThiefDen)
		unit.HomeTown, unit.OriginTown, unit.DestTown = TownThiefDen, TownThiefDen, TownThiefDen
		unit.RegionID, unit.X, unit.Y, unit.Z = den.RegionID, den.X+float32(i%5)*8-16, den.Y, den.Z+float32(i/5%2)*8-4
		if i < rosterHighwayThieves {
			unit.State = StateTraveling
			switch i {
			case 0:
				unit.OriginTown, unit.DestTown = TownJangan, TownDonwhang
			case 1:
				unit.OriginTown, unit.DestTown = TownDonwhang, TownHotan
			case 2:
				unit.OriginTown, unit.DestTown = TownDonwhang, TownJangan
			case 3:
				unit.OriginTown, unit.DestTown = TownHotan, TownDonwhang
			}
			unit.Route = SelectRoute(unit.OriginTown, unit.DestTown, 3, unit.Personality, heat)
			if len(unit.Route) > 2 {
				unit.WaypointIndex = 2
				point := unit.Route[2]
				unit.RegionID, unit.X, unit.Y, unit.Z = point.RegionID, point.X, point.Y, point.Z
			}
		} else {
			unit.AvailableForHire = true
		}
	}
	return roster
}

/*
================
nextTown

The default destination of a run leaving town.
================
*/
func nextTown(town TownID) TownID {
	switch town {
	case TownJangan:
		return TownDonwhang
	case TownDonwhang:
		return TownHotan
	default:
		return TownDonwhang
	}
}
