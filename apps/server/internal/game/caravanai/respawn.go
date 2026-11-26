/*
===========================================================================

respawn.go - caravan agent death, 4-minute grouped respawn, and resurrection

Solo caravan agents respawn in town immediately. Grouped or hired Hunters
and Thieves remain on the ground for GroupedRespawnMs (4 minutes) so players
can resurrect them in place; if not resurrected before the timer expires,
they respawn in town and resume their role.

===========================================================================
*/
package caravanai

import (
	"math"
	"strings"
)

const (
	// GroupedRespawnMs is how long a defeated grouped or hired agent waits on
	// the ground (resurrectable by players or escorting Hunters) before respawning in town.
	GroupedRespawnMs int64 = 4 * 60_000
	// ResurrectCastRange is how close a resurrecting Hunter walks to cast Ress.
	ResurrectCastRange float64 = 15 * UnitsPerMeter
)

/*
==================
SetCanResurrect

Records whether the named agent has a Resurrection skill trained.
==================
*/
func (r *Runtime) SetCanResurrect(division, name string, can bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if unit := r.findAgentLocked(division, name); unit != nil {
		unit.CanResurrect = can
	}
}

/*
==================
ResurrectTarget

If the named living Hunter has a Resurrection skill and its escorted AI Trader
is dead on the ground awaiting revival, returns the Trader's name and position.
==================
*/
func (r *Runtime) ResurrectTarget(division, hunterName string, nowMs int64) (string, Waypoint, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hunter := r.findAgentLocked(division, hunterName)
	if hunter == nil || hunter.Role != RoleHunter || !hunter.CanResurrect || hunter.IsDead(nowMs) || hunter.RespawnPending || hunter.EscortingTrader == "" {
		return "", Waypoint{}, false
	}
	trader := r.findAgentLocked(division, hunter.EscortingTrader)
	if trader == nil || trader.Role != RoleTrader || trader.DeadUntilMs == 0 || nowMs >= trader.DeadUntilMs {
		return "", Waypoint{}, false
	}
	return trader.Name, position(trader), true
}

/*
==================
CompleteThiefDenReturn

Synchronizes an AI Thief that arrived back at the Bandit Den via a Bandit Den
Return Scroll, resetting it to idle in the Den.
==================
*/
func (r *Runtime) CompleteThiefDenReturn(division, name string, at Waypoint, nowMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	unit := r.findAgentLocked(division, name)
	if unit == nil || unit.Role != RoleThief {
		return
	}
	unit.place(at, nowMs)
	unit.OriginTown = TownThiefDen
	unit.State = StateIdleInTown
	unit.UnderAttack = false
	unit.lootGID = 0
	unit.Route, unit.WaypointIndex = nil, 0
	unit.TownDepartAtMs = nowMs + TownRestDurationMs
	if unit.HiredBy == "" {
		unit.RunKind = RunNone
		unit.Vengeance = ""
		unit.vengeanceUntilMs = 0
	}
	unit.AvailableForHire = hireable(unit)
}

func (r *Runtime) hasLivingResurrectingEscortLocked(trader *AgentUnit, nowMs int64) bool {
	if trader == nil || trader.Role != RoleTrader {
		return false
	}
	for _, other := range r.agents[trader.Division] {
		if other == trader || other.Role != RoleHunter || !other.CanResurrect {
			continue
		}
		if other.IsDead(nowMs) || other.RespawnPending {
			continue
		}
		if strings.EqualFold(other.EscortingTrader, trader.Name) || strings.EqualFold(other.DedicatedEscortFor, trader.Name) {
			return true
		}
	}
	return false
}

/*
==================
IsDead

Reports whether the agent is currently defeated and awaiting resurrection or
its delayed town respawn.
==================
*/
func (u *AgentUnit) IsDead(nowMs int64) bool {
	if u == nil {
		return false
	}
	return u.HP <= 0 || (u.DeadUntilMs > 0 && nowMs < u.DeadUntilMs)
}

func (r *Runtime) isGroupedOnDeathLocked(unit *AgentUnit, nowMs int64) bool {
	if unit == nil {
		return false
	}
	if unit.Role == RoleTrader {
		return unit.HiredBy != "" || r.hasLivingResurrectingEscortLocked(unit, nowMs)
	}
	return unit.HiredBy != "" ||
		unit.EscortingTrader != "" ||
		unit.DedicatedEscortFor != "" ||
		unit.RunKind == RunGrand ||
		unit.RunKind == RunVengeance
}

func (r *Runtime) beginDeathLocked(unit *AgentUnit, attacker string, nowMs int64) int64 {
	stolenGold := int64(unit.CargoStacks) * unit.StackValueGold
	unit.deathAttacker = attacker
	unit.deathPos = position(unit)
	unit.deathWasVengeance = unit.RunKind == RunVengeance
	if r.isGroupedOnDeathLocked(unit, nowMs) {
		unit.HP = 0
		unit.RespawnPending = false
		unit.DeadUntilMs = nowMs + GroupedRespawnMs
		unit.UnderAttack = false
		unit.combatProgressAtMs = 0
		unit.ReinforcingTrader = ""
		unit.catchingUp, unit.combatRejoining, unit.roamAtMs, unit.progressAtMs = false, false, 0, 0
		unit.AvailableForHire = false
		return 0
	}

	r.completeRespawnLocked(unit, nowMs)
	return stolenGold
}

func (r *Runtime) completeRespawnLocked(unit *AgentUnit, nowMs int64) {
	fell, wasHunting, attacker := unit.deathPos, unit.deathWasVengeance, unit.deathAttacker
	if fell.RegionID == 0 {
		fell = position(unit)
	}
	unit.CargoStacks = 0
	r.contracts.Cancel(unit.Division, unit.Name)
	unit.DeadUntilMs = 0
	unit.deathAttacker = ""
	unit.deathPos = Waypoint{}
	unit.deathWasVengeance = false
	unit.RespawnPending = true

	respawnTown := unit.HomeTown
	var dedicatedLeader *AgentUnit
	if unit.Role == RoleHunter && unit.DedicatedEscortFor != "" {
		if leader := r.findAgentLocked(unit.Division, unit.DedicatedEscortFor); leader != nil {
			dedicatedLeader = leader
			respawnTown = leader.OriginTown
		}
	}
	home := TownWaypoint(respawnTown)
	if terminal, ok := r.cfg.Terminals[respawnTown]; ok {
		home = terminal
	}
	angle := escortFormationAngle(unit.Name)
	spawnAt := offsetPoint(home, 25*math.Cos(angle), 25*math.Sin(angle))
	unit.RegionID = spawnAt.RegionID
	unit.X = spawnAt.X
	unit.Y = spawnAt.Y
	unit.Z = spawnAt.Z
	unit.HP = unit.MaxHP
	unit.MP = unit.MaxMP
	unit.PotionCharges = defaultPotionCharges(unit.Role)
	unit.transportHPPercent = FullHPPercent
	unit.needsManaRecovery = false
	unit.combatProgressAtMs = 0
	unit.UnderAttack = false
	unit.State = StateIdleInTown
	unit.OriginTown = respawnTown
	if unit.DestTown == respawnTown {
		unit.DestTown = TownDonwhang
		if respawnTown == TownDonwhang {
			unit.DestTown = TownJangan
		}
	}
	if dedicatedLeader != nil {
		unit.DestTown = dedicatedLeader.DestTown
	}
	unit.Route, unit.WaypointIndex = nil, 0
	unit.TownDepartAtMs = nowMs + TownRestDurationMs
	unit.ReinforcingTrader = ""
	unit.catchingUp, unit.combatRejoining, unit.roamAtMs, unit.progressAtMs = false, false, 0, 0
	switch {
	case unit.HiredBy != "" && unit.Role == RoleTrader:
		// A robbed hired Trader's run is over (its goods are gone).
		r.endHireLocked([]*AgentUnit{unit}, HireAborted, nowMs)
		unit.CooldownUntilMs = nowMs + RobbedCooldownMs
	case unit.HiredBy != "":
		// A hire outlives a death: after the respawn delay the follow step
		// brings the agent back to its hirer.
		unit.State = StateTraveling
	case unit.Role == RoleTrader:
		// Robbed: the run is over and the Trader rests before its next own run.
		r.disbandRunLocked(unit, nowMs)
		unit.CooldownUntilMs = nowMs + RobbedCooldownMs
	case dedicatedLeader != nil:
		unit.EscortingTrader = dedicatedLeader.Name
	default:
		unit.EscortingTrader, unit.DedicatedEscortFor, unit.RunKind = "", "", RunNone
	}
	if unit.Role == RoleThief || unit.Role == RoleHunter {
		if wasHunting {
			unit.RunKind = RunVengeance
		}
		r.startVengeanceLocked(unit, attacker, fell, nowMs)
	}
	unit.AvailableForHire = hireable(unit)
}

func (r *Runtime) reviveInPlaceLocked(unit *AgentUnit, hp, mp int64) {
	unit.DeadUntilMs = 0
	unit.deathAttacker = ""
	unit.deathPos = Waypoint{}
	unit.deathWasVengeance = false
	unit.RespawnPending = false
	unit.HP = int(hp)
	unit.MP = int(mp)
	unit.UnderAttack = false
	unit.combatProgressAtMs = 0
	unit.catchingUp, unit.combatRejoining, unit.roamAtMs, unit.progressAtMs = false, false, 0, 0
	if unit.HiredBy != "" || unit.EscortingTrader != "" || !isTownRegion(unit.RegionID) {
		unit.State = StateTraveling
	} else {
		unit.State = StateIdleInTown
	}
	unit.AvailableForHire = hireable(unit)
}

/*
================
NoteCargoGone

The world found a dead Trader's transport empty: its death scattered the
goods (a killing strike, or a fall while riding). It carries none when
resurrected, so they are never both on the ground and in its wagon.
================
*/
func (r *Runtime) NoteCargoGone(division, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u := r.findAgentLocked(division, name); u != nil && u.Role == RoleTrader && u.DeadUntilMs > 0 {
		u.CargoStacks = 0
	}
}
