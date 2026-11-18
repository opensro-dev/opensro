/*
===========================================================================

scheduler.go - Active trade-run rotation, town staging, and distress aid

Maintains a living balance of AI Caravan & Thief agents across Jangan,
Donwhang, and Hotan:
  - Guarantees at least MinActiveTradeRuns (2) AI Traders are actively on
    the highway at any moment while the remaining Traders hang around town
    resting or recruiting escorts.
  - Keeps town-staged AI Hunters available for hire until contracted or
    called away by an emergency.
  - Broadcasts Caravan Distress Calls when a Trader is ambushed so nearby or
    standby AI Hunters and fellow Traders rally to help defend them.

===========================================================================
*/
package caravanai

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
)

/*
================================================================================
Scheduling constants and DistressCall state
================================================================================
*/

const (
	// MinActiveTradeRuns is the minimum number of AI Traders that must be
	// actively running a caravan on the highway at any point in time.
	MinActiveTradeRuns = 2

	// TownRestDurationMs is how long an AI Trader hangs around town after
	// completing a delivery (or respawning) before voluntarily departing
	// again if MinActiveTradeRuns is already satisfied.
	TownRestDurationMs int64 = 30_000

	// DistressWindowMs is how long an ambushed Trader's distress beacon
	// stays active for Hunters and fellow Traders to rally to their aid.
	DistressWindowMs int64 = 25_000
)

// DistressCall represents an active cry for help from an ambushed Trader.
type DistressCall struct {
	Division     string
	TraderName   string
	AttackerName string
	RegionID     uint16
	X            float32
	Z            float32
	StartedAtMs  int64
	ExpiresAtMs  int64
}

/*
==================
raiseDistressLocked

Records or refreshes an active distress beacon for division when traderUnit is
attacked by attackerName. Caller holds r.mu.
==================
*/
func (r *Runtime) raiseDistressLocked(
	division string,
	traderUnit *AgentUnit,
	attackerName string,
	nowMs int64,
) {
	if traderUnit == nil || traderUnit.Role != RoleTrader {
		return
	}
	if r.distress == nil {
		r.distress = make(map[string]DistressCall)
	}
	div := strings.TrimSpace(division)
	r.distress[div] = DistressCall{
		Division:     div,
		TraderName:   traderUnit.Name,
		AttackerName: strings.TrimSpace(attackerName),
		RegionID:     traderUnit.RegionID,
		X:            traderUnit.X,
		Z:            traderUnit.Z,
		StartedAtMs:  nowMs,
		ExpiresAtMs:  nowMs + DistressWindowMs,
	}
}

/*
==================
activeDistressForLocked

Returns the active DistressCall for division if one exists at nowMs and was
not raised by unit itself. Caller holds r.mu.
==================
*/
func (r *Runtime) activeDistressForLocked(
	division string,
	unit *AgentUnit,
	nowMs int64,
) *DistressCall {
	if r.distress == nil || unit == nil || unit.Role == RoleThief {
		return nil
	}
	div := strings.TrimSpace(division)
	call, ok := r.distress[div]
	if !ok {
		return nil
	}
	if nowMs >= call.ExpiresAtMs {
		delete(r.distress, div)
		return nil
	}
	if strings.EqualFold(call.TraderName, unit.Name) {
		return nil
	}
	if unit.EscortingTrader != "" && !strings.EqualFold(unit.EscortingTrader, call.TraderName) {
		return nil
	}
	// Town agents stay hireable, hired agents stay with their hirer, and only
	// agents within reach answer.
	if (unit.EscortingTrader == "" && (unit.HiredBy != "" || unit.State == StateIdleInTown || unit.State == StateRecruiting)) ||
		WaypointDistance(position(unit), Waypoint{RegionID: call.RegionID, X: call.X, Z: call.Z}) > distressAnswerRadius {
		return nil
	}
	// Cornered or parleying traders cannot abandon their own survival to reinforce.
	if unit.Role == RoleTrader && (unit.UnderAttack || unit.State == StateParleying || percentOf(unit.HP, unit.MaxHP) < 60) {
		return nil
	}
	cp := call
	return &cp
}

func isActiveTradeRunState(state AgentState) bool {
	switch state {
	case StateTraveling, StateInCombat, StateParleying, StateReinforcing:
		return true
	default:
		return false
	}
}

/*
==================
dispatchTraderLocked

Sends a town Trader out as a run of the given kind with its escorts.
Caller holds r.mu.
==================
*/
func (r *Runtime) dispatchTraderLocked(unit *AgentUnit, kind RunKind, guards []*AgentUnit, nowMs int64) {
	if unit.CargoStacks <= 0 {
		unit.CargoStacks = 10
	}
	unit.RunKind = kind
	unit.AvailableForHire = false
	unit.HP = unit.MaxHP
	unit.MP = unit.MaxMP
	unit.PotionCharges = defaultPotionCharges(unit.Role)
	unit.transportHPPercent = FullHPPercent
	unit.UnderAttack = false
	unit.ReinforcingTrader = ""
	unit.State = StateTraveling
	unit.TownDepartAtMs = 0
	unit.Route = r.route(unit)
	unit.WaypointIndex = 0
	for _, guard := range guards {
		r.assignEscortLocked(guard, unit, kind)
	}
	for _, other := range r.agents[unit.Division] {
		if other.Role != RoleHunter || !strings.EqualFold(other.DedicatedEscortFor, unit.Name) {
			continue
		}
		if other.State != StateIdleInTown && other.State != StateRecruiting {
			continue
		}
		if _, contracted := r.contracts.ActiveContractFor(unit.Division, other.Name); contracted {
			continue
		}
		other.HP = other.MaxHP
		other.MP = other.MaxMP
		other.PotionCharges = defaultPotionCharges(other.Role)
		other.transportHPPercent = FullHPPercent
		other.UnderAttack = false
		other.AvailableForHire = false
		other.EscortingTrader = unit.Name
		other.ReinforcingTrader = ""
		other.OriginTown, other.DestTown = unit.OriginTown, unit.DestTown
		other.RegionID, other.X, other.Y, other.Z = unit.RegionID, unit.X, unit.Y, unit.Z
		other.TownDepartAtMs = 0
		other.State = StateTraveling
	}
	unit.LastSpokenLine = fmt.Sprintf(
		"Departing %s with a %d-star caravan bound for %s!",
		unit.OriginTown, unit.CargoStars, unit.DestTown,
	)
	unit.LastSpokeAtMs = nowMs
}

/*
==================
stepReinforcementLocked

Moves a reinforcing Hunter or fellow Trader toward the ambushed Trader's
distress coordinates, switching into combat against the attacker on arrival.
Caller holds r.mu.
==================
*/
func (r *Runtime) stepReinforcementLocked(unit *AgentUnit, call *DistressCall) {
	if unit == nil || call == nil {
		return
	}
	unit.State = StateReinforcing
	unit.AvailableForHire = false
	unit.ReinforcingTrader = call.TraderName
	r.moveLocked(unit, Waypoint{
		RegionID: call.RegionID,
		X:        call.X,
		Z:        call.Z,
	}, stepDistanceFor(unit))
	unit.LastAttacker = call.AttackerName
	if WaypointDistance(Waypoint{RegionID: unit.RegionID, X: unit.X, Z: unit.Z}, Waypoint{RegionID: call.RegionID, X: call.X, Z: call.Z}) <= 20 {
		unit.State = StateInCombat
	}
}

/*
==================
HandlePlayerCommand

HandlePlayerCommand parses interactive player chat commands directed at AI
caravan agents:
  - "!caravans"                 (Lists active trade runs, town-staged traders, and hireable/reinforcing hunters)
  - "!accept [TraderAgentName]" (Hunter player accepts an AI Trader escort run)
  - "!hire [AgentName]"         (Hires an idle agent for one trade run; without a name lists them)
  - "!leed"                     (The hirer leads its hired Traders instead of the first one in the party)
  - "!dismiss"                  (Ends every hire of the player now)
  - "!attack <AgentName> [dmg]" (Simulates an ambush strike, triggering distress aid or parley)
  - "!memory [AgentName]"       (Inspects how an AI agent remembers the player)

==================
*/
func (r *Runtime) HandlePlayerCommand(
	division, playerName string,
	playerJob uint8,
	rawText string,
	nowMs int64,
) (reply string, handled bool) {
	fields := strings.Fields(strings.TrimSpace(rawText))
	if len(fields) == 0 {
		return "", false
	}
	cmd := strings.ToLower(fields[0])
	division = strings.TrimSpace(division)
	r.EnsureDivisionRoster(division)
	defer r.flushOutbox(division)

	switch cmd {
	case "!leed", "!lead":
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.toggleLeadLocked(division, playerName), true

	case "!dismiss":
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.dismissLocked(division, playerName, nowMs), true

	case "!caravans", "!caravan":
		r.mu.Lock()
		r.scheduleRunsLocked(division, nowMs)
		roster := cloneUnits(r.agents[strings.TrimSpace(division)])
		r.mu.Unlock()
		var lines []string
		for _, u := range roster {
			lines = append(lines, formatAgentRosterStatus(u))
		}
		return strings.Join(lines, "\n"), true

	case "!accept":
		targetAgent := "[AI]Trader_Jin"
		if len(fields) >= 2 {
			targetAgent = fields[1]
		}
		if playerJob != domain.JobHunter {
			return "Only Hunter guild members can accept a caravan escort contract.", true
		}
		r.mu.Lock()
		unit := r.findAgentLocked(division, targetAgent)
		if unit == nil || unit.Role != RoleTrader {
			r.mu.Unlock()
			return fmt.Sprintf("No AI Trader named %s is recruiting here.", targetAgent), true
		}
		if !r.commandNearLocked(unit, division, playerName) || !hireable(unit) {
			r.mu.Unlock()
			return "Meet a recruiting Trader in town to accept an escort.", true
		}
		contract := r.contracts.BindAITraderEscort(division, unit.Name, playerName, unit.OriginTown, unit.DestTown, unit.CargoStars, nowMs)
		if !contract.Active {
			r.mu.Unlock()
			return "You or that Trader already have a contract.", true
		}
		r.dispatchTraderLocked(unit, RunEscorted, nil, nowMs)
		agentName := unit.Name
		r.mu.Unlock()
		if r.hooks.OnContractFormed != nil {
			r.hooks.OnContractFormed(division, playerName, agentName)
		}
		return FormatContractReply(contract), true

	case "!hire":
		if len(fields) < 2 {
			var names []string
			for _, offer := range r.HireOffers(division, playerJob) {
				names = append(names, offer.Name)
			}
			if len(names) == 0 {
				return "Nobody you can hire is free right now. Talk to a specialty shop to hire caravan help.", true
			}
			return "Hire with !hire <name>: " + strings.Join(names, ", "), true
		}
		return r.HireAgent(division, playerName, playerJob, fields[1], nowMs), true

	case "!memory":
		targetAgent := "[AI]Trader_Jin"
		if len(fields) >= 2 {
			targetAgent = fields[1]
		}
		row, ok := r.memories.Relationship(division, targetAgent, playerName)
		if !ok {
			return fmt.Sprintf("%s has no recorded history with %s yet (Trust: 0).", targetAgent, playerName), true
		}
		return fmt.Sprintf(
			"%s's memory of %s -> Trust: %d, Kills: %d, Deaths: %d, Gold Stolen: %d (%s)",
			row.AgentName, row.PlayerName, row.TrustScore, row.Kills, row.Deaths, row.GoldStolen, row.LastNote,
		), true

	case "!attack":
		return "Use your weapon or skills against an opposing job outside town.", true

	default:
		return "", false
	}
}

func formatAgentRosterStatus(u *AgentUnit) string {
	duty := string(u.State)
	if u.Role == RoleHunter {
		if u.State == StateReinforcing && u.ReinforcingTrader != "" {
			duty = fmt.Sprintf("REINFORCING(%s)", u.ReinforcingTrader)
		} else if u.EscortingTrader != "" {
			duty = fmt.Sprintf("ESCORTING(%s)", u.EscortingTrader)
		} else if u.AvailableForHire {
			duty = fmt.Sprintf("%s/AVAILABLE_FOR_HIRE", u.State)
		}
	} else if u.Role == RoleTrader && u.State == StateReinforcing && u.ReinforcingTrader != "" {
		duty = fmt.Sprintf("REINFORCING(%s)", u.ReinforcingTrader)
	}
	if u.HiredBy != "" {
		duty = fmt.Sprintf("HIRED(%s)", u.HiredBy)
	} else if u.RunKind != RunNone {
		duty = fmt.Sprintf("%s/%s", duty, strings.ToUpper(string(u.RunKind)))
	}
	if u.Role == RoleTrader {
		return fmt.Sprintf(
			"%s [%s/%s] HP:%d/%d Cargo:%d-star(%d stacks) Route:%s->%s Region:%d",
			u.Name, duty, u.Personality, u.HP, u.MaxHP, u.CargoStars, u.CargoStacks, u.OriginTown, u.DestTown, u.RegionID,
		)
	}
	return fmt.Sprintf(
		"%s [%s/%s] HP:%d/%d Route:%s->%s Region:%d",
		u.Name, duty, u.Personality, u.HP, u.MaxHP, u.OriginTown, u.DestTown, u.RegionID,
	)
}
