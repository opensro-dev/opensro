/*
===========================================================================

loadingqueue_test.go - held movement commands replay at scene-ready

===========================================================================
*/
package movement

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
================
TestHeldMovesReplayAtSceneReady

A movement command sent while the scene loads stays queued, and at
scene-ready replays it through the same handler body a live dispatch
would take. The predicted command's unbound refusal is counted in the
ledger exactly as a live arrival's would be, proving the replay reached
the real handler - and the queue is left empty.
================
*/
func TestHeldMovesReplayAtSceneReady(t *testing.T) {
	t.Parallel()
	character := testCharacter()
	rt := testRuntime(character)
	if rt.predictLedger == nil {
		rt.predictLedger = newPredictionLedger()
	}
	session := &transport.Session{ID: 7}
	session.BeginSceneAdmission()

	start := simulation.EuropeStartProfile()
	body := encodeMoveBody(1, start.RegionID, int16(start.X)+200, int16(start.Y), int16(start.Z))
	if !rt.queueLoadingMove(session, false, body) {
		t.Fatal("native command was not held during loading")
	}
	predicted := make([]byte, 5+len(body))
	predicted[0] = 1
	binary.LittleEndian.PutUint32(predicted[1:5], 3)
	copy(predicted[5:], body)
	if !rt.queueLoadingMove(session, true, predicted) {
		t.Fatal("predicted command was not held during loading")
	}
	if got := len(rt.loadingQueues[session.ID]); got != 2 {
		t.Fatalf("queue holds %d commands, want 2", got)
	}

	if !session.FinishSceneReentry() {
		t.Fatal("a loading scene did not finish reentry")
	}
	rt.drainLoadingQueue(session)

	if got := len(rt.loadingQueues[session.ID]); got != 0 {
		t.Fatalf("queue holds %d commands after the replay", got)
	}
	if got := rt.predictLedger.drops[predictionDropUnbound]; got != 1 {
		t.Fatalf("unbound drops = %d, want 1: the held predicted command did not reach the handler", got)
	}
}

/*
================
TestLoadingQueueCapMatchesClientPendingCap

More held commands than the client's own pending cap means the client
long ago gave up on them: the excess drops instead of queueing (and the
arrival that crosses the cap is told).
================
*/
func TestLoadingQueueCapMatchesClientPendingCap(t *testing.T) {
	t.Parallel()
	rt := testRuntime(testCharacter())
	session := &transport.Session{ID: 9}
	for i := 0; i < loadingQueueCap; i++ {
		if !rt.queueLoadingMove(session, false, []byte{1}) {
			t.Fatalf("command %d was not held below the cap", i)
		}
	}
	if rt.queueLoadingMove(session, false, []byte{1}) {
		t.Fatal("a command past the client's pending cap was held")
	}
	if got := len(rt.loadingQueues[session.ID]); got != loadingQueueCap {
		t.Fatalf("queue holds %d commands, want the cap %d", got, loadingQueueCap)
	}
}
