/*
===========================================================================

wiring_caravanai.go - AI Caravan & Thief agent composition on this shard

Port-only, not native (SRO_AI_CARAVANS). When enabled, wires the AI Caravan
& Thief runtime (internal/game/caravanai) to the shard's SQLite authority
store for bounded Grudge & Trust memories and route danger heatmaps, and
steps the Jangan <-> Donwhang <-> Hotan caravan roster on the mission clock.
With the flag off nothing opens, no hook installs, and native behaviour is
unchanged.

===========================================================================
*/
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	caravanStepIntervalMs int64 = 100
)

/*
================
wireCaravanAgents

Called from wireJobRules at GameWorld boot. Reads SRO_AI_CARAVANS and
SRO_AGENT_BRAIN; when enabled, constructs the runtime over the shard's
persistent store and initializes the Jangan-Donwhang-Hotan agent roster.
================
*/
func (game *gameplayPlane) wireCaravanAgents() {
	cfg := caravanai.ConfigFromEnv()
	if !cfg.Enabled {
		return
	}
	if cfg.RefusedLLMURL != "" {
		// The LLM brain may only reach a model on this host (config.go).
		log.Warnf("job: %s is not a loopback URL; AI caravan agents use the hardcoded brain", caravanai.EnvAgentLLMURL)
	}
	var memStore caravanai.MemoryStore
	if s, ok := game.deps.Characters.(caravanai.MemoryStore); ok {
		memStore = s
	}
	if err := game.configureCaravanHighway(&cfg); err != nil {
		log.WithError(err).Error("AI caravan highway disabled")
		return
	}
	rt := caravanai.NewRuntime(cfg, memStore, caravanai.WorldHooks{
		Move: game.caravanMove, Player: game.caravanPlayer, Heal: game.items.HealServerActor,
		Ferry: game.caravanFerry, FollowFerry: game.caravanFollowFerry,
		OnHireFee: game.caravanHireFee, OnContractReward: game.caravanEscortReward,
		OnParleyCargoDrop: func(division, name string, _ uint16, _, _, _ float32, stacks int, _ int64) {
			frames := game.items.DropServerActorCargo(division, name, stacks)
			if c := game.deps.CharacterByName(division, name); c != nil {
				game.items.PushDivisionPeerFrames(division, name, frames)
			}
		},
		OnAgentChat: func(division, agentName string, regionID uint16, message string) {
			log.WithFields(log.Fields{
				"division": division,
				"agent":    agentName,
				"region":   regionID,
			}).Debugf("caravan-ai chat: %s", message)
			if game.chat != nil {
				game.chat.PublishAgentChat(division, agentName, message)
			}
		},
	})
	rt.EnsureDivisionRoster(game.divisionID)
	for _, npc := range game.items.NpcRoster {
		if npc.Codename == "NPC_CH_EUROPE" {
			rt.StageRecruiters(game.divisionID, caravanai.TownJangan, caravanai.Waypoint{RegionID: npc.Spawn.RegionID, X: float32(npc.Spawn.X), Y: float32(npc.Spawn.Y), Z: float32(npc.Spawn.Z)})
			break
		}
	}
	game.caravanAgents = rt
	if err := game.initCaravanActors(); err != nil {
		log.WithError(err).Error("caravan actors disabled: invalid roster")
		_ = rt.Close()
		game.caravanAgents = nil
		return
	}
	log.Infof(
		"job: AI caravan & thief agents ON (%s, %s=%s, highway=Jangan-Donwhang-Hotan)",
		caravanai.EnvAICaravans,
		caravanai.EnvAgentBrain,
		cfg.BrainMode,
	)
}

/*
================
wireCaravanChat

Connects roster, memory and escort chat commands once
the chat runtime is registered.
================
*/
func (game *gameplayPlane) wireCaravanChat() {
	if game.caravanAgents == nil || game.chat == nil {
		return
	}
	game.chat.CaravanCommand = func(divisionID, playerName string, playerJob uint8, message string) (string, bool) {
		fields := strings.Fields(message)
		if len(fields) == 0 {
			return "", false
		}
		switch strings.ToLower(fields[0]) {
		case "!caravans", "!caravan", "!accept", "!hire", "!memory", "!attack":
		default:
			return "", false
		}
		c := game.deps.CharacterByName(divisionID, playerName)
		if c == nil {
			return "Character unavailable.", true
		}
		nowMs := time.Now().UnixMilli()
		admitted := game.deps.Update(c, "caravan command", func() bool {
			if nowMs-c.CaravanCommandAtMs < 1000 {
				return false
			}
			c.CaravanCommandAtMs = nowMs
			playerJob = enterworld.DressedJob(c)
			return true
		})
		if !admitted {
			return "Wait a second before another caravan command.", true
		}
		if strings.EqualFold(fields[0], "!accept") {
			capped := false
			game.deps.Read(divisionID, func() {
				capped = c.CaravanRewardDay == time.Now().UTC().Unix()/86400 && c.CaravanRewardCount >= caravanDailyRewardLimit
			})
			if capped {
				return "You have received today's three caravan escort rewards.", true
			}
		}
		return game.caravanAgents.HandlePlayerCommand(divisionID, playerName, playerJob, message, nowMs)
	}
}

/*
================
worldNpcsForInstance

Returns the quest-gate NPCs. Caravans use the separate player appearance lane.
================
*/
func (game *gameplayPlane) worldNpcsForInstance(worldInstance uint32) []simulation.NpcDef {
	var out []simulation.NpcDef
	if game.items != nil {
		out = game.items.QuestGateNpcs(worldInstance)
	}
	if worldInstance != domain.DefaultWorldInstance || game.caravanAgents == nil || !game.caravanAgents.Enabled() {
		return out
	}
	return out
}

/*
================
caravanAgentsTick

Advances the shard's AI Caravan & Thief agents every 100ms on the
simulation clock, matching each agent against entered players in the same
region and pushing live World Map (M) markers to admin (GMPrivilege) sessions.
================
*/
func (game *gameplayPlane) caravanAgentsTick(ticker *simulation.Ticker, nowMs int64) []simulation.DivisionFrames {
	if game.caravanAgents == nil || !game.caravanAgents.Enabled() {
		return nil
	}
	if nowMs < game.caravanNextStepMs {
		return nil
	}
	game.caravanNextStepMs = nowMs + caravanStepIntervalMs

	views := sessionViews(ticker.Source)
	var adminSessions []string
	var mapPlayers []wire.BetaMapPlayer

	for _, view := range views {
		if view.DivisionID != game.divisionID {
			continue
		}
		c := game.deps.CharacterByID(game.divisionID, view.CharacterID)
		if c == nil {
			continue
		}
		pose := view.World.LiveSpawnAt(nowMs)

		if c.GMPrivilege {
			adminSessions = append(adminSessions, view.SessionID)
		}
		mapPlayers = append(mapPlayers, wire.BetaMapPlayer{
			Gid:      simulation.PlayerObjectID(view.CharacterID),
			RegionID: pose.RegionID,
			X:        float32(pose.X),
			Z:        float32(pose.Z),
			Name:     c.Name,
		})
	}

	roster := game.caravanAgents.EnsureDivisionRoster(game.divisionID)
	ctx := context.Background()
	for i, unit := range roster {
		nearby := make([]caravanai.ObservedPlayer, 0)
		for _, other := range roster {
			if allowed, known := worldgeom.RegionPlayerCombat(other.RegionID); !known || !allowed {
				continue
			}
			if other.Name == unit.Name {
				continue
			}
			distance := float32(caravanai.WaypointDistance(caravanai.Waypoint{RegionID: unit.RegionID, X: unit.X, Z: unit.Z}, caravanai.Waypoint{RegionID: other.RegionID, X: other.X, Z: other.Z}))
			if distance <= 320 {
				nearby = append(nearby, caravanai.ObservedPlayer{Name: other.Name, JobType: caravanJob(other.Role), HPPercent: caravanai.HPPercent(int64(other.HP), int64(other.MaxHP)), Distance: distance})
			}
		}
		for _, view := range views {
			if view.DivisionID != game.divisionID || view.WorldInstance != domain.DefaultWorldInstance {
				continue
			}
			c := game.deps.CharacterByID(game.divisionID, view.CharacterID)
			if c == nil {
				continue
			}
			if p, ok := game.caravanPlayer(game.divisionID, c.Name); ok {
				p.Distance = float32(caravanai.WaypointDistance(p.At, caravanai.Waypoint{RegionID: unit.RegionID, X: unit.X, Z: unit.Z}))
				if p.Distance <= 320 {
					nearby = append(nearby, p.ObservedPlayer)
				}
			}
		}
		if i < len(game.caravanActors) {
			c := game.caravanActors[i]
			hp, _, mp, _ := game.items.GameplayVitals(game.divisionID, c)
			lost := false
			game.deps.Read(game.divisionID, func() { lost = c.RuntimeTransportGID != 0 && c.ActiveCOS == nil })
			game.caravanAgents.ObserveVitals(game.divisionID, c.Name, hp, mp, lost, nowMs)
		}
		decision, updated, ok := game.caravanAgents.StepAgent(ctx, game.divisionID, unit.Name, nearby, nowMs)
		if !ok || updated == nil {
			updated = unit
		}
		if i < len(game.caravanActors) {
			c := game.caravanActors[i]
			game.publishCaravanActor(ticker, views, unit, updated, c, nowMs)
			if decision.Action == caravanai.ActionAttackTarget {
				if target := game.deps.CharacterByName(game.divisionID, decision.TargetName); target != nil {
					result := game.items.HandleTargetInteract(game.divisionID, c, (wire.SkillAction{ActionId: unit.PrimarySkillID, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(target)}).Encode())
					game.publishCaravanAction(c, result)
				}
			}
		}
		mapPlayers = append(mapPlayers, wire.BetaMapPlayer{Gid: uint32(caravanActorIDBase+int64(i)) + domain.PlayerGIDBase, RegionID: updated.RegionID, X: updated.X, Z: updated.Z, Name: formatCaravanMapLabel(updated)})
	}

	if nowMs >= game.caravanMapAtMs && len(adminSessions) > 0 && len(mapPlayers) > 0 {
		game.caravanMapAtMs = nowMs + 2000
		payload := wire.EncodeBetaPlayerMap(mapPlayers)
		frame := []simulation.Frame{{Opcode: wire.OpBetaPlayerMap, Payload: payload}}
		for _, sessionID := range adminSessions {
			ticker.Push.PushToSession(sessionID, frame)
		}
	}
	return nil
}

func formatCaravanMapLabel(u *caravanai.AgentUnit) string {
	switch u.Role {
	case caravanai.RoleTrader:
		if u.State == caravanai.StateIdleInTown || u.State == caravanai.StateRecruiting {
			return fmt.Sprintf("%s (%d* Town)", u.Name, u.CargoStars)
		}
		return fmt.Sprintf("%s (%d* %s->%s)", u.Name, u.CargoStars, u.OriginTown, u.DestTown)
	case caravanai.RoleHunter:
		if u.State == caravanai.StateReinforcing && u.ReinforcingTrader != "" {
			return fmt.Sprintf("%s (Aid:%s)", u.Name, u.ReinforcingTrader)
		}
		if u.EscortingTrader != "" {
			return fmt.Sprintf("%s (Escort:%s)", u.Name, u.EscortingTrader)
		}
		if u.AvailableForHire {
			return fmt.Sprintf("%s (Hire)", u.Name)
		}
		return fmt.Sprintf("%s (Patrol)", u.Name)
	default:
		return fmt.Sprintf("%s (Ambush)", u.Name)
	}
}

/*
================
playerHPPercent

A player's live HP as a percentage of the effective maximum the gameplay
uses (gear and statuses included, action.Runtime.GameplayVitals), so the
agents' "cornered" and parley rules read what the player's own gauge shows.
================
*/
func (game *gameplayPlane) playerHPPercent(c *enterworld.Character) int {
	if game.items == nil {
		return caravanai.FullHPPercent
	}
	current, maximum, _, _ := game.items.GameplayVitals(game.divisionID, c)
	return caravanai.HPPercent(current, maximum)
}
