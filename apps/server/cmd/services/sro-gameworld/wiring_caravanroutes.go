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
	"opensro.online/server/internal/game/caravanai"
	worldgeom "opensro.online/server/internal/game/world"
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
	// The Thieves' home: the Thief Den's stolen-goods buyer.
	for _, npc := range game.items.NpcRoster {
		if npc.Codename == caravanThiefDenNPC {
			cfg.Terminals[caravanai.TownThiefDen] = caravanai.Waypoint{Town: caravanai.TownThiefDen, Name: npc.Name, SpecialtyShop: npc.Codename, RegionID: npc.Spawn.RegionID, X: float32(npc.Spawn.X), Y: float32(npc.Spawn.Y), Z: float32(npc.Spawn.Z)}
			break
		}
	}
	var desertPost caravanai.Waypoint
	for _, npc := range game.items.NpcRoster {
		if npc.Codename == "NPC_WC_SPECIAL2" {
			desertPost = caravanai.Waypoint{Name: npc.Name, RegionID: npc.Spawn.RegionID, X: float32(npc.Spawn.X), Y: float32(npc.Spawn.Y), Z: float32(npc.Spawn.Z)}
			break
		}
	}
	if desertPost.RegionID == 0 {
		return fmt.Errorf("desert trade road post unavailable")
	}
	cfg.Highway = make(map[[2]caravanai.TownID][]caravanai.Waypoint)
	cfg.Detours = make(map[[2]caravanai.TownID][]caravanai.Waypoint)
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
		}
	}
	for _, routes := range []map[[2]caravanai.TownID][]caravanai.Waypoint{cfg.Highway, cfg.Detours} {
		for _, pair := range [][2]caravanai.TownID{{caravanai.TownJangan, caravanai.TownHotan}, {caravanai.TownHotan, caravanai.TownJangan}} {
			first, second := routes[[2]caravanai.TownID{pair[0], caravanai.TownDonwhang}], routes[[2]caravanai.TownID{caravanai.TownDonwhang, pair[1]}]
			routes[pair] = append(append([]caravanai.Waypoint(nil), first...), second[1:]...)
		}
	}
	return nil
}

/*
================
caravanFollowFerry

Only follow through an admitted native link from a nearby source ferry.
================
*/
func (game *gameplayPlane) caravanFollowFerry(division, name string, leader caravanai.Waypoint) (caravanai.Waypoint, bool) {
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
