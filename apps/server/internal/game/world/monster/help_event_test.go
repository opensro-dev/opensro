package monster

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"opensro.online/server/internal/testsupport/licensed"
	"os"
	"testing"
)

func TestFrozenHelpTraceNativeVectors(t *testing.T) {
	var data struct {
		CandidateSHA256 string
		Cases           []struct {
			Boundary                       uint8
			Limit                          int32
			Nest                           bool
			HomeDistance, Radius, Distance float32
			Expected                       uint8
		}
	}
	b, err := licensed.ServerFixture(t, "internal/game/world/monster/testdata/ai-help-trace-native-v1188.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("help_event.go")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(source)
	if hex.EncodeToString(hash[:]) != data.CandidateSHA256 {
		t.Fatal("help candidate differs from frozen native corpus")
	}
	for i, c := range data.Cases {
		got := (TacticsControls{TraceBoundary: c.Boundary, TraceData: c.Limit}).HelpWithinTrace(c.Distance, c.HomeDistance, c.Radius, c.Nest)
		if got != (c.Expected == 1) {
			t.Fatalf("native help trace %d: %+v, Go=%v", i, c, got)
		}
	}
}

func TestHelpEventWireAndReceiverBranches(t *testing.T) {
	b := make([]byte, 14)
	b[0] = 1
	b[9] = 7
	binary.LittleEndian.PutUint32(b[1:], 123)
	binary.LittleEndian.PutUint32(b[5:], 456)
	binary.LittleEndian.PutUint32(b[10:], 789)
	e, err := DecodeHelpEvent(b)
	if err != nil || e != (HelpEvent{1, 123, 456, 7, 789}) {
		t.Fatalf("native event order: %+v %v", e, err)
	}
	for _, n := range []int{0, 13, 15} {
		if _, err := DecodeHelpEvent(make([]byte, n)); err == nil {
			t.Fatalf("accepted length %d", n)
		}
	}
	c := TacticsControls{ID: 456, HelpResponse: 0}
	for response := uint8(0); response < 4; response++ {
		c.HelpResponse = response
		for _, request := range []uint8{0, 1, 2, 255} {
			for _, sender := range []uint8{0, 1, 7} {
				for _, battle := range []bool{false, true} {
					for _, hp := range []uint32{79, 80, 100} {
						e.RequestType, e.SenderParameter = request, sender
						want := response != 2 && (!battle || sender == 1) && hp >= 80
						if c.HelpReceiverHeader(e, battle, hp, 100) != want {
							t.Fatalf("receiver gate resp=%d req=%d sender=%d battle=%v hp=%d", response, request, sender, battle, hp)
						}
					}
				}
			}
		}
	}
	c.HelpResponse = 1
	e.RequestType = 0
	e.SenderTacticsID++
	if c.HelpReceiverHeader(e, false, 100, 100) {
		t.Fatal("type zero ignored tactics identity")
	}
}

func TestHelpTraceUsesDifferentNestAndUnnestedEquality(t *testing.T) {
	c := TacticsControls{TraceBoundary: 1, TraceData: 500}
	for _, row := range []struct {
		distance, home, radius float32
		nest, want             bool
	}{
		{500, 0, 100, true, true}, {500.01, 0, 100, true, false},
		{500, 0, 100, false, false}, {499, 0, 100, false, true},
		{450, 200, 100, true, true}, {451, 200, 100, true, false},
	} {
		if got := c.HelpWithinTrace(row.distance, row.home, row.radius, row.nest); got != row.want {
			t.Fatalf("trace %+v = %v", row, got)
		}
	}
	c.TraceBoundary = 2
	if !c.HelpWithinTrace(10000, 10000, 0, true) {
		t.Fatal("boundary-2 bypass lost")
	}
}

func TestHelpInboxReplacementAndBattleReentry(t *testing.T) {
	a, b := HelpEvent{TargetGID: 123}, HelpEvent{TargetGID: 456}
	box := (HelpInbox{}).Replace(a)
	old := box
	box = box.Replace(b)
	if e, ok := box.Pending(); !ok || e != b || box == old {
		t.Fatal("pending event did not replace")
	}
	if _, ok := box.Consumed().Pending(); ok {
		t.Fatal("rejected event retained")
	}
	for mode := MoverMode(0); mode < moverModeCount; mode++ {
		m := coherentMover(mode)
		battle := m.InNativeBattle()
		if err := m.Transition(MoverEventHelpAccepted, 456); err != nil {
			t.Fatal(err)
		}
		if battle {
			if m.Mode() != MoverBattleReentry || m.TargetGID() != 0 || m.HelpLatched() {
				t.Fatal("native BATTLE exit did not clear newly assigned target/latch")
			}
		} else if m.Mode() != MoverChasing || m.TargetGID() != 456 || !m.HelpLatched() {
			t.Fatal("help entry failed")
		}
	}
}
