/*
===========================================================================

publication.go - skill-object visibility from admitted transport state

Bootstrap and live updates share the same population and spatial predicate.
The reliable queue owns publication truth; rejected sends are retried on the
next tick instead of leaving a second, fictitious shown-object cache.

===========================================================================
*/
package skillobject

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
Viewer

Published is a detached transport snapshot. Nil means the scene has not
admitted an object list yet, so this owner must not publish live deltas.
================
*/
type Viewer struct {
	Division   string
	Population instance.Lease
	Position   worldgeom.RegionXZ
	Published  []uint32
	// CharacterGID identifies the viewer; concealment does not remove replication.
	CharacterGID uint32
}

/*
================
Visible

World generation participates in visibility as well as capture admission.
86C1F0 hides the model using detection levels; hidden traps must still exist
in the client object registry when their mode-3 result arrives.
================
*/
func Visible(object Object, viewer Viewer) bool {
	return object.Division == viewer.Division && object.Population == viewer.Population &&
		worldgeom.InterestVisible(viewer.Position, worldgeom.RegionXZ{
			RegionID: object.Spawn.Region, X: float64(object.Spawn.X), Z: float64(object.Spawn.Z),
		})
}

/*
================
ScopeFrames

Retire every previously published object now absent or outside scope. This
covers expiry, capture, owner death, instance transfer and viewer movement.
================
*/
func ScopeFrames(objects []Object, viewer Viewer) []wire.Frame {
	if viewer.Published == nil {
		return nil
	}
	shown := make(map[uint32]bool)
	for _, gid := range viewer.Published {
		if gid > domain.SkillObjectGIDBase && gid <= domain.SkillObjectGIDLimit {
			shown[gid] = true
		}
	}
	var frames []wire.Frame
	for _, object := range objects {
		if !Visible(object, viewer) {
			continue
		}
		gid := object.Spawn.GID
		if !shown[gid] {
			frames = append(frames, wire.Frame{
				Opcode: wire.OpSingleObjectSpawn, Payload: object.Spawn.Encode(true),
				Scope: []domain.ObjectScopeChange{{GID: gid, Visible: true}},
			})
		}
		delete(shown, gid)
	}
	// Published order comes from the transport's snapshot and remains stable
	// even when several objects retire in the same simulation turn.
	for _, gid := range viewer.Published {
		if shown[gid] {
			frames = append(frames, wire.Frame{
				Opcode: wire.OpObjectDespawn, Payload: (wire.ObjectDespawn{Gid: gid}).Encode(),
				Scope: []domain.ObjectScopeChange{{GID: gid}},
			})
		}
	}
	return frames
}
