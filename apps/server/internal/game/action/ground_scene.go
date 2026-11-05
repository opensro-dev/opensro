/*
===========================================================================

ground_scene.go - settle a scene's ground drops and trade level when it goes live

A bootstrap publishes the drops in range of the entry (ground_scope.go).
Ground changes route only to live scenes: a removal (TTL sweep, another player's
pickup) reaches viewers whose published scope holds the drop, and a new drop
reaches sessions that are not loading. A change committed between the
bootstrap and game-ready therefore reaches nobody. A removed drop stays on
the client until it is picked at, which the server refuses (BR alt-tab ghost
drops), and a new one is never shown (#617). Game-ready closes both gaps
from the registry; the per-tick pass (AdvanceGroundScope) keeps them
closed as the player moves.

Every loading scene also builds a new local player whose trade level
(CICPlayer +0x4F6) starts at 0 (CICUser constructor, 86945B), so a scene
that goes live with a loaded transport out is told its level again.

===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
================
ReconcileGroundScope

Settle a scene that just went live against the ground registry: spawn the
drops in range it does not show, despawn the ones it shows that are gone or
out of range (groundScope). Runs after FinishSceneReentry, so a change
committed after the registry read routes live; one committed before it is
settled here. publishGroundScope admits each change against the scene's
current scope, so a change that also arrived live is not repeated.
================
*/
func (rt *Runtime) ReconcileGroundScope(s *transport.Session, divisionID string, c *enterworld.Character) {
	revision, active := s.SceneRevision()
	if !active {
		return
	}
	published, current := s.PublishedObjects(revision)
	if !current {
		return
	}
	at := rt.liveSpawn(simulation.WorldKey(divisionID, c.Name), c, rt.Now().UnixMilli())
	spawn, despawn := groundScope(bucketDrops(rt.CharacterGroundItems(divisionID, c), nil), viewerBlock(at), published,
		make(map[uint32]struct{}))
	rt.publishGroundScope(s, revision, spawn, despawn)
}

/*
================
ReconcileScene

The game-ready hook (enterworld.Deps.ReconcileSceneObjects): settle the
ground drops, start a teleport arrival's untouchable grace
(teleportprotection.go), then repeat the transport's trade level (0x352D)
when goods are aboard. Without goods the new player's 0 is already right. c is the
detached snapshot enterworld takes for game-ready, so no lock is needed.
================
*/
func (rt *Runtime) ReconcileScene(s *transport.Session, divisionID string, c *enterworld.Character) {
	rt.ReconcileGroundScope(s, divisionID, c)
	rt.startTeleportGrace(divisionID, c.Name)
	frame, loaded := rt.sceneTradeStars(c)
	if !loaded {
		return
	}
	if err := s.Send(frame.Opcode, frame.Payload); err != nil {
		log.Debugf("action: trade level for session %d failed: %v", s.ID, err)
	}
}
