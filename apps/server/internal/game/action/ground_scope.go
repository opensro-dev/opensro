/*
===========================================================================

ground_scope.go - which ground drops a viewer is shown

Native publishes a ground item like every other world object: to viewers
whose 320-unit message block neighbours its own (worldgeom.InterestVisible).
The bootstrap, game-ready and a per-tick pass share groundScope, so a drop
enters a scene when its viewer comes into range and leaves it when the
viewer walks away (#617). Before, the bootstrap shipped every drop of the
population and a drop made out of sight never appeared on approach.

A drop's own creation and removal still route through the observers of its
actor (register.go); this owner only settles what those routes miss.

===========================================================================
*/
package action

import (
	"sort"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
================
placedDrop

A drop with its message block, computed once per pass and shared by every
viewer of its population.
================
*/
type placedDrop struct {
	item  grounditem.Item
	block worldgeom.InterestBlock
}

/*
================
placeDrops
================
*/
func placeDrops(items []grounditem.Item) []placedDrop {
	out := make([]placedDrop, len(items))
	for i, item := range items {
		out[i] = placedDrop{item: item, block: worldgeom.InterestBlockAt(worldgeom.RegionXZ{
			RegionID: item.Position.RegionID, X: float64(item.Position.X), Z: float64(item.Position.Z),
		})}
	}
	return out
}

/*
================
groundScope

The drops a viewer in block viewer should be shown but is not, and the
published drops it should no longer be shown: out of range, or gone from
the population.
================
*/
func groundScope(drops []placedDrop, viewer worldgeom.InterestBlock, published []uint32) ([]grounditem.Item, []uint32) {
	shown := make(map[uint32]struct{})
	for _, gid := range published {
		if gid > domain.GroundItemGIDBase && gid <= domain.GroundItemGIDLimit {
			shown[gid] = struct{}{}
		}
	}
	var spawn []grounditem.Item
	for _, drop := range drops {
		if !viewer.Adjacent(drop.block) {
			continue
		}
		if _, exists := shown[drop.item.Gid]; exists {
			delete(shown, drop.item.Gid)
			continue
		}
		spawn = append(spawn, drop.item)
	}
	despawn := make([]uint32, 0, len(shown))
	for gid := range shown {
		despawn = append(despawn, gid)
	}
	sort.Slice(despawn, func(i, j int) bool { return despawn[i] < despawn[j] })
	return spawn, despawn
}

/*
================
viewerBlock
================
*/
func viewerBlock(at simulation.Spawn) worldgeom.InterestBlock {
	return worldgeom.InterestBlockAt(worldgeom.RegionXZ{RegionID: at.RegionID, X: at.X, Z: at.Z})
}

/*
================
groundEntryFrame

A drop already lying on the ground, as a viewer coming into range meets it:
the 0x30D7 form with its appear byte clear, so it does not replay the
drop-in. NPCs entering range use the same byte (npcinterest.go).
================
*/
func groundEntryFrame(item grounditem.Item) wire.Frame {
	row := item.SpawnRow(true)
	row.AppearFlag = 0
	return wire.Frame{Opcode: wire.OpSingleObjectSpawn, Payload: row.Encode(),
		Scope: []domain.ObjectScopeChange{{GID: item.Gid, Visible: true}}}
}

/*
================
publishGroundScope

Each change is admitted under the queue lock against the scene's current
scope, not the caller's snapshot: a drop another producer introduced first
is not spawned twice, and one it already removed is not despawned twice.
The caller holds the division's publication gate, so no pickup or drop
commits between the registry read and these sends.
================
*/
func (rt *Runtime) publishGroundScope(s *transport.Session, revision uint64, spawn []grounditem.Item, despawn []uint32) {
	for _, gid := range despawn {
		frames := []wire.Frame{{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: gid}.Encode(),
			Scope: []domain.ObjectScopeChange{{GID: gid}}}}
		if err := s.SendObservedBatch(revision, gid, transportFrames(frames)); err != nil {
			log.Debugf("action: ground despawn for session %d failed: %v", s.ID, err)
			return
		}
	}
	if len(spawn) == 0 {
		return
	}
	// A viewer can only parse a drop whose reference it holds.
	if references := rt.groundReferences(spawn); len(references) != 0 {
		if err := s.SendSceneBatch(revision, transportFrames(references)); err != nil {
			log.Debugf("action: ground references for session %d failed: %v", s.ID, err)
			return
		}
	}
	for _, item := range spawn {
		if err := s.SendUnobservedBatch(revision, item.Gid, transportFrames([]wire.Frame{groundEntryFrame(item)})); err != nil {
			log.Debugf("action: ground spawn for session %d failed: %v", s.ID, err)
			return
		}
	}
}

/*
================
GroundObjectRows

The bootstrap's ground rows: the population's drops in range of the entry
position, by the same predicate as the live pass.
================
*/
func (rt *Runtime) GroundObjectRows(division string, c *enterworld.Character, entry *enterworld.LocalPlayerEntry) []enterworld.Packet {
	if c == nil || entry == nil {
		return nil
	}
	viewer := worldgeom.InterestBlockAt(worldgeom.RegionXZ{
		RegionID: uint16(entry.StartProfile.RegionID), X: entry.StartProfile.X, Z: entry.StartProfile.Z,
	})
	var items []grounditem.Item
	for _, drop := range placeDrops(rt.CharacterGroundItems(division, c)) {
		if viewer.Adjacent(drop.block) {
			items = append(items, drop.item)
		}
	}
	return enterworld.GroundObjectListRows(items)
}

/*
================
sessionGroundPopulation

The ground population a tick session views. The main world is the zero
population; any other needs a live lease for the session's world.
================
*/
func sessionGroundPopulation(session simulation.SessionView) (grounditem.Population, bool) {
	if session.WorldInstance == domain.DefaultWorldInstance {
		return grounditem.Population{}, true
	}
	if uint32(session.Population.ID) != session.WorldInstance || session.Population.Generation == 0 {
		return grounditem.Population{}, false
	}
	return grounditem.Population{World: session.WorldInstance, Generation: session.Population.Generation}, true
}

/*
================
AdvanceGroundScope

The per-tick pass: every live scene gains the drops that came into its range
and loses those that left it. scene resolves a tick session id to its
transport session and scene revision. Each division's sessions are settled
under that division's publication gate.
================
*/
func (rt *Runtime) AdvanceGroundScope(nowMs int64, sessions []simulation.SessionView,
	scene func(sessionID string) (*transport.Session, uint64, bool),
) {
	byDivision := make(map[string][]simulation.SessionView)
	for _, session := range sessions {
		if session.PublishedObjects != nil {
			byDivision[session.DivisionID] = append(byDivision[session.DivisionID], session)
		}
	}
	divisions := make([]string, 0, len(byDivision))
	for division := range byDivision {
		divisions = append(divisions, division)
	}
	sort.Strings(divisions)
	for _, division := range divisions {
		rt.advanceDivisionGroundScope(division, byDivision[division], nowMs, scene)
	}
}

/*
================
advanceDivisionGroundScope
================
*/
func (rt *Runtime) advanceDivisionGroundScope(division string, sessions []simulation.SessionView, nowMs int64,
	scene func(sessionID string) (*transport.Session, uint64, bool),
) {
	unlock := rt.LockPublication(division, "groundScope")
	defer unlock()
	placed := make(map[grounditem.Population][]placedDrop)
	for _, session := range sessions {
		population, ok := sessionGroundPopulation(session)
		if !ok {
			continue
		}
		drops, cached := placed[population]
		if !cached {
			drops = placeDrops(rt.Ground.AllInPopulation(division, population))
			placed[population] = drops
		}
		spawn, despawn := groundScope(drops, viewerBlock(session.World.LiveSpawnAt(nowMs)), session.PublishedObjects)
		if len(spawn) == 0 && len(despawn) == 0 {
			continue
		}
		s, revision, live := scene(session.SessionID)
		if !live {
			continue
		}
		rt.publishGroundScope(s, revision, spawn, despawn)
	}
}
