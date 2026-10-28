/*
===========================================================================

petdiagnostics.go - why an attack pet did not fight, in the production log

An attack pet's order and its fight end silently by design (no native
refusal reply exists for them). When a player reports a pet that "goes to
the monster and does nothing", the log must name the gate. Each reason is
logged at Info at most once per petDiagnosticQuietMs per character, so a
pet that stalls every tick costs one line, not one per tick.

A fight that ends because its target died or left the world is the normal
end of a fight, not a failure: it is only counted (PetCombatCounters, the
metrics' pet_combat block), so the log keeps the failures against a live
target. Production logged ~100 such kills in 25 minutes (2026-10-14).

===========================================================================
*/

package action

import (
	"strings"
	"sync"
	"sync/atomic"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
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
	// The fights and orders that ended on a dead or departed target.
	orderTargetDied, orderTargetGone atomic.Uint64
	fightTargetDied, fightTargetGone atomic.Uint64
}

// The ways a pet's target can be gone (petTargetEnded).
const (
	petTargetDied = "target died"
	petTargetGone = "target gone"
)

/*
================
Runtime.petTargetEnded

Why a pet's target no longer resolves, when that is the normal end of a
fight: the monster or player is dead (petTargetDied), or no monster, player
or companion holds the gid any more (petTargetGone). "" is a live target
the pet may not attack: a real failure to log.
================
*/
func (rt *Runtime) petTargetEnded(division string, owner *enterworld.Character, gid uint32) string {
	if target, ok := rt.characterMonster(division, owner, gid); ok {
		if target.CurrentHP == 0 {
			return petTargetDied
		}
		return ""
	}
	if player := rt.findCharacterByGid(division, gid); player != nil {
		if alive, _, _ := rt.characterLifeState(division, player); !alive {
			return petTargetDied
		}
		return ""
	}
	if rt.characterByCosGID(division, gid) != nil {
		return ""
	}
	return petTargetGone
}

/*
================
Runtime.countPetTargetEnded

Counts an order or a fight that ended on a dead or departed target.
================
*/
func (rt *Runtime) countPetTargetEnded(order bool, reason string) {
	switch {
	case order && reason == petTargetDied:
		rt.petDiag.orderTargetDied.Add(1)
	case order:
		rt.petDiag.orderTargetGone.Add(1)
	case reason == petTargetDied:
		rt.petDiag.fightTargetDied.Add(1)
	default:
		rt.petDiag.fightTargetGone.Add(1)
	}
}

/*
================
Runtime.PetCombatCounters

The metrics' pet_combat block: orders and fights that ended on a dead or
departed target since start, by reason.
================
*/
func (rt *Runtime) PetCombatCounters() map[string]uint64 {
	return map[string]uint64{
		"order_refused.target_died": rt.petDiag.orderTargetDied.Load(),
		"order_refused.target_gone": rt.petDiag.orderTargetGone.Load(),
		"fight_ended.target_died":   rt.petDiag.fightTargetDied.Load(),
		"fight_ended.target_gone":   rt.petDiag.fightTargetGone.Load(),
	}
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
