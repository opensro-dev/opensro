/*
===========================================================================

highway.go - Jangan <-> Donwhang <-> Hotan trade route graph (port-only)

Defines the Silkroad trade highway between Jangan, Donwhang, and Hotan as
anchor chains over the shipped world. The three terminals are the specialty
trader NPC positions and the four crossings are the shipped ferry portal
pairs; every consecutive pair of anchors is walked by the monster march
(PlanMonsterPath) on the shipped nav surface, proven by the world-data
test in internal/game/world/movement. An anchor's Y is a spawn height
(the three terminals); elsewhere it is 0 and the walking pose carries
the march surface height instead (see Walker in runtime.go).

Port-only, not native: the original game has no AI caravans. The detour rule
(cautious or 4-5 star agents avoid a chokepoint whose ambush heat reached
DetourHeatThreshold) is a port invention kept from slice 1.

===========================================================================
*/
package caravanai

import "math"

/*
================================================================================
Towns, regions, and personalities
================================================================================
*/

// TownID identifies one of the three classic Silkroad trade terminals.
type TownID string

const (
	TownJangan   TownID = "jangan"
	TownDonwhang TownID = "donwhang"
	TownHotan    TownID = "hotan"
)

const (
	// Town regions holding the specialty traders (NPC_CH_SPECIAL,
	// NPC_WC_SPECIAL, NPC_KT_SPECIAL in the shipped client data).
	RegionJanganTown   uint16 = 25000
	RegionDonwhangTown uint16 = 26265
	RegionHotanTown    uint16 = 23687

	// Ferry landing regions: the shipped portal pairs GATE_NPC_CH_FERRY
	// <-> GATE_NPC_WC_FERRY (the main Jangan crossing),
	// GATE_NPC_CH_FERRY2 <-> GATE_NPC_WC_FERRY2 (the northern detour),
	// GATE_NPC_WC_FERRY3 <-> GATE_NPC_KT_FERRY2 (the main Hotan
	// crossing) and GATE_NPC_WC_FERRY4 <-> GATE_NPC_KT_FERRY3 (the
	// far-west alternate).
	RegionJanganFerry          uint16 = 24993
	RegionDonwhangFerry        uint16 = 25761
	RegionJanganFerryNorth     uint16 = 24734
	RegionDonwhangFerryNorth   uint16 = 25244
	RegionDonwhangFerryWest    uint16 = 23439
	RegionHotanFerryWest       uint16 = 23436
	RegionDonwhangFerryFarWest uint16 = 23183
	RegionHotanFerryFarWest    uint16 = 23180

	// DetourHeatThreshold is the ambush danger score at which cautious or
	// high-star (4-5 star) traders bypass a hot chokepoint crossing for
	// its alternate.
	DetourHeatThreshold = 50
)

// Personality shapes an agent's route risk tolerance, parley generosity,
// and combat/social dialogue style.
type Personality string

const (
	PersonalityCautious  Personality = "cautious"
	PersonalityGreedy    Personality = "greedy"
	PersonalityVengeful  Personality = "vengeful"
	PersonalityHonorable Personality = "honorable"
)

// Waypoint is a single navigation anchor along the Silkroad trade highway.
// X/Z are region-local. Ferry marks a ferry landing: when two Ferry anchors
// stand next to each other in a route, the step between them is the ferry
// ride itself, not a walk (runtime.go). Chokepoint marks the landing whose
// region key carries the crossing's ambush heat.
type Waypoint struct {
	Name          string
	Town          TownID
	SpecialtyShop string
	RegionID      uint16
	X             float32
	Y             float32
	Z             float32
	Ferry         bool
	Chokepoint    bool
}

/*
================================================================================
Terminals and ferry landings (shipped NPC positions)
================================================================================
*/

var (
	waypointJanganSpecialty = Waypoint{
		Name:          "Jangan Specialty Shop",
		Town:          TownJangan,
		SpecialtyShop: "NPC_CH_SPECIAL",
		RegionID:      RegionJanganTown,
		X:             1760.32,
		Y:             0,
		Z:             480.22,
	}
	waypointDonwhangSpecialty = Waypoint{
		Name:          "Donwhang Specialty Shop",
		Town:          TownDonwhang,
		SpecialtyShop: "NPC_WC_SPECIAL",
		RegionID:      RegionDonwhangTown,
		X:             393.06,
		Y:             -106.6,
		Z:             1560.31,
	}
	waypointHotanSpecialty = Waypoint{
		Name:          "Hotan Specialty Shop",
		Town:          TownHotan,
		SpecialtyShop: "NPC_KT_SPECIAL",
		RegionID:      RegionHotanTown,
		X:             1515.55,
		Y:             244.0,
		Z:             904.55,
	}

	waypointJanganFerry = Waypoint{
		Name:       "Jangan Ferry (GATE_NPC_CH_FERRY)",
		RegionID:   RegionJanganFerry,
		X:          362.30,
		Z:          1761.33,
		Ferry:      true,
		Chokepoint: true,
	}
	waypointDonwhangFerry = Waypoint{
		Name:     "Donwhang Ferry (GATE_NPC_WC_FERRY)",
		RegionID: RegionDonwhangFerry,
		X:        513.16,
		Z:        1281.35,
		Ferry:    true,
	}
	waypointJanganFerryNorth = Waypoint{
		Name:     "Northern Ferry (GATE_NPC_CH_FERRY2)",
		RegionID: RegionJanganFerryNorth,
		X:        330.34,
		Z:        1606.66,
		Ferry:    true,
	}
	waypointDonwhangFerryNorth = Waypoint{
		Name:     "Northern Ferry (GATE_NPC_WC_FERRY2)",
		RegionID: RegionDonwhangFerryNorth,
		X:        919.72,
		Z:        366.88,
		Ferry:    true,
	}
	waypointDonwhangFerryWest = Waypoint{
		Name:       "West Ferry (GATE_NPC_WC_FERRY3)",
		RegionID:   RegionDonwhangFerryWest,
		X:          323.36,
		Z:          1747.85,
		Ferry:      true,
		Chokepoint: true,
	}
	waypointHotanFerryWest = Waypoint{
		Name:     "Hotan West Ferry (GATE_NPC_KT_FERRY2)",
		RegionID: RegionHotanFerryWest,
		X:        1189.40,
		Z:        1324.57,
		Ferry:    true,
	}
	waypointDonwhangFerryFarWest = Waypoint{
		Name:     "Far West Ferry (GATE_NPC_WC_FERRY4)",
		RegionID: RegionDonwhangFerryFarWest,
		X:        275.92,
		Z:        888.49,
		Ferry:    true,
	}
	waypointHotanFerryFarWest = Waypoint{
		Name:     "Hotan Far West Ferry (GATE_NPC_KT_FERRY3)",
		RegionID: RegionHotanFerryFarWest,
		X:        1644.16,
		Z:        752.79,
		Ferry:    true,
	}
)

/*
================================================================================
Anchor chains between stops

Each chain runs from one named stop to the next. The intermediates are
march-proven points on the shipped nav surface (planner points thinned
to anchors, with the march's own stepping stones kept where a hop needs
more than one chord); the world-data test re-proves every hop, in both
directions, whenever the data changes.
================================================================================
*/

var chainJanganToFerry = []Waypoint{
	waypointJanganSpecialty,
	{RegionID: 25000, X: 960, Z: 0},
	{RegionID: 24744, X: 960, Z: 1600},
	{RegionID: 24744, X: 480, Z: 1440},
	{RegionID: 24741, X: 320, Z: 480},
	{RegionID: 24741, X: 320, Z: 960},
	{RegionID: 24741, X: 480, Z: 1120},
	{RegionID: 24997, X: 800, Z: 160},
	{RegionID: 24997, X: 640, Z: 480},
	{RegionID: 24996, X: 1440, Z: 640},
	{RegionID: 24224, X: 960, Z: 1440},
	{RegionID: 24224, X: 160, Z: 1280},
	{RegionID: 24223, X: 1760, Z: 1280},
	{RegionID: 24223, X: 1280, Z: 1760},
	{RegionID: 24479, X: 1600, Z: 1120},
	waypointJanganFerry,
}

var chainFerryToDonwhang = []Waypoint{
	waypointDonwhangFerry,
	{RegionID: 25761, X: 480, Z: 1280},
	{RegionID: 25761, X: 320, Z: 1440},
	{RegionID: 26013, X: 160, Z: 0},
	{RegionID: 26012, X: 1760, Z: 320},
	{RegionID: 26012, X: 1440, Z: 640},
	{RegionID: 26266, X: 960, Z: 1120},
	{RegionID: 26266, X: 896, Z: 1120},
	{RegionID: 26522, X: 0, Z: 0},
	{RegionID: 26265, X: 1120, Z: 1760},
	waypointDonwhangSpecialty,
}

var chainJanganToNorthFerry = []Waypoint{
	waypointJanganSpecialty,
	{RegionID: 25000, X: 960, Z: 0},
	{RegionID: 24744, X: 960, Z: 1600},
	{RegionID: 24744, X: 480, Z: 1440},
	{RegionID: 24741, X: 720, Z: 480},
	{RegionID: 24741, X: 400, Z: 320},
	{RegionID: 24741, X: 400, Z: 480},
	{RegionID: 24741, X: 320, Z: 560},
	{RegionID: 24741, X: 320, Z: 960},
	{RegionID: 24741, X: 400, Z: 1040},
	{RegionID: 24997, X: 800, Z: 160},
	{RegionID: 24997, X: 720, Z: 320},
	{RegionID: 24997, X: 640, Z: 400},
	{RegionID: 24997, X: 80, Z: 560},
	{RegionID: 24995, X: 560, Z: 560},
	{RegionID: 24481, X: 80, Z: 240},
	{RegionID: 24224, X: 1600, Z: 1840},
	{RegionID: 24224, X: 640, Z: 1440},
	{RegionID: 24224, X: 160, Z: 1280},
	{RegionID: 24222, X: 1200, Z: 1760},
	{RegionID: 24478, X: 880, Z: 80},
	waypointJanganFerryNorth,
}

var chainNorthFerryToDonwhang = []Waypoint{
	waypointDonwhangFerryNorth,
	{RegionID: 25500, X: 24, Z: 367},
	{RegionID: 25499, X: 1272, Z: 1679},
	{RegionID: 25754, X: 280, Z: 815},
	{RegionID: 25754, X: 24, Z: 1039},
	{RegionID: 25753, X: 1880, Z: 1039},
	{RegionID: 25753, X: 1432, Z: 1647},
	{RegionID: 26009, X: 1304, Z: 207},
	{RegionID: 26265, X: 632, Z: 1487},
	{RegionID: 26265, X: 600, Z: 1519},
	waypointDonwhangSpecialty,
}

var chainDonwhangToFerryWest = []Waypoint{
	waypointDonwhangSpecialty,
	{RegionID: 26265, X: 1120, Z: 1664},
	{RegionID: 26265, X: 1632, Z: 1664},
	{RegionID: 26265, X: 1728, Z: 1568},
	{RegionID: 26265, X: 1760, Z: 1600},
	{RegionID: 26521, X: 1760, Z: 0},
	{RegionID: 26522, X: 0, Z: 0},
	{RegionID: 26266, X: 0, Z: 0},
	{RegionID: 26009, X: 1280, Z: 320},
	{RegionID: 25753, X: 1120, Z: 320},
	{RegionID: 25496, X: 1600, Z: 1120},
	{RegionID: 25495, X: 1760, Z: 800},
	{RegionID: 25495, X: 800, Z: 320},
	{RegionID: 25239, X: 160, Z: 1760},
	{RegionID: 24981, X: 1440, Z: 1120},
	{RegionID: 24981, X: 1440, Z: 960},
	{RegionID: 24980, X: 800, Z: 480},
	{RegionID: 24980, X: 352, Z: 32},
	{RegionID: 24723, X: 1440, Z: 1120},
	{RegionID: 24723, X: 1440, Z: 640},
	{RegionID: 24467, X: 640, Z: 1120},
	{RegionID: 24210, X: 1120, Z: 1600},
	{RegionID: 24210, X: 160, Z: 1600},
	{RegionID: 24209, X: 320, Z: 1280},
	{RegionID: 24208, X: 1280, Z: 320},
	{RegionID: 23696, X: 1280, Z: 1440},
	{RegionID: 23696, X: 960, Z: 960},
	{RegionID: 23696, X: 160, Z: 320},
	{RegionID: 23695, X: 1280, Z: 160},
	{RegionID: 23439, X: 960, Z: 1760},
	waypointDonwhangFerryWest,
}

var chainFerryWestToHotan = []Waypoint{
	waypointHotanFerryWest,
	{RegionID: 23689, X: 421, Y: 243.8, Z: 429},
	{RegionID: 23688, X: 165, Y: 243.8, Z: 461},
	waypointHotanSpecialty,
}

var chainDonwhangToFerryFarWest = []Waypoint{
	waypointDonwhangSpecialty,
	{RegionID: 26265, X: 1120, Z: 1664},
	{RegionID: 26265, X: 1632, Z: 1664},
	{RegionID: 26265, X: 1728, Z: 1568},
	{RegionID: 26265, X: 1760, Z: 1600},
	{RegionID: 26521, X: 1760, Z: 0},
	{RegionID: 26522, X: 0, Z: 0},
	{RegionID: 26266, X: 0, Z: 0},
	{RegionID: 26009, X: 1280, Z: 320},
	{RegionID: 25753, X: 1120, Z: 320},
	{RegionID: 25496, X: 1600, Z: 1120},
	{RegionID: 25495, X: 1760, Z: 800},
	{RegionID: 25495, X: 800, Z: 320},
	{RegionID: 25239, X: 160, Z: 1760},
	{RegionID: 24981, X: 1440, Z: 1120},
	{RegionID: 24981, X: 1440, Z: 960},
	{RegionID: 24980, X: 800, Z: 480},
	{RegionID: 24980, X: 352, Z: 32},
	{RegionID: 24723, X: 1440, Z: 1120},
	{RegionID: 24723, X: 1440, Z: 640},
	{RegionID: 24467, X: 640, Z: 1120},
	{RegionID: 24210, X: 1120, Z: 1600},
	{RegionID: 24210, X: 160, Z: 1600},
	{RegionID: 24209, X: 320, Z: 1280},
	{RegionID: 24208, X: 1760, Z: 800},
	{RegionID: 23952, X: 1280, Z: 1280},
	{RegionID: 23696, X: 1280, Z: 1440},
	{RegionID: 23183, X: 480, Z: 1440},
	{RegionID: 23183, X: 480, Z: 960},
	waypointDonwhangFerryFarWest,
}

var chainFerryFarWestToHotan = []Waypoint{
	waypointHotanFerryFarWest,
	{RegionID: 23180, X: 1516, Z: 785},
	{RegionID: 23689, X: 460, Z: 433},
	{RegionID: 23689, X: 492, Z: 433},
	{RegionID: 23688, X: 172, Z: 465},
	waypointHotanSpecialty,
}

/*
==================
TownWaypoint

TownWaypoint returns the specialty shop anchor for the requested town.
==================
*/
func TownWaypoint(town TownID) Waypoint {
	switch town {
	case TownDonwhang:
		return waypointDonwhangSpecialty
	case TownHotan:
		return waypointHotanSpecialty
	default:
		return waypointJanganSpecialty
	}
}

/*
==================
SelectRoute

SelectRoute returns the ordered anchor chain from origin town to
destination town. When a crossing's chokepoint has accumulated high ambush
heat (>= DetourHeatThreshold) and the agent is Cautious or hauling a 4-5
star load, the leg diverts to the alternate ferry crossing.
==================
*/
func SelectRoute(
	from TownID,
	to TownID,
	cargoStars uint8,
	personality Personality,
	heat map[uint16]int,
) []Waypoint {
	if from == to {
		return []Waypoint{TownWaypoint(from)}
	}
	useDetour := shouldAvoidChokepoint(cargoStars, personality, heat, RegionJanganFerry)
	useFarWest := shouldAvoidChokepoint(cargoStars, personality, heat, RegionDonwhangFerryWest)

	if from == TownJangan && to == TownDonwhang {
		return legJanganToDonwhang(useDetour)
	}
	if from == TownDonwhang && to == TownJangan {
		return reverseWaypoints(legJanganToDonwhang(useDetour))
	}
	if from == TownDonwhang && to == TownHotan {
		return legDonwhangToHotan(useFarWest)
	}
	if from == TownHotan && to == TownDonwhang {
		return reverseWaypoints(legDonwhangToHotan(useFarWest))
	}
	if from == TownJangan && to == TownHotan {
		first := legJanganToDonwhang(useDetour)
		second := legDonwhangToHotan(useFarWest)
		return append(first, second[1:]...)
	}
	if from == TownHotan && to == TownJangan {
		first := reverseWaypoints(legDonwhangToHotan(useFarWest))
		second := reverseWaypoints(legJanganToDonwhang(useDetour))
		return append(first, second[1:]...)
	}
	return []Waypoint{TownWaypoint(from), TownWaypoint(to)}
}

/*
==================
shouldAvoidChokepoint

Whether an agent with this cargo and temperament leaves a crossing whose
chokepoint region carries heat at or past DetourHeatThreshold.
==================
*/
func shouldAvoidChokepoint(cargoStars uint8, personality Personality, heat map[uint16]int, chokepointRegion uint16) bool {
	if heat[chokepointRegion] < DetourHeatThreshold {
		return false
	}
	return personality == PersonalityCautious || cargoStars >= 4
}

func legJanganToDonwhang(useDetour bool) []Waypoint {
	if useDetour {
		return joinChains(chainJanganToNorthFerry, chainNorthFerryToDonwhang)
	}
	return joinChains(chainJanganToFerry, chainFerryToDonwhang)
}

func legDonwhangToHotan(useFarWest bool) []Waypoint {
	if useFarWest {
		return joinChains(chainDonwhangToFerryFarWest, chainFerryFarWestToHotan)
	}
	return joinChains(chainDonwhangToFerryWest, chainFerryWestToHotan)
}

// joinChains concatenates two anchor chains into a fresh route slice.
func joinChains(a, b []Waypoint) []Waypoint {
	out := make([]Waypoint, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

func reverseWaypoints(src []Waypoint) []Waypoint {
	out := make([]Waypoint, len(src))
	for i := range src {
		out[i] = src[len(src)-1-i]
	}
	return out
}

/*
==================
StepPosition

StepPosition advances (curX, curZ) toward (targetX, targetZ) by at most
maxStep world units and reports whether the target was reached. The
slice-2a runtime still glides on this straight line; the nav-surface
walker replaces it in slice 2b.
==================
*/
func StepPosition(
	curX, curZ float32,
	targetX, targetZ float32,
	maxStep float32,
) (nextX, nextZ float32, arrived bool) {
	if maxStep <= 0 {
		return curX, curZ, false
	}
	dx := float64(targetX - curX)
	dz := float64(targetZ - curZ)
	dist := math.Hypot(dx, dz)
	if dist <= float64(maxStep) {
		return targetX, targetZ, true
	}
	ratio := float64(maxStep) / dist
	return curX + float32(dx*ratio), curZ + float32(dz*ratio), false
}
