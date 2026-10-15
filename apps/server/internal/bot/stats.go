/*
===========================================================================

stats.go - spend the level stat points

The server grants stat points on each level. The points wait until the
player spends them. The bot spends them into one configured stat, so it
grows a real build (issue #200).

The allocation is the ordinary client path: one request per point with an
empty body (0x727A for STR, 0x7552 for INT) and one acknowledgement per
request (0xB27A, 0xB552). The server reports the absolute remaining pool in
the 0x30D2 experience update of a level-up and in the 0x30B3 type-3 update.

===========================================================================
*/
package bot

import (
	"encoding/binary"
	"fmt"
	"strings"

	"opensro.online/server/internal/game/item/wire"
)

// The stat allocation acknowledgements. Both carry [u8 1] on success and
// [u8 2][u8 errorCode] on a refusal.
const (
	OpcodeAllocStrAck uint16 = wire.OpAllocStrResponse
	OpcodeAllocIntAck uint16 = wire.OpAllocIntResponse
)

// StatBuild is the stat that gets the level stat points.
type StatBuild uint8

const (
	// StatNone keeps the points. The bot sends no allocation request.
	StatNone StatBuild = iota
	StatSTR
	StatINT
)

func (b StatBuild) String() string {
	switch b {
	case StatSTR:
		return "str"
	case StatINT:
		return "int"
	default:
		return "none"
	}
}

// ParseStatBuild reads the operator value: str, int or none.
func ParseStatBuild(value string) (StatBuild, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "str":
		return StatSTR, nil
	case "int":
		return StatINT, nil
	case "", "none":
		return StatNone, nil
	}
	return StatNone, fmt.Errorf("bot: stat build %q is not str, int or none", value)
}

// StatOpcode maps a build to the native allocation request (issue #200):
// StatSTR is 0x727A and StatINT is 0x7552. StatNone reports 0, so the caller
// sends no request.
func StatOpcode(build StatBuild) uint16 {
	switch build {
	case StatSTR:
		return wire.OpAllocStrRequest
	case StatINT:
		return wire.OpAllocIntRequest
	default:
		return 0
	}
}

// StatName returns the canonical build name (issue #200): "str", "int" or
// "none".
func StatName(build StatBuild) string { return build.String() }

// statAckTicks is the number of ticks the spender waits for an
// acknowledgement before it sends the request again.
const statAckTicks = 10

// StatSpender spends the remaining stat points, one request at a time. A new
// request goes out only after the acknowledgement of the last one, so the bot
// never asks for more points than the pool holds. A nil *StatSpender is a
// no-op.
type StatSpender struct {
	build   StatBuild
	pool    int
	pending bool
	wait    int
}

// NewStatSpender returns a spender for one build.
func NewStatSpender(build StatBuild) *StatSpender {
	return &StatSpender{build: build}
}

// Build returns the configured stat.
func (s *StatSpender) Build() StatBuild {
	if s == nil {
		return StatNone
	}
	return s.build
}

// Pool returns the remaining stat points the spender knows.
func (s *StatSpender) Pool() int {
	if s == nil {
		return 0
	}
	return s.pool
}

// SetPool applies an absolute remaining-point value from the server.
func (s *StatSpender) SetPool(points int) {
	if s == nil {
		return
	}
	if points < 0 {
		points = 0
	}
	s.pool = points
}

// Next is called once per tick. It returns the allocation opcode to send, or
// false when there is nothing to spend or an acknowledgement is outstanding.
func (s *StatSpender) Next() (uint16, bool) {
	if s == nil || s.build == StatNone || s.pool <= 0 {
		return 0, false
	}
	if s.pending {
		s.wait--
		if s.wait > 0 {
			return 0, false
		}
	}
	opcode := StatOpcode(s.build)
	if opcode == 0 {
		return 0, false
	}
	s.pending = true
	s.wait = statAckTicks
	return opcode, true
}

// Ack applies an allocation acknowledgement. A success takes one point from
// the pool. A refusal empties the pool: the server holds the real count, and
// the next absolute update fills the pool again. It reports false for a
// short body.
func (s *StatSpender) Ack(payload []byte) (spent bool, ok bool) {
	if s == nil || len(payload) < 1 {
		return false, false
	}
	s.pending = false
	s.wait = 0
	if payload[0] != wire.ResultSuccess {
		s.pool = 0
		return false, true
	}
	if s.pool > 0 {
		s.pool--
	}
	return true, true
}

// ParsePointsStat reads the absolute remaining stat points from a 0x30B3
// type-3 body: [u8 3][u16 statPoints]. It rejects every other type.
func ParsePointsStat(payload []byte) (int, bool) {
	if len(payload) < 3 || payload[0] != wire.PointsTypeStat {
		return 0, false
	}
	return int(binary.LittleEndian.Uint16(payload[1:3])), true
}
