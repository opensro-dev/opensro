package combat

import (
	"bufio"
	"fmt"
	"math"
	"opensro.online/server/internal/testsupport/licensed"
	"reflect"
	"strings"
	"testing"
)

type deferredProbe struct {
	now    uint32
	player bool
	events [][4]uint32
}

func (h *deferredProbe) NowMillis() uint32 { return h.now }
func (h *deferredProbe) CurrentHP() uint32 { return 43 }
func (h *deferredProbe) CurrentMP() uint32 { return 27 }
func (h *deferredProbe) IsPlayer() bool    { return h.player }
func (h *deferredProbe) ApplyHit(v int32)  { h.events = append(h.events, [4]uint32{1, uint32(v), 0, 0}) }
func (h *deferredProbe) ConsumeResources(hp, mp int32) {
	h.events = append(h.events, [4]uint32{2, uint32(hp), uint32(mp), 0})
}
func (h *deferredProbe) WriteParameter(id, ch uint32, v float32) {
	h.events = append(h.events, [4]uint32{3, id, ch, math.Float32bits(v)})
}
func (h *deferredProbe) SendParameterStats() { h.events = append(h.events, [4]uint32{4, 0, 0, 0}) }
func (h *deferredProbe) CancelActionsAndRetireSkills() {
	h.events = append(h.events, [4]uint32{5, 0, 0, 0})
}
func (h *deferredProbe) SetMotion(v float32) {
	h.events = append(h.events, [4]uint32{6, math.Float32bits(v), 0, 0})
}
func (h *deferredProbe) DamageEquipment(k, p uint32) {
	h.events = append(h.events, [4]uint32{7, k, p, 0})
}

func TestNativeDeferredInstructions(t *testing.T) {
	f, err := licensed.ServerFixtureReader(t, "internal/game/combat/testdata/native-deferred-instructions-20260921.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	count := 0
	for s.Scan() {
		r := strings.NewReader(s.Text())
		var mask, mode, elapsed, initial, player, queued, keep, flags, n uint32
		if _, err := fmt.Fscan(r, &mask, &mode, &elapsed, &initial, &player, &queued, &keep, &flags, &n); err != nil {
			t.Fatal(err)
		}
		d := DeferredInstructions{StartedAt: 0xffffffd0, Applied: initial&4 != 0}
		for i := range d.Parameters {
			if mask&(1<<i) != 0 {
				d.Parameters[i] = &[4]uint32{100, 7, 19, mode}
			}
		}
		if d.NeedsQueue() != (queued != 0) {
			t.Fatalf("producer mismatch: %s", s.Text())
		}
		h := deferredProbe{now: d.StartedAt + elapsed, player: player != 0}
		l := DeferredVitalLatches{HP: initial&1 != 0, MP: initial&2 != 0}
		gotKeep := d.Advance(&h, &l)
		var gotFlags uint32
		if l.HP {
			gotFlags |= 1
		}
		if l.MP {
			gotFlags |= 2
		}
		if d.Applied {
			gotFlags |= 4
		}
		var want [][4]uint32
		for i := uint32(0); i < n; i++ {
			var e [4]uint32
			if _, err := fmt.Fscan(r, &e[0], &e[1], &e[2], &e[3]); err != nil {
				t.Fatal(err)
			}
			want = append(want, e)
		}
		if gotKeep != (keep != 0) || gotFlags != flags || !reflect.DeepEqual(h.events, want) {
			t.Fatalf("native mismatch row %d: %s; got %v %d %v", count, s.Text(), gotKeep, gotFlags, h.events)
		}
		count++
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 12288 {
		t.Fatalf("incomplete evidence: %d", count)
	}
}
