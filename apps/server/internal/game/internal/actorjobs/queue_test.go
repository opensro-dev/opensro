package actorjobs

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"opensro.online/server/internal/testsupport/licensed"
	"reflect"
	"testing"
)

type probe struct {
	q                        *Queue
	trace                    *[][3]uint32
	id, result, flags, match uint32
	appendOnce               *bool
	appendThird              bool
}

func (p *probe) Advance(delta float32) uint32 {
	*p.trace = append(*p.trace, [3]uint32{1, p.id, math.Float32bits(delta)})
	if p.id == 0 && p.appendThird && !*p.appendOnce {
		*p.appendOnce = true
		p.q.Append(&probe{q: p.q, trace: p.trace, id: 2, result: 2})
	}
	return p.result
}
func (p *probe) Release()                          { *p.trace = append(*p.trace, [3]uint32{2, p.id, uint32(p.q.Len())}) }
func (p *probe) Flags() uint32                     { return p.flags }
func (p *probe) Category() uint8                   { return 1 }
func (p *probe) Matches(kind, value uint32) uint32 { return p.match }
func TestNativeQueue(t *testing.T) {
	f, err := licensed.ServerFixtureReader(t, "internal/game/internal/actorjobs/testdata/native-actor-jobs-20260921.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	in := bufio.NewReader(f)
	var q *Queue
	var trace [][3]uint32
	var now uint32
	appended := false
	rows := 0
	for {
		var last, initial, r0, r1, appendThird, stage, wantTick, wantSize, n uint32
		_, err := fmt.Fscan(in, &last, &initial, &r0, &r1, &appendThird, &stage, &wantTick, &wantSize, &n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		want := make([][3]uint32, 0, n)
		for i := uint32(0); i < n; i++ {
			var e [3]uint32
			if _, err := fmt.Fscan(in, &e[0], &e[1], &e[2]); err != nil {
				t.Fatal(err)
			}
			want = append(want, e)
		}
		if stage == 0 {
			now = last
			appended = false
			q = New(func() uint32 { trace = append(trace, [3]uint32{0, 0, now}); return now })
			q.Append(&probe{q: q, trace: &trace, id: 0, result: r0, appendOnce: &appended, appendThird: appendThird != 0})
			q.Append(&probe{q: q, trace: &trace, id: 1, result: r1})
		}
		trace = make([][3]uint32, 0)
		now = initial + stage*300
		if stage < 2 {
			q.Advance()
		} else {
			q.Clear()
		}
		if q.lastTick != wantTick || uint32(q.Len()) != wantSize || !reflect.DeepEqual(trace, want) {
			t.Fatalf("row %d: tick %d/%d size %d/%d trace %v/%v", rows, q.lastTick, wantTick, q.Len(), wantSize, trace, want)
		}
		rows++
	}
	if rows != 270 {
		t.Fatal(rows)
	}
}
func TestMatchingAndClock(t *testing.T) {
	now := uint32(7)
	reads := 0
	q := New(func() uint32 { reads++; return now })
	var trace [][3]uint32
	for i, flags := range []uint32{1, 0, 0} {
		q.Append(&probe{q: q, trace: &trace, id: uint32(i), flags: flags, match: 1})
	}
	if reads != 2 {
		t.Fatal("nonempty append resampled clock")
	}
	if !q.RemoveFirstMatching(1, 0, 0) || q.Len() != 2 || len(trace) != 1 || trace[0] != [3]uint32{2, 1, 3} {
		t.Fatal(trace)
	}
	for n := q.jobs.Front(); n != nil; n = n.Next() {
		n.Value.(*probe).match = 2
	}
	if q.RemoveFirstMatching(1, 0, 0) {
		t.Fatal("truthy match accepted")
	}
	q.Clear()
	now = 100
	q.Advance()
	if q.lastTick != 0 || reads != 2 {
		t.Fatal("empty tick changed clock")
	}
	q.Append(&probe{q: q, trace: &trace})
	if q.lastTick != 100 || reads != 3 {
		t.Fatal("first append did not reset clock")
	}
	q.Clear()
}
