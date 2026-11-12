package simulation

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
)

// NpcVisibleAt is shared by bootstrap, ongoing publication and interaction.
// Static NPCs and city gates use the same native message blocks as other
// world objects. RegionScopeRing is a data-loading boundary, not visibility.
// The explicit non-authored development roster retains its anchored contract.
func NpcVisibleAt(npc NpcDef, viewer Spawn) bool {
	return !npc.AuthoredSpawn || worldgeom.InterestVisible(
		worldgeom.RegionXZ{RegionID: viewer.RegionID, X: viewer.X, Z: viewer.Z},
		worldgeom.RegionXZ{RegionID: npc.Spawn.RegionID, X: npc.Spawn.X, Z: npc.Spawn.Z},
	)
}

/*
================
NpcScopeBuffer

The scratch one scope pass works in: the admitted-NPC set it derives
from the viewer's published list, and the frame slice it fills. The
tick runs one pass per session per tick and the set was rebuilt from
scratch every time (#3291: 2.8 % of the live profile's allocated
bytes, ~19 KB per call in a busy town). One buffer per division tick
state, passed from call to call, keeps both allocations: the map is
cleared, never regrown to a smaller town, and the frame slice keeps
its capacity. The shown set itself is still derived fresh from the
viewer's published list on every call - this is scratch reuse, not a
second "shown NPCs" cache.

The returned frames borrow the buffer: they are valid until the next
call with the same buffer. The tick consumes them synchronously
(PushToSession copies the batch before it returns), and spawn payloads
are built fresh per frame as before - a published payload is retained
by the session and is never the buffer's to reuse.
================
*/
type NpcScopeBuffer struct {
	shown  map[uint32]bool
	frames []Frame
}

// NpcScopeFrames reconciles against transport-ADMITTED objects, including the
// bootstrap list. Do not add a second "shown NPCs" cache: enqueue rejection,
// reconnect and scene replacement must never leave a fictitious publication.
// The pusher admits ScopeGID and the complete packet atomically for this scene.
func NpcScopeFrames(roster []NpcDef, session SessionSnapshot, nowMs int64) []Frame {
	return NpcScopeFramesInto(&NpcScopeBuffer{}, roster, session, nowMs)
}

/*
================
NpcScopeFramesInto

NpcScopeFrames with the pass's scratch supplied by the caller, for the
per-tick caller that runs it back to back (the session legs).
================
*/
func NpcScopeFramesInto(buffer *NpcScopeBuffer, roster []NpcDef, session SessionSnapshot, nowMs int64) []Frame {
	if session.PublishedObjects == nil {
		return nil // No publication snapshot: caller has no scene authority.
	}
	if buffer.shown == nil {
		buffer.shown = make(map[uint32]bool, len(session.PublishedObjects))
	} else {
		clear(buffer.shown)
	}
	for _, gid := range session.PublishedObjects {
		if gid > domain.NPCGIDBase && gid < domain.GroundItemGIDBase {
			buffer.shown[gid] = true
		}
	}
	viewer := session.World.LiveSpawnAt(nowMs)
	frames := buffer.frames[:0]
	for _, npc := range roster {
		visible := session.NpcsEnabled && NpcVisibleAt(npc, viewer)
		if visible && !buffer.shown[npc.ObjectID] {
			payload := BuildNpcCreateRow(npc, session.NpcAnchor)
			// CICNPC single-create has the appear byte after the list body;
			// CITeleportGate has no character appearance tail.
			if npc.Teleport == nil {
				payload = append(payload, 0)
			}
			frames = append(frames, Frame{Opcode: wire.OpSingleObjectSpawn, Payload: payload, ScopeGID: npc.ObjectID, ScopeVisible: true})
		} else if !visible && buffer.shown[npc.ObjectID] {
			frames = append(frames, Frame{Opcode: wire.OpObjectDespawn, Payload: (wire.ObjectDespawn{Gid: npc.ObjectID}).Encode(), ScopeGID: npc.ObjectID})
		}
	}
	buffer.frames = frames
	return frames
}
