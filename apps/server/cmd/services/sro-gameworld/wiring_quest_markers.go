/*
===========================================================================

wiring_quest_markers.go - the quest marker publication lane

One tick hook publishes each viewer's quest marker rows. The rows a
viewer is shown are a pure function of the character record, the
static quest definitions and the quest calendar's hour (the marker
derivation reads nothing else; period-start quotas are bypassed for
markers), so the hook derives them once per (record, write epoch,
hour) and re-derives on the first move of any of the three - the
per-tick full definition scan the profile charged to the store's
read door is gone (#3099). Transport and world rosters remain
root-owned.

===========================================================================
*/

package main

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
questMarkerRows

One session's marker rows as last derived, with the key that proved
them: the record itself, its store write epoch, and the quest
calendar's hour. MarkerStates reads only the record, the static
definitions (period quotas bypassed for markers) and that hour, so
an unchanged key proves a fresh derivation returns the same rows.
================
*/
type questMarkerRows struct {
	character *enterworld.Character
	epoch     uint64
	hour      int
	rows      map[uint32][18]byte
}

/*
================
freshFor

Whether rows derived under this entry's key still answer for the
record now. The epoch must be wired: with no write epoch read, no
key proves the record stood still and every tick re-derives (the
pre-cache behaviour of detached compositions).
================
*/
func (entry *questMarkerRows) freshFor(character *enterworld.Character, epoch uint64, wired bool, hour int) bool {
	if entry == nil || entry.character != character || !wired {
		return false
	}
	return entry.epoch == epoch && entry.hour == hour
}

/*
================
questMarkerDerivations

The hook's derived rows, one per session. Owned by the tick hook's
closure; sessions that leave drop out with their publications.
================
*/
type questMarkerDerivations struct {
	bySession map[uint64]*questMarkerRows
}

/*
================
rows

The session's current marker rows: the cached derivation while its
key stands, else a fresh derive kept under the key. derive performs
the read-door derivation; it runs only on a key move.
================
*/
func (d *questMarkerDerivations) rows(sessionID uint64, character *enterworld.Character, epoch uint64, wired bool, hour int, derive func() map[uint32][18]byte) map[uint32][18]byte {
	if entry := d.bySession[sessionID]; entry.freshFor(character, epoch, wired, hour) {
		return entry.rows
	}
	rows := derive()
	if d.bySession == nil {
		d.bySession = make(map[uint64]*questMarkerRows)
	}
	d.bySession[sessionID] = &questMarkerRows{character: character, epoch: epoch, hour: hour, rows: rows}
	return rows
}

/*
================
forget

Drops every derivation whose session is no longer live.
================
*/
func (d *questMarkerDerivations) forget(live map[uint64]bool) {
	for id := range d.bySession {
		if !live[id] {
			delete(d.bySession, id)
		}
	}
}

/*
================
questMarkerHour

The hour the quest calendar's marker availability reads; -1 when no
calendar is wired, the state in which availability is a constant and
the record's epoch alone keys the rows.
================
*/
func questMarkerHour(quests *quest.Runtime) int {
	if quests == nil || quests.CalendarNow == nil {
		return -1
	}
	return int(quests.CalendarNow().Hour)
}

/*
================
questMarkerTick

The quest-markers tick hook: publishes each live viewer's marker
rows, deriving them on a derivation-key move only (see the file
banner). A world entry (initial entry, teleport, return scroll)
bumps the session's scene revision, and the entry payload
re-establishes full quest state on the client, so the publication
resets and the next tick re-emits the full row set - exactly what a
reconnect gets. Without this, a teleport keeps the session, the rows
are unchanged, and no frame ever refills the client's wiped marker
array.
================
*/
func (game *gameplayPlane) questMarkerTick() simulation.TickHook {
	publications := make(map[uint64]*quest.MarkerPublication)
	// entered is each session's last seen scene revision.
	entered := make(map[uint64]uint64)
	derived := &questMarkerDerivations{}
	anchors := make(map[string]simulation.NpcDef)
	for _, npc := range game.items.NpcRoster {
		if npc.AuthoredSpawn {
			if _, exists := anchors[npc.Codename]; !exists {
				anchors[npc.Codename] = npc
			}
		}
	}
	return func(nowMs int64) []simulation.DivisionFrames {
		if game.quests == nil {
			return nil
		}
		live := make(map[uint64]bool)
		for _, session := range game.hub.Sessions() {
			if session.Evicted() {
				continue
			}
			if _, ok := session.WorldSnapshot(); !ok {
				continue
			}
			c, division, ok := enterworld.SessionCharacter(game.deps, session)
			if !ok {
				continue
			}
			live[session.ID] = true
			epoch, wired := game.deps.WriteEpoch(c)
			rows := derived.rows(session.ID, c, epoch, wired, questMarkerHour(game.quests), func() map[uint32][18]byte {
				var states map[uint32]quest.NpcMarker
				game.deps.Read(division, func() { states = quest.MarkersByNpc(game.quests.MarkerStates(c)) })
				encoded := make(map[uint32][18]byte, len(states))
				for id, state := range states {
					npc, ok := anchors[state.Codename]
					if !ok {
						continue
					}
					s := npc.Spawn
					encoded[id] = quest.EncodeNpcMarker(id, npc.ObjectID, state.State, s.RegionID, int16(s.X), int16(s.Y), int16(s.Z))
				}
				return encoded
			})
			publication := publications[session.ID]
			if publication == nil {
				publication = &quest.MarkerPublication{}
				publications[session.ID] = publication
			}
			if revision, active := session.SceneRevision(); active && entered[session.ID] != revision {
				entered[session.ID] = revision
				publication.Reset()
			}
			for _, frame := range publication.Update(rows) {
				if err := session.Send(frame.Opcode, frame.Payload); err != nil {
					delete(publications, session.ID)
					log.Debugf("quest marker publication failed for session %d: %v", session.ID, err)
					break
				}
			}
		}
		for id := range publications {
			if !live[id] {
				delete(publications, id)
				delete(entered, id)
			}
		}
		derived.forget(live)
		return nil
	}
}

/*
================
validateQuestMarkerRoster

Every marker NPC a definition names must have exactly one authored
placement, or the marker's anchor is ambiguous; the journal target
list is resolved against the same placements (SQuestInfo 0x40).
================
*/
func (game *gameplayPlane) validateQuestMarkerRoster() error {
	counts := map[string]int{}
	refs := map[string]uint32{}
	for _, npc := range game.items.NpcRoster {
		if npc.AuthoredSpawn {
			counts[npc.Codename]++
			refs[npc.Codename] = npc.RefObjID
		}
	}
	for _, def := range game.quests.Defs.All() {
		specs := []quest.QuestSpec{def.QuestSpec}
		for _, stage := range def.Stages {
			specs = append(specs, stage.QuestSpec)
		}
		for _, spec := range specs {
			for _, code := range []string{spec.StartNpcCodename, spec.EndNpcCodename, spec.DeliveryNpcCodename} {
				if code != "" && counts[code] != 1 {
					return fmt.Errorf("quest %s marker NPC %s has %d authored placements; resolve its identity before publication", def.Codename, code, counts[code])
				}
			}
		}
	}
	// The journal target list names these same placements (SQuestInfo 0x40).
	return game.quests.Defs.ResolveJournalNpcs(func(code string) (uint32, bool) {
		ref, ok := refs[code]
		return ref, ok
	})
}
