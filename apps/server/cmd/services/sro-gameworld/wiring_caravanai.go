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
	"opensro.online/server/internal/game/enterworld"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
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
	if cfg.RefusedLLMURL != "" {
		// The LLM brain may only reach a model on this host (config.go).
		log.Warnf("job: %s is not a loopback URL; AI caravan agents use the hardcoded brain", caravanai.EnvAgentLLMURL)
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
		Walker: caravanWalker{water: game.water},
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
			HPPercent: game.playerHPPercent(c),
		})
	}
	roster := game.caravanAgents.EnsureDivisionRoster(game.divisionID)
	// The step never waits on a remote brain (caravanai.Runtime), so
	// the tick's own background context is enough here.
	ctx := context.Background()
	for _, unit := range roster {
		_, _, _ = game.caravanAgents.StepAgent(ctx, game.divisionID, unit.Name, byRegion[unit.RegionID], nowMs)
	}
	return nil
}

/*
================================================================================
Nav-surface walking (the caravanai.Walker port)
================================================================================
*/

// caravanArrivalRadius is how close a step must land to its anchor to
// count as reaching it: the tolerance the march chord itself is proven
// to (PlanMonsterPath's Rest lands within a few units of the goal).
const caravanArrivalRadius = 24.0

// caravanWalker walks caravan agents on the shipped nav surface with
// the monster march: each step plans the chord to the current anchor
// and advances along it by the step's distance budget.
type caravanWalker struct {
	water *movement.WaterValidator
}

/*
================
caravanWalker.Step

Plans the march chord from -> to and steps toward the chord's Rest by
at most maxDist. A stalled chord (no progress) holds the pose. The
returned pose's Y is the march surface height.
================
*/
func (w caravanWalker) Step(from, to caravanai.Pose, maxDist float64) (caravanai.Pose, bool) {
	if w.water == nil {
		return from, false
	}
	// The start pose carries the march surface height (the last
	// step's Rest, or the anchor's own Y at spawn); the march plans
	// from the surface owner under it, so it is passed through as-is.
	toMonster := monster.Pose{RegionID: to.RegionID, X: float64(to.X), Y: float64(to.Y), Z: float64(to.Z)}
	path := w.water.PlanMonsterPath(
		monster.Pose{RegionID: from.RegionID, X: float64(from.X), Y: float64(from.Y), Z: float64(from.Z)},
		toMonster,
	)
	if path == nil {
		return from, false
	}
	next := stepTowardRest(from, path.Rest(), maxDist)
	arrived := worldgeom.Distance(
		worldgeom.RegionXZ{RegionID: next.RegionID, X: float64(next.X), Z: float64(next.Z)},
		worldgeom.RegionXZ{RegionID: to.RegionID, X: float64(to.X), Z: float64(to.Z)},
	) <= caravanArrivalRadius
	return next, arrived
}

/*
================
stepTowardRest

Advances from toward the march's Rest pose by at most maxDist world
units (cross-region safe); Y follows the chord from the start height
to the Rest's surface height. Zero or negative budget, or a Rest no
farther than the budget, lands exactly on the Rest.
================
*/
func stepTowardRest(from caravanai.Pose, rest monster.Pose, maxDist float64) caravanai.Pose {
	fromXZ := worldgeom.RegionXZ{RegionID: from.RegionID, X: float64(from.X), Z: float64(from.Z)}
	restXZ := worldgeom.RegionXZ{RegionID: rest.RegionID, X: rest.X, Z: rest.Z}
	dist := worldgeom.Distance(fromXZ, restXZ)
	if dist <= maxDist {
		return caravanai.Pose{RegionID: rest.RegionID, X: float32(rest.X), Y: float32(rest.Y), Z: float32(rest.Z)}
	}
	fromGrid := worldgeom.ExpandGrid(fromXZ)
	restGrid := worldgeom.ExpandGrid(restXZ)
	ratio := maxDist / dist
	grid := worldgeom.GridXZ{
		X: fromGrid.X + (restGrid.X-fromGrid.X)*ratio,
		Z: fromGrid.Z + (restGrid.Z-fromGrid.Z)*ratio,
	}
	regionID := uint16(int(grid.X/worldgeom.OutdoorRegionSize) | int(grid.Z/worldgeom.OutdoorRegionSize)<<8)
	local := worldgeom.LocalFromGrid(regionID, grid)
	y := float64(from.Y) + (rest.Y-float64(from.Y))*ratio
	return caravanai.Pose{RegionID: regionID, X: float32(local.X), Y: float32(y), Z: float32(local.Z)}
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
