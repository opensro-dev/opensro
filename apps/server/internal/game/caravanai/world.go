/*
===========================================================================

world.go - port-only caravan movement and player observations

Geometry and live player state come from GameWorld; deterministic tests
can keep the grid-only movement path without loading retail assets.

===========================================================================
*/
package caravanai

import (
	"strings"
	"unicode"

	worldgeom "opensro.online/server/internal/game/world"
)

/*
================
PlayerPosition
================
*/
type PlayerPosition struct {
	ObservedPlayer
	At Waypoint
}

/*
================
agentEffects

Call world owners after releasing the AI lock: combat callbacks reenter AI.
================
*/
type agentEffects struct {
	drop, heal int
	reward     *EscortContract
	follow     *Waypoint
}

/*
================
SafeDialogue

Untrusted model output is plain chat text, never client markup or links.
================
*/
func SafeDialogue(raw string) string {
	raw = strings.ReplaceAll(raw, "[AI]", "AI")
	if strings.ContainsAny(raw, "<>[]") || strings.Contains(strings.ToLower(raw), "://") {
		return ""
	}
	var out []rune
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		out = append(out, r)
		if len(out) == 160 {
			break
		}
	}
	return strings.TrimSpace(string(out))
}

/*
================
route
================
*/
func (r *Runtime) route(unit *AgentUnit) []Waypoint {
	points := SelectRoute(unit.OriginTown, unit.DestTown, unit.CargoStars, unit.Personality, r.memories.HeatSnapshot(unit.Division))
	key := [2]TownID{unit.OriginTown, unit.DestTown}
	if route, ok := r.cfg.Highway[key]; ok {
		points = append([]Waypoint(nil), route...)
	}
	detour := shouldTakeNorthernDetour(unit.CargoStars, unit.Personality, r.memories.HeatSnapshot(unit.Division))
	if route, ok := r.cfg.Highway[key]; ok {
		detour = false
		for _, point := range route {
			if point.Chokepoint && r.memories.HeatSnapshot(unit.Division)[point.RegionID] >= DetourHeatThreshold && (unit.Personality == PersonalityCautious || unit.CargoStars >= 4) {
				detour = true
				break
			}
		}
	}
	if detour {
		if route, ok := r.cfg.Detours[key]; ok {
			points = append([]Waypoint(nil), route...)
		}
	}
	for i, point := range points {
		if terminal, ok := r.cfg.Terminals[point.Town]; ok {
			points[i] = terminal
		}
	}
	return points
}

/*
================
moveLocked
================
*/
func (r *Runtime) moveLocked(unit *AgentUnit, target Waypoint, distance float32) bool {
	from := Waypoint{RegionID: unit.RegionID, X: unit.X, Y: unit.Y, Z: unit.Z}
	if target.FerryTargetID != 0 {
		return false
	}
	if r.hooks.Move != nil {
		next, arrived := r.hooks.Move(unit.Name, from, target, distance)
		unit.RegionID, unit.X, unit.Y, unit.Z = next.RegionID, next.X, next.Y, next.Z
		return arrived
	}
	region, x, z, arrived := StepWaypoint(unit.RegionID, unit.X, unit.Z, target, distance)
	unit.RegionID, unit.X, unit.Z = region, x, z
	if arrived {
		unit.Y = target.Y
	}
	return arrived
}

/*
================
WaypointDistance
================
*/
func WaypointDistance(a, b Waypoint) float64 {
	return worldgeom.Distance(worldgeom.RegionXZ{RegionID: a.RegionID, X: float64(a.X), Z: float64(a.Z)}, worldgeom.RegionXZ{RegionID: b.RegionID, X: float64(b.X), Z: float64(b.Z)})
}

/*
================
ConfigureVitals

Use the same derived keeper maxima as the real combat actor.
================
*/
func (r *Runtime) ConfigureVitals(division, name string, hp, mp int64, skill uint32, run float32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if unit := r.findAgentLocked(division, name); unit != nil {
		unit.MaxHP, unit.HP, unit.MaxMP, unit.MP = int(hp), int(hp), int(mp), int(mp)
		unit.PrimarySkillID = skill
		unit.RunSpeed = run
	}
}

/*
================
commandNearLocked
================
*/
func (r *Runtime) commandNearLocked(unit *AgentUnit, division, player string) bool {
	if r.hooks.Player == nil {
		return false
	}
	p, ok := r.hooks.Player(division, player)
	return ok && p.HPPercent > 0 && isTownRegion(unit.RegionID) && WaypointDistance(p.At, Waypoint{RegionID: unit.RegionID, X: unit.X, Z: unit.Z}) <= 64
}

/*
================
StageRecruiters

Use a shipped town landmark so the live recruiters are easy to find.
================
*/
func (r *Runtime) StageRecruiters(division string, town TownID, at Waypoint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	offset := float32(10)
	for _, unit := range r.agents[division] {
		if unit.HomeTown == town && (unit.State == StateIdleInTown || unit.State == StateRecruiting) {
			unit.RegionID, unit.X, unit.Y, unit.Z = at.RegionID, at.X+offset, at.Y, at.Z
			offset += 10
		}
	}
}

/*
================
ObserveVitals

The combat authority owns HP, including monster damage and transport death.
An already scheduled respawn keeps its new-town state until published.
================
*/
func (r *Runtime) ObserveVitals(division, name string, hp, mp int64, transportLost bool, nowMs int64) {
	r.mu.Lock()
	unit := r.findAgentLocked(division, name)
	if unit == nil || unit.RespawnPending {
		r.mu.Unlock()
		return
	}
	dead := hp <= 0 || transportLost
	if !dead {
		unit.HP, unit.MP = int(hp), int(mp)
	}
	damage := unit.MaxHP
	r.mu.Unlock()
	if dead {
		r.RecordPlayerAttack(division, name, "", damage, nowMs)
	}
}

/*
================
ActorSynchronized
================
*/
func (r *Runtime) ActorSynchronized(division, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if unit := r.findAgentLocked(division, name); unit != nil {
		unit.RespawnPending = false
	}
}
