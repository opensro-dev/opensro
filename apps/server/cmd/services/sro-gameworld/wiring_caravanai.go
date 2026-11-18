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
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
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
	caravanSpeedAtMs      map[string]int64
	caravanSpeedRefused   map[string]bool
	caravanSpeedSeen      bool
	caravanLootAtMs       map[string]int64
	caravanLootSeen       map[string]uint32
	caravanLootLogAtMs    int64
	caravanGround         []grounditem.Item
	caravanGroundAtMs     int64
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
		UseItem: game.caravanUseItem,
		PickUp:  game.caravanPickUp,
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
	if game.items != nil {
		game.items.CaravanHire = game.caravanHireRequest
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
			game.keepCaravanSpeed(c, unit, nowMs)
			game.showCaravanLoot(c, unit, nearby, nowMs)
			hp, _, mp, _ := game.items.GameplayVitals(game.divisionID, c)
			cosHP, cosMaxHP, lost := game.items.ServerActorTransportVitals(game.divisionID, c.Name)
			game.caravanAgents.ObserveVitals(game.divisionID, c.Name, hp, mp, lost, nowMs)
			game.caravanAgents.ObserveTransportVitals(game.divisionID, c.Name, cosHP, cosMaxHP)
		}
		mustRejoin := game.caravanAgents.EscortMustRejoin(game.divisionID, unit.Name)
		var npcGID uint32
		var npcAt caravanai.Waypoint
		// Idle agents stroll in town, where no monster reaches them: no scan.
		// A Hunter with an AI or player Thief in sight goes for the Thief
		// before any of the world's own monsters.
		if !mustRejoin && unit.State != caravanai.StateIdleInTown && unit.State != caravanai.StateRecruiting && !(unit.Role == caravanai.RoleHunter && thiefInSight(nearby)) {
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

// thiefInSight reports an AI or player Thief among the observed.
func thiefInSight(nearby []caravanai.ObservedPlayer) bool {
	for _, p := range nearby {
		if p.JobType == domain.JobThief && p.HPPercent > 0 {
			return true
		}
	}
	return false
}

/*
================
caravanUseItem / keepCaravanSpeed

Agents drink real potions from their bags (action.ServerActorUseItem),
seen by everyone around like a player's; and keep a 50% speed potion
running, which their own movement follows. A mounted Trader rides at the
transport's speed: the potion does not move a transport, as natively.
================
*/
func (game *gameplayPlane) caravanUseItem(division, name, codename string) bool {
	c := game.deps.CharacterByName(division, name)
	if c == nil {
		return false
	}
	result := game.items.ServerActorUseItem(division, name, codename)
	game.publishCaravanAction(c, result)
	return action.ItemUsed(result)
}

func (game *gameplayPlane) keepCaravanSpeed(c *enterworld.Character, unit *caravanai.AgentUnit, nowMs int64) {
	if unit.RespawnPending || unit.HP <= 0 || nowMs < game.caravanSpeedAtMs[c.Name] {
		return
	}
	if game.caravanSpeedAtMs == nil {
		game.caravanSpeedAtMs = make(map[string]int64)
	}
	game.caravanSpeedAtMs[c.Name] = nowMs + 2000
	buffed := game.items.ServerActorMovementBuff(game.divisionID, c.Name)
	if !buffed {
		buffed = game.caravanUseItem(game.divisionID, c.Name, caravanai.SpeedPotionItem)
		switch {
		case !buffed && !game.caravanSpeedRefused[c.Name]:
			if game.caravanSpeedRefused == nil {
				game.caravanSpeedRefused = make(map[string]bool)
			}
			game.caravanSpeedRefused[c.Name] = true
			log.Warnf("caravan-ai: %s could not drink %s", c.Name, caravanai.SpeedPotionItem)
		case buffed && !game.caravanSpeedSeen:
			game.caravanSpeedSeen = true
			log.Infof("caravan-ai: agents keep %s running (first: %s)", caravanai.SpeedPotionItem, c.Name)
		}
	}
	run, factor := game.caravanRunSpeed(c), float32(1)
	if buffed && (c.ActiveCOS == nil || !c.ActiveCOS.Mounted) {
		// The potion's speed (the client's committed speed); a Hunter's
		// own 75 is already that fast.
		run, factor = max(run, simulation.RunSpeed*caravanai.SpeedPotionFactor), caravanai.SpeedPotionFactor
	}
	if run != unit.RunSpeed {
		game.caravanAgents.SetRunSpeed(game.divisionID, c.Name, run, factor)
	}
}

/*
================
showCaravanLoot / caravanPickUp

Once a second each agent that looks for loot (caravanai.LootSought) is
shown the nearest item it may take within 30m: a Trader its own dropped
goods, while no Thief is in sight; a Thief other caravans' goods, gold
and items. It walks there and picks it up through the native grant.
================
*/
func (game *gameplayPlane) showCaravanLoot(c *enterworld.Character, unit *caravanai.AgentUnit, nearby []caravanai.ObservedPlayer, nowMs int64) {
	if nowMs < game.caravanLootAtMs[c.Name] {
		return
	}
	if game.caravanLootAtMs == nil {
		game.caravanLootAtMs = make(map[string]int64)
	}
	game.caravanLootAtMs[c.Name] = nowMs + 1000
	trader := unit.Role == caravanai.RoleTrader
	if !game.caravanAgents.LootSought(game.divisionID, c.Name, nowMs) || (trader && thiefInSight(nearby)) {
		game.caravanAgents.NoteLoot(game.divisionID, c.Name, 0, caravanai.Waypoint{}, nowMs)
		return
	}
	here := caravanai.Waypoint{RegionID: unit.RegionID, X: unit.X, Z: unit.Z}
	if nowMs >= game.caravanGroundAtMs {
		// One read of the ground a second serves every agent.
		game.caravanGround, game.caravanGroundAtMs = game.items.GroundItems(game.divisionID), nowMs+1000
	}
	var best caravanai.Waypoint
	var bestGID uint32
	bestDist := caravanai.LootReach
	nearest := math.MaxFloat64
	seen := game.items.ServerActorLootNear(game.divisionID, c.Name, trader, game.caravanGround, func(p grounditem.Point) bool {
		return caravanai.WaypointDistance(here, caravanai.Waypoint{RegionID: p.RegionID, X: p.X, Z: p.Z}) <= caravanai.LootReach
	})
	for _, loot := range seen {
		at := caravanai.Waypoint{RegionID: loot.RegionID, X: loot.X, Y: loot.Y, Z: loot.Z}
		d := caravanai.WaypointDistance(here, at)
		nearest = min(nearest, d)
		if d <= bestDist {
			best, bestGID, bestDist = at, loot.Gid, d
		}
	}
	if unit.HiredBy != "" && len(seen) > 0 && bestGID == 0 && nowMs >= game.caravanLootLogAtMs {
		game.caravanLootLogAtMs = nowMs + 30_000
		log.Debugf("caravan-ai: %s sees %d loot, nearest %.0f units (reach %.0f)", c.Name, len(seen), nearest, caravanai.LootReach)
	}
	if bestGID != 0 && game.caravanLootSeen[c.Name] != bestGID {
		if game.caravanLootSeen == nil {
			game.caravanLootSeen = make(map[string]uint32)
		}
		game.caravanLootSeen[c.Name] = bestGID
		log.Debugf("caravan-ai: %s goes for ground item %d (%.0f units away)", c.Name, bestGID, bestDist)
	}
	game.caravanAgents.NoteLoot(game.divisionID, c.Name, bestGID, best, nowMs)
}

func (game *gameplayPlane) caravanPickUp(division, name string, itemGID uint32) bool {
	c := game.deps.CharacterByName(division, name)
	if c == nil {
		return false
	}
	result := game.items.ServerActorPickup(division, name, itemGID)
	game.publishCaravanAction(c, result)
	picked := len(result.Broadcast) > 0
	if picked {
		log.Debugf("caravan-ai: %s picked up ground item %d", name, itemGID)
	} else {
		log.WithField("refusal", result.DiagnosticRefusal).Warnf("caravan-ai: %s could not pick up ground item %d", name, itemGID)
	}
	return picked
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
	label := "Hire Hunters and Traders"
	if den {
		label = "Hire Thieves"
	}
	// One row opens the hire window (OpCaravanHire).
	return []action.NpcQuestOption{{Codename: caravanHireServicePrefix + "window", TitleSymbol: caravanTextSymbol + label, PromptSymbol: caravanTextSymbol + label, Immediate: true}}
}

func (game *gameplayPlane) caravanHireService(character *enterworld.Character, codename string) ([]wire.Frame, bool) {
	if _, ok := strings.CutPrefix(codename, caravanHireServicePrefix); !ok || game.caravanAgents == nil {
		return nil, false
	}
	return []wire.Frame{game.caravanHireWindow(character, "")}, true
}

/*
================
caravanHireWindow / caravanHireRequest

The hire window (port-only 0x3FC3): every agent hired where the player
stands, available ones first, with the Trader goods choices. The request
(0x7FC3) refreshes it or hires one agent and answers with the window again.
================
*/
type caravanHireRowJSON struct {
	Name      string `json:"name"`
	Display   string `json:"display"`
	Role      string `json:"role"`
	Town      string `json:"town"`
	Available bool   `json:"available"`
	Status    string `json:"status"`
}

type caravanHireStarsJSON struct {
	Stars uint8  `json:"stars"`
	Goods string `json:"goods"`
	Total string `json:"total"`
}

type caravanHireJSON struct {
	Version int                    `json:"version"`
	Title   string                 `json:"title"`
	Fee     string                 `json:"fee"`
	Message string                 `json:"message"`
	Rows    []caravanHireRowJSON   `json:"rows"`
	Stars   []caravanHireStarsJSON `json:"stars"`
}

func (game *gameplayPlane) caravanHireWindow(character *enterworld.Character, message string) wire.Frame {
	var job uint8
	var name string
	game.deps.Read(game.divisionID, func() { job, name = enterworld.DressedJob(character), character.Name })
	out := caravanHireJSON{Version: 1, Title: "Caravan Hire", Fee: caravanai.GoldText(caravanai.HireFeeGold), Message: message, Rows: []caravanHireRowJSON{}, Stars: []caravanHireStarsJSON{}}
	p, ok := game.caravanPlayer(game.divisionID, name)
	place, inPlace := caravanai.TownID(""), false
	if ok {
		place, inPlace = game.caravanAgents.PlaceAt(p.At)
	}
	if !inPlace {
		if out.Message == "" {
			out.Message = "Caravan help is hired at a specialty shop or in the Thief Den."
		}
	} else {
		if place == caravanai.TownThiefDen {
			out.Title = "Thieves for Hire"
		} else {
			out.Title = "Hunters and Traders for Hire - " + formatCaravanTownName(place)
			for s := uint8(1); s <= caravanai.MaxHireStars; s++ {
				goods := caravanai.GoodsForStars(s)
				out.Stars = append(out.Stars, caravanHireStarsJSON{Stars: s, Goods: caravanai.GoldText(goods), Total: caravanai.GoldText(caravanai.HireFeeGold + goods)})
			}
		}
		for _, row := range game.caravanAgents.HireRoster(game.divisionID, name, job, place) {
			out.Rows = append(out.Rows, caravanHireRowJSON{
				Name: row.Name, Display: caravanai.DisplayName(row.Name), Role: row.Role.Title(),
				Town: formatCaravanTownName(row.Town), Available: row.Available, Status: row.Status,
			})
		}
	}
	payload, _ := json.Marshal(out)
	return wire.Frame{Opcode: action.OpCaravanHire, Payload: payload}
}

func (game *gameplayPlane) caravanHireRequest(division string, character *enterworld.Character, payload []byte) []wire.Frame {
	if game.caravanAgents == nil || division != game.divisionID {
		return nil
	}
	if payload[0] == 0 {
		return []wire.Frame{game.caravanHireWindow(character, "")}
	}
	var job uint8
	var name string
	game.deps.Read(division, func() { job, name = enterworld.DressedJob(character), character.Name })
	reply := game.caravanAgents.HireAgentStars(division, name, job, string(payload[2:]), payload[1], time.Now().UnixMilli())
	return []wire.Frame{
		{Opcode: chat.OpChatBroadcast, Payload: chat.EncodeChatNotice(reply)},
		game.caravanHireWindow(character, reply),
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
