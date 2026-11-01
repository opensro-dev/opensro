/*
===========================================================================

prediction.go - ordered movement command receipts for explicit prediction IDs

The actor movement lock spans admission and response enqueue, so the terminal
correction cannot overtake the ID-bearing receipt that admits its path.

===========================================================================
*/
package movement

import (
	"encoding/binary"
	"encoding/json"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

// The replacement client's explicit extension. The native movement payload is
// unchanged. IDs are strictly increasing per logical transport session; resume
// retains the high-water mark. A command is never executed twice.
/*
================
registerPredictedMovement
================
*/
func (rt *Runtime) registerPredictedMovement(hub *transport.Hub) {
	ledger := newPredictionLedger()
	rt.predictLedger = ledger
	hub.OnSessionClose(func(s *transport.Session, _ error) { ledger.forget(s.ID) })
	hub.Handle(transport.OpPredictedMove, func(s *transport.Session, _ uint16, p []byte) {
		if _, live := s.SceneRevision(); !live {
			// Scene still loading: hold the command for scene-ready so
			// its receipt answers inside the freshly built scene.
			rt.queueLoadingMove(s, true, p)
			return
		}
		character, division, bound := enterworld.SessionCharacter(rt.deps, s)
		if !bound {
			character = nil
		}
		rt.predictedMove(ledger, s.ID, division, character, p, s.Send)
	})
}

// Why a predicted move got no ordinary execution. Every drop is counted and
// logged: a command left without a receipt stalls the client's prediction
// until its receipt watchdog reconnects the session.
const (
	predictionDropMalformed = "malformed"
	predictionDropZeroID    = "zero-id"
	predictionDropUnbound   = "unbound"
	predictionDropStale     = "stale"
)

// predictionStaleReason is the refusal a stale or repeated id answers with.
const predictionStaleReason = "staleCommand"

// predictionSceneReplacedReason is the refusal a command held across a
// scene replacement answers with (loadingqueue.go): the scene it was
// clicked in is gone, so it must not execute into the new one. It is
// also the ledger key the not-executed count lands under; on the wire
// it degrades to movementRefused like any unlisted reason.
const predictionSceneReplacedReason = "sceneReplaced"

// predictionSceneClosedReason is the same answer when the session
// itself closed with commands still held.
const predictionSceneClosedReason = "sceneClosed"

/*
================
predictionLedger

Each session's id high-water mark, and the not-executed counts by
reason: the drops (malformed, zero-id, unbound, stale) and the
post-admission refusals under their normalized codes.
================
*/
type predictionLedger struct {
	mu    sync.Mutex
	last  map[uint64]uint32
	drops map[string]uint64
}

/*
================
newPredictionLedger
================
*/
func newPredictionLedger() *predictionLedger {
	return &predictionLedger{last: make(map[uint64]uint32), drops: make(map[string]uint64)}
}

/*
================
predictionLedger.forget
================
*/
func (ledger *predictionLedger) forget(session uint64) {
	ledger.mu.Lock()
	delete(ledger.last, session)
	ledger.mu.Unlock()
}

/*
================
predictionLedger.admit

Advances the session's high-water mark; false for an id at or below it.
The second return is the mark after the check: the admitted id, or the
mark that refused this one.
================
*/
func (ledger *predictionLedger) admit(session uint64, id uint32) (bool, uint32) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if id <= ledger.last[session] {
		return false, ledger.last[session]
	}
	ledger.last[session] = id
	return true, id
}

/*
================
predictionLedger.highWater

The session's current high-water mark (0 before its first admitted
command), for logging a drop admission never saw.
================
*/
func (ledger *predictionLedger) highWater(session uint64) uint32 {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return ledger.last[session]
}

/*
================
predictionLedger.countRefusal

Counts one post-admission refusal under its normalized code (see
predictionErrorCode), in the same table as the drops: how many commands
of this session the authority turned away, by why.
================
*/
func (ledger *predictionLedger) countRefusal(code string) uint64 {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	ledger.drops[code]++
	return ledger.drops[code]
}

/*
================
predictionFrameInfo

What a drop or refusal log line says about the frame it answers, beyond
the ledger's own numbers: whose command it was and what shape it took.
Mode is the native move mode byte (-1 when the frame carries none);
cosGid is the COS envelope's owner (0 for a player command).
================
*/
type predictionFrameInfo struct {
	gid      uint32
	envelope int
	cosGid   uint32
	mode     int
}

/*
================
predictionFrameInfoOf

Reads the log context out of one inbound frame. Length-guarded
throughout: malformed frames are exactly the ones worth logging, so
nothing here may trust the payload's shape. A COS envelope is decoded
best-effort for its owner and inner mode; a failed decode logs zeroes.
================
*/
func predictionFrameInfoOf(character *enterworld.Character, p []byte) predictionFrameInfo {
	info := predictionFrameInfo{envelope: -1, mode: -1}
	if character != nil {
		info.gid = enterworld.ObjectIDForCharacter(character)
	}
	if len(p) < 1 {
		return info
	}
	info.envelope = int(p[0])
	switch {
	case p[0] == 2 && len(p) > 5:
		if command, err := wire.DecodeCosCommand(p[5:]); err == nil {
			info.cosGid = command.CosGid
			if len(command.Movement) >= 1 {
				info.mode = int(command.Movement[0])
			}
		}
	case len(p) >= 6:
		info.mode = int(p[5])
	}
	return info
}

/*
================
predictionLedger.drop

Counts and logs one drop. The high-water mark rides along so a stale
line names the mark that refused the command; without it the log could
not be told apart from the client's own id sequence (BR-261015-0700-482F
was diagnosed by byte-length algebra for want of these fields).
================
*/
func (ledger *predictionLedger) drop(reason string, session uint64, id uint32, last uint32, info predictionFrameInfo) {
	ledger.mu.Lock()
	ledger.drops[reason]++
	count := ledger.drops[reason]
	ledger.mu.Unlock()
	log.WithFields(log.Fields{
		"reason": reason, "session": session, "gid": info.gid, "id": id, "last": last,
		"envelope": info.envelope, "cosGid": info.cosGid, "mode": info.mode, "count": count,
	}).Info("movement: predicted move not executed")
}

/*
================
predictedMove

One 0x7F extension command. send is the session's reliable enqueue; it
runs under the actor movement lock, so the terminal correction cannot
overtake the receipt. A stale or repeated id is answered with a refusal
receipt carrying the current authority instead of silence: the command was
already settled (its receipt may have died with a transport), and the
client resynchronizes from the authority rather than waiting for its
receipt watchdog to drop the session. Commands without an id or a bound
character cannot be answered; they are counted and logged.
================
*/
func (rt *Runtime) predictedMove(ledger *predictionLedger, session uint64, division string, character *enterworld.Character, p []byte, send func(uint16, []byte) error) {
	info := predictionFrameInfoOf(character, p)
	if len(p) < 5 || (p[0] != 1 && p[0] != 2) {
		ledger.drop(predictionDropMalformed, session, 0, ledger.highWater(session), info)
		return
	}
	id := binary.LittleEndian.Uint32(p[1:5])
	if id == 0 {
		ledger.drop(predictionDropZeroID, session, id, ledger.highWater(session), info)
		return
	}
	if character == nil {
		ledger.drop(predictionDropUnbound, session, id, ledger.highWater(session), info)
		return
	}
	admitted, last := ledger.admit(session, id)
	if !admitted {
		ledger.drop(predictionDropStale, session, id, last, info)
		unlock := rt.lockCharacter(division, character.Name)
		defer unlock()
		result := MoveOutcome{Refusal: &simulation.MoveError{Reason: predictionStaleReason}}
		rt.deps.Read(division, func() {
			result.Authority = rt.Worlds.Snapshot(simulation.WorldKey(division, character.Name),
				func() simulation.WorldState { return simulation.SeedWorldState(character) })
		})
		result.ServerTimeMs = rt.Now().UnixMilli()
		sendPredictionReceipt(send, id, character, result)
		return
	}
	if p[0] == 1 && rt.AdvanceResidentRegion != nil {
		rt.AdvanceResidentRegion(division, character.Name, rt.Now().UnixMilli())
	}
	unlock := rt.lockCharacter(division, character.Name)
	var result MoveOutcome
	retireEffects := false
	defer func() {
		unlock()
		if retireEffects {
			rt.RetireMoveEffects(division, character.Name, result.ServerTimeMs)
		}
	}()
	if p[0] == 2 {
		command, err := wire.DecodeCosCommand(p[5:])
		if err != nil || command.Tag != wire.CosCommandMovementTag || command.CosGid == 0 {
			result = rt.handleMoveLocked(division, character, []byte{99}, 0)
		} else {
			result = rt.handleMoveLocked(division, character, command.Movement, command.CosGid)
		}
	} else {
		result = rt.handleMoveLocked(division, character, p[5:], 0)
		// The accepted move's event retires the mover's
		// move-cancelled effects (the rider's own move only; a mount
		// move moves the vehicle).
		if result.Refusal == nil && result.Result != nil && rt.RetireMoveEffects != nil {
			retireEffects = true
		}
	}
	// An admitted command the authority still refused is the drop log's
	// blind spot (it used to be visible nowhere server-side). Count it
	// under its normalized code and log it with the raw reason kept for
	// diagnosis; only the code crosses the wire (see predictionErrorCode).
	if result.Refusal != nil {
		code := predictionError(result)
		log.WithFields(log.Fields{
			"reason": code, "rawReason": result.Refusal.Reason, "session": session, "gid": info.gid,
			"id": id, "last": id, "envelope": info.envelope, "cosGid": info.cosGid, "mode": info.mode,
			"count": ledger.countRefusal(code),
		}).Info("movement: predicted move refused")
	}
	for _, frame := range predictionFeedback(result) {
		if send(frame.Opcode, frame.Payload) != nil {
			return
		}
	}
	sendPredictionReceipt(send, id, character, result)
}

/*
================
sendPredictionReceipt

The ID-bearing receipt (OpPredictedMoveResult, JSON v1).
================
*/
func sendPredictionReceipt(send func(uint16, []byte) error, id uint32, character *enterworld.Character, result MoveOutcome) {
	body, err := json.Marshal(struct {
		Version      int         `json:"v"`
		ID           uint32      `json:"id"`
		GID          uint32      `json:"gid"`
		Accepted     bool        `json:"accepted"`
		Error        string      `json:"error,omitempty"`
		ServerTimeMs int64       `json:"serverTimeMs"`
		World        interface{} `json:"world"`
	}{1, id, enterworld.ObjectIDForCharacter(character), result.Refusal == nil, predictionError(result), result.ServerTimeMs, result.Authority})
	if err == nil {
		_ = send(transport.OpPredictedMoveResult, body)
	}
}

// Prediction has one movement authority: the ID-bearing receipt below. Sending
// B738 as well installs an untagged path before that receipt and can rewind a
// newer click. Native callers retain their ordinary B738 response unchanged.
/*
================
predictionFeedback
================
*/
func predictionFeedback(result MoveOutcome) []wire.Frame {
	frames := make([]wire.Frame, 0, len(result.Frames))
	for _, frame := range result.Frames {
		if frame.Opcode != simulation.OpMovementAck {
			frames = append(frames, frame)
		}
	}
	return frames
}

/*
================
predictionError
================
*/
func predictionError(outcome MoveOutcome) string {
	if outcome.Refusal != nil {
		return predictionErrorCode(outcome.Refusal.Reason)
	}
	return ""
}

/*
================
predictionErrorCode

The closed refusal vocabulary of the ID-bearing receipt. Refusal
reasons inside the authority are prose, formats with coordinates and
other free text (water.go's deep-water format names the region and the
destination); none of that may cross 0x000A, where the client archives
the code in bug reports and whitelists exactly this set. An unrecognized
non-empty reason degrades to movementRefused, never to the raw string.
================
*/
func predictionErrorCode(reason string) string {
	switch reason {
	case "":
		return ""
	case predictionStaleReason, "attackLocked", "abnormalState", "characterDead", "knockedDown",
		"teleportCasting", "deletePending", "invalidCosOwner", "areaAccessDenied", "thiefCargoInTown",
		"characterNotFound", "characterLifeChanged":
		// Already closed tokens (the admission and commit gates).
		return reason
	case "ground movement cannot cross disconnected navigation spaces",
		"ground destination sits on a disconnected navigation surface":
		// The second phrasing in these paired cases (and the three
		// "movement ..." sentences below) is the diagnostics spec's
		// wording, kept defensively: no producer in this tree emits it
		// today, but a peer converging on the spec must not fall
		// through to movementRefused.
		return "disconnectedNavigation"
	case "ground movement cannot cross the outdoor/dungeon authority boundary",
		"dungeon movement does not cross regions":
		return "dungeonBoundary"
	case "movement destination is blocked by world collision":
		return "endpointBlocked"
	case "movement region has no server navigation coverage":
		return "noCoverage"
	case "movement path crosses blocked world collision":
		return "pathTruncated"
	}
	switch {
	case strings.HasPrefix(reason, "deepWaterDestination"):
		return "deepWaterDestination"
	case strings.HasPrefix(reason, "pathguard endpointBlocked"):
		return "endpointBlocked"
	case strings.HasPrefix(reason, "pathguard noCoverage"):
		return "noCoverage"
	case strings.HasPrefix(reason, "pathguard truncated"):
		return "pathTruncated"
	case strings.HasPrefix(reason, "malformedMovementRequest"),
		strings.HasPrefix(reason, "malformedHeadingRequest"),
		// invalidHeadingRequest is the spec's spelling; this tree's
		// producer (simulation/moverequest.go) says malformedHeadingRequest.
		strings.HasPrefix(reason, "invalidHeadingRequest"),
		strings.HasPrefix(reason, "unsupportedNativeMoveOpcode"):
		return "malformedMovementRequest"
	case strings.HasPrefix(reason, "unsupportedMovementMode"):
		return "unsupportedMovementMode"
	}
	return "movementRefused"
}
