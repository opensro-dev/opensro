/*
===========================================================================

styria_test.go - the Styria Clash status snapshot's byte contract

===========================================================================
*/

package wire

import (
	"encoding/binary"
	"testing"
)

/*
================
styriaReader

Walks EncodeStyriaStatus's body in the tests.
================
*/
type styriaReader struct {
	buf []byte
	at  int
}

func (r *styriaReader) u8() int {
	v := int(r.buf[r.at])
	r.at++
	return v
}

func (r *styriaReader) u16() int {
	v := int(binary.LittleEndian.Uint16(r.buf[r.at:]))
	r.at += 2
	return v
}

func (r *styriaReader) u32() int64 {
	v := int64(binary.LittleEndian.Uint32(r.buf[r.at:]))
	r.at += 4
	return v
}

func (r *styriaReader) u64() int64 {
	v := int64(binary.LittleEndian.Uint64(r.buf[r.at:]))
	r.at += 8
	return v
}

/*
================
TestStyriaStatusRoundTrips
================
*/
func TestStyriaStatusRoundTrips(t *testing.T) {
	want := StyriaStatus{
		Phase: StyriaPhaseResult, MatchSeq: 7,
		NextStartMs: 111, WindowOpensMs: 22, PhaseEndsMs: 333,
		OwnSide: 1, OwnState: StyriaStateEntered,
		Hunters: 5, Thieves: 6, ScoreHunters: 9, ScoreThieves: 4,
		Outcome: 2, JobExp: 20, SkillExp: 11040, ServerNowMs: 999,
	}
	r := &styriaReader{buf: EncodeStyriaStatus(want)}
	got := StyriaStatus{
		Phase: r.u8(), MatchSeq: uint32(r.u32()),
		NextStartMs: r.u64(), WindowOpensMs: r.u64(), PhaseEndsMs: r.u64(),
		OwnSide: r.u8(), OwnState: r.u8(),
		Hunters: r.u16(), Thieves: r.u16(),
		ScoreHunters: r.u16(), ScoreThieves: r.u16(),
		Outcome: r.u8(), JobExp: r.u32(), SkillExp: r.u32(), ServerNowMs: r.u64(),
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	if r.at != len(r.buf) {
		t.Fatalf("read %d of %d bytes", r.at, len(r.buf))
	}
}

/*
================
TestStyriaStatusClamps

A sideless viewer encodes 255; negative figures and oversized
counts clamp instead of wrapping.
================
*/
func TestStyriaStatusClamps(t *testing.T) {
	r := &styriaReader{buf: EncodeStyriaStatus(StyriaStatus{
		Phase: StyriaPhaseRegistration, OwnSide: -1,
		Hunters: 70000, JobExp: -5,
	})}
	_ = r.u8()
	_ = r.u32()
	_ = r.u64()
	_ = r.u64()
	_ = r.u64()
	if side := r.u8(); side != StyriaNoSide {
		t.Fatalf("ownSide = %d, want %d", side, StyriaNoSide)
	}
	_ = r.u8()
	if hunters := r.u16(); hunters != 0xffff {
		t.Fatalf("hunters = %d, want clamped 65535", hunters)
	}
}
