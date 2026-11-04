/*
===========================================================================

tick_prepare.go - one tick's preparation: the session order and division work

prepareTick takes the session snapshot, orders it and groups it into
division work. The order is kept between ticks because the population
changes only on attach and detach, so a steady tick places the sessions
without sorting (#2140).

===========================================================================
*/

package simulation

import (
	"sort"
)

/*
================
sessionOrderKey

The sort identity of one session snapshot: the (division, session,
character) triple prepareTick orders by. Session IDs are unique per
live session, so keys never collide within a tick.
================
*/
type sessionOrderKey struct {
	divisionID  string
	sessionID   string
	characterID int64
}

/*
================
sessionOrderKeyOf
================
*/
func sessionOrderKeyOf(s SessionSnapshot) sessionOrderKey {
	return sessionOrderKey{divisionID: s.DivisionID, sessionID: s.SessionID, characterID: s.CharacterID}
}

/*
================
orderSessions

Orders one tick's snapshots by (division, session, character). The
population changes only on attach and detach, so the kept order is
rebuilt only then: a tick carrying the same sessions is placed into
the kept order, and the sort runs on membership change alone.
================
*/
func (t *Ticker) orderSessions(sessions []SessionSnapshot) {
	if len(sessions) == len(t.orderKeys) && t.placeInKeptOrder(sessions) {
		return
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].DivisionID != sessions[j].DivisionID {
			return sessions[i].DivisionID < sessions[j].DivisionID
		}
		if sessions[i].SessionID != sessions[j].SessionID {
			return sessions[i].SessionID < sessions[j].SessionID
		}
		return sessions[i].CharacterID < sessions[j].CharacterID
	})
	t.orderKeys = t.orderKeys[:0]
	if t.orderIndex == nil {
		t.orderIndex = make(map[sessionOrderKey]int)
	} else {
		clear(t.orderIndex)
	}
	for i, s := range sessions {
		key := sessionOrderKeyOf(s)
		t.orderKeys = append(t.orderKeys, key)
		t.orderIndex[key] = i
	}
}

/*
================
placeInKeptOrder

Places the snapshots into the kept order when they are exactly the
kept sessions; reports false on any membership change. The scratch
is reused; the used marks are cleared on every exit.
================
*/
func (t *Ticker) placeInKeptOrder(sessions []SessionSnapshot) bool {
	if cap(t.orderScratch) < len(sessions) {
		t.orderScratch = make([]int, len(sessions))
		t.orderUsed = make([]bool, len(sessions))
		t.orderValues = make([]SessionSnapshot, len(sessions))
	} else {
		t.orderScratch = t.orderScratch[:len(sessions)]
		t.orderUsed = t.orderUsed[:len(sessions)]
		t.orderValues = t.orderValues[:len(sessions)]
	}
	placed := true
	for i, s := range sessions {
		pos, ok := t.orderIndex[sessionOrderKeyOf(s)]
		if !ok || t.orderUsed[pos] {
			placed = false
			break
		}
		t.orderUsed[pos] = true
		t.orderScratch[i] = pos
	}
	if placed {
		for i, s := range sessions {
			t.orderValues[t.orderScratch[i]] = s
		}
		copy(sessions, t.orderValues)
	}
	// Clear the marks, not the scratch slots: after a shrink the
	// slots past the break hold positions from the larger kept
	// order, outside this slice. Every mark set above is inside
	// it, so clearing the slice clears exactly them.
	clear(t.orderUsed)
	return placed
}

/*
================
prepareTick
================
*/
func (t *Ticker) prepareTick() (int64, []divisionTickWork) {
	tick := t.tickIndex
	t.tickIndex++

	sessions := t.Source.SnapshotSessions()

	// The resume registry drains once per tick, before any leg runs, so a
	// mark can never be served twice or leak into a later tick. A drained
	// identity with no snapshot here (the character re-entered under a new
	// scene revision, or has not entered yet) is dropped: that entry
	// bootstraps its own state.
	resumedIDs := t.resumedScratch
	if resumedIDs == nil {
		resumedIDs = make(map[string]bool)
		t.resumedScratch = resumedIDs
	} else {
		clear(resumedIDs)
	}
	if source, ok := t.Source.(ResumeSource); ok {
		for _, id := range source.DrainResumedSessions() {
			resumedIDs[id] = true
		}
	}
	t.orderSessions(sessions)

	if t.states == nil {
		t.states = make(map[string]*divisionTickState)
	}
	liveDivisions := t.liveScratch
	if liveDivisions == nil {
		liveDivisions = make(map[string]bool)
		t.liveScratch = liveDivisions
	} else {
		clear(liveDivisions)
	}
	work := t.workScratch[:0]
	for i := 0; i < len(sessions); {
		j := i + 1
		for j < len(sessions) && sessions[j].DivisionID == sessions[i].DivisionID {
			j++
		}
		divisionID := sessions[i].DivisionID
		liveDivisions[divisionID] = true
		state := t.states[divisionID]
		if state == nil {
			state = &divisionTickState{
				settled:    make(map[string]int64),
				shownPeers: make(map[string]map[uint32]bool),
				monsters:   cloneMonsterMoverOps(t.Monsters),
			}
			t.states[divisionID] = state
		}
		if state.monsters != nil {
			state.monsters.divisionID = divisionID
			state.monsters.watch = &t.watch
		}
		work = append(work, divisionTickWork{
			divisionID: divisionID,
			sessions:   sessions[i:j],
			state:      state,
			resumed:    resumedIDs,
		})
		i = j
	}
	// Actor lifetimes belong to allocated worlds. Removing the last session
	// must not delete the division's AI work before it reaches PENDING.
	if t.Monsters != nil && t.Monsters.Monsters != nil {
		for _, divisionID := range t.Monsters.Monsters.behaviorDivisions() {
			if liveDivisions[divisionID] {
				continue
			}
			liveDivisions[divisionID] = true
			state := t.states[divisionID]
			if state == nil {
				state = &divisionTickState{settled: make(map[string]int64), shownPeers: make(map[string]map[uint32]bool), monsters: cloneMonsterMoverOps(t.Monsters)}
				t.states[divisionID] = state
			}
			state.monsters.divisionID = divisionID
			state.monsters.watch = &t.watch
			work = append(work, divisionTickWork{divisionID: divisionID, state: state, resumed: resumedIDs})
		}
	}
	for divisionID := range t.states {
		if !liveDivisions[divisionID] {
			delete(t.states, divisionID)
		}
	}
	t.workScratch = work
	return tick, work
}
