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

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/world/simulation"
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
	var memStore caravanai.MemoryStore
	if s, ok := game.deps.Characters.(caravanai.MemoryStore); ok {
		memStore = s
	}
	rt := caravanai.NewRuntime(cfg, memStore, caravanai.WorldHooks{
		OnAgentChat: func(division, agentName string, regionID uint16, message string) {
			log.WithFields(log.Fields{
				"division": division,
				"agent":    agentName,
				"region":   regionID,
			}).Debugf("caravan-ai chat: %s", message)
		},
	})
	rt.EnsureDivisionRoster(game.divisionID)
	game.caravanAgents = rt
	log.Infof(
		"job: AI caravan & thief agents ON (%s, %s=%s, highway=Jangan-Donwhang-Hotan)",
		caravanai.EnvAICaravans,
		caravanai.EnvAgentBrain,
		cfg.BrainMode,
	)
}

/*
================
caravanAgentsTick

Advances the shard's AI Caravan & Thief agents on the simulation clock,
matching each agent against entered players in the same region.
================
*/
func (game *gameplayPlane) caravanAgentsTick(ticker *simulation.Ticker, nowMs int64) []simulation.DivisionFrames {
	if game.caravanAgents == nil || !game.caravanAgents.Enabled() {
		return nil
	}
	views := sessionViews(ticker.Source)
	byRegion := make(map[uint16][]caravanai.ObservedPlayer)
	for _, view := range views {
		if view.DivisionID != game.divisionID {
			continue
		}
		c := game.deps.CharacterByID(game.divisionID, view.CharacterID)
		if c == nil {
			continue
		}
		pose := view.World.LiveSpawnAt(nowMs)
		byRegion[pose.RegionID] = append(byRegion[pose.RegionID], caravanai.ObservedPlayer{
			Name:      c.Name,
			JobType:   c.Job.Type,
			HPPercent: 100,
		})
	}
	roster := game.caravanAgents.EnsureDivisionRoster(game.divisionID)
	ctx := context.Background()
	for _, unit := range roster {
		_, _, _ = game.caravanAgents.StepAgent(ctx, game.divisionID, unit.Name, byRegion[unit.RegionID], nowMs)
	}
	return nil
}
