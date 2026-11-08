/*
===========================================================================

mostwantedstate.go - the holder's wanted state, sent for the buff board

Port-only, not native (#1849). With SRO_MOST_WANTED on, the server keeps
the streak holder's own client told of the state the buff-board row
renders: the wanted level, the counted kills into the next level, the
dressed job (whose icon the row wears), the bounty the level is worth
and how long remains until the next decay step. It rides one port-only
opcode to the holder only:

	0x3FBA: u8 level, u8 kills into the level, u8 dressed job,
	        u8 kills per level, u64 bounty gold,
	        u64 ms until the next decay step (0 = none stamped)

The countdown is a duration the server measures, never a clock time:
the client counts it down from receipt on its own frame clock, so a
skewed client clock cannot age the row early, and the row changes only
when a new state arrives. The server owns the decay (mostwanted.go).

The state is sent at world entry (the tick sweep's first pass for a
tracked holder), on every counted kill, on a dress or undress with a
live level, and cleared (level 0) when the streak ends. The same sweep
judges the decay for online holders; kills and deaths judge it in
their own doors (mostwanted.go). No display-only record is created:
the row renders this state, and the bonuses stay the stat
projection's (mostWantedWrites). Off (Most Wanted off), nothing here
runs and no frame is sent.

===========================================================================
*/
package action

import (
	"sort"
	"sync"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// mostWantedStateOpcode is the port-only S->C wanted state (above);
// only this port's browser client consumes it (release protocol 13).
const mostWantedStateOpcode uint16 = 0x3FBA

/*
================
Runtime.mostWantedStateFrame

The holder's current wanted state as one frame. A cleared or absent
streak reads level 0, which drops the client's row.
================
*/
func (rt *Runtime) mostWantedStateFrame(c *enterworld.Character, nowMs int64) wire.Frame {
	level := mostWantedLevel(c)
	step := max(rt.mostWantedStreakStep(), 1)
	perLevel := max(mostWantedKillsPerLevel/step, 1)
	into := int64(0)
	var decayInMs int64
	if c != nil && c.MostWanted != nil {
		if stamp := c.MostWanted.NextDecayAtMs; stamp > nowMs {
			decayInMs = stamp - nowMs
		}
		if level >= mostWantedMaxLevel {
			into = perLevel
		} else {
			into = (c.MostWanted.Streak % mostWantedKillsPerLevel) / step
		}
	}
	return wire.Frame{Opcode: mostWantedStateOpcode, Payload: wire.NewWriter(20).
		U8(uint8(level)).U8(uint8(into)).U8(enterworld.DressedJob(c)).U8(uint8(perLevel)).
		U64(rt.mostWantedBountyGold(level)).U64(uint64(decayInMs)).Payload()}
}

/*
================
mostWantedHolderIndex

The live streak holders the tick sweep owes work: an entry state frame
(sent false) and a decay step to watch. The stamp the index remembers
is the one the holder's record carried when last noted; the sweep
re-reads the record before it acts, so a stale note only costs a door
that finds nothing to do.
================
*/
type mostWantedHolderIndex struct {
	mu      sync.Mutex
	holders map[petOwnerKey]mostWantedHolder
}

type mostWantedHolder struct {
	nextDecayMs int64
	sent        bool
}

/*
================
trackEntry

Hand a holder to the sweep at world entry: its state frame is owed.
================
*/
func (index *mostWantedHolderIndex) trackEntry(key petOwnerKey) {
	index.mu.Lock()
	defer index.mu.Unlock()
	if index.holders == nil {
		index.holders = map[petOwnerKey]mostWantedHolder{}
	}
	// A (re-)entry always owes a state: the new session's client holds
	// none, whatever a previous session was sent.
	holder := index.holders[key]
	holder.sent = false
	index.holders[key] = holder
}

/*
================
note

Record that the holder's state was just sent with this decay stamp (a
counted kill): nothing is owed until the step approaches.
================
*/
func (index *mostWantedHolderIndex) note(key petOwnerKey, nextDecayMs int64) {
	index.mu.Lock()
	defer index.mu.Unlock()
	if index.holders == nil {
		index.holders = map[petOwnerKey]mostWantedHolder{}
	}
	index.holders[key] = mostWantedHolder{nextDecayMs: nextDecayMs, sent: true}
}

/*
================
forget

Drop a holder: its streak ended, decayed to none, or its character is gone.
================
*/
func (index *mostWantedHolderIndex) forget(key petOwnerKey) {
	index.mu.Lock()
	defer index.mu.Unlock()
	delete(index.holders, key)
}

/*
================
due

The holders owing the sweep a pass now: an unsent entry state, or a
decay step that has arrived. Sorted, as the pet-window sweep's keys are.
================
*/
func (index *mostWantedHolderIndex) due(nowMs int64) []petOwnerKey {
	index.mu.Lock()
	defer index.mu.Unlock()
	keys := make([]petOwnerKey, 0, len(index.holders))
	for key, holder := range index.holders {
		if !holder.sent || holder.nextDecayMs > 0 && nowMs >= holder.nextDecayMs {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].division != keys[j].division {
			return keys[i].division < keys[j].division
		}
		return keys[i].name < keys[j].name
	})
	return keys
}

/*
================
TrackMostWanted

World entry hands a streak holder to the tick sweep
(enterworld.Deps.TrackMostWanted): the sweep sends the entry state on
its first pass and watches the decay from there. A holder whose streak
ends forgets itself (clearMostWantedInDoor).
================
*/
func (rt *Runtime) TrackMostWanted(divisionID, characterName string) {
	if !rt.MostWanted {
		return
	}
	rt.mostWantedHolders.trackEntry(petOwnerKey{division: divisionID, name: characterName})
}

/*
================
advanceMostWanted

The tick sweep's Most Wanted pass: send each due holder its entry
state, and judge the decay of a streak whose step has arrived (its
stats refresh and new state ride the same private frames). Runs on
the action tick with the pet-window sweep (runtime_lifecycle.go).
================
*/
func (rt *Runtime) advanceMostWanted(nowMs int64) {
	if !rt.MostWanted {
		return
	}
	for _, key := range rt.mostWantedHolders.due(nowMs) {
		frames := rt.advanceMostWantedHolder(key, nowMs)
		if len(frames) > 0 && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(key.division, key.name, frames)
		}
	}
}

/*
================
advanceMostWantedHolder

One holder's sweep pass inside its division and character doors (the
pet-window sweep's order): a due decay drops through
decayMostWantedInDoor (its public refresh waits for the holder's next
kill or death to re-tell it, as the retired lapse clear's did); a live
streak is (re)sent its state and noted, so the pass costs a door only
when a state is owed or a decay step is due.
================
*/
func (rt *Runtime) advanceMostWantedHolder(key petOwnerKey, nowMs int64) []wire.Frame {
	unlock := rt.lockDivision(key.division)
	defer unlock()
	character := rt.findCharacter(key.division, key.name)
	if character == nil {
		rt.mostWantedHolders.forget(key)
		return nil
	}
	var frames []wire.Frame
	rt.deps.UpdateDeferred(character, "most-wanted-state", func() bool {
		if character.MostWanted == nil {
			rt.mostWantedHolders.forget(key)
			return false
		}
		if private, _ := rt.decayMostWantedInDoor(key.division, character, nowMs); private != nil {
			frames = private
			return true
		}
		stamped := false
		if character.MostWanted.NextDecayAtMs <= 0 {
			// A streak saved before the decay carries no stamp: this
			// first judged pass stamps a fresh day rather than dropping
			// a level (mostWantedDecayDrops), and the sweep watches the
			// streak from there.
			character.MostWanted.NextDecayAtMs = nowMs + mostWantedDecayMs
			stamped = true
		}
		frames = []wire.Frame{rt.mostWantedStateFrame(character, nowMs)}
		rt.mostWantedHolders.note(key, character.MostWanted.NextDecayAtMs)
		return stamped
	})
	return frames
}
