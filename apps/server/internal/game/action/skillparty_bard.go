/*
===========================================================================

skillparty_bard.go - Bard instrument and dance coexistence policy

The shared aura lifecycle owns installation and retirement. This file owns
the Bard-specific decisions about which source instances may coexist.

===========================================================================
*/

package action

import "opensro.online/server/internal/game/enterworld"

/*
==================
replaceOwnAura

Owner's rule 1: a Bard plays one instrument aura (Guard Tambour, Mana
Tambour, Hit March, Clout March) at a time; casting another replaces the
one already playing. Inferred: a dance replaces the Bard's previous dance
the same way, since a Bard dances one Dancing at a time. Moving and Swing
March are timed buffs of no family and are never touched here.

The old caster instance is asked to stop, exactly as a client cancel does;
the next update finds it stopped and retires its children (retireAuraStep).
Only the caster's own instances are read: a child the Bard holds from
another Bard's aura has an AuraParentToken. The caller holds c's door.
==================
*/
func (rt *Runtime) replaceOwnAura(division string, c *enterworld.Character, skill enterworld.SkillRow, token uint32) {
	family := skill.AuraFamily()
	if family == enterworld.AuraFamilyNone {
		return
	}
	for _, effect := range rt.effects.Snapshot(division, c.Name) {
		if effect.InstanceToken == token || effect.AuraParentToken != 0 || effect.StopRequested {
			continue
		}
		row, ok := rt.deps.SkillData().SkillByID(effect.SkillID)
		if !ok || row.AuraFamily() != family {
			continue
		}
		rt.effects.RequestVoluntaryStop(division, c.Name, effect.SkillID, effect.InstanceToken)
	}
}

/*
==================
settleRivalInstruments

Owner's rule 3: when two Bards of one party play instrument auras, the
lower-level one is cancelled and the higher one stays; while a Dancing
plays in that party, two DIFFERENT instruments may both stay. Two auras of
one kind (the same ovl2 overlap word: both Guard Tambours, or Hit March
and Clout March) never share a party.

Inferred:
  - the level is the row's required mastery level (skilldata column 36),
    the one number that orders Guard Tambour against Mana Tambour; the
    tier inside a line does not compare across lines
  - the whole loser aura ends (its Bard's instance and every child), not
    one member's copy: the Bard is the one the rule cancels
  - on equal levels the newer cast stays, as the same Bard's new cast
    replaces its old one
  - the rule is settled after every update, so a dance that stops ends the
    coexistence at the next update

Settled in the retirement pass, before any join: a loser cast since the
last update never hands out a child.

The caller holds partyAuraMu.
==================
*/
func (rt *Runtime) settleRivalInstruments(u *auraUpdate) {
	for {
		loser := rt.rivalInstrumentLoser()
		if loser < 0 {
			return
		}
		rt.retireAura(u, rt.partyAuras[loser])
		rt.partyAuras = append(rt.partyAuras[:loser], rt.partyAuras[loser+1:]...)
	}
}

/*
==================
rivalInstrumentLoser

The index of the first aura rule 3 cancels, or -1. The list is in cast
order, so of two equal levels the earlier index is the older cast.
==================
*/
func (rt *Runtime) rivalInstrumentLoser() int {
	skills := rt.deps.SkillData()
	for i := range rt.partyAuras {
		older := rt.partyAuras[i]
		olderRow, ok := skills.SkillByID(older.skillID)
		if !ok || olderRow.AuraFamily() != enterworld.AuraFamilyInstrument {
			continue
		}
		caster := rt.findCharacter(older.division, older.casterName)
		if caster == nil {
			continue
		}
		party := rt.auraParty(older.division, caster)
		for j := i + 1; j < len(rt.partyAuras); j++ {
			newer := rt.partyAuras[j]
			newerRow, ok := skills.SkillByID(newer.skillID)
			if !ok || newerRow.AuraFamily() != enterworld.AuraFamilyInstrument ||
				newer.division != older.division || newer.casterName == older.casterName {
				continue
			}
			rival := rt.findCharacter(newer.division, newer.casterName)
			if rival == nil || !party[enterworld.ObjectIDForCharacter(rival)] {
				continue
			}
			sameKind := newerRow.Replacement.Ovl2 == olderRow.Replacement.Ovl2
			if !sameKind && rt.danceInParty(older.division, party) {
				continue
			}
			if auraLevel(olderRow) > auraLevel(newerRow) {
				return j
			}
			return i
		}
	}
	return -1
}

// danceInParty reports an open dance aura whose Bard is in party.
/*
================
danceInParty
================
*/
func (rt *Runtime) danceInParty(division string, party map[uint32]bool) bool {
	for _, aura := range rt.partyAuras {
		row, ok := rt.deps.SkillData().SkillByID(aura.skillID)
		if !ok || aura.division != division || row.AuraFamily() != enterworld.AuraFamilyDance {
			continue
		}
		if caster := rt.findCharacter(division, aura.casterName); caster != nil && party[enterworld.ObjectIDForCharacter(caster)] {
			return true
		}
	}
	return false
}

// auraLevel is the level rule 3 compares: the row's first required mastery
// level (see settleRivalInstruments).
/*
================
auraLevel
================
*/
func auraLevel(row enterworld.SkillRow) int64 {
	return row.Masteries[0].Level
}
