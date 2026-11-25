/*
===========================================================================

commands.go - the players' chat commands to the caravan agents

Port-only, not native (SRO_AI_CARAVANS; #3704 slice 4). !accept, !hire
and !memory are typed in ordinary chat and consumed by the chat lane,
which asks HandlePlayerCommand here. The replies are the agents' own
words; the chat lane speaks them as the named agent. Answers are
rate-limited per player (claimCommandSlot) so a command flood cannot
make an agent spam. Slice 5 (#3704) moves the hire fee through the
consumer's ChargeHireFee hook before a hire binds; the escort reward
side pays out of deliveries, never here.

===========================================================================
*/
package caravanai

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
)

// playerCommandCooldownMs spaces one player's answered caravan chat
// commands: at most one answer per player per window, the excess is
// consumed silently.
const playerCommandCooldownMs int64 = 5_000

/*
==================
HandlePlayerCommand

HandlePlayerCommand parses interactive player chat commands directed at AI
caravan agents:
  - "!accept <TraderAgentName>" (Hunter player accepts an AI Trader escort run)
  - "!hire <HunterAgentName>"   (Trader player hires an AI Hunter escort)
  - "!memory <AgentName>"       (Inspects how an AI agent remembers the player)

Answers are rate-limited per player (claimCommandSlot): inside the
cooldown the command is handled with an empty reply. Unrecognized text
is not handled, so it stays ordinary chat.
==================
*/
func (r *Runtime) HandlePlayerCommand(
	division, playerName string,
	playerJob uint8,
	rawText string,
	nowMs int64,
) (reply string, handled bool) {
	fields := strings.Fields(strings.TrimSpace(rawText))
	if len(fields) < 2 {
		return "", false
	}
	cmd := strings.ToLower(fields[0])
	targetAgent := fields[1]
	switch cmd {
	case "!accept", "!hire", "!memory":
	default:
		return "", false
	}
	// The commands are answered at most once per
	// playerCommandCooldownMs per player; the excess is consumed
	// silently (handled, no reply) so it never reaches the room as
	// ordinary chat either.
	if !r.claimCommandSlot(division, playerName, nowMs) {
		return "", true
	}
	r.EnsureDivisionRoster(division)

	switch cmd {
	case "!accept":
		if playerJob != domain.JobHunter {
			return "Only Hunter guild members can accept a caravan escort contract.", true
		}
		r.mu.Lock()
		unit := r.findAgentLocked(division, targetAgent)
		if unit == nil || unit.Role != RoleTrader {
			r.mu.Unlock()
			return fmt.Sprintf("No AI Trader named %s is recruiting here.", targetAgent), true
		}
		contract, bound := r.contracts.BindAITraderEscort(division, unit.Name, playerName, unit.OriginTown, unit.DestTown, unit.CargoStars, nowMs)
		r.mu.Unlock()
		if !bound {
			// Slice 5: one Hunter guards one caravan at a time.
			return "You already hold an escort contract, Hunter. Finish that run before you sign another.", true
		}
		return FormatContractReply(contract), true

	case "!hire":
		if playerJob != domain.JobTrader {
			return "Only Traders can hire an AI Hunter mercenary escort.", true
		}
		r.mu.Lock()
		unit := r.findAgentLocked(division, targetAgent)
		r.mu.Unlock()
		if unit == nil || unit.Role != RoleHunter {
			return fmt.Sprintf("No AI Hunter named %s is available for hire.", targetAgent), true
		}
		// Slice 5: the fee leaves the trader's purse before the hire
		// binds; a purse that cannot pay hires nobody. A nil hook
		// leaves hiring free, as before slice 5. The charge runs off
		// r.mu: the consumer's purse door must never nest inside the
		// runtime lock (the damage door nests the other way).
		if r.hooks.ChargeHireFee != nil && !r.hooks.ChargeHireFee(division, playerName, BaseHunterHireFeeGold, unit.Name) {
			return fmt.Sprintf("My hire costs %d gold, and your purse cannot pay it. No contract, no escort.", BaseHunterHireFeeGold), true
		}
		r.mu.Lock()
		contract := r.contracts.HireAIHunter(division, playerName, unit.Name, unit.OriginTown, unit.DestTown, nowMs)
		r.mu.Unlock()
		return FormatContractReply(contract), true

	case "!memory":
		row, ok := r.memories.Relationship(division, targetAgent, playerName)
		if !ok {
			return fmt.Sprintf("%s has no recorded history with %s yet (Trust: 0).", targetAgent, playerName), true
		}
		return fmt.Sprintf(
			"%s's memory of %s -> Trust: %d, Kills: %d, Deaths: %d, Gold Stolen: %d (%s)",
			row.AgentName, row.PlayerName, row.TrustScore, row.Kills, row.Deaths, row.GoldStolen, row.LastNote,
		), true
	default:
		return "", false
	}
}

/*
==================
claimCommandSlot

The command rate limit: true when the player may be answered now (and
stamps the slot); false means the caller consumes the line silently.
Refusals are answers too and cost the slot; unrecognized text never
reaches here, so it is never limited.
==================
*/
func (r *Runtime) claimCommandSlot(division, playerName string, nowMs int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastCommandAt == nil {
		r.lastCommandAt = make(map[string]int64)
	}
	key := division + "\x00" + strings.ToLower(playerName)
	if last, seen := r.lastCommandAt[key]; seen && nowMs-last < playerCommandCooldownMs {
		return false
	}
	r.lastCommandAt[key] = nowMs
	return true
}

/*
==================
AgentNamed

The canonical roster name for a player-typed agent name, so a command
reply can be authored by the agent itself; false when the division has
no such agent.
==================
*/
func (r *Runtime) AgentNamed(division, agentName string) (string, bool) {
	r.EnsureDivisionRoster(division)
	r.mu.Lock()
	defer r.mu.Unlock()
	unit := r.findAgentLocked(division, agentName)
	if unit == nil {
		return "", false
	}
	return unit.Name, true
}
