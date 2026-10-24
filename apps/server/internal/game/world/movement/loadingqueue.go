/*
===========================================================================

loadingqueue.go - movement commands held while the client's scene loads

The native movement lanes send no failure frame for a refused move, so a
request inside the loading window used to lose its receipt and trip the
client's ten-second receipt watchdog (#1478). Held commands replay into
the freshly built scene at scene-ready, in arrival order, through the
same handler bodies a live dispatch takes.

===========================================================================
*/
package movement

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
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
	rt.loadingQueues[s.ID] = append(rt.loadingQueues[s.ID], loadingMove{
		session:   s,
		predicted: predicted,
		payload:   append([]byte(nil), payload...),
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
	for _, move := range queue {
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
forgetLoadingQueue

A closed session keeps nothing: its held commands die with it, like the
prediction ledger's per-session mark.
================
*/
func (rt *Runtime) forgetLoadingQueue(session uint64) {
	rt.loadingMu.Lock()
	defer rt.loadingMu.Unlock()
	delete(rt.loadingQueues, session)
}
