/*
===========================================================================

registry.go - lifetimes and clocks for linked hostile effects

This owner stores source/recipient pairs, never actor health or combat stats.
The action lane executes returned pulses through the existing damage authority.
Snapshots leave the mutex before callers acquire world or character locks.

===========================================================================
*/

package linkedpulse

import (
	"strings"
	"sync"
)

const (
	ErrDuplicate uint16 = 0x300c
	ErrCapacity  uint16 = 0x3029
	ErrInvalid   uint16 = 0x3003
	percentScale        = 100
)

/*
================
Effect

Tokens identify this lifetime even when an actor recasts the same skill. The
source owner is a logical session, so reconnecting cannot inherit old attacks.
================
*/
type Effect struct {
	StopRequested                    bool
	Division, SourceName             string
	SourceSession                    uint64
	SourceGID, TargetGID             uint32
	SourceToken, TargetToken         uint32
	SkillID, LinkGroup, MaxPerTarget uint32
	StartedMs, LastPulseMs           int64
	DurationMs, PeriodMs             uint32
}

/*
================
Step

A native update may pulse and then expire the same pair. Keeping both flags
preserves that order on late frames (5830B0: pulse precedes duration test).
================
*/
type Step struct {
	Effect        Effect
	Pulse, Expire bool
}

/*
================
Registry

Zero-value ready. Mutations are bounded operations over plain value snapshots;
no callback executes while this mutex is held.
================
*/
type Registry struct {
	mu      sync.Mutex
	effects []Effect
}

/*
================
Duration

5833EB..583450 divides the caster's unsigned DTDR value by 100 before
multiplying the base duration, then truncates the x87 result to an integer.
================
*/
func Duration(base, percent uint32) uint32 {
	extension := uint64(float64(percent) / percentScale * float64(base))
	return base + uint32(extension)
}

/*
================
refusal

59DC80's lks2 branch counts different skill IDs only against the same target.
Other targets never consume this link's capacity. Same-ID recasts are refused.
================
*/
func refusal(active []Effect, next Effect) uint16 {
	if next.Division == "" || next.SourceGID == 0 || next.TargetGID == 0 || next.SkillID == 0 ||
		next.LinkGroup == 0 || next.PeriodMs == 0 || next.DurationMs == 0 {
		return ErrInvalid
	}
	var count uint32
	for _, old := range active {
		if old.Division != next.Division || old.SourceGID != next.SourceGID ||
			old.LinkGroup != next.LinkGroup || old.TargetGID != next.TargetGID {
			continue
		}
		if old.SkillID == next.SkillID {
			return ErrDuplicate
		}
		count++
	}
	if next.MaxPerTarget != 0 && count >= next.MaxPerTarget {
		return ErrCapacity
	}
	return 0
}

/*
================
Refusal

Read-only admission for preparation. Install repeats it at the release edge.
================
*/
func (r *Registry) Refusal(next Effect) uint16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return refusal(r.effects, next)
}

/*
================
Install

The first pulse is one full period after activation, not the casting start.
================
*/
func (r *Registry) Install(next Effect) uint16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if code := refusal(r.effects, next); code != 0 {
		return code
	}
	if next.SourceToken == 0 || next.TargetToken == 0 || next.SourceToken == next.TargetToken {
		return ErrInvalid
	}
	for _, old := range r.effects {
		if old.Division == next.Division && (old.SourceToken == next.SourceToken || old.TargetToken == next.TargetToken) {
			return ErrInvalid
		}
	}
	next.LastPulseMs = next.StartedMs
	r.effects = append(r.effects, next)
	return 0
}

/*
================
Frame

5848BF uses elapsed >= puls and resets the clock to now. 5851E7 expires only
when elapsed > dura. Return every pair so the action owner can check actor
lifetimes even on frames where no damage pulse is due.
================
*/
func (r *Registry) Frame(nowMs int64) []Step {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Step, 0, len(r.effects))
	for i := range r.effects {
		effect := &r.effects[i]
		pulse := !effect.StopRequested && nowMs-effect.LastPulseMs >= int64(effect.PeriodMs)
		if pulse {
			effect.LastPulseMs = nowMs
		}
		out = append(out, Step{Effect: *effect, Pulse: pulse, Expire: effect.StopRequested || nowMs-effect.StartedMs > int64(effect.DurationMs)})
	}
	return out
}

/*
================
StopSource

Logout destroys the source lifetime immediately. Keep its tokens until the
next frame can publish retirement; a reconnect must never resume its pulses.
================
*/
func (r *Registry) StopSource(division, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.effects {
		if r.effects[i].Division == division && strings.EqualFold(r.effects[i].SourceName, name) {
			r.effects[i].StopRequested = true
		}
	}
}

/*
================
Active

Revalidate a frame snapshot after acquiring the division operation lock. Actor
teardown may have stopped the pair while that snapshot waited for the lock.
================
*/
func (r *Registry) Active(division string, token uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, effect := range r.effects {
		if effect.Division == division && effect.SourceToken == token {
			return !effect.StopRequested
		}
	}
	return false
}

/*
================
Remove

Retire by token, so a stale frame cannot erase a replacement lifetime.
================
*/
func (r *Registry) Remove(division string, token uint32) (Effect, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, effect := range r.effects {
		if effect.Division == division && effect.SourceToken == token {
			copy(r.effects[i:], r.effects[i+1:])
			r.effects[len(r.effects)-1] = Effect{}
			r.effects = r.effects[:len(r.effects)-1]
			return effect, true
		}
	}
	return Effect{}, false
}
