package movement

import (
	"encoding/binary"
	"encoding/json"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

func TestPredictionUsesReceiptWithoutAlsoReplayingNativeMovement(t *testing.T) {
	native := MoveOutcome{Frames: []wire.Frame{{Opcode: simulation.OpMovementAck, Payload: []byte{1}}, {Opcode: 0x30b5, Payload: []byte{2}}}}
	feedback := predictionFeedback(native)
	if len(feedback) != 1 || feedback[0].Opcode != 0x30b5 {
		t.Fatalf("unrelated feedback lost or duplicate movement retained: %+v", feedback)
	}
	if len(native.Frames) != 2 || native.Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatal("native movement response was mutated")
	}
	refused := MoveOutcome{Frames: []wire.Frame{{Opcode: 0xb074, Payload: []byte{2, 1}}}}
	if got := predictionFeedback(refused); len(got) != 1 || got[0].Opcode != 0xb074 {
		t.Fatal("refusal feedback lost")
	}
}

/*
================
predictionSends

Records a predicted move's sends: feedback frames and receipts.
================
*/
type predictionSends struct {
	receipts []map[string]any
	frames   int
}

/*
================
predictionSends.send
================
*/
func (r *predictionSends) send(opcode uint16, payload []byte) error {
	if opcode != transport.OpPredictedMoveResult {
		r.frames++
		return nil
	}
	var receipt map[string]any
	if err := json.Unmarshal(payload, &receipt); err != nil {
		return err
	}
	r.receipts = append(r.receipts, receipt)
	return nil
}

/*
================
predictedMovePayload
================
*/
func predictedMovePayload(id uint32, body []byte) []byte {
	p := []byte{1, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(p[1:5], id)
	return append(p, body...)
}

/*
================
TestStalePredictedMoveIsAnsweredWithAuthority

A repeated or older id is not executed again, but it is answered: a
refusal receipt with the current authority, so the client's pending
command settles instead of timing out the session.
================
*/
func TestStalePredictedMoveIsAnsweredWithAuthority(t *testing.T) {
	character := testCharacter()
	nowMs := testStartMs
	rt := motionTestRuntime(character, &nowMs)
	ledger := newPredictionLedger()
	start := simulation.EuropeStartProfile()
	body := encodeMoveBody(1, start.RegionID, int16(start.X)+200, int16(start.Y), int16(start.Z))
	var sends predictionSends
	rt.predictedMove(ledger, 1, "0", character, predictedMovePayload(5, body), sends.send)
	if len(sends.receipts) != 1 || sends.receipts[0]["accepted"] != true {
		t.Fatalf("first command receipts = %v", sends.receipts)
	}
	for _, id := range []uint32{5, 3} {
		sends = predictionSends{}
		rt.predictedMove(ledger, 1, "0", character, predictedMovePayload(id, body), sends.send)
		if len(sends.receipts) != 1 || sends.frames != 0 {
			t.Fatalf("id %d: receipts %v, %d feedback frames", id, sends.receipts, sends.frames)
		}
		receipt := sends.receipts[0]
		world, _ := receipt["world"].(map[string]any)
		if receipt["accepted"] != false || receipt["error"] != predictionStaleReason ||
			receipt["id"] != float64(id) || world["spawn"] == nil {
			t.Fatalf("id %d: stale receipt = %v", id, receipt)
		}
	}
	if ledger.drops[predictionDropStale] != 2 {
		t.Fatalf("stale drops counted %d, want 2", ledger.drops[predictionDropStale])
	}
}

/*
================
TestUnanswerablePredictedMovesAreCounted

Without an id or a bound character no receipt can be addressed; each such
command is counted by reason.
================
*/
func TestUnanswerablePredictedMovesAreCounted(t *testing.T) {
	character := testCharacter()
	nowMs := testStartMs
	rt := motionTestRuntime(character, &nowMs)
	ledger := newPredictionLedger()
	var sends predictionSends
	rt.predictedMove(ledger, 1, "0", character, []byte{1, 2}, sends.send)
	rt.predictedMove(ledger, 1, "0", character, predictedMovePayload(0, nil), sends.send)
	rt.predictedMove(ledger, 1, "0", nil, predictedMovePayload(9, nil), sends.send)
	if len(sends.receipts) != 0 || sends.frames != 0 {
		t.Fatalf("unanswerable commands sent %v / %d", sends.receipts, sends.frames)
	}
	for _, reason := range []string{predictionDropMalformed, predictionDropZeroID, predictionDropUnbound} {
		if ledger.drops[reason] != 1 {
			t.Fatalf("drops[%s] = %d", reason, ledger.drops[reason])
		}
	}
	// An unbound command does not advance the session's high-water mark.
	if !ledger.admit(1, 9) {
		t.Fatal("unbound command consumed its id")
	}
}
