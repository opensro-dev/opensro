/*
===========================================================================

highway.go - Jangan <-> Donwhang <-> Hotan trade route graph (port-only)

Defines the Silkroad trade highway between Jangan, Donwhang, and Hotan,
including specialty shop terminals, primary ambush chokepoints (Jangan Ferry
and Karakoram Pass), and the Northern Mountain detour taken when ambush heat
on the Ferry crossing spikes.

===========================================================================
*/
package caravanai

import (
	"math"

	"opensro.online/server/internal/game/world"
)

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
	// TownThiefDen is the Thieves' home, where they wait to be hired (port-only).
	TownThiefDen TownID = "thiefden"
)

const (
	// Classic Silkroad overworld region IDs along the Jangan-Donwhang-Hotan highway.
	RegionJanganTown        uint16 = 25000
	RegionJanganWestGate    uint16 = 24998
	RegionJanganFerryEast   uint16 = 24744
	RegionJanganNorthDetour uint16 = 25257
	RegionDonwhangEastRoad  uint16 = 26010
	RegionDonwhangTown      uint16 = 26265
	RegionDonwhangWestGate  uint16 = 26263
	RegionKarakoramPass     uint16 = 24990
	RegionTaklamakanOasis   uint16 = 24218
	RegionHotanTown         uint16 = 23687

	// DetourHeatThreshold is the ambush danger score at which cautious or
	// high-star (4★-5★) traders bypass the Jangan Ferry chokepoint via the
	// Northern Mountain Detour.
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
type Waypoint struct {
	FerrySourceRef uint32
	FerryTargetID  uint32
	Name           string
	Town           TownID
	SpecialtyShop  string
	RegionID       uint16
	X              float32
	Y              float32
	Z              float32
	Chokepoint     bool
	// Scenic marks a solo Trader's off-road bend; a run stuck on it skips it.
	Scenic bool
}

/*
================================================================================
Terminal waypoints and route construction
================================================================================
*/

var (
	waypointJanganSpecialty = Waypoint{
		Name:          "Jangan Specialty Shop",
		Town:          TownJangan,
		SpecialtyShop: "NPC_CH_SPECIAL",
		RegionID:      RegionJanganTown,
		X:             1760.32,
		Y:             11.0,
		Z:             480.22,
	}
	waypointJanganWestGate = Waypoint{
		Name:     "Jangan West Gate",
		RegionID: RegionJanganWestGate,
		X:        120.0,
		Y:        0,
		Z:        1080.0,
	}
	waypointJanganFerry = Waypoint{
		Name:       "Jangan Ferry Crossing",
		RegionID:   RegionJanganFerryEast,
		X:          405.0,
		Y:          -53.15,
		Z:          545.0,
		Chokepoint: true,
	}
	waypointJanganNorthDetour = Waypoint{
		Name:     "Northern Mountain Detour",
		RegionID: RegionJanganNorthDetour,
		X:        980.0,
		Y:        0,
		Z:        1620.0,
	}
	waypointDonwhangEastRoad = Waypoint{
		Name:     "Donwhang East Approach",
		RegionID: RegionDonwhangEastRoad,
		X:        1420.0,
		Y:        0,
		Z:        1280.0,
	}
	waypointDonwhangSpecialty = Waypoint{
		Name:          "Donwhang Specialty Shop",
		Town:          TownDonwhang,
		SpecialtyShop: "NPC_WC_SPECIAL",
		RegionID:      RegionDonwhangTown,
		X:             393.06,
		Y:             -106.62,
		Z:             1560.31,
	}
	waypointDonwhangWestGate = Waypoint{
		Name:     "Donwhang West Gate",
		RegionID: RegionDonwhangWestGate,
		X:        280.0,
		Y:        -106.0,
		Z:        1050.0,
	}
	waypointKarakoramPass = Waypoint{
		Name:       "Karakoram Pass",
		RegionID:   RegionKarakoramPass,
		X:          1180.0,
		Y:          0,
		Z:          1420.0,
		Chokepoint: true,
	}
	waypointTaklamakanOasis = Waypoint{
		Name:     "Taklamakan Oasis",
		RegionID: RegionTaklamakanOasis,
		X:        1120.0,
		Y:        0,
		Z:        680.0,
	}
	// The Thief Den's stolen-goods buyer (npcpos, NPC_TD_THIEF_BUY).
	waypointThiefDen = Waypoint{
		Name:          "Thief Den",
		Town:          TownThiefDen,
		SpecialtyShop: "NPC_TD_THIEF_BUY",
		RegionID:      24758,
		X:             953.42,
		Y:             3.66,
		Z:             1227.63,
	}
	waypointHotanSpecialty = Waypoint{
		Name:          "Hotan Specialty Shop",
		Town:          TownHotan,
		SpecialtyShop: "NPC_KT_SPECIAL",
		RegionID:      RegionHotanTown,
		X:             1515.55,
		Y:             243.99,
		Z:             904.55,
	}
)

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
	case TownThiefDen:
		return waypointThiefDen
	default:
		return waypointJanganSpecialty
	}
}

/*
==================
SelectRoute

SelectRoute returns the ordered slice of waypoints from origin town to
destination town. When the Jangan Ferry crossing has accumulated high ambush
heat (>= DetourHeatThreshold) and the agent is Cautious or hauling a 4★-5★
load, the route automatically diverts through the Northern Mountain Detour.
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
	useDetour := shouldTakeNorthernDetour(cargoStars, personality, heat)

	if from == TownJangan && to == TownDonwhang {
		return legJanganToDonwhang(useDetour)
	}
	if from == TownDonwhang && to == TownJangan {
		return reverseWaypoints(legJanganToDonwhang(useDetour))
	}
	if from == TownDonwhang && to == TownHotan {
		return legDonwhangToHotan()
	}
	if from == TownHotan && to == TownDonwhang {
		return reverseWaypoints(legDonwhangToHotan())
	}
	if from == TownJangan && to == TownHotan {
		first := legJanganToDonwhang(useDetour)
		second := legDonwhangToHotan()
		return append(first, second[1:]...)
	}
	if from == TownHotan && to == TownJangan {
		first := reverseWaypoints(legDonwhangToHotan())
		second := reverseWaypoints(legJanganToDonwhang(useDetour))
		return append(first, second[1:]...)
	}
	return []Waypoint{TownWaypoint(from), TownWaypoint(to)}
}

func shouldTakeNorthernDetour(cargoStars uint8, personality Personality, heat map[uint16]int) bool {
	if heat == nil {
		return false
	}
	ferryHeat := heat[RegionJanganFerryEast]
	if ferryHeat < DetourHeatThreshold {
		return false
	}
	return personality == PersonalityCautious || cargoStars >= 4
}

func legJanganToDonwhang(useDetour bool) []Waypoint {
	crossing := waypointJanganFerry
	if useDetour {
		crossing = waypointJanganNorthDetour
	}
	return []Waypoint{
		waypointJanganSpecialty,
		waypointJanganWestGate,
		crossing,
		waypointDonwhangEastRoad,
		waypointDonwhangSpecialty,
	}
}

func legDonwhangToHotan() []Waypoint {
	return []Waypoint{
		waypointDonwhangSpecialty,
		waypointDonwhangWestGate,
		waypointKarakoramPass,
		waypointTaklamakanOasis,
		waypointHotanSpecialty,
	}
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
maxStep world units and reports whether the target was reached.
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

/*
==================
StepWaypoint

Advances (curRegion, curX, curZ) across the continuous outdoor sector grid
toward target by at most maxStep world units, folding the resulting position
into its canonical (RegionID, localX, localZ) frame via world.NormalizeOutdoor.
==================
*/
func StepWaypoint(
	curRegion uint16,
	curX, curZ float32,
	target Waypoint,
	maxStep float32,
) (nextRegion uint16, nextX, nextZ float32, arrived bool) {
	if maxStep <= 0 {
		return curRegion, curX, curZ, false
	}
	from := world.RegionXZ{RegionID: curRegion, X: float64(curX), Z: float64(curZ)}
	to := world.RegionXZ{RegionID: target.RegionID, X: float64(target.X), Z: float64(target.Z)}
	dx, dz := world.Delta(from, to)
	dist := math.Hypot(dx, dz)
	if dist <= float64(maxStep) {
		return target.RegionID, target.X, target.Z, true
	}
	ratio := float64(maxStep) / dist
	folded := world.NormalizeOutdoor(world.RegionXZ{
		RegionID: curRegion,
		X:        from.X + dx*ratio,
		Z:        from.Z + dz*ratio,
	})
	return folded.RegionID, float32(folded.X), float32(folded.Z), false
}
