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
	"sort"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/chat"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
caravanRuntimeState

Port-only caravan state belongs with its feature wiring.
================
*/
type caravanRuntimeState struct {
	caravanSkills         map[string][]uint32
	caravanAttackAt       map[string]int64
	caravanAgents         *caravanai.Runtime
	caravanNextStepMs     int64
	caravanActors         []*enterworld.Character
	caravanMapAtMs        int64
	caravanMoving         map[string]bool
	caravanMode           map[string]uint8
	caravanLook           map[string]bool
	caravanRoads          map[string]*caravanRoad
	caravanIgnoredThreats map[string]map[uint32]int64
}

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
		Casting: game.items.ServerActorCasting,
		Ferry:   game.caravanFerry, FollowFerry: game.caravanFollowFerry,
		OnHireFee: game.caravanHireFee, OnHirePayout: game.caravanHirePayout,
		OnContractFormed: func(division, playerName, agentName string) {
			if game.parties != nil {
				game.parties.InvitePlayerFromRuntimeActor(division, agentName, playerName)
			}
		},
		OnHireEnded: func(division, playerName, agentName string, end caravanai.HireEnd, gold int64) {
			if game.parties != nil {
				game.parties.RemoveRuntimeActor(division, agentName)
			}
			name := caravanai.DisplayName(agentName)
			text := fmt.Sprintf("%s's hire has %s.", name, end)
			switch {
			case gold > 0 && end == caravanai.HireCompleted:
				text = fmt.Sprintf("%s sold the goods: %s gold paid to you.", name, caravanai.GoldText(gold))
			case gold > 0:
				text = fmt.Sprintf("%s's hire has %s: %s gold for the goods paid back.", name, end, caravanai.GoldText(gold))
			}
			game.caravanNotice(division, playerName, text)
		},
		OnContractReward: game.caravanEscortReward,
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
	game.caravanAgents = rt
	if err := game.initCaravanActors(); err != nil {
		log.WithError(err).Error("caravan actors disabled: invalid roster")
		_ = rt.Close()
		game.caravanAgents = nil
		return
	}
	if game.parties != nil {
		game.parties.CanPartyWithRuntimeActor = rt.IsContractPartner
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
		case "!caravans", "!caravan", "!accept", "!hire", "!memory", "!attack", "!leed", "!lead", "!dismiss", "!grandcaravan":
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
		if strings.EqualFold(fields[0], "!grandcaravan") {
			// GM only: start the grand caravan muster now (testing and events).
			gm := false
			game.deps.Read(divisionID, func() { gm = c.GMPrivilege })
			if !gm {
				return "", false
			}
			if !game.caravanAgents.StartGrandCaravan(divisionID, nowMs) {
				return "No grand caravan can muster now (one is mustering, or no Trader is free).", true
			}
			return "Grand caravan mustering.", true
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

		if c.GMPrivilege || simulation.BetaPlayerMapEnabled() {
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

	// Party order decides which hired Trader leads; read here, outside the
	// AI lock, because the party runtime calls back into the AI runtime.
	if game.parties != nil {
		for _, hirer := range game.caravanAgents.Hirers(game.divisionID) {
			game.caravanAgents.SetPartyOrder(game.divisionID, hirer, game.parties.MemberNames(game.divisionID, hirer))
		}
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
			cosHP, cosMaxHP, lost := game.items.ServerActorTransportVitals(game.divisionID, c.Name)
			game.caravanAgents.ObserveVitals(game.divisionID, c.Name, hp, mp, lost, nowMs)
			game.caravanAgents.ObserveTransportVitals(game.divisionID, c.Name, cosHP, cosMaxHP)
		}
		mustRejoin := game.caravanAgents.EscortMustRejoin(game.divisionID, unit.Name)
		var npcGID uint32
		var npcAt caravanai.Waypoint
		// Idle agents stroll in town, where no monster reaches them: no scan.
		if !mustRejoin && unit.State != caravanai.StateIdleInTown && unit.State != caravanai.StateRecruiting {
			npcGID, npcAt = game.caravanNPCThreat(unit, nowMs)
		}
		decision, updated, ok := game.caravanAgents.StepAgentWithWorldThreat(ctx, game.divisionID, unit.Name, nearby, npcGID != 0, nowMs)
		if !ok || updated == nil {
			updated = unit
		}
		casting := game.items.ServerActorCasting(game.divisionID, updated.Name)
		if npcGID != 0 && !mustRejoin && !casting {
			engaged, stalled := game.caravanAgents.ApproachWorldThreatAt(game.divisionID, updated.Name, npcAt, nowMs)
			if engaged != nil {
				updated = engaged
			}
			if stalled {
				game.ignoreCaravanThreat(updated.Name, npcGID, nowMs+30_000)
				npcGID = 0
			}
		}
		if i < len(game.caravanActors) {
			c := game.caravanActors[i]
			if updated.Role == caravanai.RoleTrader && !casting {
				game.publishCaravanAction(c, game.items.ServerActorRide(game.divisionID, c.Name, updated.State != caravanai.StateInCombat && updated.State != caravanai.StateParleying))
			}
			game.publishCaravanActor(ticker, views, unit, updated, c, nowMs)
			if npcGID != 0 && !mustRejoin {
				npcDist := caravanai.WaypointDistance(caravanai.Waypoint{RegionID: updated.RegionID, X: updated.X, Z: updated.Z}, npcAt)
				if caravanai.InCastRange(updated, npcDist) {
					game.publishCaravanAction(c, game.caravanCast(c, npcGID, nowMs))
				}
			} else if decision.Action == caravanai.ActionAttackTarget && !mustRejoin {
				if target := game.deps.CharacterByName(game.divisionID, decision.TargetName); target != nil {
					tSpawn := game.items.LiveSpawnFor(game.divisionID, target)
					targetDist := caravanai.WaypointDistance(
						caravanai.Waypoint{RegionID: updated.RegionID, X: updated.X, Z: updated.Z},
						caravanai.Waypoint{RegionID: tSpawn.RegionID, X: float32(tSpawn.X), Z: float32(tSpawn.Z)},
					)
					if caravanai.InCastRange(updated, targetDist) {
						result := game.caravanCast(c, enterworld.ObjectIDForCharacter(target), nowMs)
						game.publishCaravanAction(c, result)
					}
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

func formatCaravanTownName(town caravanai.TownID) string {
	switch town {
	case caravanai.TownJangan:
		return "Jangan"
	case caravanai.TownDonwhang:
		return "Donwhang"
	case caravanai.TownHotan:
		return "Hotan"
	case caravanai.TownThiefDen:
		return "Thief Den"
	default:
		return string(town)
	}
}

func formatCaravanMapLabel(u *caravanai.AgentUnit) string {
	if u.HiredBy != "" {
		return fmt.Sprintf("%s (Hired by %s)", u.Name, u.HiredBy)
	}
	if u.RunKind == caravanai.RunGrand {
		return fmt.Sprintf("%s (Grand caravan %s -> %s)", u.Name, formatCaravanTownName(u.OriginTown), formatCaravanTownName(u.DestTown))
	}
	switch u.Role {
	case caravanai.RoleTrader:
		if u.State == caravanai.StateIdleInTown || u.State == caravanai.StateRecruiting {
			return fmt.Sprintf("%s (%d* %s)", u.Name, u.CargoStars, formatCaravanTownName(u.OriginTown))
		}
		return fmt.Sprintf("%s (%d* %s -> %s)", u.Name, u.CargoStars, formatCaravanTownName(u.OriginTown), formatCaravanTownName(u.DestTown))
	case caravanai.RoleHunter:
		if u.State == caravanai.StateReinforcing && u.ReinforcingTrader != "" {
			return fmt.Sprintf("%s (Aiding %s)", u.Name, u.ReinforcingTrader)
		}
		if u.EscortingTrader != "" {
			return fmt.Sprintf("%s (Escorting %s)", u.Name, u.EscortingTrader)
		}
		if u.AvailableForHire {
			return fmt.Sprintf("%s (Hireable - %s)", u.Name, formatCaravanTownName(u.OriginTown))
		}
		return fmt.Sprintf("%s (Patrol)", u.Name)
	default:
		if u.AvailableForHire {
			return fmt.Sprintf("%s (Hireable - %s)", u.Name, formatCaravanTownName(u.OriginTown))
		}
		return fmt.Sprintf("%s (Thief - %s)", u.Name, formatCaravanTownName(u.OriginTown))
	}
}

/*
================
caravanNotice
================
*/
func (game *gameplayPlane) caravanNotice(division, player, text string) {
	if game.presence == nil {
		return
	}
	if session, ok := game.presence.SessionByName(division, player); ok && session.WorldReady() {
		_ = session.Send(chat.OpChatBroadcast, chat.EncodeChatNotice(text))
	}
}

/*
================
caravanHireOptions / caravanHireService

The specialty shops' talk menu lists every agent the player may hire now
(port-only rows; the client shows "[AI]" symbols as written).
================
*/
const (
	caravanHireServicePrefix = "caravan-hire:"
	// caravanTextSymbol marks a port-only dialog symbol that is its own text
	// (client ui.ts hudCopy shows what follows it).
	caravanTextSymbol  = "UIIT_PORT_CARAVAN:"
	caravanThiefDenNPC = "NPC_TD_THIEF_BUY"
)

func (game *gameplayPlane) caravanHireOptions(divisionID string, character *enterworld.Character, npcCodename string) []action.NpcQuestOption {
	if game.caravanAgents == nil || !game.caravanAgents.Enabled() {
		return nil
	}
	// City specialty shops hire Hunters and Traders; the Thief Den's buyer hires Thieves.
	var den bool
	switch npcCodename {
	case "NPC_CH_SPECIAL", "NPC_WC_SPECIAL", "NPC_KT_SPECIAL":
	case caravanThiefDenNPC:
		den = true
	default:
		return nil
	}
	var job uint8
	game.deps.Read(divisionID, func() { job = enterworld.DressedJob(character) })
	here := map[string]caravanai.TownID{"NPC_CH_SPECIAL": caravanai.TownJangan, "NPC_WC_SPECIAL": caravanai.TownDonwhang, "NPC_KT_SPECIAL": caravanai.TownHotan}[npcCodename]
	offers := game.caravanAgents.HireOffers(divisionID, job)
	// The ones strolling around this shop first.
	sort.SliceStable(offers, func(i, j int) bool { return offers[i].Town == here && offers[j].Town != here })
	var out []action.NpcQuestOption
	for _, offer := range offers {
		if (offer.Role == caravanai.RoleThief) != den {
			continue
		}
		label := caravanHireLabel(offer)
		out = append(out, action.NpcQuestOption{Codename: caravanHireServicePrefix + offer.Name, TitleSymbol: caravanTextSymbol + label, PromptSymbol: caravanTextSymbol + label, Immediate: true})
	}
	return out
}

/*
================
caravanHireLabel

One talk row: "Hire Kwan, Hunter from Donwhang (15,000 gold)".
================
*/
func caravanHireLabel(offer caravanai.HireOffer) string {
	name, fee := caravanai.DisplayName(offer.Name), caravanai.GoldText(offer.Fee)
	if offer.Goods > 0 {
		return fmt.Sprintf("Hire %s, Trader from %s (%s gold + %s for goods)", name, formatCaravanTownName(offer.Town), fee, caravanai.GoldText(offer.Goods))
	}
	if offer.Role == caravanai.RoleThief {
		return fmt.Sprintf("Hire %s, Thief (%s gold)", name, fee)
	}
	return fmt.Sprintf("Hire %s, %s from %s (%s gold)", name, offer.Role.Title(), formatCaravanTownName(offer.Town), fee)
}

func (game *gameplayPlane) caravanHireService(character *enterworld.Character, codename string) ([]wire.Frame, bool) {
	agent, ok := strings.CutPrefix(codename, caravanHireServicePrefix)
	if !ok || game.caravanAgents == nil {
		return nil, false
	}
	var job uint8
	var name string
	game.deps.Read(game.divisionID, func() { job, name = enterworld.DressedJob(character), character.Name })
	reply := game.caravanAgents.HireAgent(game.divisionID, name, job, agent, time.Now().UnixMilli())
	return []wire.Frame{
		{Opcode: chat.OpChatBroadcast, Payload: chat.EncodeChatNotice(reply)},
		{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(caravanTextSymbol + reply)},
	}, true
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
