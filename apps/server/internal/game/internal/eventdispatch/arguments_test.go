package eventdispatch

import (
	"bufio"
	"fmt"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
)

func mustRejectArguments(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("invalid event argument transition accepted")
		}
	}()
	f()
}
func TestArgumentsRepeatedCallbacks(t *testing.T) {
	var a Arguments
	a.AppendWord(0x81234567)
	a.AppendWord(0xfedcba98)
	mustRejectArguments(t, func() { a.ReadWord() }) // dispatcher reset is required
	for i := 0; i < 3; i++ {
		a.ResetCursor()
		if a.ReadWord() != 0x81234567 || a.ReadWord() != 0xfedcba98 {
			t.Fatal("payload order/cursor drift")
		}
		mustRejectArguments(t, func() { a.ReadWord() })
	}
	a.ResetCursor()
	mustRejectArguments(t, func() { a.AppendWord(7) }) // reset must preserve read mode
}
func TestArgumentsCapacity(t *testing.T) {
	var a Arguments
	mustRejectArguments(t, func() { a.Read(nil) })
	for i := 0; i < 32; i++ {
		a.AppendWord(uint32(i))
	}
	mustRejectArguments(t, func() { a.AppendWord(32) })
	a.ResetCursor()
	for i := 0; i < 32; i++ {
		if a.ReadWord() != uint32(i) {
			t.Fatal(i)
		}
	}
}

func TestArgumentsNativeReads(t *testing.T) {
	f, err := licensed.ServerFixtureReader(t, "internal/game/internal/eventdispatch/testdata/native-event-arguments-20260921.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	in := bufio.NewReader(f)
	rows := 0
	for count := 1; count <= 32; count++ {
		var a Arguments
		for i := 0; i < count; i++ {
			a.AppendWord(0x81234567 + uint32(i)*0x10203)
		}
		for cycle := 0; cycle < 3; cycle++ {
			a.ResetCursor()
			for i := 0; i < count; i++ {
				var n, c, index int
				var value, cursor, reading uint32
				if _, err := fmt.Fscan(in, &n, &c, &index, &value, &cursor, &reading); err != nil {
					t.Fatal(err)
				}
				if n != count || c != cycle || index != i || a.ReadWord() != value || a.cursor != cursor || !a.reading || reading != 1 {
					t.Fatalf("native mismatch row %d", rows)
				}
				rows++
			}
		}
	}
	if rows != 1584 {
		t.Fatal(rows)
	}
}

func TestArgumentsRegisteredDispatch(t *testing.T) {
	r := Receiver{OrderKey: 1}
	first, second := NewHandler(0), NewHandler(0)
	r.Register(0, 2, first, nil)
	r.Register(0, 2, second, nil)
	var a Arguments
	a.AppendWord(7)
	a.AppendWord(9)
	calls := 0
	invoke := func(h *Handler, a *Arguments) uint32 {
		calls++
		if a.ReadWord() != 7 || a.ReadWord() != 9 {
			t.Fatal("callback payload drift")
		}
		if h == second {
			return 4
		}
		return 2
	}
	if r.DispatchArguments(0, 2, &a, func() uint32 { return 1 }, invoke) != 4 || calls != 2 || second.Enabled != 0 {
		t.Fatal("callback/ownership mismatch")
	}
	if r.DispatchArguments(0, 2, &a, func() uint32 { return 2 }, invoke) != 0 || calls != 3 {
		t.Fatal("redispatch mismatch")
	}
}
