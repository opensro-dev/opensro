/*
===========================================================================

loadingqueue.go - movement commands held while the client's scene loads

The native movement lanes send no failure frame for a refused move, so a
request inside the loading window used to lose its receipt and trip the
client's ten-second receipt watchdog (#1478). Held commands replay into
the freshly built scene at scene-ready, in arrival order, through the
same handler bodies a live dispatch takes. A command whose scene was
replaced before it ever went live is answered with a refusal receipt
instead of executing into the new scene, and a session that closes
with commands still held counts them rather than forgetting them
silently (#1978): every held command is answered or accounted for.

===========================================================================
*/
package movement

import (
	"encoding/binary"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

// loadingQueueCap matches the client's own pending cap (movement.ts): a
// client with more commands than that outstanding has already given up
// on the oldest of them as its own pending map would overflow first.
const loadingQueueCap = 32

/*
================
loadingMove

One held command: the session, which lane it came on, and its body.
================
*/
type loadingMove struct {
	session   *transport.Session
	predicted bool
	payload   []byte
	// revision is the scene the command was held under; a drain that
	// finds a different revision answers instead of executing.
	revision uint64
}

/*
================
queueLoadingMove

Holds one command for scene-ready. Returns false when the session's
queue is past the client's own pending cap: that command is dropped and
counted, like the prediction ledger's other drops.
================
*/
func (rt *Runtime) queueLoadingMove(s *transport.Session, predicted bool, payload []byte) bool {
	rt.loadingMu.Lock()
	defer rt.loadingMu.Unlock()
	if rt.loadingQueues == nil {
		rt.loadingQueues = make(map[uint64][]loadingMove)
	}
	if len(rt.loadingQueues[s.ID]) >= loadingQueueCap {
		log.WithFields(log.Fields{
			"session": s.ID, "queued": len(rt.loadingQueues[s.ID]),
		}).Info("movement: loading queue past its cap; command dropped")
		return false
	}
	revision, _ := s.SceneRevision()
	rt.loadingQueues[s.ID] = append(rt.loadingQueues[s.ID], loadingMove{
		session:   s,
		predicted: predicted,
		payload:   append([]byte(nil), payload...),
		revision:  revision,
	})
	return true
}

/*
================
drainLoadingQueue

Replays one session's held commands into the now-live scene, oldest
first, through the same handler bodies a live dispatch would take, so
the receipts and refusals each command settles are unchanged.
================
*/
func (rt *Runtime) drainLoadingQueue(s *transport.Session) {
	rt.loadingMu.Lock()
	queue := rt.loadingQueues[s.ID]
	delete(rt.loadingQueues, s.ID)
	rt.loadingMu.Unlock()
	current, _ := s.SceneRevision()
	for _, move := range queue {
		if move.revision != current {
			// The scene this command was clicked in was replaced
			// before it went live: answer it, never execute it
			// into the new scene (#1978).
			if move.predicted {
				rt.refuseHeldPredicted(s, move.payload, predictionSceneReplacedReason, s.Send)
			} else {
				log.WithFields(log.Fields{"session": s.ID}).
					Info("movement: held native move dropped; its scene was replaced")
			}
			continue
		}
		if move.predicted {
			character, division, bound := enterworld.SessionCharacter(rt.deps, s)
			if !bound {
				character = nil
			}
			if rt.predictLedger != nil {
				rt.predictedMove(rt.predictLedger, s.ID, division, character, move.payload, s.Send)
			}
			continue
		}
		rt.handleNativeMove(s, move.payload)
	}
}

/*
================
answerHeldOnClose

A session that closes with commands still held answers what it can
and counts the rest (#1978): each held predicted command is counted
under sceneClosed and, while the session can still send and a
character is still bound, refused with a receipt carrying the current
authority. The receipt usually cannot leave a closing session - the
count and its log line are the guarantee that the command was not
silently forgotten. Held native commands have no receipt to answer
with; they are logged.
================
*/
func (rt *Runtime) answerHeldOnClose(s *transport.Session, send func(uint16, []byte) error) {
	rt.loadingMu.Lock()
	queue := rt.loadingQueues[s.ID]
	delete(rt.loadingQueues, s.ID)
	rt.loadingMu.Unlock()
	for _, move := range queue {
		if move.predicted {
			rt.refuseHeldPredicted(s, move.payload, predictionSceneClosedReason, send)
			continue
		}
		log.WithFields(log.Fields{"session": s.ID}).
			Info("movement: held native move dropped; session closed")
	}
}

/*
================
refuseHeldPredicted

Answers one held predicted command with a refusal receipt instead of
executing it, mirroring predictedMove's stale answer: the ledger
counts it under reason (so the command is never silently forgotten),
and a bound character's receipt carries the current authority so the
client resynchronizes. Malformed, zero-id and unbound commands cannot
be answered; they are counted exactly as a live arrival's would be.
================
*/
func (rt *Runtime) refuseHeldPredicted(s *transport.Session, payload []byte, reason string, send func(uint16, []byte) error) {
	character, division, bound := enterworld.SessionCharacter(rt.deps, s)
	if !bound {
		character = nil
	}
	ledger := rt.predictLedger
	info := predictionFrameInfoOf(character, payload)
	if len(payload) < 5 || (payload[0] != 1 && payload[0] != 2) {
		if ledger != nil {
			ledger.drop(predictionDropMalformed, s.ID, 0, ledger.highWater(s.ID), info)
		}
		return
	}
	id := binary.LittleEndian.Uint32(payload[1:5])
	if id == 0 {
		if ledger != nil {
			ledger.drop(predictionDropZeroID, s.ID, id, ledger.highWater(s.ID), info)
		}
		return
	}
	if ledger != nil {
		ledger.drop(reason, s.ID, id, ledger.highWater(s.ID), info)
	}
	if character == nil {
		return
	}
	unlock := rt.lockCharacter(division, character.Name)
	defer unlock()
	result := MoveOutcome{Refusal: &simulation.MoveError{Reason: reason}}
	rt.deps.Read(division, func() {
		result.Authority = rt.Worlds.Snapshot(simulation.WorldKey(division, character.Name),
			func() simulation.WorldState { return simulation.SeedWorldState(character) })
	})
	result.ServerTimeMs = rt.Now().UnixMilli()
	sendPredictionReceipt(send, id, character, result)
}
