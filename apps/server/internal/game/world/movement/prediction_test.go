package movement

import (
	"encoding/binary"
	"encoding/json"
	"testing"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"opensro.online/server/internal/game/enterworld"
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
	if admitted, _ := ledger.admit(1, 9); !admitted {
		t.Fatal("unbound command consumed its id")
	}
}

/*
================
TestPredictionErrorCodeClosedSet

Every refusal reason the authority can produce normalizes into the
closed receipt vocabulary: the pass-through tokens, the prose sentences
and coordinate formats mapped onto codes, and anything unrecognized
degraded to movementRefused. The raw reason never survives.
================
*/
func TestPredictionErrorCodeClosedSet(t *testing.T) {
	for _, tc := range []struct{ reason, want string }{
		{"", ""},
		{predictionStaleReason, "staleCommand"},
		{"attackLocked", "attackLocked"},
		{"abnormalState", "abnormalState"},
		{"characterDead", "characterDead"},
		{"knockedDown", "knockedDown"},
		{"teleportCasting", "teleportCasting"},
		{"deletePending", "deletePending"},
		{"invalidCosOwner", "invalidCosOwner"},
		{"areaAccessDenied", "areaAccessDenied"},
		{"thiefCargoInTown", "thiefCargoInTown"},
		{"characterNotFound", "characterNotFound"},
		{"characterLifeChanged", "characterLifeChanged"},
		{"ground movement cannot cross disconnected navigation spaces", "disconnectedNavigation"},
		{"ground destination sits on a disconnected navigation surface", "disconnectedNavigation"},
		{"ground movement cannot cross the outdoor/dungeon authority boundary", "dungeonBoundary"},
		{"dungeon movement does not cross regions", "dungeonBoundary"},
		{"movement destination is blocked by world collision", "endpointBlocked"},
		{"pathguard endpointBlocked: destination tile is unwalkable (stock client cannot compose this click)", "endpointBlocked"},
		{"movement region has no server navigation coverage", "noCoverage"},
		{"pathguard noCoverage: movement authority data is incomplete", "noCoverage"},
		{"movement path crosses blocked world collision", "pathTruncated"},
		{"pathguard truncated: movement chord exceeds the bounded authority scan", "pathTruncated"},
		{"deepWaterDestination region=0x8001 (986.5, 0.0) submersion=3.2 (surface=0.0 destY=-3.2)", "deepWaterDestination"},
		{"deepWaterDestination", "deepWaterDestination"},
		{"malformedMovementRequest len=0", "malformedMovementRequest"},
		{"malformedMovementRequest angular len=3", "malformedMovementRequest"},
		{"malformedMovementRequest len=7", "malformedMovementRequest"},
		{"malformedHeadingRequest len=2", "malformedMovementRequest"},
		{"invalidHeadingRequest len=2", "malformedMovementRequest"},
		{"unsupportedNativeMoveOpcode 0x7738", "malformedMovementRequest"},
		{"unsupportedMovementMode 0x7F (the client serializer emits only 0 angular / 1 positional)", "unsupportedMovementMode"},
		{"a refusal nobody has named yet", "movementRefused"},
	} {
		if got := predictionErrorCode(tc.reason); got != tc.want {
			t.Errorf("predictionErrorCode(%q) = %q, want %q", tc.reason, got, tc.want)
		}
	}
}

/*
================
TestPredictionReceiptCarriesNormalizedCode

The 0x000A receipt carries the normalized code: a deep-water refusal's
coordinates and a prose refusal alike stay server-side.
================
*/
func TestPredictionReceiptCarriesNormalizedCode(t *testing.T) {
	character := testCharacter()
	for _, tc := range []struct{ reason, want string }{
		{"deepWaterDestination region=0x8001 (986.5, 0.0) submersion=3.2 (surface=0.0 destY=-3.2)", "deepWaterDestination"},
		{"ground movement cannot cross disconnected navigation spaces", "disconnectedNavigation"},
		{"a refusal nobody has named yet", "movementRefused"},
	} {
		var sends predictionSends
		result := MoveOutcome{Refusal: &simulation.MoveError{Reason: tc.reason}}
		sendPredictionReceipt(sends.send, 9, character, result)
		if len(sends.receipts) != 1 {
			t.Fatalf("reason %q: %d receipts", tc.reason, len(sends.receipts))
		}
		receipt := sends.receipts[0]
		if receipt["accepted"] != false || receipt["error"] != tc.want {
			t.Fatalf("reason %q: receipt = %v, want error %q", tc.reason, receipt, tc.want)
		}
	}
}

/*
================
TestPredictionLedgerMarksAndRefusalCounts

Admission returns the high-water mark that decides it, drops and
post-admission refusals are counted under their keys, and both log
lines carry the fields a bug report needs: gid, last, envelope, mode.
================
*/
func TestPredictionLedgerMarksAndRefusalCounts(t *testing.T) {
	character := testCharacter()
	nowMs := testStartMs
	rt := motionTestRuntime(character, &nowMs)
	ledger := newPredictionLedger()
	hook := logtest.NewLocal(log.StandardLogger())
	defer hook.Reset()

	if admitted, last := ledger.admit(7, 5); !admitted || last != 5 {
		t.Fatalf("admit(7, 5) = %v, %d, want true, 5", admitted, last)
	}
	if admitted, last := ledger.admit(7, 3); admitted || last != 5 {
		t.Fatalf("admit(7, 3) = %v, %d, want false, 5", admitted, last)
	}
	if got := ledger.highWater(7); got != 5 {
		t.Fatalf("highWater(7) = %d, want 5", got)
	}
	if got := ledger.highWater(8); got != 0 {
		t.Fatalf("highWater(8) = %d, want 0", got)
	}

	var sends predictionSends
	rt.predictedMove(ledger, 7, "0", character, predictedMovePayload(3, encodeMoveBody(1, 0x6B4F, 100, 0, 100)), sends.send)
	if ledger.drops[predictionDropStale] != 1 {
		t.Fatalf("stale drops = %d, want 1", ledger.drops[predictionDropStale])
	}
	// An admitted command with an unknown mode byte is refused by the
	// authority and counted under its normalized code.
	sends = predictionSends{}
	rt.predictedMove(ledger, 7, "0", character, predictedMovePayload(9, []byte{0x7F}), sends.send)
	if len(sends.receipts) != 1 || sends.receipts[0]["error"] != "unsupportedMovementMode" {
		t.Fatalf("refused command receipts = %v", sends.receipts)
	}
	if ledger.drops["unsupportedMovementMode"] != 1 {
		t.Fatalf("refusal counts = %v, want unsupportedMovementMode 1", ledger.drops)
	}

	gid := enterworld.ObjectIDForCharacter(character)
	var dropSeen, refusalSeen bool
	for _, entry := range hook.AllEntries() {
		switch entry.Message {
		case "movement: predicted move not executed":
			dropSeen = true
			if entry.Data["reason"] != predictionDropStale || entry.Data["id"] != uint32(3) ||
				entry.Data["last"] != uint32(5) || entry.Data["gid"] != gid ||
				entry.Data["envelope"] != 1 || entry.Data["mode"] != 1 || entry.Data["count"] != uint64(1) {
				t.Fatalf("drop log fields = %v", entry.Data)
			}
		case "movement: predicted move refused":
			refusalSeen = true
			if entry.Data["reason"] != "unsupportedMovementMode" || entry.Data["id"] != uint32(9) ||
				entry.Data["last"] != uint32(9) || entry.Data["gid"] != gid ||
				entry.Data["envelope"] != 1 || entry.Data["mode"] != 0x7F || entry.Data["count"] != uint64(1) {
				t.Fatalf("refusal log fields = %v", entry.Data)
			}
		}
	}
	if !dropSeen || !refusalSeen {
		t.Fatalf("log lines seen: drop %v, refusal %v", dropSeen, refusalSeen)
	}
}
