/*
===========================================================================

lifecycle_test.go - native fortress flags under repeated and concurrent calls

Replay emitted frames with the original client's OR/XOR semantics. Duplicate
end messages must never reactivate war for a client that remains connected.

===========================================================================
*/
package siege

import (
	"sync"
	"testing"
)

/*
================
warCapture

Runtime serializes publication. Tests read the captured stream after callers
join, preserving actual wire order without an independent test-side lock.
================
*/
type warCapture struct {
	opcodes []uint16
	bodies  [][]byte
}

/*
================
Broadcast
================
*/
func (capture *warCapture) Broadcast(opcode uint16, payload []byte) {
	capture.opcodes = append(capture.opcodes, opcode)
	capture.bodies = append(capture.bodies, append([]byte(nil), payload...))
}

/*
================
replayWarFlags

The native 7E2100 helper ORs when enabling and XORs when disabling.
================
*/
func replayWarFlags(t *testing.T, capture *warCapture, active bool) bool {
	t.Helper()
	for i, body := range capture.bodies {
		if capture.opcodes[i] != 0x3887 || len(body) != 1 {
			t.Fatalf("invalid transition %04x %x", capture.opcodes[i], body)
		}
		switch body[0] {
		case 2:
			if active {
				t.Fatal("duplicate war begin")
			}
			active = true
		case 6:
			active = !active
			if active {
				t.Fatal("war end reactivated the native client's flag")
			}
		default:
			t.Fatalf("unexpected transition %x", body)
		}
	}
	return active
}

/*
================
TestWarTransitionsRetireExactlyOnce
================
*/
func TestWarTransitionsRetireExactlyOnce(t *testing.T) {
	for _, initiallyActive := range []bool{false, true} {
		capture := &warCapture{}
		rt := &Runtime{hub: capture, warActive: initiallyActive}
		rt.BroadcastWarEnd()
		rt.BroadcastWarEnd()
		rt.BroadcastWarBegin()
		rt.BroadcastWarBegin()
		rt.BroadcastWarEnd()
		rt.BroadcastWarEnd()
		if replayWarFlags(t, capture, initiallyActive) || rt.warActive {
			t.Fatal("server and native client did not finish outside war")
		}
		want := 2
		if initiallyActive {
			want++
		}
		if len(capture.bodies) != want {
			t.Fatalf("emitted %d transitions, want %d", len(capture.bodies), want)
		}
	}
	var inert *Runtime
	inert.BroadcastWarBegin()
	inert.BroadcastWarEnd()
}

/*
================
TestConcurrentWarTransitionsPreserveNativeWireOrder
================
*/
func TestConcurrentWarTransitionsPreserveNativeWireOrder(t *testing.T) {
	const callers = 32
	capture := &warCapture{}
	rt := &Runtime{hub: capture}
	var workers sync.WaitGroup
	for range callers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			rt.BroadcastWarBegin()
			rt.BroadcastWarEnd()
		}()
	}
	workers.Wait()
	rt.BroadcastWarEnd()
	if replayWarFlags(t, capture, false) || rt.warActive {
		t.Fatal("concurrent transitions left an active war")
	}
}
