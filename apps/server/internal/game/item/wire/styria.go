/*
===========================================================================

styria.go - the Styria Clash status snapshot (port-only, not native)

#3648 slice 5: with SRO_STYRIA_CLASH on, an applicant, a registrant or
an entered participant is kept told of the arena for the client's
registration, score and result windows: one snapshot (0x3FC8) when
their standing changes, the clocks carried as absolute instants so a
quiet standing sends nothing. 0x3FC8 is a port-only opcode: no v1.150
client or v1.188 server uses it (0x3FC0..0x3FC7 and 0x3FCA..0x3FCE
are the band's other port-only allocations, server wire and client
dispatch searched), and a client that predates it drops the unknown
opcode, so no release-protocol bump carries it. With the flag off
nothing is ever sent.

Body, little-endian:

	u8 phase (0 registration, 1 match, 2 result)
	u32 matchSeq (the match the snapshot belongs to, 0 before any)
	u64 nextStartMs (registration: the next match start, else 0)
	u64 windowOpensMs (registration: applications open, else 0)
	u64 phaseEndsMs (match: the clock's end; result: the world's
		close; registration: 0)
	u8 ownSide (0 Hunters, 1 Thieves, 255 none)
	u8 ownState (0 watching, 1 applied, 2 entered)
	u16 hunters, u16 thieves (registration: the division's
		applications per side; match/result: entered standing)
	u16 scoreHunters, u16 scoreThieves
	u8 outcome (0 open, 1 Hunters, 2 Thieves, 3 draw)
	u32 jobExp, u32 skillExp (result: the viewer's own reward
		figures, else 0)
	u64 serverNowMs (the build instant; the client anchors its
		countdowns on it. It rides last so the sender's
		changed-bytes check can ignore it: a standing whose
		only news is the clock sends nothing)

The arena coin figure of the later client's result window is not
carried: the group's config pays none (action/styria_reward.go).

===========================================================================
*/

package wire

import "encoding/binary"

// OpStyriaStatus is the port-only S->C Styria Clash status snapshot.
const OpStyriaStatus uint16 = 0x3FC8

// The snapshot's phases.
const (
	StyriaPhaseRegistration = 0
	StyriaPhaseMatch        = 1
	StyriaPhaseResult       = 2
)

// The viewer's own standing in a snapshot.
const (
	StyriaStateWatching = 0
	StyriaStateApplied  = 1
	StyriaStateEntered  = 2
)

// StyriaNoSide is the ownSide byte of a viewer with no side.
const StyriaNoSide = 255

/*
================
StyriaStatus

One viewer's Styria Clash standing, encoded by EncodeStyriaStatus.
Counts and scores are the division's; the own fields are the
viewer's. Sides are 0 (Hunters) and 1 (Thieves); OwnSide -1 encodes
as StyriaNoSide.
================
*/
type StyriaStatus struct {
	Phase         int
	MatchSeq      uint32
	NextStartMs   int64
	WindowOpensMs int64
	PhaseEndsMs   int64
	OwnSide       int
	OwnState      int
	Hunters       int
	Thieves       int
	ScoreHunters  int
	ScoreThieves  int
	Outcome       int
	JobExp        int64
	SkillExp      int64
	ServerNowMs   int64
}

/*
================
EncodeStyriaStatus

The snapshot's bytes. Negative figures encode as 0 and counts clamp
to the u16 the body carries, so a wild book degrades the frame
instead of wrapping it.
================
*/
func EncodeStyriaStatus(status StyriaStatus) []byte {
	ownSide := byte(StyriaNoSide)
	if status.OwnSide == 0 || status.OwnSide == 1 {
		ownSide = byte(status.OwnSide)
	}
	out := make([]byte, 0, 56)
	out = append(out, byte(status.Phase))
	out = binary.LittleEndian.AppendUint32(out, status.MatchSeq)
	out = binary.LittleEndian.AppendUint64(out, uint64(max(0, status.NextStartMs)))
	out = binary.LittleEndian.AppendUint64(out, uint64(max(0, status.WindowOpensMs)))
	out = binary.LittleEndian.AppendUint64(out, uint64(max(0, status.PhaseEndsMs)))
	out = append(out, ownSide, byte(status.OwnState))
	out = binary.LittleEndian.AppendUint16(out, uint16(min(max(0, status.Hunters), 0xffff)))
	out = binary.LittleEndian.AppendUint16(out, uint16(min(max(0, status.Thieves), 0xffff)))
	out = binary.LittleEndian.AppendUint16(out, uint16(min(max(0, status.ScoreHunters), 0xffff)))
	out = binary.LittleEndian.AppendUint16(out, uint16(min(max(0, status.ScoreThieves), 0xffff)))
	out = append(out, byte(status.Outcome))
	out = binary.LittleEndian.AppendUint32(out, uint32(min(max(0, status.JobExp), 0xffffffff)))
	out = binary.LittleEndian.AppendUint32(out, uint32(min(max(0, status.SkillExp), 0xffffffff)))
	out = binary.LittleEndian.AppendUint64(out, uint64(max(0, status.ServerNowMs)))
	return out
}
