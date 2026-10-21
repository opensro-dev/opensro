/*
===========================================================================

petwolfvisual.go - port-only wolf skill signals for the client's effects

The wolf's passives happen off the pet's own strike (owner hits, sweeps,
bonus hits), so their visuals ride a COS emote (0x324B) on the object the
effect belongs to. Actions 0xF0+ are port-only: the client draws an effect
and never routes them to an emote clip. Gameplay never reads them.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// wolfEmoteExecuted marks an executed monster (Execute, any book).
	wolfEmoteExecuted uint8 = 0xF0
	// wolfEmoteSweep centers Execute III's 10 m sweep on the wolf or owner.
	wolfEmoteSweep uint8 = 0xF1
	// wolfEmoteHunt marks prey whose Blood Hunt mark a Rend consumed.
	wolfEmoteHunt uint8 = 0xF2
	// wolfEmoteHuntBurst marks a bleeding enemy hit by Blood Hunt III's burst.
	wolfEmoteHuntBurst uint8 = 0xF3
	// wolfEmoteBloodFull marks the wolf or owner reaching full Bloodscent stacks.
	wolfEmoteBloodFull uint8 = 0xF4
)

/*
================
wolfEmoteFrame
================
*/
func wolfEmoteFrame(gid uint32, action uint8) wire.Frame {
	return wire.Frame{Opcode: wire.OpActionEmote, Payload: wire.ActionEmotePush{Gid: gid, Action: action}.Encode()}
}

/*
================
wolfEmote

Publishes the signal to the object's viewers; a player source sees its own.
================
*/
func (rt *Runtime) wolfEmote(division string, gid uint32, action uint8) {
	rt.queueMonsterLegFrames(rt.sealTickPrivate([]simulation.DivisionFrames{{DivisionID: division, SourceGID: gid,
		Frames: simFrames([]wire.Frame{wolfEmoteFrame(gid, action)})}}))
}

/*
================
wolfBonusEmote

The signal of a wolfBonusHit reason; 0 for none.
================
*/
func wolfBonusEmote(reason string) uint8 {
	switch reason {
	case "wolf-execute":
		return wolfEmoteExecuted
	case "wolf-blood-hunt":
		return wolfEmoteHunt
	case "wolf-blood-hunt-burst":
		return wolfEmoteHuntBurst
	}
	return 0
}

/*
================
wolfRendQueued

The queued Savage Rend book a wolf's next strike will use, when its row is
ready; the windup then announces the rend clip with the cast.
================
*/
func (rt *Runtime) wolfRendQueued(step petCombatStep) (uint8, bool) {
	s, ok := companion.PetSkill(step.pet, step.pet.Training.Queued)
	if !ok || s.EffectName() != "Savage Rend" || !rt.petExpansionAllowed(step.snapshot, step.pet) ||
		!companion.RowReady(&step.pet.Training, s.Row, step.nowMs) {
		return 0, false
	}
	return s.ID, true
}
