/*
===========================================================================
petcrowvisual.go - port-only crow cues at committed effect positions.

Cosmetics never apply gameplay or interrupt movement/casting. Position events
survive hatching, death and despawn, and are visible to nearby recipients.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const crowVisualOpcode uint16 = 0x3FBB
const (
	crowPlumeVisual uint8 = iota + 1
	crowEggVisual
	crowHatchVisual
	crowManaVisual
	crowTailwindVisual
	crowGuidanceVisual
	crowReadyVisual
	crowStunVisual
)

/*
================
crowVisual
================
*/
type crowVisual struct {
	gid        uint32
	kind, book uint8
	at         simulation.Spawn
}

/*
================
crowVisualFrame
================
*/
func crowVisualFrame(c crowVisual) wire.Frame {
	return wire.Frame{Opcode: crowVisualOpcode, Payload: wire.NewWriter(20).U32(c.gid).U8(c.kind).U8(c.book).U16(c.at.RegionID).F32(float32(c.at.X)).F32(float32(c.at.Y)).F32(float32(c.at.Z)).Payload()}
}

/*
================
queueCrowVisual
================
*/
func (rt *Runtime) queueCrowVisual(division string, c crowVisual) {
	rt.queueMonsterLegFrames(rt.sealTickPrivate([]simulation.DivisionFrames{{DivisionID: division, SourceGID: c.gid, Frames: simFrames([]wire.Frame{crowVisualFrame(c)})}}))
}

/*
================
crowRecipientVisual
================
*/
func (rt *Runtime) crowRecipientVisual(step petCombatStep, member *enterworld.Character, kind, book uint8) {
	at := rt.liveSpawn(simulation.WorldKey(step.key.division, member.Name), rt.characterSnapshot(step.key.division, member), step.nowMs)
	rt.queueCrowVisual(step.key.division, crowVisual{gid: enterworld.ObjectIDForCharacter(member), kind: kind, book: book, at: at})
}
