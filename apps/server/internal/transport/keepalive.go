/*
===========================================================================

keepalive.go - a session's liveness: keepalive pings and idle detach

The keepalive loop pings an attached client that has gone quiet, detaches
one silent past the idle timeout (into the resume grace window), and pings
on a short cadence while a scene is loading, because a healthy world entry
sends nothing while the server builds it (#1417). Moved verbatim out of
session.go, which owns attachment, queues and IO.

===========================================================================
*/
package transport

import "time"

// sceneLoadingPingInterval is how often the keepalive proves the wire is
// alive while a scene admission is in flight. World entry can sit silent
// for seconds (publication gate, bootstrap build) inside the client's
// admission stall window, and the ping is the only frame that can cross in
// that window: the client's network owner answers it without waking the
// game handlers, and counts it as the liveness it is.
const sceneLoadingPingInterval = 2 * time.Second

/*
================
keepalivePingDue

Whether a tick sent since ping ago owes the next PING at cadence. Half a
tick of slack: the ticker keeps its own schedule, so a late tick followed
by an on-time one reads a little under the cadence. Without the slack
that tick is skipped and the 2 s loading cadence silently becomes 4 s
(the TestKeepalivePingsWhileSceneIsLoading CI flake, 2026-10-28).
================
*/
func keepalivePingDue(since, cadence, tick time.Duration) bool {
	return since >= cadence-tick/2
}

// keepaliveLoop pings an attached-but-quiet client and detaches one that
// has gone silent past the idle timeout, giving it the resume grace window.
// While a scene is loading the ping cadence shortens to
// sceneLoadingPingInterval: the entry is silent by design, so the ping is
// what separates a slow entry from a dead link at the client.
/*
================
keepaliveLoop
================
*/
func (s *Session) keepaliveLoop() {
	interval := s.hub.cfg.KeepaliveInterval
	tick := interval
	if tick > sceneLoadingPingInterval {
		tick = sceneLoadingPingInterval
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	var lastPing time.Time
	lastGen := -1
	for {
		select {
		case <-s.done:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				return
			}
			if !s.attached {
				s.mu.Unlock()
				continue
			}
			idle := now.Sub(s.lastRecv)
			gen := s.gen
			loading := s.sceneLoading
			s.mu.Unlock()
			if gen != lastGen {
				// A fresh attachment already proved the wire with its
				// WELCOME; the ping cadence restarts with it.
				lastGen = gen
				lastPing = time.Time{}
			}
			if idle > s.hub.cfg.IdleTimeout {
				s.detachGen(gen, errIdle)
				continue
			}
			cadence := interval
			if loading {
				cadence = sceneLoadingPingInterval
			} else if idle < interval {
				continue
			}
			if !keepalivePingDue(now.Sub(lastPing), cadence, tick) {
				continue
			}
			if err := s.Send(OpPing, nil); err == nil {
				lastPing = now
			}
		}
	}
}
