package eventdispatch

import (
	"errors"
	"fmt"
	"io"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
)

type probe struct {
	h                                              [2]*Handler
	now, result, disable, resets, invoked, removed uint32
}

func (p *probe) ResetArguments()   { p.resets++ }
func (p *probe) NowMillis() uint32 { return p.now }
func (p *probe) Invoke(h *Handler) uint32 {
	i := uint32(0)
	if h == p.h[1] {
		i = 1
	}
	p.invoked |= 1 << i
	if i == 0 && p.disable != 0 {
		p.h[1].Enabled = 0
	}
	if i == 0 {
		return p.result
	}
	return 2
}
func (p *probe) Unregister(h *Handler) {
	i := uint32(0)
	if h == p.h[1] {
		i = 1
	}
	p.removed |= 1 << i
	h.Enabled = 0
}
func TestNativeDispatchSequences(t *testing.T) {
	f, err := licensed.ServerFixtureReader(t, "internal/game/internal/eventdispatch/testdata/native-event-dispatch-20260921.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	count := 0
	for {
		var r, e0, e1, now, disable, want, resets, invoked, removed, t0, d0, t1, d1 uint32
		_, err = fmt.Fscan(f, &r, &e0, &e1, &now, &disable, &want, &resets, &invoked, &removed, &t0, &d0, &t1, &d1)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		p := &probe{now: now, result: r, disable: disable}
		p.h = [2]*Handler{{LastTick: 7, Deadline: 100, Interval: 20, Enabled: uint8(e0)}, {LastTick: 7, Deadline: 100, Interval: 20, Enabled: uint8(e1)}}
		actual := Dispatch(p.h[:], p)
		if actual != want || p.resets != resets || p.invoked != invoked || p.removed != removed || p.h[0].LastTick != t0 || p.h[0].Deadline != d0 || p.h[1].LastTick != t1 || p.h[1].Deadline != d1 {
			t.Fatalf("native sequence %d differs: result=%d host=%+v handlers=%+v %+v", count, actual, p, p.h[0], p.h[1])
		}
		count++
	}
	if count != 270 {
		t.Fatal(count)
	}
	if Dispatch(nil, &probe{}) != 2 {
		t.Fatal("empty range")
	}
}
func TestTimingUsesNativeUnsignedAbsoluteDeadline(t *testing.T) {
	h := NewHandler(0xfffffff0)
	h.SetTiming(0xfffffff0, 32, 20)
	if h.Deadline != 16 || !h.Due(0xfffffff1) || h.Deadline != 36 {
		t.Fatal(h)
	}
	h.SetTiming(100, 0, 0)
	if !h.Due(100) || !h.Due(101) || h.Deadline != 100 {
		t.Fatal(h)
	}
	h.Deadline = 0
	h.Interval = 10
	if !h.Due(1) || h.Deadline != 0 {
		t.Fatal(h)
	}
}
