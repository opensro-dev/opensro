/*
===========================================================================

tickwindow_test.go - the tick's write window changes writes, not frames

A session the tick pushes to is held until the tick's last push and then
writes the tick's output in one transport write (Bridge.BeginTick and
EndTick, transport/session_write.go). The same pushes without the window
are today's one write per frame. Both must deliver the same frames in
the same order, including a frame another producer sends during the
tick; a frame sent outside a tick must leave at once, and a tick with
nothing to send must write nothing.

===========================================================================
*/
package worldsession

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

/*
================
windowFrame

An ordered tick frame labelled by its one payload byte.
================
*/
func windowFrame(label byte) simulation.Frame {
	return simulation.Frame{Opcode: simulation.OpMovementAck, Payload: []byte{label}}
}

/*
================
windowMove

A loss-tolerant live sample (0x30E3, gid last) labelled by its first
payload byte: a movement-only batch, so it rides the lossy lane.
================
*/
func windowMove(label byte, gid uint32) simulation.Frame {
	payload := make([]byte, 20)
	payload[0] = label
	payload[16] = byte(gid)
	return simulation.Frame{Opcode: transport.OpObjectSourceMove, Payload: payload}
}

/*
================
windowRun

What two sessions' connections were handed for one fixed script of
pushes, with or without the tick's window around the tick's pushes.
================
*/
type windowRun struct {
	a, b [][]transport.Frame
}

/*
================
runWindowScript
================
*/
func runWindowScript(t *testing.T, window bool) windowRun {
	t.Helper()
	c := newCrowd(t, crowdConfig{players: 2})
	defer c.close()
	wait.Eventually(t, 5*time.Second, "the entry frames written", func() bool {
		return c.hub.Metrics().OutboundQueueDepth == 0
	})
	ma, mb := c.mems[0], c.mems[1]
	ma.record()
	mb.record()
	bridge := c.ticker.Push.(*Bridge)
	a, b := c.sessions[0], c.sessions[1]
	idA, idB := SessionSceneID(a), SessionSceneID(b)

	// Outside any tick a handler's frame leaves at once.
	c.handlers.PushToSession(idA, []simulation.Frame{windowFrame(1)})
	wait.Eventually(t, 3*time.Second, "the out-of-tick frame written", func() bool { return len(ma.recorded()) == 1 })

	if window {
		bridge.BeginTick()
	}
	bridge.PushToSession(idA, []simulation.Frame{windowFrame(2), windowFrame(3)})
	bridge.PushToSession(idA, []simulation.Frame{windowMove(4, 9)})
	bridge.PushToSession(idB, []simulation.Frame{windowFrame(5)})
	// Another producer inside the tick (a hook's captured publish).
	if err := a.Send(simulation.OpMovementAck, []byte{6}); err != nil {
		t.Fatal(err)
	}
	bridge.PushToSession(idA, []simulation.Frame{windowFrame(7)})
	if window {
		wait.Consistently(t, 50*time.Millisecond, "nothing written inside the tick", func() bool {
			return len(ma.recorded()) == 1 && len(mb.recorded()) == 0
		})
		bridge.EndTick()
	}
	wait.Eventually(t, 3*time.Second, "the tick's frames written", func() bool {
		return countFrames(ma.recorded()) == 6 && countFrames(mb.recorded()) == 1
	})

	// A tick with nothing to send writes nothing.
	if window {
		bridge.BeginTick()
		bridge.EndTick()
	}
	wait.Consistently(t, 30*time.Millisecond, "no write after an empty tick", func() bool {
		return countFrames(ma.recorded()) == 6 && countFrames(mb.recorded()) == 1
	})
	return windowRun{a: ma.recorded(), b: mb.recorded()}
}

/*
================
countFrames
================
*/
func countFrames(writes [][]transport.Frame) int {
	n := 0
	for _, w := range writes {
		n += len(w)
	}
	return n
}

/*
================
splitLanes

The labels of the ordered frames in write order, and of the lossy ones.
The writer drains the ordered queue before the lossy lane, so only the
ordered order is the transport's promise.
================
*/
func splitLanes(writes [][]transport.Frame) (ordered, lossy []byte) {
	for _, w := range writes {
		for _, f := range w {
			if f.Opcode == transport.OpObjectSourceMove {
				lossy = append(lossy, f.Payload[0])
			} else {
				ordered = append(ordered, f.Payload[0])
			}
		}
	}
	return ordered, lossy
}

/*
================
TestTickWindowDeliversTheSameFramesInOneWrite
================
*/
func TestTickWindowDeliversTheSameFramesInOneWrite(t *testing.T) {
	perFrame := runWindowScript(t, false)
	perTick := runWindowScript(t, true)
	for _, session := range []struct {
		name          string
		perFrame, got [][]transport.Frame
	}{{"A", perFrame.a, perTick.a}, {"B", perFrame.b, perTick.b}} {
		wantOrdered, wantLossy := splitLanes(session.perFrame)
		gotOrdered, gotLossy := splitLanes(session.got)
		if !bytes.Equal(gotOrdered, wantOrdered) || !bytes.Equal(gotLossy, wantLossy) {
			t.Fatalf("session %s: per-tick writes delivered %v + lossy %v, per-frame writes %v + lossy %v",
				session.name, gotOrdered, gotLossy, wantOrdered, wantLossy)
		}
	}
	if want := []byte{1, 2, 3, 6, 7}; !bytes.Equal(mustOrdered(perTick.a), want) {
		t.Fatalf("session A's ordered frames = %v, want %v", mustOrdered(perTick.a), want)
	}
	if len(perTick.a) != 2 || len(perTick.b) != 1 {
		t.Fatalf("per-tick transport writes: A %d, B %d; want 2 (the out-of-tick frame, the tick) and 1", len(perTick.a), len(perTick.b))
	}
	if got := len(perTick.a[1]); got != 5 {
		t.Fatalf("session A's tick write carried %d frames, want all 5", got)
	}
}

/*
================
mustOrdered
================
*/
func mustOrdered(writes [][]transport.Frame) []byte {
	ordered, _ := splitLanes(writes)
	return ordered
}
