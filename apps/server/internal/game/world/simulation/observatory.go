/*
===========================================================================

observatory.go - a read-only copy of the monster population for operators

The operations dashboard and the benchmark harnesses read the population
from here. The reply is capped (observatoryMonsterCap rows) to bound its
size, and the cap must never drop what a check near a player needs: the
monsters in the 3x3 regions around each focus region (the online players)
are always copied, and the rest of the population fills what remains.

===========================================================================
*/

package simulation

import (
	"sort"

	"opensro.online/server/internal/game/world/monster"
)

// observatoryMonsterCap bounds the monster rows in one capture.
const observatoryMonsterCap = 50000

/*
================
ObservatoryMonster
================
*/
type ObservatoryMonster struct {
	GID    uint32  `json:"gid"`
	Ref    uint32  `json:"ref"`
	Name   string  `json:"name"`
	Level  uint8   `json:"level"`
	Rarity uint8   `json:"rarity"`
	HP     uint32  `json:"hp"`
	MaxHP  uint32  `json:"maxHp"`
	Region uint16  `json:"region"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Z      float64 `json:"z"`
	Mode   string  `json:"mode"`
	Target uint32  `json:"target"`
}

/*
================
ObservatoryPopulation

Truncated: the cap dropped rows. FocusComplete: every monster in the focus
neighbourhood is listed, so absence there proves absence.
================
*/
type ObservatoryPopulation struct {
	Monsters      []ObservatoryMonster `json:"monsters"`
	Resident      int                  `json:"resident"`
	Regions       int                  `json:"regions"`
	Nests         int                  `json:"nests"`
	Respawns      int                  `json:"respawns"`
	Truncated     bool                 `json:"truncated"`
	FocusComplete bool                 `json:"focusComplete"`
}

/*
================
focusNeighbourhood

The focus regions and their eight neighbours (region id = z<<8 | x).
================
*/
func focusNeighbourhood(focus []uint16) map[uint16]bool {
	near := make(map[uint16]bool, len(focus)*9)
	for _, region := range focus {
		x, z := int(region&0xff), int(region>>8)
		for dz := -1; dz <= 1; dz++ {
			for dx := -1; dx <= 1; dx++ {
				if nx, nz := x+dx, z+dz; nx >= 0 && nx <= 0xff && nz >= 0 && nz <= 0xff {
					near[uint16(nz<<8|nx)] = true
				}
			}
		}
	}
	return near
}

/*
================
capObservatory

Fits the focus rows, then the others, into limit rows. dropped says the
copy already left rows out. The focus is complete only if all of it fits.
================
*/
func capObservatory(focus, others []ObservatoryMonster, dropped bool, limit int) ([]ObservatoryMonster, bool, bool) {
	complete := true
	if len(focus) > limit {
		focus, complete, dropped = focus[:limit], false, true
	}
	if room := limit - len(focus); len(others) > room {
		others, dropped = others[:room], true
	}
	return append(focus, others...), dropped, complete
}

/*
================
MonsterState.Observatory

Copies only existing state under its owner lock. Never calls division(),
materialization, timers, RNG or notice drains. Sorting occurs after
unlocking. Monsters near a focus region are kept ahead of the cap.
================
*/
func (s *MonsterState) Observatory(division string, focus []uint16) ObservatoryPopulation {
	out := ObservatoryPopulation{Monsters: []ObservatoryMonster{}, Nests: len(s.template.Nests)}
	near := focusNeighbourhood(focus)
	var others []ObservatoryMonster
	s.mu.Lock()
	if state := s.divs[division]; state != nil {
		out.Resident = state.instances.len()
		out.Regions = len(state.materialized)
		out.Respawns = state.pendingRefills()
		now := s.nowMillis()
		for gid := range state.instances.ids() {
			ref, rarity, hp, maxHP := state.instances.projection(gid)
			pose := monster.Pose{}
			mode := "idle"
			var target uint32
			if mover, ok := state.movers.lookup(gid); ok {
				pose = mover.LivePoseAt(now, nil)
				mode = mover.Mode().String()
				target = mover.TargetGID()
			} else {
				actor := state.instances.get(gid)
				pose = monster.Pose{RegionID: actor.Spawn.RegionID, X: actor.Spawn.X, Y: actor.Spawn.Y, Z: actor.Spawn.Z}
			}
			row := ObservatoryMonster{GID: gid, Ref: ref.RefObjID, Name: ref.Name, Level: ref.Level, Rarity: rarity, HP: hp, MaxHP: maxHP, Region: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z, Mode: mode, Target: target}
			if near[pose.RegionID] {
				out.Monsters = append(out.Monsters, row)
			} else if len(others) < observatoryMonsterCap {
				others = append(others, row)
			} else {
				out.Truncated = true
			}
		}
	}
	s.mu.Unlock()
	out.Monsters, out.Truncated, out.FocusComplete = capObservatory(out.Monsters, others, out.Truncated, observatoryMonsterCap)
	sort.Slice(out.Monsters, func(i, j int) bool { return out.Monsters[i].GID < out.Monsters[j].GID })
	return out
}
