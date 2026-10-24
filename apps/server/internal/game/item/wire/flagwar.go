/*
===========================================================================

flagwar.go - the Capture the Flag (flag war) event wire

C->S 0x7753 is the registration toggle: an empty body sent from the
event NPC's talk window (client CIFNpcTalk menu action 0x38). The same
send joins or cancels; the server answers on the event channel.

S->C 0x30A9 is the event channel (client
CFlagWarClientState_HandleEventPacket 0x4B2530). The first byte is the
type: 0x02 standby 10 minutes, 0x04 game start, 0x05/0x09 timer stop,
0x08 mission start + u16 team value, 0x0A carrier + u32 gid, 0x0D
standby 5, 0x0E standby 1, and 0xFF introduces the result/notice
subtype family (join result, level error, cancel, master key, elapsed
sync, flag drop, mission clear/fail, win). The full decoded table
lives in the research tree (FlagWarClient.md); encoders land here as
the server grows the sends.

===========================================================================
*/

package wire

const (
	// OpFlagWarRegisterRequest is the C->S registration toggle (0x7753).
	OpFlagWarRegisterRequest uint16 = 0x7753
	// OpFlagWarEvent is the S->C flag war event channel (0x30A9).
	OpFlagWarEvent uint16 = 0x30A9
)

// Flag war event types (the first 0x30A9 byte).
const (
	FlagWarStandby10  uint8 = 0x02
	FlagWarGameStart  uint8 = 0x04
	FlagWarTimerStop  uint8 = 0x05
	FlagWarTimerStop2 uint8 = 0x09
	FlagWarStandby5   uint8 = 0x0D
	FlagWarStandby1   uint8 = 0x0E
	FlagWarSubtypeTag uint8 = 0xFF
)

// Flag war notice subtypes (the byte after FlagWarSubtypeTag).
const (
	FlagWarJoinResult    uint8 = 0x00
	FlagWarStartNotEnter uint8 = 0x04
	FlagWarLevelError    uint8 = 0x05
	FlagWarGameCancel    uint8 = 0x06
	FlagWarPickupOurs    uint8 = 0x09
	FlagWarPickupTheirs  uint8 = 0x0A
	FlagWarMasterKey     uint8 = 0x0B
	FlagWarEventJoin     uint8 = 0x0C
	FlagWarElapsedSync   uint8 = 0x0E
	FlagWarFlagDrop      uint8 = 0x0F
	FlagWarMissionResult uint8 = 0x10
	FlagWarGameWin       uint8 = 0x11
	FlagWarStealBusy     uint8 = 0x12
	FlagWarScoreBusy     uint8 = 0x13
	FlagWarEnteredError  uint8 = 0x15
)

// Flag war mission-start type (the 0x30A9 type byte carrying a team).
const (
	FlagWarMissionStart uint8 = 0x08
	FlagWarCarrier      uint8 = 0x0A
)

/*
================
EncodeFlagWarEventType

EncodeFlagWarEventType composes a one-byte 0x30A9 event (the standby
and game-start notices carry no payload).
================
*/
func EncodeFlagWarEventType(eventType uint8) []byte {
	return NewWriter(1).U8(eventType).Payload()
}

/*
================
EncodeFlagWarEventSubtype

EncodeFlagWarEventSubtype composes a [0xFF, subtype] 0x30A9 notice.
================
*/
func EncodeFlagWarEventSubtype(subtype uint8) []byte {
	return NewWriter(2).U8(FlagWarSubtypeTag).U8(subtype).Payload()
}

/*
================
EncodeFlagWarMissionStart

Mission start: [0x08][u16 team]. The client opens the mission notice
and the twenty-minute timer only when the team value equals its own
side, so the value rides every send.
================
*/
func EncodeFlagWarMissionStart(team uint16) []byte {
	return NewWriter(3).U8(FlagWarMissionStart).U16(team).Payload()
}

/*
================
EncodeFlagWarCarrier

Carrier mark: [0x0A][u32 gid]. The client sets interaction state 5 on
that entity; the previous carrier's mark is the client's to replace.
================
*/
func EncodeFlagWarCarrier(gid uint32) []byte {
	return NewWriter(5).U8(FlagWarCarrier).U32(gid).Payload()
}

/*
================
EncodeFlagWarNamed

A [0xFF, subtype] notice carrying one character name. PORT CHOICE:
the native name framing was never pinned at the decisive layer, so
the port writes a u16 count followed by single-byte characters, the
same shape the join family uses elsewhere on this channel; the client
port decodes exactly this.
================
*/
func EncodeFlagWarNamed(subtype uint8, name string) []byte {
	w := NewWriter(4 + len(name))
	w.U8(FlagWarSubtypeTag).U8(subtype).U16(uint16(len(name)))
	for _, b := range []byte(name) {
		w.U8(b)
	}
	return w.Payload()
}

/*
================
EncodeFlagWarMasterKey

Master-key pickup: [0xFF, 0x0B][u32 key npc gid][u8 team][name]. The
client notices only when the team byte equals its own side.
================
*/
func EncodeFlagWarMasterKey(gid uint32, team uint8, name string) []byte {
	w := NewWriter(8 + len(name))
	w.U8(FlagWarSubtypeTag).U8(FlagWarMasterKey).U32(gid).U8(team).U16(uint16(len(name)))
	for _, b := range []byte(name) {
		w.U8(b)
	}
	return w.Payload()
}

/*
================
EncodeFlagWarMissionResult

Mission clear/fail: [0xFF, 0x10][u8 scoring team][name of scorer].
================
*/
func EncodeFlagWarMissionResult(team uint8, name string) []byte {
	w := NewWriter(5 + len(name))
	w.U8(FlagWarSubtypeTag).U8(FlagWarMissionResult).U8(team).U16(uint16(len(name)))
	for _, b := range []byte(name) {
		w.U8(b)
	}
	return w.Payload()
}

/*
================
EncodeFlagWarElapsed

Elapsed sync: [0xFF, 0x0E][u32 elapsed ms], sent every ten seconds
of match time.
================
*/
func EncodeFlagWarElapsed(elapsedMs uint32) []byte {
	return NewWriter(6).U8(FlagWarSubtypeTag).U8(FlagWarElapsedSync).U32(elapsedMs).Payload()
}
