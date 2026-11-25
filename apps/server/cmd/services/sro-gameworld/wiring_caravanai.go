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
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
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
		OnAgentStrike: func(division, agentName, targetPlayerName string, nowMs int64) (defeated bool) {
			return game.caravanAgentStrike(division, agentName, targetPlayerName, nowMs)
		},
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
	// Slice 3c (#3704): players can fight the agents. The action
	// runtime resolves agent gids and lands real damage through this
	// directory; with the flag off it is never installed.
	game.items.Agents = caravanAgentDirectory{game: game}
	// Slice 3a (#3704): the agents appear in the world as peers. The
	// reference catalog is wired later than this constructor runs, so
	// the closure reads it at tick time.
	game.caravanPeers = newCaravanPeerLeg(game.caravanAgentAppearance, func(ids []uint32) []simulation.Frame {
		if game.peerReferences == nil {
			return nil
		}
		return game.peerReferences.Frames(ids)
	})
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
	byRegion := make(map[uint16][]observedPlayerPose)
	var viewers []caravanPeerViewer
	for _, view := range views {
		if view.DivisionID != game.divisionID {
			continue
		}
		pose := view.World.LiveSpawnAt(nowMs)
		viewers = append(viewers, caravanPeerViewer{
			SessionID:   view.SessionID,
			CharacterID: view.CharacterID,
			Pose:        pose,
		})
		c := game.deps.CharacterByID(game.divisionID, view.CharacterID)
		if c == nil {
			continue
		}
		byRegion[pose.RegionID] = append(byRegion[pose.RegionID], observedPlayerPose{
			name:      c.Name,
			jobType:   c.Job.Type,
			hpPercent: game.playerHPPercent(c),
			pose:      pose,
		})
	}
	roster := game.caravanAgents.EnsureDivisionRoster(game.divisionID)
	// The step never waits on a remote brain (caravanai.Runtime), so
	// the tick's own background context is enough here.
	ctx := context.Background()
	for _, unit := range roster {
		_, _, _ = game.caravanAgents.StepAgent(ctx, game.divisionID, unit.Name, observedPlayersFor(unit, byRegion[unit.RegionID]), nowMs)
	}
	var frames []simulation.DivisionFrames
	if game.caravanPeers != nil {
		entities := game.caravanAgents.AgentEntities(game.divisionID)
		frames = game.caravanPeers.frames(game.divisionID, viewers, entities)
	}
	frames = append(frames, game.caravanStrikeFrames...)
	game.caravanStrikeFrames = nil
	return frames
}

// observedPlayerPose is one entered player beside the pose the caravan
// tick saw it at: the raw material of an agent's ObservedPlayer list.
type observedPlayerPose struct {
	name      string
	jobType   uint8
	hpPercent int
	pose      simulation.Spawn
}

/*
================
observedPlayersFor

One agent's nearby players with their real distances from where the
agent stands (the brain's rules and the strike range read Distance;
before slice 3e the tick never computed it).
================
*/
func observedPlayersFor(unit *caravanai.AgentUnit, players []observedPlayerPose) []caravanai.ObservedPlayer {
	if len(players) == 0 {
		return nil
	}
	from := worldgeom.RegionXZ{RegionID: unit.RegionID, X: float64(unit.X), Z: float64(unit.Z)}
	out := make([]caravanai.ObservedPlayer, 0, len(players))
	for _, p := range players {
		out = append(out, caravanai.ObservedPlayer{
			Name:      p.name,
			JobType:   p.jobType,
			HPPercent: p.hpPercent,
			Distance:  float32(worldgeom.Distance(from, worldgeom.RegionXZ{RegionID: p.pose.RegionID, X: p.pose.X, Z: p.pose.Z})),
		})
	}
	return out
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

/*
================================================================================
Peer presentation (slice 3a): agents appear, move and leave like peers

The peer-visibility leg (simulation/peer_visibility.go) is built over
session snapshots, so agents cannot join it without pretending to be
sessions. This sibling leg runs from the same tick hook instead: it
keeps its own per-viewer shown sets and emits the same frames the peer
leg emits (item references ahead of the 0x30D7 spawn row, the movement
channel + 0xB738 goal while marching, the 0x36AB despawn on leaving),
targeted at the one viewer through DivisionFrames.OnlyCharacterID.
Visibility is the peer rule itself, worldgeom.InterestVisible.
================================================================================
*/

// caravanPeerViewer is one entered session as the leg sees it: the
// delivery identity and where its character stands.
type caravanPeerViewer struct {
	SessionID   string
	CharacterID int64
	Pose        simulation.Spawn
}

// caravanPeerAck is what one viewer was last told about one agent:
// the goal it is walking to and the pose it was last placed at, so a
// goal is re-acked only when it changes and a region jump re-shows
// instead of acking across the map.
type caravanPeerAck struct {
	Target caravanai.Pose
	Pose   caravanai.Pose
	Moving bool
}

// caravanPeerLeg owns the (viewer, agent) presentation state. The
// resolve and refs ports are the shard's appearance data and item
// reference catalog; both are consulted lazily so boot order between
// the caravan wiring and the catalog build does not matter.
type caravanPeerLeg struct {
	resolve func(entity caravanai.AgentEntity) (simulation.PeerAppearance, bool)
	refs    func(ids []uint32) []simulation.Frame

	appearances map[int64]*simulation.PeerAppearance
	shown       map[string]map[uint32]bool
	sentRefs    map[string]map[uint32]bool
	acked       map[string]map[uint32]caravanPeerAck
	heading     map[uint32]uint16
}

/*
================
newCaravanPeerLeg
================
*/
func newCaravanPeerLeg(
	resolve func(entity caravanai.AgentEntity) (simulation.PeerAppearance, bool),
	refs func(ids []uint32) []simulation.Frame,
) *caravanPeerLeg {
	return &caravanPeerLeg{
		resolve:     resolve,
		refs:        refs,
		appearances: make(map[int64]*simulation.PeerAppearance),
		shown:       make(map[string]map[uint32]bool),
		sentRefs:    make(map[string]map[uint32]bool),
		acked:       make(map[string]map[uint32]caravanPeerAck),
		heading:     make(map[uint32]uint16),
	}
}

/*
================
caravanPeerLeg.frames

One tick of presentation: show each viewer the agents it newly sees,
keep the marching ones walking, despawn the ones it lost, and forget
viewers whose sessions are gone. A frame batch is addressed to the
one viewer (OnlyCharacterID); nothing here broadcasts.
================
*/
func (leg *caravanPeerLeg) frames(division string, viewers []caravanPeerViewer, entities []caravanai.AgentEntity) []simulation.DivisionFrames {
	live := make(map[string]bool, len(viewers))
	var out []simulation.DivisionFrames
	for _, viewer := range viewers {
		live[viewer.SessionID] = true
		shown := leg.shownFor(viewer.SessionID)
		acked := leg.ackedFor(viewer.SessionID)
		visible := make(map[uint32]caravanai.AgentEntity, len(entities))
		for _, entity := range entities {
			if caravanEntityVisible(viewer.Pose, entity.Pose) {
				visible[simulation.PlayerObjectID(entity.CharacterID)] = entity
			}
		}
		for gid, entity := range visible {
			if !shown[gid] {
				if batch := leg.showBatch(viewer, entity); batch != nil {
					batch.DivisionID = division
					out = append(out, *batch)
					shown[gid] = true
				}
				continue
			}
			if batch := leg.moveBatch(viewer, entity, acked[gid]); batch != nil {
				batch.DivisionID = division
				out = append(out, *batch)
			}
		}
		for gid := range shown {
			if _, ok := visible[gid]; ok {
				continue
			}
			out = append(out, simulation.DivisionFrames{
				DivisionID:      division,
				OnlyCharacterID: viewer.CharacterID,
				Frames: []simulation.Frame{{
					ScopeGID: gid,
					Opcode:   wire.OpObjectDespawn,
					Payload:  (wire.ObjectDespawn{Gid: gid}).Encode(),
				}},
			})
			delete(shown, gid)
			delete(acked, gid)
		}
	}
	for sessionID := range leg.shown {
		if !live[sessionID] {
			delete(leg.shown, sessionID)
			delete(leg.sentRefs, sessionID)
			delete(leg.acked, sessionID)
		}
	}
	return out
}

/*
================
caravanPeerLeg.showBatch

First sight of an agent: the item references the viewer has not been
sent ride ahead of the spawn row (the #340 contract the peer leg
keeps), then the row itself, then the march frames when the agent is
underway. A nil batch means the appearance data did not resolve and
the agent stays invisible rather than spawning broken.
================
*/
func (leg *caravanPeerLeg) showBatch(viewer caravanPeerViewer, entity caravanai.AgentEntity) *simulation.DivisionFrames {
	appearance, ok := leg.appearanceFor(entity)
	if !ok {
		return nil
	}
	gid := simulation.PlayerObjectID(entity.CharacterID)
	frames := leg.refs(freshCaravanRefIDs(leg.sentFor(viewer.SessionID), appearance.ItemIDs()))
	pose := caravanSpawnPose(entity.Pose, leg.heading[gid])
	if entity.Moving {
		if heading, ok := simulation.HeadingFromMovement(pose, caravanSpawnPose(entity.Target, 0)); ok {
			leg.heading[gid] = heading
			pose.Angle = heading
		}
	}
	frames = append(frames, simulation.Frame{
		ScopeGID: gid, ScopeVisible: true,
		Opcode:  wire.OpSingleObjectSpawn,
		Payload: simulation.BuildPeerSpawnRow(appearance, gid, pose),
	})
	batch := &simulation.DivisionFrames{OnlyCharacterID: viewer.CharacterID, Frames: frames}
	if entity.Moving {
		if move := leg.moveBatch(viewer, entity, caravanPeerAck{}); move != nil {
			batch.Frames = append(batch.Frames, move.Frames...)
		}
	}
	return batch
}

/*
================
caravanPeerLeg.moveBatch

While an agent marches, its viewers hold a 0xB738 goal: the client
walks the peer to it at the row's speeds. The goal is re-acked only
when the anchor changes (or marching starts/stops), and a pose that
jumped regions (the ferry hop) re-settles with a plain goal at the
new pose instead of a cross-map ack. The movement channel byte rides
ahead of the first goal, as the monster leg sends it.
================
*/
func (leg *caravanPeerLeg) moveBatch(viewer caravanPeerViewer, entity caravanai.AgentEntity, ack caravanPeerAck) *simulation.DivisionFrames {
	gid := simulation.PlayerObjectID(entity.CharacterID)
	acked := leg.ackedFor(viewer.SessionID)
	if entity.Pose.RegionID != ack.Pose.RegionID && ack.Pose != (caravanai.Pose{}) {
		// The ferry hop (or any region jump): settle the peer where it
		// now stands, then let the marching branch below re-goal it.
		ack = caravanPeerAck{Pose: entity.Pose}
		acked[gid] = ack
	}
	if !entity.Moving {
		if !ack.Moving {
			acked[gid] = caravanPeerAck{Pose: entity.Pose}
			return nil
		}
		acked[gid] = caravanPeerAck{Pose: entity.Pose}
		return &simulation.DivisionFrames{
			OnlyCharacterID: viewer.CharacterID,
			Frames:          []simulation.Frame{caravanGoalFrame(gid, entity.Pose, entity.Pose)},
		}
	}
	if ack.Moving && ack.Target == entity.Target {
		acked[gid] = caravanPeerAck{Target: entity.Target, Pose: entity.Pose, Moving: true}
		return nil
	}
	frames := make([]simulation.Frame, 0, 2)
	if !ack.Moving {
		frames = append(frames, simulation.Frame{
			Opcode:  wire.OpObjectStateRefresh,
			Payload: (wire.ObjectStateRefresh{Gid: gid, StateType: wire.StateChannelMove, Value: wire.MoveStateWalk}).Encode(),
		})
	}
	frames = append(frames, caravanGoalFrame(gid, entity.Pose, entity.Target))
	if heading, ok := simulation.HeadingFromMovement(
		caravanSpawnPose(entity.Pose, 0), caravanSpawnPose(entity.Target, 0)); ok {
		leg.heading[gid] = heading
	}
	acked[gid] = caravanPeerAck{Target: entity.Target, Pose: entity.Pose, Moving: true}
	return &simulation.DivisionFrames{OnlyCharacterID: viewer.CharacterID, Frames: frames}
}

/*
================
caravanGoalFrame

The 0xB738 destination goal for one agent: walk from where it stands
to the goal pose.
================
*/
func caravanGoalFrame(gid uint32, from, to caravanai.Pose) simulation.Frame {
	source := simulation.MovementSourceFromSpawn(caravanSpawnPose(from, 0))
	return simulation.Frame{
		Opcode: simulation.OpMovementAck,
		Payload: simulation.BuildMovementAckPayload(gid, simulation.MovementRequest{
			Mode:     simulation.MovementAckDestinationMode,
			RegionID: to.RegionID,
			X:        float64(to.X),
			Y:        float64(to.Y),
			Z:        float64(to.Z),
		}, &source),
	}
}

/*
================
caravanPeerLeg.appearanceFor

The resolved appearance of one agent, built once per agent from the
shard's shipped data. An agent whose model or gear does not resolve
stays invisible (the resolver logs the miss) instead of spawning
with a placeholder row.
================
*/
func (leg *caravanPeerLeg) appearanceFor(entity caravanai.AgentEntity) (simulation.PeerAppearance, bool) {
	if appearance, ok := leg.appearances[entity.CharacterID]; ok {
		if appearance == nil {
			return simulation.PeerAppearance{}, false
		}
		return *appearance, true
	}
	appearance, ok := leg.resolve(entity)
	if !ok {
		// A nil entry is a known miss: the resolver logged it, and
		// the agent is not retried (or re-logged) every tick.
		leg.appearances[entity.CharacterID] = nil
		return simulation.PeerAppearance{}, false
	}
	leg.appearances[entity.CharacterID] = &appearance
	return appearance, true
}

/*
================
caravanPeerLeg.shownFor
================
*/
func (leg *caravanPeerLeg) shownFor(sessionID string) map[uint32]bool {
	shown, ok := leg.shown[sessionID]
	if !ok {
		shown = make(map[uint32]bool)
		leg.shown[sessionID] = shown
	}
	return shown
}

/*
================
caravanPeerLeg.sentFor
================
*/
func (leg *caravanPeerLeg) sentFor(sessionID string) map[uint32]bool {
	sent, ok := leg.sentRefs[sessionID]
	if !ok {
		sent = make(map[uint32]bool)
		leg.sentRefs[sessionID] = sent
	}
	return sent
}

/*
================
caravanPeerLeg.ackedFor
================
*/
func (leg *caravanPeerLeg) ackedFor(sessionID string) map[uint32]caravanPeerAck {
	acked, ok := leg.acked[sessionID]
	if !ok {
		acked = make(map[uint32]caravanPeerAck)
		leg.acked[sessionID] = acked
	}
	return acked
}

/*
================
freshCaravanRefIDs

The demanded ids the viewer has not been sent yet, each marked sent:
the same once-per-viewer reference contract the peer leg keeps
(simulation's freshReferenceIDs), held here because the shown sets
are this leg's own.
================
*/
func freshCaravanRefIDs(sent map[uint32]bool, ids []uint32) []uint32 {
	fresh := make([]uint32, 0, len(ids))
	for _, id := range ids {
		if id == 0 || sent[id] {
			continue
		}
		sent[id] = true
		fresh = append(fresh, id)
	}
	return fresh
}

/*
================
caravanEntityVisible

Whether a viewer at pose sees an agent at pose: the peer interest
rule (the viewer's 3x3 sector neighbourhood).
================
*/
func caravanEntityVisible(viewer simulation.Spawn, entity caravanai.Pose) bool {
	return worldgeom.InterestVisible(
		worldgeom.RegionXZ{RegionID: viewer.RegionID, X: viewer.X, Z: viewer.Z},
		worldgeom.RegionXZ{RegionID: entity.RegionID, X: float64(entity.X), Z: float64(entity.Z)},
	)
}

/*
================
caravanSpawnPose

An agent pose as a wire spawn pose.
================
*/
func caravanSpawnPose(pose caravanai.Pose, angle uint16) simulation.Spawn {
	return simulation.Spawn{
		RegionID: pose.RegionID,
		X:        float64(pose.X),
		Y:        float64(pose.Y),
		Z:        float64(pose.Z),
		Angle:    angle,
	}
}

/*
================
caravanAgentAppearance

Resolves one agent's peer appearance from the shard's shipped data:
the character model, and the worn set (job suit, then weapon) as
equipment rows. The leg calls this once per agent (it caches misses
too) and logs the miss; the agent simply never appears.
================
*/
func (game *gameplayPlane) caravanAgentAppearance(entity caravanai.AgentEntity) (simulation.PeerAppearance, bool) {
	model := game.deps.Roster.ModelByCodename(entity.ModelCode)
	suit, suitOK := game.deps.Items.ItemRefByCodename(entity.SuitCode)
	weapon, weaponOK := game.deps.Items.ItemRefByCodename(entity.WeaponCode)
	if model == nil || !suitOK || !weaponOK {
		log.WithFields(log.Fields{
			"agent":  entity.Name,
			"model":  entity.ModelCode,
			"suit":   entity.SuitCode,
			"weapon": entity.WeaponCode,
		}).Warn("caravan-ai: agent appearance data did not resolve; agent stays invisible")
		return simulation.PeerAppearance{}, false
	}
	return simulation.PeerAppearance{
		RefObjID:  model.RefObjID,
		Name:      entity.Name,
		JobType:   entity.JobType,
		Equipment: []wire.PlayerEquipItem{caravanEquipItem(suit), caravanEquipItem(weapon)},
	}, true
}

/*
================
caravanEquipItem

One worn item as a spawn-row equipment entry.
================
*/
func caravanEquipItem(ref *enterworld.ItemRef) wire.PlayerEquipItem {
	return wire.PlayerEquipItem{
		RefObjID: ref.RefObjID,
		TypeFlags: wire.PackTypeFlags(
			uint8(ref.TypeIDs[0]), uint8(ref.TypeIDs[1]),
			uint8(ref.TypeIDs[2]), uint8(ref.TypeIDs[3])),
	}
}

//============================================================================

/*
================
caravanAgentDirectory

The caravan runtime as the action package's combat directory (slice
3c): an object gid in the player band that names no stored character
resolves to the agent wearing that synthetic identity, and damage
lands through RecordPlayerAttack, the runtime's one damage door.
================
*/
type caravanAgentDirectory struct {
	game *gameplayPlane
}

/*
================
caravanAgentDirectory.AgentByGID

The living agent whose synthetic character id maps to gid, with its
model's body radius from the shipped roster. The dead are absent from
the entity snapshot, so a corpse resolves to nothing.
================
*/
func (dir caravanAgentDirectory) AgentByGID(division string, gid uint32) (action.AgentTarget, bool) {
	if dir.game.caravanAgents == nil {
		return action.AgentTarget{}, false
	}
	for _, entity := range dir.game.caravanAgents.AgentEntities(division) {
		if simulation.PlayerObjectID(entity.CharacterID) != gid {
			continue
		}
		target := action.AgentTarget{
			GID:     gid,
			Name:    entity.Name,
			JobType: entity.JobType,
			Pose: simulation.Spawn{RegionID: entity.Pose.RegionID,
				X: float64(entity.Pose.X), Y: float64(entity.Pose.Y), Z: float64(entity.Pose.Z)},
			Level:        entity.Level,
			HP:           entity.HP,
			MaxHP:        entity.MaxHP,
			LastAttacker: entity.LastAttacker,
			SkillID:      entity.SkillID,
			CombatTarget: entity.CombatTarget,
		}
		if model := dir.game.deps.Roster.ModelByCodename(entity.ModelCode); model != nil {
			target.BodyRadius = model.BodyRadius
		}
		if dir.game.deps.Items == nil {
			return target, true
		}
		if weapon, ok := dir.game.deps.Items.ItemRefByCodename(entity.WeaponCode); ok && weapon.Combat != nil {
			target.PhysicalAttackMin = float64(weapon.Combat.PhysicalAttack.Minimum.Min)
			target.PhysicalAttackMax = float64(weapon.Combat.PhysicalAttack.Maximum.Max)
			target.MagicalAttackMin = float64(weapon.Combat.MagicalAttack.Minimum.Min)
			target.MagicalAttackMax = float64(weapon.Combat.MagicalAttack.Maximum.Max)
		}
		return target, true
	}
	return action.AgentTarget{}, false
}

/*
================
caravanAgentDirectory.ApplyAgentDamage

Real damage from a player's strike, through the runtime's damage
door: parley betrayal, robbery and death bookkeeping stay there.
================
*/
func (dir caravanAgentDirectory) ApplyAgentDamage(division, agentName, attackerName string, damage int, nowMs int64) (defeated bool) {
	if dir.game.caravanAgents == nil {
		return false
	}
	_, defeated, _ = dir.game.caravanAgents.RecordPlayerAttack(division, agentName, attackerName, damage, nowMs)
	return defeated
}

/*
================
caravanAgentStrike

The runtime's strike hook (slice 3e): resolve the agent through the
directory, strike its player through the action runtime's agent lane,
and queue the publication for this tick's flush - the room's frames
scoped on the victim, the victim's own frames addressed to it.
================
*/
func (game *gameplayPlane) caravanAgentStrike(division, agentName, targetPlayerName string, nowMs int64) (defeated bool) {
	if game.caravanAgents == nil || game.items == nil {
		return false
	}
	dir := caravanAgentDirectory{game: game}
	for _, entity := range game.caravanAgents.AgentEntities(division) {
		if entity.Name != agentName {
			continue
		}
		target, ok := dir.AgentByGID(division, simulation.PlayerObjectID(entity.CharacterID))
		if !ok {
			return false
		}
		result := game.items.StrikePlayerByAgent(division, target, targetPlayerName, nowMs)
		if !result.Committed {
			return false
		}
		game.caravanStrikeFrames = append(game.caravanStrikeFrames,
			simulation.DivisionFrames{
				DivisionID: division,
				SourceGID:  simulation.PlayerObjectID(result.Victim.CharacterID),
				Frames:     caravanStrikeFrames(result.Public),
			},
			simulation.DivisionFrames{
				DivisionID:      division,
				OnlyCharacterID: result.Victim.CharacterID,
				Frames:          caravanStrikeFrames(result.Victim.Frames),
			},
		)
		return result.Defeated
	}
	return false
}

/*
================
caravanStrikeFrames

The action package's wire frames as simulation frames for the tick.
================
*/
func caravanStrikeFrames(frames []wire.Frame) []simulation.Frame {
	out := make([]simulation.Frame, 0, len(frames))
	for _, f := range frames {
		out = append(out, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
	}
	return out
}
