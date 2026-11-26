/*
===========================================================================

world.go - port-only caravan movement and player observations

Geometry and live player state come from GameWorld; deterministic tests
can keep the grid-only movement path without loading retail assets.

===========================================================================
*/
package caravanai

import (
	"math/rand/v2"
	"slices"
	"strings"
	"unicode"

	"opensro.online/server/internal/domain"
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
	// potion is the item the agent drinks after the lock (WorldHooks.UseItem).
	potion string
	// pickup is the ground item the agent picks up after the lock.
	pickup   uint32
	reward   *EscortContract
	follow   *Waypoint
	teleport *Waypoint
}

func stepDistanceFor(unit *AgentUnit) float32 {
	if unit == nil || unit.RunSpeed <= 0 {
		return 5
	}
	return max(unit.RunSpeed/10, 5)
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
	heat := r.memories.HeatSnapshot(unit.Division)
	points := SelectRoute(unit.OriginTown, unit.DestTown, unit.CargoStars, unit.Personality, heat)
	key := [2]TownID{unit.OriginTown, unit.DestTown}
	if variants := r.cfg.Routes[key]; len(variants) > 0 {
		avoidHot := unit.Personality == PersonalityCautious || unit.CargoStars >= 4
		var pool [][]Waypoint
		for _, cand := range variants {
			hot := false
			if avoidHot {
				for _, pt := range cand {
					if pt.Chokepoint && heat[pt.RegionID] >= DetourHeatThreshold {
						hot = true
						break
					}
				}
			}
			if !hot {
				pool = append(pool, cand)
			}
		}
		if len(pool) == 0 {
			pool = variants
		}
		chosen := pool[rand.IntN(len(pool))]
		points = append([]Waypoint(nil), chosen...)
	} else {
		if route, ok := r.cfg.Highway[key]; ok {
			points = append([]Waypoint(nil), route...)
		}
		detour := shouldTakeNorthernDetour(unit.CargoStars, unit.Personality, heat)
		if route, ok := r.cfg.Highway[key]; ok {
			detour = false
			for _, point := range route {
				if point.Chokepoint && heat[point.RegionID] >= DetourHeatThreshold && (unit.Personality == PersonalityCautious || unit.CargoStars >= 4) {
					detour = true
					break
				}
			}
		}
		if unit.RunKind == RunSolo {
			// Solo Traders pick either crossing at random and bend off the road.
			detour = rand.IntN(2) == 0
		}
		if detour {
			if route, ok := r.cfg.Detours[key]; ok {
				points = append([]Waypoint(nil), route...)
			}
		}
	}
	for i, point := range points {
		if terminal, ok := r.cfg.Terminals[point.Town]; ok {
			points[i] = terminal
		}
	}
	if unit.RunKind == RunSolo {
		points = scenicRoute(points)
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

// SetRunSpeed sets the speed the agent runs at and its walk factor (a live
// speed buff scales both, as it does the client's).
func (r *Runtime) SetRunSpeed(division, name string, run, walkFactor float32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if unit := r.findAgentLocked(division, name); unit != nil {
		unit.RunSpeed, unit.walkFactor = run, walkFactor
	}
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
	if unit.DeadUntilMs > 0 {
		if hp > 0 {
			r.reviveInPlaceLocked(unit, hp, mp)
			// If a resurrected Trader also lost its transport horse, end the
			// run cleanly back to town so it never walks the road on foot.
			if unit.Role == RoleTrader && transportLost {
				r.completeRespawnLocked(unit, nowMs)
			}
		} else if nowMs >= unit.DeadUntilMs {
			r.completeRespawnLocked(unit, nowMs)
		}
		r.mu.Unlock()
		return
	}
	// Transport loss while the Trader itself is alive destroys the caravan:
	// a dead horse cannot be resurrected, so complete the run back to town
	// immediately instead of entering the character-resurrection wait window.
	if transportLost && hp > 0 && unit.Role == RoleTrader {
		stolenGold := int64(unit.CargoStacks) * unit.StackValueGold
		regionID := unit.RegionID
		unit.CargoStacks = 0
		r.contracts.Cancel(unit.Division, unit.Name)
		r.completeRespawnLocked(unit, nowMs)
		r.mu.Unlock()
		r.memories.RecordRobberyOrKill(division, name, "", stolenGold, regionID, "", nowMs)
		return
	}
	dead := hp <= 0
	if !dead {
		unit.HP, unit.MP = int(hp), int(mp)
	}
	damage := unit.MaxHP
	r.mu.Unlock()
	if dead {
		r.RecordPlayerStrike(division, name, "", damage, true, nowMs)
	}
}

/*
================
ObserveTransportVitals

Tracks active transport HP percentage for trader potion sustain.
================
*/
func (r *Runtime) ObserveTransportVitals(division, name string, hp, maxHP int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	unit := r.findAgentLocked(division, name)
	if unit == nil || unit.RespawnPending {
		return
	}
	if maxHP <= 0 {
		unit.transportHPPercent = FullHPPercent
		return
	}
	unit.transportHPPercent = HPPercent(hp, maxHP)
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

/*
================
ApproachWorldThreat

Native NPC enemies use their authoritative pose, outside the player-memory list.
================
*/
func (r *Runtime) ApproachWorldThreat(division, name string, target Waypoint) *AgentUnit {
	unit, _ := r.ApproachWorldThreatAt(division, name, target, 0)
	return unit
}

/*
================
ApproachWorldThreatAt

Approaches an authoritative NPC threat and reports stalled=true when the agent
fails to make movement or casting progress within combatStallTimeoutMs so the
convoy does not remain deadlocked by an unreachable target.
================
*/
func (r *Runtime) ApproachWorldThreatAt(division, name string, target Waypoint, nowMs int64) (*AgentUnit, bool) {
	// World hooks run outside r.mu (the combat path calls back into it).
	casting := r.hooks.Casting != nil && r.hooks.Casting(division, name)
	r.mu.Lock()
	defer r.mu.Unlock()
	unit := r.findAgentLocked(division, name)
	if unit == nil || unit.RespawnPending {
		return nil, false
	}
	if r.escortMustRejoinLocked(unit) {
		unit.combatProgressAtMs = 0
		snapshot := *unit
		return &snapshot, false
	}
	unit.State = StateInCombat
	before := Waypoint{RegionID: unit.RegionID, X: unit.X, Z: unit.Z}
	dist := WaypointDistance(before, target)
	stopDist := attackStopDistance(unit)
	if !casting && dist > stopDist {
		r.moveLocked(unit, target, unit.RunSpeed/10)
	}
	after := Waypoint{RegionID: unit.RegionID, X: unit.X, Z: unit.Z}
	newDist := WaypointDistance(after, target)
	if nowMs > 0 {
		if casting || InCastRange(unit, newDist) || WaypointDistance(before, after) >= 0.5 {
			unit.combatProgressAtMs = nowMs
		} else if unit.combatProgressAtMs == 0 {
			unit.combatProgressAtMs = nowMs
		} else if nowMs-unit.combatProgressAtMs >= combatStallTimeoutMs {
			unit.combatProgressAtMs = 0
			unit.State = StateTraveling
			snapshot := *unit
			return &snapshot, true
		}
	}
	snapshot := *unit
	return &snapshot, false
}

/*
================
waitForEscortLocked

On the road the trader waits for cover when all nearby escorts are lagging.
At the ferry it waits for the escorts to come within boarding range.
================
*/
func (r *Runtime) waitForEscortLocked(trader *AgentUnit) bool {
	if trader.Role != RoleTrader {
		return false
	}
	boarding := trader.WaypointIndex < len(trader.Route) && trader.Route[trader.WaypointIndex].FerryTargetID != 0
	lagging := false
	for _, guard := range r.agents[trader.Division] {
		if guard == trader || guard.Role == RoleThief || guard.RespawnPending || guard.HP <= 0 {
			continue
		}
		if !strings.EqualFold(guard.EscortingTrader, trader.Name) {
			second := r.findAgentLocked(trader.Division, guard.EscortingTrader)
			if !boarding || guard.RunKind != RunGrand || second == nil || r.ferryGuideLocked(second) != trader {
				continue
			}
		}
		if guard.State == StateIdleInTown || isTownRegion(guard.RegionID) {
			continue
		}
		dist := WaypointDistance(position(trader), position(guard))
		if dist > EscortTeleportDistance {
			continue
		}
		if boarding {
			if dist > escortFerryWaitDistance {
				return true
			}
			continue
		}
		if dist <= EscortFollowDistance {
			return false
		}
		lagging = true
	}
	return lagging
}

/*
================
RequestManaRecovery

A native skill refusal can exhaust an expensive build above the percentage threshold.
================
*/
func (r *Runtime) RequestManaRecovery(division, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if unit := r.findAgentLocked(division, name); unit != nil {
		unit.needsManaRecovery = true
	}
}

/*
================
stepEscortFollowLocked

Advances an AI Hunter escort along its Trader's highway gates while exiting
town or catching up from far behind before closing directly onto the Trader.
================
*/
func (r *Runtime) stepEscortFollowLocked(unit, leader *AgentUnit) {
	target := Waypoint{RegionID: leader.RegionID, X: leader.X, Y: leader.Y, Z: leader.Z}
	if isTownRegion(unit.RegionID) && leader.WaypointIndex > 0 {
		if unit.OriginTown != leader.OriginTown || unit.DestTown != leader.DestTown || !slices.Equal(unit.Route, leader.Route) {
			unit.OriginTown, unit.DestTown = leader.OriginTown, leader.DestTown
			unit.Route = leader.Route
			unit.WaypointIndex = 0
		}
		before := Waypoint{RegionID: unit.RegionID, X: unit.X, Z: unit.Z}
		for unit.WaypointIndex < len(unit.Route) && unit.WaypointIndex < leader.WaypointIndex {
			wp := unit.Route[unit.WaypointIndex]
			if wp.FerryTargetID != 0 || WaypointDistance(before, wp) <= 0.01 {
				unit.WaypointIndex++
				continue
			}
			if r.moveLocked(unit, wp, stepDistanceFor(unit)) {
				unit.WaypointIndex++
			}
			return
		}
	}
	r.moveLocked(unit, target, stepDistanceFor(unit))
}

func isTownRegion(regionID uint16) bool {
	if allowed, known := worldgeom.RegionPlayerCombat(regionID); known && !allowed {
		return true
	}
	return regionID == RegionJanganTown || regionID == RegionDonwhangTown || regionID == RegionHotanTown
}

// FullHPPercent is what a vital reads when its maximum is unknown.
const FullHPPercent = 100

func percentOf(cur, maxVal int) int {
	return HPPercent(int64(cur), int64(maxVal))
}

/*
==================
HPPercent

current as a whole percentage of maximum, clamped to [0, 100]; a maximum
of zero or less reads as full.
==================
*/
func HPPercent(current, maximum int64) int {
	if maximum <= 0 {
		return FullHPPercent
	}
	pct := (current * 100) / maximum
	if pct < 0 {
		return 0
	}
	if pct > FullHPPercent {
		return FullHPPercent
	}
	return int(pct)
}

func countFriendlyHunters(players []ObservedPlayer) int {
	n := 0
	for _, p := range players {
		if p.HPPercent > 0 && p.JobType == domain.JobHunter && p.TrustScore > domain.CaravanVendettaScore {
			n++
		}
	}
	return n
}

func defaultPotionCharges(_ AgentRole) int {
	return 300
}

func attackStopDistance(unit *AgentUnit) float64 {
	if unit == nil {
		return 20
	}
	code := strings.ToUpper(unit.WeaponCode)
	if strings.Contains(code, "_BOW_") || strings.Contains(code, "_CROSSBOW_") {
		return 140
	}
	return 20
}

/*
==================
InCastRange

Reports whether dist is close enough to open an attack or skill cast
without triggering out-of-range approach restarts.
==================
*/
func InCastRange(unit *AgentUnit, dist float64) bool {
	if unit == nil {
		return dist <= 45
	}
	code := strings.ToUpper(unit.WeaponCode)
	if strings.Contains(code, "_BOW_") || strings.Contains(code, "_CROSSBOW_") {
		return dist <= 175
	}
	return dist <= 45
}

func (r *Runtime) hasLiveEscortLocked(unit *AgentUnit) bool {
	if unit == nil || unit.Role != RoleTrader {
		return false
	}
	for _, guard := range r.agents[unit.Division] {
		if guard.Role != RoleHunter || !strings.EqualFold(guard.EscortingTrader, unit.Name) {
			continue
		}
		if !guard.RespawnPending && guard.HP > 0 && isActiveTradeRunState(guard.State) {
			return true
		}
	}
	return false
}

/*
================
escortMustRejoinLocked

An escort can fight within 100m of its leader. Once it breaks that tether,
it must come back to normal follow range before re-engaging.
================
*/
func (r *Runtime) escortMustRejoinLocked(unit *AgentUnit) bool {
	if unit == nil || unit.EscortingTrader == "" || unit.DeadUntilMs > 0 || unit.HP <= 0 {
		return false
	}
	at, leader, ok := r.leaderPositionLocked(unit)
	if !ok || (leader != nil && (leader.RespawnPending || leader.HP <= 0)) {
		return false
	}
	if isTownRegion(at.RegionID) && !isTownRegion(unit.RegionID) {
		return true
	}
	if leader != nil {
		guide := r.ferryGuideLocked(leader)
		if guide.WaypointIndex < len(guide.Route) && guide.Route[guide.WaypointIndex].FerryTargetID != 0 {
			return WaypointDistance(position(unit), position(guide)) > escortFerryWaitDistance
		}
	}
	dist := WaypointDistance(position(unit), at)
	unit.combatRejoining = dist > escortCombatDistance || (unit.combatRejoining && dist > EscortRoamRadius)
	return unit.combatRejoining
}

/*
==================
ferryGuideLocked

Both groups of a grand caravan stage at the lead trader's ferry.
==================
*/
func (r *Runtime) ferryGuideLocked(leader *AgentUnit) *AgentUnit {
	if leader.RunKind == RunGrand && leader.Role == RoleTrader && leader.EscortingTrader != "" {
		if head := r.findAgentLocked(leader.Division, leader.EscortingTrader); head != nil {
			return head
		}
	}
	return leader
}

/*
==================
IsContractPartner

Reports whether playerName and agentName currently share an active escort
contract in division.
==================
*/
func (r *Runtime) IsContractPartner(division, playerName, agentName string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if contract, ok := r.contracts.ActiveContractFor(division, agentName); ok && contract.Active {
		if (strings.EqualFold(contract.TraderName, playerName) && strings.EqualFold(contract.HunterName, agentName)) ||
			(strings.EqualFold(contract.TraderName, agentName) && strings.EqualFold(contract.HunterName, playerName)) {
			return true
		}
	}
	if unit := r.findAgentLocked(division, agentName); unit != nil && unit.HiredBy != "" && strings.EqualFold(unit.HiredBy, playerName) {
		return true
	}
	return false
}

/*
==================
EscortMustRejoin

Reports whether a follower must disengage combat to return to its leader.
==================
*/
func (r *Runtime) EscortMustRejoin(division, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.escortMustRejoinLocked(r.findAgentLocked(division, name))
}
