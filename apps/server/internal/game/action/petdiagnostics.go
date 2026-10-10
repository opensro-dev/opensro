/*
===========================================================================

petdiagnostics.go - why an attack pet did not fight, in the production log

An attack pet's order and its fight end silently by design (no native
refusal reply exists for them). When a player reports a pet that "goes to
the monster and does nothing", the log must name the gate. Each reason is
logged at Info at most once per petDiagnosticQuietMs per character, so a
pet that stalls every tick costs one line, not one per tick.

===========================================================================
*/

package action

import (
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

const (
	// petDiagnosticQuietMs is the per character and reason log interval.
	petDiagnosticQuietMs = 30000
	// petDiagnosticMaxKeys bounds the throttle table; it is cleared when full.
	petDiagnosticMaxKeys = 4096
	// petApproachStallMs is how long a pursuit may last without reaching
	// strike range before it is reported.
	petApproachStallMs = 3000
)

/*
================
petDiagnostics

The throttle of the pet fight log: the last time each character and
reason was reported.
================
*/
type petDiagnostics struct {
	mu   sync.Mutex
	last map[string]int64
}

/*
================
Runtime.petDiagnostic

Reports one reason a pet did not attack, throttled per character and
reason. fields add the numbers that tell the cases apart.
================
*/
func (rt *Runtime) petDiagnostic(division, owner string, pet uint32, reason string, fields log.Fields) {
	now := rt.Now().UnixMilli()
	key := division + "/" + strings.ToLower(owner) + "/" + reason
	rt.petDiag.mu.Lock()
	if last, seen := rt.petDiag.last[key]; seen && now-last < petDiagnosticQuietMs {
		rt.petDiag.mu.Unlock()
		return
	}
	if rt.petDiag.last == nil || len(rt.petDiag.last) >= petDiagnosticMaxKeys {
		rt.petDiag.last = make(map[string]int64)
	}
	rt.petDiag.last[key] = now
	rt.petDiag.mu.Unlock()
	entry := log.Fields{"division": division, "owner": owner, "pet": pet, "reason": reason}
	for name, value := range fields {
		entry[name] = value
	}
	log.WithFields(entry).Info("action: attack pet did not fight")
}
