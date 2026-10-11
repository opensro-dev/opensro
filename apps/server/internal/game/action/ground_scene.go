/*
===========================================================================

ground_scene.go - settle a scene's ground drops when it goes live

A bootstrap publishes every drop in the character's population. Ground
removals (TTL sweep, another player's pickup) route only to viewers whose
published scope holds the drop, and a loading scene publishes none, so a
removal committed between the bootstrap and game-ready reaches nobody. The
client keeps that drop until it is picked at, which the server refuses
(BR alt-tab ghost drops). Game-ready closes the gap from the registry.

===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

/*
================
ReconcileGroundScope

Despawn every ground drop the live scene still shows that the character's
population no longer holds. Runs after FinishSceneReentry, so a removal
committed after the registry read routes through the published scope; one
committed before it is despawned here. A drop removed in between may be
despawned twice, which the client ignores for an unknown object.
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
	live := make(map[uint32]struct{})
	for _, item := range rt.CharacterGroundItems(divisionID, c) {
		live[item.Gid] = struct{}{}
	}
	var frames []transport.Frame
	for _, gid := range published {
		if gid <= domain.GroundItemGIDBase || gid > domain.GroundItemGIDLimit {
			continue
		}
		if _, exists := live[gid]; exists {
			continue
		}
		frames = append(frames, transport.Frame{
			Opcode:  wire.OpObjectDespawn,
			Payload: wire.ObjectDespawn{Gid: gid}.Encode(),
			Scope:   []transport.ObjectScopeChange{{GID: gid}},
		})
	}
	if err := s.PublishSceneObjects(revision, nil, frames); err != nil {
		log.Debugf("action: ground scene reconcile for session %d failed: %v", s.ID, err)
	}
}
