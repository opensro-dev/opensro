/*
===========================================================================
petcrowkit.go - port-only recipient-owned Guiding Feathers.

The division owns transient marks and charges. Only committed direct attacks
advance them; reflected hits, periodic effects and bonus procs never recurse.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

const crowChargeCap = 5
const crowPrecisionDuration = 10000

/*
================
crowKit / crowPrecision
================
*/
type crowKit struct {
	precision map[uint32]crowPrecision
	views     map[uint32][8]byte
}
type crowPrecision struct {
	book   uint8
	stacks uint8
	until  int64
}

/*
================
crowSources

Only the recipient's own crow and currently eligible party crows may grant bonuses.
================
*/
func (rt *Runtime) crowSources(division string, c *enterworld.Character, now int64) []petCombatStep {
	if c == nil || !rt.AttackPetExpansion {
		return nil
	}
	gids := rt.auraParty(division, c)
	ownerGID := enterworld.ObjectIDForCharacter(c)
	var out []petCombatStep
	add := func(owner *enterworld.Character) {
		step, ok := rt.elementalCompanion(division, owner, 0, "Crow", now)
		if ok && rt.crowSupportRecipients(step, true, false)[ownerGID] != nil {
			out = append(out, step)
		}
	}
	add(c)
	for gid := range gids {
		if gid != ownerGID {
			add(rt.findCharacterByGid(division, gid))
		}
	}
	return out
}

/*
================
crowSkillEmpowers

The native starter-skill catalog excludes normal attacks; only damaging skills
consume Precision. Empty reference keys cannot accidentally spend charges.
================
*/
func crowSkillEmpowers(c *enterworld.Character, skill enterworld.SkillRow) bool {
	return skill.Codename != "" && !isPinnedBaseAttack(c, skill.Codename) && skill.Attack.Present
}

/*
================
crowPlayerBonuses

Several crows cannot multiply the same effect; use each category's strongest grant.
================
*/
func (rt *Runtime) crowPlayerBonuses(division string, c *enterworld.Character, target uint32, skill enterworld.SkillRow, now int64) (float64, float64) {
	precision, empower := 0.0, 0.0
	for _, step := range rt.crowSources(division, c, now) {
		own := enterworld.ObjectIDForCharacter(step.snapshot) == enterworld.ObjectIDForCharacter(c)
		s, level, ok := companion.Passive(step.pet, "Guiding Feathers", companion.ModifiersFor(step.pet))
		row := step.state.crow.precision[enterworld.ObjectIDForCharacter(c)]
		if !ok || !own && s.Option < 3 || row.book != s.ID || row.until <= now {
			continue
		}
		precision = max(precision, float64(row.stacks)*s.ValueAt(level))
		if s.Option >= 2 && row.stacks == crowChargeCap && crowSkillEmpowers(c, skill) {
			empower = max(empower, 20+float64(level-1)*2)
		}
	}
	return empower, precision
}

/*
================
crowPlayerDamage
================
*/
func (rt *Runtime) crowPlayerDamage(division string, c *enterworld.Character, target monster.Instance, skill enterworld.SkillRow, hits []combat.Result, now int64) {
	bonus, _ := rt.crowPlayerBonuses(division, c, target.Gid, skill, now)
	for i := range hits {
		wolfBonusDamage(&hits[i], bonus)
	}
}

/*
================
crowPlayerAttack

One call after a successful action, not per impact or victim. Each supported
party member consumes only their own pool, and marks gain at most one charge.
================
*/
func (rt *Runtime) crowPlayerAttack(division string, c *enterworld.Character, skill enterworld.SkillRow, targets []uint32, now int64) {
	if len(targets) == 0 {
		return
	}
	for _, step := range rt.crowSources(division, c, now) {
		run := &step.state.crow
		s, _, ok := companion.Passive(step.pet, "Guiding Feathers", companion.ModifiersFor(step.pet))
		gid := enterworld.ObjectIDForCharacter(c)
		row := run.precision[gid]
		if ok && s.Option >= 2 && (gid == enterworld.ObjectIDForCharacter(step.snapshot) || s.Option == 3) && row.book == s.ID && row.until > now && row.stacks == crowChargeCap && crowSkillEmpowers(c, skill) {
			delete(run.precision, gid)
			rt.crowRecipientVisual(step, c, crowReadyVisual, uint8(s.Option))
		}

	}
}

/*
================
crowAfterAttack
================
*/
func (rt *Runtime) crowAfterAttack(step petCombatStep, targets []uint32) {
	if len(targets) == 0 {
		return
	}
	live, ok := rt.elementalCompanion(step.key.division, step.state.character, step.pet.GID, "Crow", step.nowMs)
	if !ok {
		return
	}
	run := &live.state.crow
	s, _, ok := companion.Passive(live.pet, "Guiding Feathers", companion.ModifiersFor(live.pet))
	if !ok {
		return
	}
	if run.precision == nil {
		run.precision = make(map[uint32]crowPrecision)
	}
	for gid, member := range rt.crowSupportRecipients(live, s.Option == 3, false) {
		row := run.precision[gid]
		if row.book != s.ID || row.until <= step.nowMs {
			row = crowPrecision{book: s.ID}
		}
		if row.stacks < crowChargeCap {
			kind := crowGuidanceVisual
			if row.stacks+1 == crowChargeCap {
				kind = crowReadyVisual
			}
			rt.crowRecipientVisual(live, member, kind, uint8(s.Option))
		}
		row.stacks, row.until = min(crowChargeCap, row.stacks+1), step.nowMs+crowPrecisionDuration
		run.precision[gid] = row
	}
}

/*
================
advanceCrowKit

Expire memory and withdraw departed recipients before another attack can read it.
================
*/
func (rt *Runtime) advanceCrowKit(key petOwnerKey, now int64) {
	state := rt.petSessionFor(key.division, key.name, key.gid)
	if state == nil {
		return
	}
	// With no precision or views to retire, a crow without Guiding
	// Feathers slotted does nothing below: refuse it before its owner is
	// copied (#1943). The refusal's reset of an empty kit is the same.
	q := elementalQuery{gid: key.gid, kind: "Crow"}
	if len(state.crow.precision) == 0 && len(state.crow.views) == 0 {
		q.passive = "Guiding Feathers"
	}
	step, ok := rt.elementalCompanionMatching(key.division, state.character, q, now)
	if !ok {
		views := state.crow.views
		state.crow = crowKit{}
		for gid := range views {
			rt.publishCrowPrecision(key.division, gid, now)
		}
		return
	}
	run := &state.crow
	s, _, learned := companion.Passive(step.pet, "Guiding Feathers", companion.ModifiersFor(step.pet))
	members := rt.crowSupportRecipients(step, s.Option == 3, false)
	for gid, row := range run.precision {
		if !learned || row.book != s.ID || row.until <= now || members[gid] == nil {
			delete(run.precision, gid)
		}
	}
	if run.views == nil {
		run.views = make(map[uint32][8]byte)
	}
	for gid := range run.precision {
		if _, found := run.views[gid]; !found {
			run.views[gid] = [8]byte{}
		}
	}
	for gid, previous := range run.views {
		view := rt.crowPrecisionView(key.division, gid, now)
		if view != previous {
			rt.publishCrowPrecision(key.division, gid, now)
			run.views[gid] = view
		}
		if run.precision[gid].stacks == 0 {
			delete(run.views, gid)
		}
	}
}

/*
================
crowPrecisionView / publishCrowPrecision

One private recipient counter, including party members without a crow of their own.
================
*/
func (rt *Runtime) crowPrecisionView(division string, gid uint32, now int64) [8]byte {
	c := rt.findCharacterByGid(division, gid)
	book, stacks, percent := uint8(0), uint8(0), 0.0
	for _, step := range rt.crowSources(division, c, now) {
		s, level, ok := companion.Passive(step.pet, "Guiding Feathers", companion.ModifiersFor(step.pet))
		row := step.state.crow.precision[gid]
		if !ok || row.book != s.ID || row.until <= now || s.Option < 3 && gid != enterworld.ObjectIDForCharacter(step.snapshot) {
			continue
		}
		value := float64(row.stacks) * s.ValueAt(level)
		if value > percent {
			book, stacks, percent = s.ID, row.stacks, value
		}
	}
	var out [8]byte
	copy(out[:], wire.NewWriter(8).U32(gid).U8(book).U8(stacks).U16(uint16(percent*10)).Payload())
	return out
}
func (rt *Runtime) publishCrowPrecision(division string, gid uint32, now int64) {
	c := rt.findCharacterByGid(division, gid)
	if c == nil {
		return
	}
	view := rt.crowPrecisionView(division, gid, now)
	rt.queueMonsterLegRecipients(division, []RecipientFrames{{CharacterID: c.ID, Frames: []wire.Frame{{Opcode: 0x3fb9, Payload: view[:]}}}})
}
