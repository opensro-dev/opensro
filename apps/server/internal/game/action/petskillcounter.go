/*
===========================================================================
petskillcounter.go - port-only owner HUD counters, from authoritative pet memory.

The existing pet tick sends changed snapshots privately. Counts never persist;
the 40-attack cycle preserves both the eight- and ten-hit passive progress.
The tick judges the counts behind the read guard and composes the frame only
on a change; the training door still composes unconditionally (#2030).
===========================================================================
*/
package action

import (
	"encoding/binary"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const petSkillCounterOpcode uint16 = 0x3fb6

// The payload's fixed body: the pet GID, nine one-byte counts and the
// hunt target. A penguin frame carries three more bytes of stacks.
const (
	petCounterBodyBytes = 17
	petCounterTailBytes = 3
)

/*
================
petCounterCounts

The counts one counter frame carries. The eleven scalars are all the
publication ever reads off the live records: the pet's GID and
resolve stacks, the trained forty-attack cycle and bear/owner hit
counters, the wolf kit's four counters plus its hunt target, and a
penguin's two stacks and hourglass.
================
*/
type petCounterCounts struct {
	gid          uint32
	target       uint32
	attacks      uint8
	bear         uint8
	owner        uint8
	resolve      uint8
	wolf         uint8
	ownerExecute uint8
	blood        uint8
	ownerBlood   uint8
	hunt         uint8
	tail         [petCounterTailBytes]uint8
	penguin      bool
}

/*
================
petSkillCounterCounts

Reads the counts off the live records; the caller holds the
division's read door. False is the frame the composer resolves to
nothing for: no session character, no companion by that GID, a
brood-stage pet, or a codename with no pet kind. Every counter,
the resolve stacks included, rides only while the pet is summoned,
alive and beside a living owner; the GID and the penguin tail ride
either way, as the composed frame has always carried them.
================
*/
func petSkillCounterCounts(state *petSession, gid uint32) (petCounterCounts, bool) {
	if state == nil || state.character == nil {
		return petCounterCounts{}, false
	}
	pet := state.character.CompanionByGID(gid)
	if pet == nil || pet.BroodStage != 0 || companion.PetKind(pet.Codename) == "" {
		return petCounterCounts{}, false
	}
	counts := petCounterCounts{
		gid:     pet.GID,
		penguin: companion.PetKind(pet.Codename) == "Penguin",
	}
	if pet.Summoned && pet.CurrentHP > 0 && enterworld.CharacterAlive(state.character) {
		counts.attacks = uint8(state.trained.attacks % 40)
		counts.resolve = pet.ResolveStacks
		counts.bear = uint8(state.trained.counterHits[0])
		counts.owner = uint8(state.trained.counterHits[1])
		counts.wolf = state.wolfHits[0]
		counts.ownerExecute = state.wolfHits[1]
		counts.blood = state.wolf.blood[0]
		counts.ownerBlood = state.wolf.blood[1]
		counts.hunt = state.wolf.huntHits
		counts.target = state.wolf.huntTarget
	}
	if counts.penguin {
		counts.tail = [petCounterTailBytes]uint8{state.penguin.stacks[0], state.penguin.stacks[1], state.penguin.hourglass}
	}
	return counts, true
}

/*
================
petCounterCounts.view

The compared form of the counts: the payload bytes at their wire
offsets in a stack array, zero past the payload's end. The session's
remembered view is this array, so the change judgement compares
counts, never a composed payload.
================
*/
func (counts petCounterCounts) view() [22]byte {
	var view [22]byte
	binary.LittleEndian.PutUint32(view[0:], counts.gid)
	view[4] = counts.attacks
	view[5] = counts.bear
	view[6] = counts.owner
	view[7] = counts.resolve
	view[8] = counts.wolf
	view[9] = counts.ownerExecute
	view[10] = counts.blood
	view[11] = counts.ownerBlood
	view[12] = counts.hunt
	binary.LittleEndian.PutUint32(view[13:], counts.target)
	if counts.penguin {
		copy(view[petCounterBodyBytes:], counts.tail[:])
	}
	return view
}

/*
================
petCounterCounts.frame

Composes the counter frame from the counts: the view's payload
length, copied out. Called only for a frame that will be sent.
================
*/
func (counts petCounterCounts) frame() wire.Frame {
	view := counts.view()
	length := petCounterBodyBytes
	if counts.penguin {
		length += petCounterTailBytes
	}
	payload := make([]byte, length)
	copy(payload, view[:length])
	return wire.Frame{Opcode: petSkillCounterOpcode, Payload: payload}
}

/*
================
petSkillCounterFrame

Caller holds the division's read/write door. No audience/global GID lookup.
================
*/
func petSkillCounterFrame(state *petSession, gid uint32) wire.Frame {
	counts, ok := petSkillCounterCounts(state, gid)
	if !ok {
		return wire.Frame{}
	}
	return counts.frame()
}

/*
================
judgePetCounter

The per-tick publication judgement for one bound pet: the counter
frame to push, or none. The counts are read behind the read guard
(no door closure per pet per tick) and the frame is composed only
when the view moved, so a steady tick allocates nothing (#2030).
The session remembers the last pushed view; an unresolved pet
leaves it untouched, exactly as the composed-empty result did.
Caller holds no door: the guard below takes it.
================
*/
func (rt *Runtime) judgePetCounter(key petOwnerKey, state *petSession) (wire.Frame, bool) {
	guard := rt.deps.AcquireRead(key.division)
	counts, ok := petSkillCounterCounts(state, key.gid)
	guard.Release()
	if !ok {
		return wire.Frame{}, false
	}
	view := counts.view()
	if state.counterSent && view == state.counterView {
		return wire.Frame{}, false
	}
	state.counterView, state.counterSent = view, true
	return counts.frame(), true
}
