/*
===========================================================================

ratelimit.go - per-session inbound frame rate limiting at the dispatch boundary

One uniform token bucket per session. The comment after the imports is the
budget's provenance: the measured shape of legitimate inbound traffic.

===========================================================================
*/
package transport

import (
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Per-session inbound rate limiting at the dispatch boundary.
//
// Budget provenance — every number below comes from the measured shape of
// legitimate inbound traffic, because a limiter that clamps normal play is
// worse than no limiter at all:
//
//   - The keepalive machinery (PING/PONG/BYE/HELLO, opcodes 0x0001-0x0005)
//     is handled inside Session.readLoop and NEVER reaches Hub.dispatch, so
//     it is structurally outside the limiter — no exemption needed.
//   - 0x72CD target/action requests are user-driven. sub_693190 may add a
//     throttled bare cancel/recovery frame during manual movement, at most
//     once per 2s while its motion-state gate is active = <=0.5/s extra.
//   - 0x7738 movement: one frame per ground click
//     (goMissionBootstrapClient.sendMissionMove); the native client can
//     add an occasional EVENT-DRIVEN resend (CharMovement_ResendGoalMove
//     sub_878100 on an accepted tiny-retarget, latch cleared after one
//     resend in sub_878410) — never a per-frame repeat, so the fastest
//     sustained rate stays human click-spam at ~8-10/s plus stragglers.
//   - 0x72CF steer / 0x72F5 stop while a direction walk runs: the drift
//     correction of CNavigationDeadreckon_UpdateMovementHeading re-arms
//     every 500 ms (<=2/s), a held Left/Right arrow re-sends only past
//     45 degrees of turn at pi rad/s (<=4/s), and the stop is one frame per
//     Up release - about 6/s on top of the walk, which replaces clicking.
//   - 0x706D item moves, 0x707B guide acks, chat, and the progression
//     clicks (0x727A/0x7552/0x7165/0x72CB) are all user-driven at a few
//     per second at most; EnterWorld/0x3012 arrive once per bind.
//
// Worst plausible legitimate aggregate is therefore ~12-19 frames/s, and
// the default budget of 25/s sustained with a burst of 75 keeps headroom
// over the fastest producible human input while still shedding
// 97%+ of a hostile 1000/s empty-payload flood (the progression opcodes
// are empty frames, so flooding them is free for the attacker and burns
// handler CPU + refusal log volume per frame server-side).
//
// The bucket is deliberately UNIFORM across opcodes: any exempted opcode
// would immediately become the unlimited attack channel, and the inventory
// above shows nothing legitimate needs one.

// clampLogInterval throttles the per-session clamp warning so the clamp
// log cannot itself become the flood the limiter exists to stop.
const clampLogInterval = 5 * time.Second

/*
================
frameLimiter

frameLimiter is one session's inbound token bucket. It has its own tiny
mutex, held only for the token arithmetic — never across a handler call
and never nested with h.mu or s.mu.
================
*/
type frameLimiter struct {
	mu     sync.Mutex
	perSec float64
	burst  float64
	tokens float64
	last   time.Time

	// Clamp-log throttle state, under the same mutex.
	lastWarn     time.Time
	droppedSince uint64

	// now is the clock seam for deterministic tests.
	now func() time.Time
}

/*
================
newFrameLimiter

newFrameLimiter builds a full bucket from positive, validated inputs.
================
*/
func newFrameLimiter(perSec, burst int) *frameLimiter {
	return &frameLimiter{
		perSec: float64(perSec),
		burst:  float64(burst),
		tokens: float64(burst),
		now:    time.Now,
	}
}

/*
================
frameLimiter.admit

admit spends one token. It remains the test and small-frame convenience
path; live dispatch uses admitCost so large requests pay for their bytes.
================
*/
func (l *frameLimiter) admit() (ok bool, dropped uint64, warn bool) {
	return l.admitCost(1)
}

/*
================
frameLimiter.admitCost

admitCost spends cost tokens if available. On refusal it also answers
whether the caller should emit the rate-limited clamp warning and how many
frames were dropped since the last one, resetting that window.
================
*/
func (l *frameLimiter) admitCost(cost float64) (ok bool, dropped uint64, warn bool) {
	if cost < 1 {
		cost = 1
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if !l.last.IsZero() {
		l.tokens += now.Sub(l.last).Seconds() * l.perSec
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
	}
	l.last = now
	if l.tokens >= cost {
		l.tokens -= cost
		return true, 0, false
	}
	l.droppedSince++
	if l.lastWarn.IsZero() || now.Sub(l.lastWarn) >= clampLogInterval {
		l.lastWarn = now
		dropped = l.droppedSince
		l.droppedSince = 0
		return false, dropped, true
	}
	return false, 0, false
}

/*
================
Session.admitFrame

admitFrame is the dispatch-boundary gate: true means the frame may be
dispatched. A refusal drops the frame (never the session — a legitimate
client sharing a machine with a runaway script must survive), counts it
in the hub metrics, and warns at most once per clampLogInterval per
session. The limiter lives on the Session, so its state dies with the
session and cannot leak across churn. The nil case is defensive for
package-level tests and partially constructed sessions; production config
always installs a limiter.
================
*/
func (s *Session) admitFrame(opcode uint16, encodedBytes int) bool {
	if s.limiter == nil {
		return true
	}
	cost := float64((encodedBytes + 1023) / 1024)
	ok, dropped, warn := s.limiter.admitCost(cost)
	if ok {
		return true
	}
	s.hub.metrics.rlDropped.Add(1)
	if warn {
		log.WithFields(log.Fields{
			"session": s.ID,
			"opcode":  fmt.Sprintf("0x%04X", opcode),
			"dropped": dropped,
			"bytes":   encodedBytes,
			"cost":    cost,
			"budget":  fmt.Sprintf("%.0f/s burst %.0f", s.limiter.perSec, s.limiter.burst),
		}).Warn("transport: inbound rate limit clamped, dropping frames")
	}
	return false
}
