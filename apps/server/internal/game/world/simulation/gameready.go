package simulation

import (
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/calendar"
)

// GameTimePayload uses the same server calendar as EnterWorld's 0x32A6.
func GameTimePayload() []byte { return calendar.Current().Payload() }

// Vitals is the resolved HP/MP snapshot for the 0x33A6 refresh. The caller
// (character state owner) resolves caps and currents; the reference defaults
// both caps to 100 with currents at cap.
type Vitals struct {
	CurrentHP uint32
	CurrentMP uint32
}

// VitalsSourceFlags are the native 0x33A6 cause bits. They are part of the
// client reconciliation contract, not presentation metadata: source zero
// applies an HP delta from the last wire baseline, while combat damage
// advances that baseline without applying the fatal damage a second time.
type VitalsSourceFlags uint16

const (
	// The retail server gives skill damage cause 4; 0x400 requests extra
	// source-less damage text in client 77A080 -> 8E2840.
	VitalsSourceCombatDamage VitalsSourceFlags = 0x0004
	// The retail server carries the skill recovery cause into the vitals update.
	VitalsSourceSkillRecovery VitalsSourceFlags = 0x0040
	// The retail server: ordinary four-second HP/MP recovery.
	VitalsSourceNaturalRecovery VitalsSourceFlags = 0x0010
)

// VitalsRefreshPayload encodes the 0x33A6 refresh (client sub_77a080):
// [u32 objectId][u16 stateFlags=0][u8 updateMask=0x03][u32 hp][u32 mp],
// matching the reference buildV150VitalsRefreshPayload.
func VitalsRefreshPayload(objectID uint32, vitals Vitals) []byte {
	return VitalsRefreshWithSourcePayload(objectID, 0, vitals)
}

func VitalsRefreshWithSourcePayload(objectID uint32, source VitalsSourceFlags, vitals Vitals) []byte {
	return wire.NewWriter(15).
		U32(objectID).
		U16(uint16(source)).
		U8(0x03).
		U32(vitals.CurrentHP).
		U32(vitals.CurrentMP).
		Payload()
}

// HPRefreshPayload encodes an HP-only 0x33A6 refresh:
// [u32 objectId][u16 sourceFlags][u8 updateMask=0x01][u32 hp].
//
// Fatal combat uses VitalsSourceCombatDamage after B245 has already
// registered the damage for its impact callback. This records HP zero as the next delta baseline without
// subtracting the same damage from the client's effective current-HP field.
func HPRefreshPayload(objectID uint32, sourceFlags VitalsSourceFlags, currentHP uint32) []byte {
	return wire.NewWriter(11).
		U32(objectID).
		U16(uint16(sourceFlags)).
		U8(0x01).
		U32(currentHP).
		Payload()
}

// GameReadyFrames is the 0x3012 AGENT_GAME_READY answer: the post-entry
// runtime push (game clock + a vitals refresh), the retail-safe point the
// reference buildGameReadyRuntimePush uses. Initial nearby-object spawns
// (NPC create rows / ground items) ride the bootstrap object list instead.
func GameReadyFrames(objectID uint32, vitals Vitals) []Frame {
	return []Frame{
		{Opcode: OpGameTime, Payload: GameTimePayload()},
		{Opcode: OpVitalsUpdate, Payload: VitalsRefreshPayload(objectID, vitals)},
	}
}
