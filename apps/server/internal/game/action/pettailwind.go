/*
===========================================================================
pettailwind.go - port-only crow travel stacks and the full-stack stun.

The pet tick owns eligible recipients. Damage consumes each recipient's own
charges; speed readers use the strongest grant without opening authority doors.
===========================================================================
*/
package action

import (
	"sync"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const tailwindPeriod int64 = 5000
const tailwindOpcode uint16 = 0x3fb7 // Private: recipient u32, book u8, stacks u8, speed percent x10 u16.

type tailwindCharge struct {
	name        string
	book        uint8
	stacks      uint8
	percent     float64
	next, until int64
}
type tailwindStore struct {
	sync.Mutex
	sources map[petOwnerKey]map[uint32]tailwindCharge
}

/*
================
crowSupportRecipients

Party recipients must be alive, in the same world and within twenty metres.
================
*/
func (rt *Runtime) crowSupportRecipients(step petCombatStep, party, allowTown bool) map[uint32]*enterworld.Character {
	result := map[uint32]*enterworld.Character{enterworld.ObjectIDForCharacter(step.snapshot): step.state.character}
	if !party {
		return result
	}
	from := rt.liveSpawn(simulation.WorldKey(step.key.division, step.snapshot.Name), step.snapshot, step.nowMs)
	for gid := range rt.auraParty(step.key.division, step.snapshot) {
		member := rt.findCharacterByGid(step.key.division, gid)
		// A dead or deletion-pending member is refused on its live
		// facts; only an admitted member pays for the copy the runtime
		// checks below read (#1943).
		snapshot := rt.characterSnapshotAdmitted(step.key.division, member, func(live *enterworld.Character) bool {
			return !live.DeletePending && enterworld.CharacterAlive(live)
		})
		if snapshot == nil || !rt.petExpansionAllowed(snapshot, step.pet) || !allowTown && rt.inTown(step.key.division, snapshot) {
			continue
		}
		session := rt.petSessionFor(step.key.division, snapshot.Name, 0)
		if session == nil || !session.ready {
			continue
		}
		at := rt.liveSpawn(simulation.WorldKey(step.key.division, snapshot.Name), snapshot, step.nowMs)
		if samePlaneAdjacent(from, at) && areaDistance(from, at) <= 20*petAreaPerMeter {
			result[gid] = member
		}
	}
	return result
}

/*
================
advanceTailwind

No free first stack, catch-up beyond ten, or retained charges after book changes.
================
*/
func (rt *Runtime) advanceTailwind(key petOwnerKey, now int64) {
	state := rt.petSessionFor(key.division, key.name, key.gid)
	var step petCombatStep
	var skill companion.SkillDef
	var level uint8
	eligible := false
	if state != nil {
		step, eligible = rt.elementalCompanionEligibility(key.division, state.character, key.gid, "Crow", now, true)
		if eligible {
			skill, level, eligible = companion.Passive(step.pet, "Tailwind", companion.ModifiersFor(step.pet))
		}
	}
	if !eligible {
		rt.clearTailwindSource(key, now)
		return
	}
	members := rt.crowSupportRecipients(step, skill.Option >= 3, true)
	changed := make(map[uint32]bool)
	rt.tailwinds.Lock()
	if rt.tailwinds.sources == nil {
		rt.tailwinds.sources = make(map[petOwnerKey]map[uint32]tailwindCharge)
	}
	rows := rt.tailwinds.sources[key]
	if rows == nil {
		rows = make(map[uint32]tailwindCharge)
		rt.tailwinds.sources[key] = rows
	}
	for gid := range rows {
		if members[gid] == nil {
			delete(rows, gid)
			changed[gid] = true
		}
	}
	for gid, member := range members {
		row, found := rows[gid]
		if !found || row.book != skill.ID || row.name != member.Name {
			row = tailwindCharge{name: member.Name, book: skill.ID, next: now + tailwindPeriod}
			changed[gid] = true
		}
		if now >= row.next {
			before := row.stacks
			row.stacks = uint8(min(int64(10), int64(row.stacks)+1+(now-row.next)/tailwindPeriod))
			row.next = now + tailwindPeriod
			changed[gid] = changed[gid] || before != row.stacks
		}
		percent := skill.ValueAt(level)
		changed[gid] = changed[gid] || row.percent != percent
		row.percent, row.until = percent, now+2000
		rows[gid] = row
	}
	rt.tailwinds.Unlock()
	for gid, publish := range changed {
		if publish {
			rt.publishTailwind(key.division, gid, now)
		}
	}
}

/*
================
tailwindView / tailwindSpeedFactor

Several crows provide one strongest movement bonus, never multiplied grants.
================
*/
func (rt *Runtime) tailwindView(division string, gid uint32, now int64) tailwindCharge {
	var best tailwindCharge
	rt.tailwinds.Lock()
	defer rt.tailwinds.Unlock()
	for key, rows := range rt.tailwinds.sources {
		row := rows[gid]
		if key.division == division && row.until > now && (best.book == 0 || row.percent*float64(row.stacks) > best.percent*float64(best.stacks)) {
			best = row
		}
	}
	return best
}
func (rt *Runtime) tailwindSpeedFactor(division string, c *enterworld.Character, now int64) float32 {
	if !rt.AttackPetExpansion || c == nil || !enterworld.CharacterAlive(c) {
		return 1
	}
	row := rt.tailwindView(division, enterworld.ObjectIDForCharacter(c), now)
	return float32(1 + row.percent*float64(row.stacks)/100)
}

/*
================
publishTailwind / clearTailwindSource

Movement uses native observer publication. Stack details go only to the recipient.
================
*/
func (rt *Runtime) publishTailwind(division string, gid uint32, now int64) {
	c := rt.findCharacterByGid(division, gid)
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil {
		return
	}
	row := rt.tailwindView(division, gid, now)
	if row.stacks > 0 {
		at := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
		rt.queueCrowVisual(division, crowVisual{gid: gid, kind: crowTailwindVisual, book: row.book - 54, at: at})
	}
	f := wire.Frame{Opcode: tailwindOpcode, Payload: wire.NewWriter(8).U32(gid).U8(row.book).U8(row.stacks).U16(uint16(row.percent * float64(row.stacks) * 10)).Payload()}
	rt.queueMonsterLegRecipients(division, []RecipientFrames{{CharacterID: c.ID, Frames: []wire.Frame{f}}})
	if frames := rt.refreshMovementEffects(division, snapshot, now); len(frames) != 0 {
		rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: division, SourceGID: gid, Frames: simFrames(frames)}})
	}
}
func (rt *Runtime) clearTailwindSource(key petOwnerKey, now int64) {
	rt.tailwinds.Lock()
	rows := rt.tailwinds.sources[key]
	delete(rt.tailwinds.sources, key)
	rt.tailwinds.Unlock()
	for gid := range rows {
		rt.publishTailwind(key.division, gid, now)
	}
}

/*
================
tailwindDamage

Consume before status application: neither splash nor several crows can stun twice.
Only a living monster receives the stun; player combat still removes travel speed.
================
*/
func (rt *Runtime) tailwindDamage(division string, c *enterworld.Character, target uint32, now int64) []wire.Frame {
	if c == nil {
		return nil
	}
	gid := enterworld.ObjectIDForCharacter(c)
	var sources []struct {
		key  petOwnerKey
		book uint8
	}
	changed := false
	rt.tailwinds.Lock()
	for key, rows := range rt.tailwinds.sources {
		row, found := rows[gid]
		if key.division != division || !found {
			continue
		}
		if row.until > now && row.stacks == 10 && row.book >= 56 {
			sources = append(sources, struct {
				key  petOwnerKey
				book uint8
			}{key, row.book})
		}
		changed = changed || row.stacks != 0
		row.stacks, row.next = 0, now+tailwindPeriod
		rows[gid] = row
	}
	rt.tailwinds.Unlock()
	if changed {
		rt.publishTailwind(division, gid, now)
	}
	for _, source := range sources {
		key := source.key
		state := rt.petSessionFor(division, key.name, key.gid)
		if state == nil {
			continue
		}
		step, ok := rt.elementalCompanion(division, state.character, key.gid, "Crow", now)
		if !ok {
			continue
		}
		skill, _, ok := companion.Passive(step.pet, "Tailwind", companion.ModifiersFor(step.pet))
		if !ok || skill.ID != source.book || skill.Option < 2 || rt.crowSupportRecipients(step, skill.Option >= 3, false)[gid] == nil {
			continue
		}
		row := enterworld.SkillRow{}
		setPetStatus(&row, tagStun, [6]uint32{2000, 100, 0})
		frames := rt.applyPetCounterStun(step, row, target)
		if len(frames) > 0 {
			if victim, ok := rt.resolvePetCombatTarget(step, target); ok {
				rt.queueCrowVisual(division, crowVisual{gid: target, kind: crowStunVisual, book: uint8(skill.Option), at: victim.at})
			}
		}
		return frames
	}
	return nil
}

/*
================
tailwindCommittedDamage

Travel charges are spent by a landed impact, never a refused or blocked cast.
================
*/
func (rt *Runtime) tailwindCommittedDamage(division string, c *enterworld.Character, target uint32, impacts []simulation.MonsterDamageResult, now int64) []wire.Frame {
	for _, impact := range impacts {
		if impact.Damage > 0 {
			return rt.tailwindDamage(division, c, target, now)
		}
	}
	return nil
}
