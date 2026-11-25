/*
===========================================================================

wiring_caravanroutes.go - caravan terminals and crossings from shipped data

Town shops and ferry managers come from the live NPC roster; crossing
arrivals come from the validated teleport catalogue, including return legs.

===========================================================================
*/
package main

import (
	"fmt"
	"math"
	"opensro.online/server/internal/game/caravanai"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
caravanFerry
================
*/
func (game *gameplayPlane) caravanFerry(division, name string, source, target uint32) (caravanai.Waypoint, bool) {
	at, ok := game.items.CrossServerActorFerry(division, name, source, target)
	return caravanai.Waypoint{RegionID: at.RegionID, X: float32(at.X), Y: float32(at.Y), Z: float32(at.Z)}, ok
}

/*
================
configureCaravanHighway
================
*/
func (game *gameplayPlane) configureCaravanHighway(cfg *caravanai.Config) error {
	cfg.Terminals = make(map[caravanai.TownID]caravanai.Waypoint)
	for _, npc := range game.items.NpcRoster {
		var town caravanai.TownID
		switch npc.Codename {
		case "NPC_CH_SPECIAL":
			town = caravanai.TownJangan
		case "NPC_WC_SPECIAL":
			town = caravanai.TownDonwhang
		case "NPC_KT_SPECIAL":
			town = caravanai.TownHotan
		default:
			continue
		}
		cfg.Terminals[town] = caravanai.Waypoint{Town: town, Name: npc.Name, SpecialtyShop: npc.Codename, RegionID: npc.Spawn.RegionID, X: float32(npc.Spawn.X), Y: float32(npc.Spawn.Y), Z: float32(npc.Spawn.Z)}
	}
	if len(cfg.Terminals) != 3 {
		return fmt.Errorf("three shipped specialty shops are required")
	}
	// The Thieves' home: an open walkable courtyard spot in front of the
	// Thief Den's stolen-goods buyer (the NPC itself stands against the stairs).
	for _, npc := range game.items.NpcRoster {
		if npc.Codename == caravanThiefDenNPC {
			raw := caravanai.Waypoint{Town: caravanai.TownThiefDen, Name: npc.Name, SpecialtyShop: npc.Codename, RegionID: npc.Spawn.RegionID, X: float32(npc.Spawn.X), Y: float32(npc.Spawn.Y), Z: float32(npc.Spawn.Z)}
			cfg.Terminals[caravanai.TownThiefDen] = game.caravanWalkableNear(raw)
			break
		}
	}
	var desertPost, tigerPost caravanai.Waypoint
	for _, npc := range game.items.NpcRoster {
		switch npc.Codename {
		case "NPC_WC_SPECIAL2":
			desertPost = caravanai.Waypoint{Name: npc.Name, RegionID: npc.Spawn.RegionID, X: float32(npc.Spawn.X), Y: float32(npc.Spawn.Y), Z: float32(npc.Spawn.Z)}
		case "NPC_CH_SPECIAL2":
			tigerPost = caravanai.Waypoint{Name: npc.Name, RegionID: npc.Spawn.RegionID, X: float32(npc.Spawn.X), Y: float32(npc.Spawn.Y), Z: float32(npc.Spawn.Z), Scenic: true}
		}
	}
	if desertPost.RegionID == 0 {
		return fmt.Errorf("desert trade road post unavailable")
	}
	cfg.Highway = make(map[[2]caravanai.TownID][]caravanai.Waypoint)
	cfg.Detours = make(map[[2]caravanai.TownID][]caravanai.Waypoint)
	cfg.Routes = make(map[[2]caravanai.TownID][][]caravanai.Waypoint)
	gates := make(map[[2]caravanai.TownID][]caravanai.Waypoint)
	for _, entry := range []struct {
		from, to caravanai.TownID
		codes    [2]string
		outsideX float64
	}{
		{caravanai.TownJangan, caravanai.TownDonwhang, [2]string{"NPC_CH_SOLDIER_WE1", "NPC_CH_SOLDIER_WE2"}, -128},
		{caravanai.TownDonwhang, caravanai.TownJangan, [2]string{"NPC_WC_SOLDIER_EA1", "NPC_WC_SOLDIER_EA2"}, 128},
		{caravanai.TownDonwhang, caravanai.TownHotan, [2]string{"NPC_WC_SOLDIER_WE1", "NPC_WC_SOLDIER_WE2"}, -128},
		{caravanai.TownHotan, caravanai.TownDonwhang, [2]string{"NPC_KT_SOLDIER_EA1", "NPC_KT_SOLDIER_EA2"}, 128},
	} {
		positions := []caravanai.Waypoint{}
		for _, code := range entry.codes {
			for _, npc := range game.items.NpcRoster {
				if npc.Codename == code {
					positions = append(positions, caravanai.Waypoint{RegionID: npc.Spawn.RegionID, X: float32(npc.Spawn.X), Y: float32(npc.Spawn.Y), Z: float32(npc.Spawn.Z)})
					break
				}
			}
		}
		if len(positions) != 2 {
			return fmt.Errorf("caravan gate guards %v unavailable", entry.codes)
		}
		base := worldgeom.RegionXZ{RegionID: positions[0].RegionID, X: float64(positions[0].X), Z: float64(positions[0].Z)}
		dx, dz := worldgeom.Delta(base, worldgeom.RegionXZ{RegionID: positions[1].RegionID, X: float64(positions[1].X), Z: float64(positions[1].Z)})
		middle := worldgeom.NormalizeOutdoor(worldgeom.RegionXZ{RegionID: base.RegionID, X: base.X + dx/2, Z: base.Z + dz/2})
		gate := caravanai.Waypoint{Name: string(entry.from) + " road gate", RegionID: middle.RegionID, X: float32(middle.X), Y: (positions[0].Y + positions[1].Y) / 2, Z: float32(middle.Z)}
		outside := worldgeom.NormalizeOutdoor(worldgeom.RegionXZ{RegionID: middle.RegionID, X: middle.X + entry.outsideX, Z: middle.Z})
		gates[[2]caravanai.TownID{entry.from, entry.to}] = []caravanai.Waypoint{gate, {Name: string(entry.from) + " road", RegionID: outside.RegionID, X: float32(outside.X), Y: gate.Y, Z: float32(outside.Z)}}
	}
	links := []struct {
		from, to                                         caravanai.TownID
		source, target, alternateSource, alternateTarget string
	}{
		{caravanai.TownJangan, caravanai.TownDonwhang, "GATE_NPC_CH_FERRY", "GATE_NPC_WC_FERRY", "GATE_NPC_CH_FERRY2", "GATE_NPC_WC_FERRY2"},
		{caravanai.TownDonwhang, caravanai.TownJangan, "GATE_NPC_WC_FERRY", "GATE_NPC_CH_FERRY", "GATE_NPC_WC_FERRY2", "GATE_NPC_CH_FERRY2"},
		{caravanai.TownDonwhang, caravanai.TownHotan, "GATE_NPC_WC_FERRY3", "GATE_NPC_KT_FERRY2", "GATE_NPC_WC_FERRY4", "GATE_NPC_KT_FERRY3"},
		{caravanai.TownHotan, caravanai.TownDonwhang, "GATE_NPC_KT_FERRY2", "GATE_NPC_WC_FERRY3", "GATE_NPC_KT_FERRY3", "GATE_NPC_WC_FERRY4"},
	}
	for _, link := range links {
		for variant, pair := range [][2]string{{link.source, link.target}, {link.alternateSource, link.alternateTarget}} {
			npc, target, arrival, ok := game.items.ServerActorFerryRoute(pair[0], pair[1])
			if !ok {
				return fmt.Errorf("caravan ferry %s -> %s unavailable", pair[0], pair[1])
			}
			points := []caravanai.Waypoint{cfg.Terminals[link.from]}
			points = append(points, gates[[2]caravanai.TownID{link.from, link.to}]...)
			if link.from == caravanai.TownDonwhang && link.to == caravanai.TownHotan {
				points = append(points, desertPost)
			}
			points = append(points,
				caravanai.Waypoint{Name: npc.Name, RegionID: npc.Spawn.RegionID, X: float32(npc.Spawn.X), Y: float32(npc.Spawn.Y), Z: float32(npc.Spawn.Z), Chokepoint: true},
				caravanai.Waypoint{Name: pair[1], FerrySourceRef: npc.RefObjID, FerryTargetID: target, RegionID: arrival.RegionID, X: float32(arrival.X), Y: float32(arrival.Y), Z: float32(arrival.Z)})
			if link.from == caravanai.TownHotan && link.to == caravanai.TownDonwhang {
				points = append(points, desertPost)
			}
			entry := gates[[2]caravanai.TownID{link.to, link.from}]
			for i := len(entry) - 1; i >= 0; i-- {
				points = append(points, entry[i])
			}
			points = append(points, cfg.Terminals[link.to])
			key := [2]caravanai.TownID{link.from, link.to}
			if variant == 0 {
				cfg.Highway[key] = points
			} else {
				cfg.Detours[key] = points
			}
			cfg.Routes[key] = append(cfg.Routes[key], points)
		}
	}
	// Additional named trade routes so caravans spread across Dead Ravine,
	// Central-Tarim, Highland Oasis, and Tiger Mountain instead of all taking
	// one highway line.
	buildRoute := func(from, to caravanai.TownID, midBeforeFerry []caravanai.Waypoint, ferryFrom, ferryTo string, midAfterFerry []caravanai.Waypoint) ([]caravanai.Waypoint, bool) {
		npc, target, arrival, ok := game.items.ServerActorFerryRoute(ferryFrom, ferryTo)
		if !ok {
			return nil, false
		}
		points := []caravanai.Waypoint{cfg.Terminals[from]}
		points = append(points, gates[[2]caravanai.TownID{from, to}]...)
		points = append(points, midBeforeFerry...)
		points = append(points,
			caravanai.Waypoint{Name: npc.Name, RegionID: npc.Spawn.RegionID, X: float32(npc.Spawn.X), Y: float32(npc.Spawn.Y), Z: float32(npc.Spawn.Z), Chokepoint: true},
			caravanai.Waypoint{Name: ferryTo, FerrySourceRef: npc.RefObjID, FerryTargetID: target, RegionID: arrival.RegionID, X: float32(arrival.X), Y: float32(arrival.Y), Z: float32(arrival.Z)})
		points = append(points, midAfterFerry...)
		entry := gates[[2]caravanai.TownID{to, from}]
		for i := len(entry) - 1; i >= 0; i-- {
			points = append(points, entry[i])
		}
		points = append(points, cfg.Terminals[to])
		return points, true
	}
	deadRavineNorth := game.caravanWalkableWaypoint("Dead Ravine North", 24466)
	deadRavineMid := game.caravanWalkableWaypoint("Dead Ravine", 24209)
	deadRavineSouth := game.caravanWalkableWaypoint("Dead Ravine South", 23952)
	highlandOasis := game.caravanWalkableWaypoint("Highland Oasis", 24470)
	centralTarim := game.caravanWalkableWaypoint("Central-Tarim", 24211)
	northTigerMt := game.caravanWalkableWaypoint("North-Tiger Mt.", 24480)

	// Donwhang <-> Hotan: Dead Ravine pass (both ferries) + Central-Tarim.
	if deadRavineMid.RegionID != 0 && deadRavineSouth.RegionID != 0 {
		ravineDWToHT := []caravanai.Waypoint{}
		if highlandOasis.RegionID != 0 {
			ravineDWToHT = append(ravineDWToHT, highlandOasis)
		}
		if deadRavineNorth.RegionID != 0 {
			ravineDWToHT = append(ravineDWToHT, deadRavineNorth)
		}
		ravineDWToHT = append(ravineDWToHT, deadRavineMid, deadRavineSouth)
		ravineHTToDW := make([]caravanai.Waypoint, len(ravineDWToHT))
		for i := range ravineDWToHT {
			ravineHTToDW[len(ravineDWToHT)-1-i] = ravineDWToHT[i]
		}
		for _, ferries := range [][2]string{{"GATE_NPC_WC_FERRY3", "GATE_NPC_KT_FERRY2"}, {"GATE_NPC_WC_FERRY4", "GATE_NPC_KT_FERRY3"}} {
			if r, ok := buildRoute(caravanai.TownDonwhang, caravanai.TownHotan, ravineDWToHT, ferries[0], ferries[1], nil); ok {
				cfg.Routes[[2]caravanai.TownID{caravanai.TownDonwhang, caravanai.TownHotan}] = append(cfg.Routes[[2]caravanai.TownID{caravanai.TownDonwhang, caravanai.TownHotan}], r)
			}
			if r, ok := buildRoute(caravanai.TownHotan, caravanai.TownDonwhang, nil, ferries[1], ferries[0], ravineHTToDW); ok {
				cfg.Routes[[2]caravanai.TownID{caravanai.TownHotan, caravanai.TownDonwhang}] = append(cfg.Routes[[2]caravanai.TownID{caravanai.TownHotan, caravanai.TownDonwhang}], r)
			}
		}
	}
	if centralTarim.RegionID != 0 {
		if r, ok := buildRoute(caravanai.TownDonwhang, caravanai.TownHotan, []caravanai.Waypoint{centralTarim}, "GATE_NPC_WC_FERRY4", "GATE_NPC_KT_FERRY3", nil); ok {
			cfg.Routes[[2]caravanai.TownID{caravanai.TownDonwhang, caravanai.TownHotan}] = append(cfg.Routes[[2]caravanai.TownID{caravanai.TownDonwhang, caravanai.TownHotan}], r)
		}
		if r, ok := buildRoute(caravanai.TownHotan, caravanai.TownDonwhang, nil, "GATE_NPC_KT_FERRY3", "GATE_NPC_WC_FERRY4", []caravanai.Waypoint{centralTarim}); ok {
			cfg.Routes[[2]caravanai.TownID{caravanai.TownHotan, caravanai.TownDonwhang}] = append(cfg.Routes[[2]caravanai.TownID{caravanai.TownHotan, caravanai.TownDonwhang}], r)
		}
	}
	// Jangan <-> Donwhang: North-Tiger Mt. + Tiger Mountain Trade Post (Bandit Stronghold).
	if northTigerMt.RegionID != 0 {
		if r, ok := buildRoute(caravanai.TownJangan, caravanai.TownDonwhang, []caravanai.Waypoint{northTigerMt}, "GATE_NPC_CH_FERRY2", "GATE_NPC_WC_FERRY2", nil); ok {
			cfg.Routes[[2]caravanai.TownID{caravanai.TownJangan, caravanai.TownDonwhang}] = append(cfg.Routes[[2]caravanai.TownID{caravanai.TownJangan, caravanai.TownDonwhang}], r)
		}
		if r, ok := buildRoute(caravanai.TownDonwhang, caravanai.TownJangan, nil, "GATE_NPC_WC_FERRY2", "GATE_NPC_CH_FERRY2", []caravanai.Waypoint{northTigerMt}); ok {
			cfg.Routes[[2]caravanai.TownID{caravanai.TownDonwhang, caravanai.TownJangan}] = append(cfg.Routes[[2]caravanai.TownID{caravanai.TownDonwhang, caravanai.TownJangan}], r)
		}
	}
	if tigerPost.RegionID != 0 {
		if r, ok := buildRoute(caravanai.TownJangan, caravanai.TownDonwhang, []caravanai.Waypoint{tigerPost}, "GATE_NPC_CH_FERRY2", "GATE_NPC_WC_FERRY2", nil); ok {
			cfg.Routes[[2]caravanai.TownID{caravanai.TownJangan, caravanai.TownDonwhang}] = append(cfg.Routes[[2]caravanai.TownID{caravanai.TownJangan, caravanai.TownDonwhang}], r)
		}
		if r, ok := buildRoute(caravanai.TownDonwhang, caravanai.TownJangan, nil, "GATE_NPC_WC_FERRY2", "GATE_NPC_CH_FERRY2", []caravanai.Waypoint{tigerPost}); ok {
			cfg.Routes[[2]caravanai.TownID{caravanai.TownDonwhang, caravanai.TownJangan}] = append(cfg.Routes[[2]caravanai.TownID{caravanai.TownDonwhang, caravanai.TownJangan}], r)
		}
	}
	for _, routes := range []map[[2]caravanai.TownID][]caravanai.Waypoint{cfg.Highway, cfg.Detours} {
		for _, pair := range [][2]caravanai.TownID{{caravanai.TownJangan, caravanai.TownHotan}, {caravanai.TownHotan, caravanai.TownJangan}} {
			first, second := routes[[2]caravanai.TownID{pair[0], caravanai.TownDonwhang}], routes[[2]caravanai.TownID{caravanai.TownDonwhang, pair[1]}]
			routes[pair] = append(append([]caravanai.Waypoint(nil), first...), second[1:]...)
		}
	}
	for _, pair := range [][2]caravanai.TownID{{caravanai.TownJangan, caravanai.TownHotan}, {caravanai.TownHotan, caravanai.TownJangan}} {
		leg1 := cfg.Routes[[2]caravanai.TownID{pair[0], caravanai.TownDonwhang}]
		leg2 := cfg.Routes[[2]caravanai.TownID{caravanai.TownDonwhang, pair[1]}]
		for i, first := range leg1 {
			second := leg2[i%len(leg2)]
			cfg.Routes[pair] = append(cfg.Routes[pair], append(append([]caravanai.Waypoint(nil), first...), second[1:]...))
		}
		for j, second := range leg2 {
			if j < len(leg1) {
				continue
			}
			first := leg1[j%len(leg1)]
			cfg.Routes[pair] = append(cfg.Routes[pair], append(append([]caravanai.Waypoint(nil), first...), second[1:]...))
		}
	}
	return nil
}

func (game *gameplayPlane) caravanWalkableWaypoint(name string, regionID uint16) caravanai.Waypoint {
	if game.water == nil || game.movement == nil {
		return caravanai.Waypoint{Name: name, RegionID: regionID, X: 960, Z: 960, Scenic: true}
	}
	plane := movement.NewPlanes(game.movement, game.water).For(regionID)
	for _, coord := range [][2]float64{{960, 960}, {640, 960}, {1280, 960}, {960, 640}, {960, 1280}, {480, 480}, {1440, 1440}, {480, 1440}, {1440, 480}} {
		from := monster.Pose{RegionID: regionID, X: coord[0], Z: coord[1]}
		to := monster.Pose{RegionID: regionID, X: coord[0] + 4, Z: coord[1] + 4}
		path := plane.PlanPath(from, simulation.NavOwner{}, to)
		if path != nil && path.Result()&(monster.NavResultBlocked|monster.NavResultClipped) == 0 {
			rest := path.Rest()
			return caravanai.Waypoint{Name: name, RegionID: rest.RegionID, X: float32(rest.X), Y: float32(rest.Y), Z: float32(rest.Z), Scenic: true}
		}
	}
	return caravanai.Waypoint{}
}

func (game *gameplayPlane) caravanWalkableNear(origin caravanai.Waypoint) caravanai.Waypoint {
	if game.water == nil || game.movement == nil || origin.RegionID == 0 {
		return origin
	}
	plane := movement.NewPlanes(game.movement, game.water).For(origin.RegionID)
	openAt := func(x, y, z float64) (caravanai.Waypoint, bool) {
		from := monster.Pose{RegionID: origin.RegionID, X: x, Y: y, Z: z}
		for _, delta := range [][2]float64{{35, 0}, {-35, 0}, {0, 35}, {0, -35}} {
			to := monster.Pose{RegionID: origin.RegionID, X: x + delta[0], Y: y, Z: z + delta[1]}
			path := plane.PlanPath(from, simulation.NavOwner{}, to)
			if path == nil || path.Result()&(monster.NavResultBlocked|monster.NavResultClipped) != 0 {
				return caravanai.Waypoint{}, false
			}
		}
		probe := plane.PlanPath(from, simulation.NavOwner{}, monster.Pose{RegionID: origin.RegionID, X: x + 2, Y: y, Z: z + 2})
		if probe == nil || probe.Result()&(monster.NavResultBlocked|monster.NavResultClipped) != 0 {
			return caravanai.Waypoint{}, false
		}
		rest := probe.Rest()
		out := origin
		out.RegionID, out.X, out.Y, out.Z = rest.RegionID, float32(rest.X), float32(rest.Y), float32(rest.Z)
		return out, true
	}
	if wp, ok := openAt(float64(origin.X), float64(origin.Y), float64(origin.Z)); ok {
		return wp
	}
	for _, radius := range []float64{50, 80, 110, 150, 200} {
		for step := 0; step < 16; step++ {
			angle := float64(step) * (2 * math.Pi / 16)
			cx := float64(origin.X) + radius*math.Cos(angle)
			cz := float64(origin.Z) + radius*math.Sin(angle)
			if wp, ok := openAt(cx, float64(origin.Y), cz); ok {
				return wp
			}
		}
	}
	return origin
}

/*
================
caravanFollowFerry

Only follow through an admitted native link from a nearby source ferry.
================
*/
func (game *gameplayPlane) caravanFollowFerry(division, name string, leader caravanai.Waypoint) (caravanai.Waypoint, bool) {
	if leader.FerryTargetID != 0 {
		return game.caravanFerry(division, name, leader.FerrySourceRef, leader.FerryTargetID)
	}
	codes := []string{"GATE_NPC_CH_FERRY", "GATE_NPC_CH_FERRY2", "GATE_NPC_WC_FERRY", "GATE_NPC_WC_FERRY2", "GATE_NPC_WC_FERRY3", "GATE_NPC_WC_FERRY4", "GATE_NPC_KT_FERRY2", "GATE_NPC_KT_FERRY3"}
	for _, from := range codes {
		for _, to := range codes {
			npc, target, arrival, ok := game.items.ServerActorFerryRoute(from, to)
			if !ok {
				continue
			}
			at := caravanai.Waypoint{RegionID: arrival.RegionID, X: float32(arrival.X), Y: float32(arrival.Y), Z: float32(arrival.Z)}
			if caravanai.WaypointDistance(at, leader) > 320 {
				continue
			}
			if next, ok := game.caravanFerry(division, name, npc.RefObjID, target); ok {
				return next, true
			}
		}
	}
	return caravanai.Waypoint{}, false
}
