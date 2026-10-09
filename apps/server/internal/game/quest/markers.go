/*
===========================================================================

markers.go - the quest NPC markers each viewer sees

MarkerStates derives every quest's marker from the transaction's own
acceptance and objective predicates; MarkersByNpc reduces them to the one
row per NPC the client can show; MarkerPublication sends each session only
the rows that changed (0x3498 add/replace, 0x30EA remove).

===========================================================================
*/

package quest

import (
	"encoding/binary"
	"fmt"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"slices"
	"sort"
)

// Marker states CICharactor_QuestMarkerStateEffect (856500) maps to the
// start, in-progress, report and red-scroll (SYSTEM_QUEST_MARK2) effects.
const (
	markerStateOffer      = 1
	markerStateInProgress = 2
	markerStateReport     = 3
	markerStateTooLow     = 4
)

// Formulae_ClassifyLevelDiff_Extended (40FE90) category 4: the quest is more
// than this many levels below the character, and its NPC shows no offer.
const markerTrivialLevelGap = 6

// SQuestInfo flag 0x40: the NPC target list follows the contents.
const questFlagTargets = 0x40

// MarkerStates derives presentation from the same acceptance and objective
// predicates as the quest transaction. Call under the character read door.
// A quest names one current NPC; native 787DB0 resolves competing rows by
// ascending quest key, not by a browser-side severity heuristic.
func (rt *Runtime) MarkerStates(c *enterworld.Character) map[uint32]NpcMarker {
	out := make(map[uint32]NpcMarker)
	if c == nil || c.DeletePending {
		return out
	}
	country := enterworld.NativeCountryByte9C(c)
	level := int64(1)
	if c.Level != nil {
		level = *c.Level
	}
	for _, def := range rt.Defs.All() {
		if def.StartNpcCodename == "" || (def.CountryByte != 3 && int(def.CountryByte) != country) {
			continue
		}
		at := activeQuestIndex(c, def.RefID)
		if at < 0 {
			// 925D20 calls condition slot +11C with arg4=1: the marker
			// checks the hour but bypasses the first-come quota (926B01).
			if canAcceptAgain(c, def) && prerequisitesMet(c, def) && rt.calendarAvailable(def, true) {
				if state, shown := offerMarkerState(level, int64(def.Level)); shown {
					out[def.RefID] = NpcMarker{Codename: def.StartNpcCodename, State: state}
				}
			}
			continue
		}
		record := c.ActiveQuests[at]
		current := def
		if len(def.Stages) > 0 {
			var ok bool
			current, ok = definitionAtStage(def, record.Stage)
			if !ok {
				continue
			}
		}
		if target := questTarget(c, current, record); target.Codename != "" {
			out[def.RefID] = NpcMarker{Codename: target.Codename, State: target.State}
		}
	}
	return out
}

/*
================
offerMarkerState

925D20's level rule for a quest the character has not taken:
QuestGameServer_ClassifyLevelDiff (57BE00 -> 40FE90) of the quest level
(+0x23) against the character's hides a quest more than six levels below
it; a quest above the character's level shows the red scroll, any other the
offer mark.
================
*/
func offerMarkerState(level, questLevel int64) (uint8, bool) {
	if questLevel-level < -markerTrivialLevelGap {
		return 0, false
	}
	if level < questLevel {
		return markerStateTooLow, true
	}
	return markerStateOffer, true
}

/*
================
questTarget

The NPC an accepted quest currently sends the player to: the stage's end
NPC (in progress, or ready to report once the objective is met), or the
delivery NPC while items remain to hand over. The overhead marker and the
journal target list both name this one NPC.
================
*/
type journalTarget struct {
	NpcMarker
	ref uint32
}

func questTarget(c *enterworld.Character, current *Definition, record enterworld.ActiveQuestRecord) journalTarget {
	target := journalTarget{NpcMarker{Codename: current.EndNpcCodename, State: markerStateInProgress}, current.endNpcRef}
	if stageObjectiveMet(c, current, record) {
		target.State = markerStateReport
	}
	if current.DeliveryNpcCodename != "" && heldCollectCount(c, current) < current.CollectCount {
		target = journalTarget{NpcMarker{Codename: current.DeliveryNpcCodename, State: markerStateReport}, current.deliveryNpcRef}
	}
	// A two-leg delivery sends the player to its hand-over NPC first.
	if current.HandOverNpcCodename != "" && !handedOver(record) {
		state := uint8(markerStateInProgress)
		if deliveryMet(c, current) {
			state = markerStateReport
		}
		target = journalTarget{NpcMarker{Codename: current.HandOverNpcCodename, State: state}, current.handOverNpcRef}
	}
	return target
}

/*
================
withJournalTargets

SQuestInfo flag 0x40 carries the NPC RefObjIDs CIFWorldMap_DrawQuestNpcMarkers
(57B1C0) and the minimap resolve through npcpos.txt for the selected quest.
SQuestInfo_Deserialize (788210) clears that list on every update without
the timer flag 4 and then appends, so the record keeps the full current
list and every full update resends it; flag-4 timer deltas carry none.
Reports whether the list changed, so the caller publishes the move. With
no resolved NPC the record keeps whatever envelope it carries.
================
*/
func withJournalTargets(c *enterworld.Character, current *Definition, record enterworld.ActiveQuestRecord) (enterworld.ActiveQuestRecord, bool) {
	ref := questTarget(c, current, record).ref
	if ref == 0 {
		// No resolved placement (an unwired roster): keep the envelope.
		return record, false
	}
	targets := []uint32{ref}
	flags := record.Flags | questFlagTargets
	if flags == record.Flags && slices.Equal(targets, record.TargetIds) {
		return record, false
	}
	record.Flags, record.TargetIds = flags, targets
	return record, true
}

/*
================
ResolveJournalNpcs

Resolve the end and delivery NPCs of every quest and stage to the RefObjIDs
npcpos.txt is keyed by, from the world NPC roster. The gameworld calls it
once at startup, before any session reads a definition; until then no
target list is published.
================
*/
func (d *Definitions) ResolveJournalNpcs(refID func(codename string) (uint32, bool)) error {
	resolve := func(def *Definition, code string) (uint32, error) {
		if code == "" {
			return 0, nil
		}
		ref, ok := refID(code)
		if !ok || ref == 0 {
			return 0, fmt.Errorf("quest %s names unplaced NPC %s", def.Codename, code)
		}
		return ref, nil
	}
	var err error
	for _, def := range d.ordered {
		if def.endNpcRef, err = resolve(def, def.EndNpcCodename); err != nil {
			return err
		}
		if def.deliveryNpcRef, err = resolve(def, def.DeliveryNpcCodename); err != nil {
			return err
		}
		if def.handOverNpcRef, err = resolve(def, def.HandOverNpcCodename); err != nil {
			return err
		}
		for i := range def.Stages {
			s := &def.Stages[i]
			if s.endNpcRef, err = resolve(def, s.EndNpcCodename); err != nil {
				return err
			}
			if s.deliveryNpcRef, err = resolve(def, s.DeliveryNpcCodename); err != nil {
				return err
			}
		}
	}
	return nil
}

/*
================
MarkersByNpc

The client keeps one marker per NPC: CQuestMarkerRegistry_RebuildNpcIndex
(787DB0) walks the rows in ascending key order and indexes the first one per
NPC GID. Publishing a row per quest therefore let a lower quest the NPC
offers hide the quest the player came back to report. Neither binary shows
the retail server's aggregation, so this is an inference: the server sends
one row per NPC, ranked report (3) over offer (1) over in progress (2), the
lowest quest key breaking a tie.
================
*/
func MarkersByNpc(states map[uint32]NpcMarker) map[uint32]NpcMarker {
	best := make(map[string]uint32, len(states))
	for id, m := range states {
		if at, ok := best[m.Codename]; !ok || markerOutranks(id, m, at, states[at]) {
			best[m.Codename] = id
		}
	}
	out := make(map[uint32]NpcMarker, len(best))
	for _, id := range best {
		out[id] = states[id]
	}
	return out
}

/*
================
markerOutranks
================
*/
func markerOutranks(id uint32, m NpcMarker, otherID uint32, other NpcMarker) bool {
	if rank, otherRank := markerRank(m.State), markerRank(other.State); rank != otherRank {
		return rank < otherRank
	}
	return id < otherID
}

/*
================
markerRank

Lower ranks win: report, offer, in progress, then anything else.
================
*/
func markerRank(state uint8) int {
	switch state {
	case markerStateReport:
		return 0
	case markerStateOffer:
		return 1
	case markerStateInProgress:
		return 2
	}
	return 3
}

type NpcMarker struct {
	Codename string
	State    uint8
}

// MarkerPublication is private to one admitted transport session. Only changed
// records cross the wire; reconnect creates a new publication, including when
// the preceding connection never received its final delta.
type MarkerPublication struct{ rows map[uint32][18]byte }

func (p *MarkerPublication) Update(rows map[uint32][18]byte) []wire.Frame {
	ids := make([]uint32, 0, len(rows)+len(p.rows))
	seen := make(map[uint32]bool)
	for id := range rows {
		ids = append(ids, id)
		seen[id] = true
	}
	for id := range p.rows {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var out []wire.Frame
	for _, id := range ids {
		next, present := rows[id]
		previous, existed := p.rows[id]
		if !present {
			payload := make([]byte, 4)
			binary.LittleEndian.PutUint32(payload, id)
			out = append(out, wire.Frame{Opcode: 0x30ea, Payload: payload})
		} else if !existed || next != previous {
			out = append(out, wire.Frame{Opcode: 0x3498, Payload: append([]byte(nil), next[:]...)})
		}
	}
	p.rows = rows
	return out
}

// Native 75C0F0: key, flags, effect-state, region, three i16 coordinates,
// optional NPC object gid when flags&2. This is not the journal tracked ID.
func EncodeNpcMarker(id, gid uint32, state uint8, region uint16, x, y, z int16) [18]byte {
	var p [18]byte
	binary.LittleEndian.PutUint32(p[:], id)
	p[4] = 2
	p[5] = state
	binary.LittleEndian.PutUint16(p[6:], region)
	binary.LittleEndian.PutUint16(p[8:], uint16(x))
	binary.LittleEndian.PutUint16(p[10:], uint16(y))
	binary.LittleEndian.PutUint16(p[12:], uint16(z))
	binary.LittleEndian.PutUint32(p[14:], gid)
	return p
}
