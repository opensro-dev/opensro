/*
===========================================================================

fortress_structure_aura.go - the fortress structures' holder-side auras

The defensive positions, the headquarters and the flags carry no
attack: their authored skill is a standing area (efr kind 3, select 32)
whose program is a buff - flat and percent attack and defense words
(dru / odar), hit point and mana recovery rates (irgc), or, for the
healing flag, a five-second share-of-maximum heal on its pulse. At war
the lane keeps every holder-side resident inside a standing structure's
area holding an instance of its skill, retiring the instance of one
that steps out, and pulses the healing flag's heal on the same clock
the towers strike by. Off a war nothing stands: the native target
checks refuse anyone out of the war, so the war's end retires every
hold, as it silences the pulses (fortress_structure_pulse.go).

The instance lifetime is inferred: the rows author no duration word,
so an installed instance is given a standing half minute and the scan
re-admits a holder whose instance has ended, while the tracked retire
on exit does the real work. Only characters hold instances; companions
take no buff in this port's stat model and are left out, untraced.

===========================================================================
*/

package action

import (
	"math"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/skillobject"
)

// auraAreaSelect is the standing area's select word of a holder-side
// aura; the damage family (towers, the fort stone) authors 24 there.
const auraAreaSelect = 32

// auraInstanceDurationMs is the standing instance's lifetime, long
// enough that only the tracked retire ends it in practice.
const auraInstanceDurationMs = 30_000

/*
================
structureAuraHold

One standing structure's hold: the skill it works by, the residents
holding its instance, and the lane's clocks. The scan clock serves
the buff holds, the pulse clock the healing flag's.
================
*/
type structureAuraHold struct {
	skillID    uint32
	recipients []skillobject.FieldRecipient
	nextScan   int64
	nextPulse  int64
}

/*
================
advanceFortressStructureAuras

Every division at war: each standing position, headquarters or flag
keeps its aura on the holder-side residents in its area, scanned on
the skill-object tick the buff fields use; the healing flag heals on
its authored pulse. A structure first seen standing in a live war is
cast at once (the appearance cast), so its first scan lands on the
first tick that sees it. A structure that falls, is replaced or
leaves the war loses its hold and its recipients their instances.
================
*/
func (rt *Runtime) advanceFortressStructureAuras(nowMs int64) []simulation.DivisionFrames {
	if rt.Fortresses == nil || rt.Monsters == nil {
		return nil
	}
	if rt.structureAuras == nil {
		rt.structureAuras = map[string]map[uint32]*structureAuraHold{}
	}
	var out []simulation.DivisionFrames
	for _, division := range rt.Fortresses.Divisions() {
		holds := rt.structureAuras[division]
		if !rt.Fortresses.WarActive(division) {
			for _, hold := range holds {
				rt.retireStructureAura(division, hold, nowMs)
			}
			delete(rt.structureAuras, division)
			continue
		}
		if holds == nil {
			holds = map[uint32]*structureAuraHold{}
			rt.structureAuras[division] = holds
		}
		standing := map[uint32]bool{}
		for fortressID, world := range rt.fortressWorlds() {
			record, ok := rt.Fortresses.Get(division, fortressID)
			if !ok {
				continue
			}
			for _, structure := range rt.Monsters.WorldStructures(division, world) {
				if structure.CurrentHP == 0 {
					continue
				}
				skill, ok := rt.structureAuraSkill(structure)
				if !ok {
					continue
				}
				standing[structure.Gid] = true
				hold := holds[structure.Gid]
				if hold != nil && hold.skillID != skill.ID {
					rt.retireStructureAura(division, hold, nowMs)
					hold = nil
				}
				if hold == nil {
					hold = &structureAuraHold{skillID: skill.ID}
					holds[structure.Gid] = hold
				}
				if skill.Heal.Present {
					out = append(out, rt.pulseStructureHeal(division, record, structure, skill, hold, nowMs)...)
					continue
				}
				if nowMs < hold.nextScan {
					continue
				}
				hold.nextScan = nowMs + int64(enterworld.SkillFieldScanMs)
				rt.scanStructureAura(division, record, structure, skill, hold, nowMs)
			}
		}
		for gid, hold := range holds {
			if !standing[gid] {
				rt.retireStructureAura(division, hold, nowMs)
				delete(holds, gid)
			}
		}
	}
	return out
}

/*
================
structureAuraSkill

The structure's aura skill: the first of its authored default skills
whose row carries the holder-side standing area together with a buff
program - the modifier words, the recovery rates, or the healing
flag's pulsed heal. Towers and the fort stone carry none of this
(their area selects the damage family).
================
*/
func (rt *Runtime) structureAuraSkill(structure monster.Instance) (enterworld.SkillRow, bool) {
	skills := rt.deps.SkillData()
	if skills == nil {
		return enterworld.SkillRow{}, false
	}
	for _, id := range structure.Ref.DefaultSkillIDs {
		if id == 0 {
			continue
		}
		row, ok := skills.SkillByID(id)
		if !ok {
			continue
		}
		area := row.Abnormal.PersistentArea
		if area.Present && area.Select == auraAreaSelect &&
			(row.BuffModifiers.Present() || row.TimedEffect.Recovery.Present || row.Heal.Present) {
			return row, true
		}
	}
	return enterworld.SkillRow{}, false
}

/*
================
structureAuraBeneficiaries

The holder-side characters standing in the structure's area, in the
candidate order the monster scans walk, bounded by the area's authored
cap. A guildless resident, an enemy of the holder, a resident in body
modes 2..4 and one past the reach (the area's radius with both body
radii added, as the shape-1 selection measures) is refused, as the
structure skills' native target checks refuse them.
================
*/
func (rt *Runtime) structureAuraBeneficiaries(division string, record fortress.Record, structure monster.Instance, skill enterworld.SkillRow, nowMs int64) []*enterworld.Character {
	area := skill.Abnormal.PersistentArea
	center := simulation.Spawn{RegionID: structure.Spawn.RegionID, X: structure.Spawn.X, Y: structure.Spawn.Y, Z: structure.Spawn.Z}
	in := monsterStrikeInput{division: division, instance: structure, skill: skill, now: nowMs}
	var out []*enterworld.Character
	for _, candidate := range rt.monsterAreaCandidates(in) {
		if len(out) >= int(area.MaxTargets) {
			break
		}
		if candidate.owner == nil || candidate.hostility.OwnerGid != 0 {
			continue
		}
		if candidate.hostility.NativeBodyStatus >= 2 && candidate.hostility.NativeBodyStatus <= 4 {
			continue
		}
		if !worldgeom.SamePlane(candidate.pose.RegionID, center.RegionID) {
			continue
		}
		var guildID int64
		if candidate.owner.GuildID != nil {
			guildID = *candidate.owner.GuildID
		}
		if !rt.fortressDefender(division, record, guildID) {
			continue
		}
		if math.Hypot(simulation.WorldDistance2D(center, candidate.pose), candidate.pose.Y-center.Y) >
			float64(area.Radius)+float64(structure.Ref.BodyRadius)+candidate.radius {
			continue
		}
		out = append(out, candidate.owner)
	}
	return out
}

/*
================
scanStructureAura

One scan of a standing buff: the tracked residents that have left the
area lose their instance of the skill; the admitted ones that hold
none receive one. The install is the row with its timed program
claimed: the structure rows author none, so the installer would
otherwise file no recovery rates, and their modifier words install
from the row's own blocks.
================
*/
func (rt *Runtime) scanStructureAura(division string, record fortress.Record, structure monster.Instance, skill enterworld.SkillRow, hold *structureAuraHold, nowMs int64) {
	admitted := rt.structureAuraBeneficiaries(division, record, structure, skill, nowMs)
	inside := map[*enterworld.Character]bool{}
	for _, c := range admitted {
		inside[c] = true
	}
	var kept, left []skillobject.FieldRecipient
	for _, r := range hold.recipients {
		c := rt.findCharacter(division, r.Name)
		if c == nil || enterworld.ObjectIDForCharacter(c) != r.GID {
			continue
		}
		if !inside[c] {
			left = append(left, r)
			continue
		}
		if _, holds := rt.fieldInstance(division, c.Name, skill.ID); holds {
			kept = append(kept, r)
		}
	}
	rt.retireStructureAuraRecipients(division, skill.ID, left, nowMs)
	var fresh []*enterworld.Character
	for _, c := range admitted {
		if _, holds := rt.fieldInstance(division, c.Name, skill.ID); !holds {
			fresh = append(fresh, c)
		}
	}
	install := skill
	install.TimedEffect.Pinned = true
	install.EffectDurationPresent = true
	install.EffectDurationMs = auraInstanceDurationMs
	for _, c := range rt.installTickRecipientEffects(division, fresh, install, 0, nowMs) {
		kept = append(kept, skillobject.FieldRecipient{Name: c.Name, GID: enterworld.ObjectIDForCharacter(c)})
	}
	hold.recipients = kept
}

/*
================
pulseStructureHeal

One pulse of the healing flag: every admitted holder-side resident in
its area recovers the skill's authored shares of maximum health and
mana, through the same heal recovery the auras' heals ride.
================
*/
func (rt *Runtime) pulseStructureHeal(division string, record fortress.Record, structure monster.Instance, skill enterworld.SkillRow, hold *structureAuraHold, nowMs int64) []simulation.DivisionFrames {
	if !skill.Abnormal.PulsePresent || skill.Abnormal.Pulse == 0 {
		return nil
	}
	if hold.nextPulse == 0 {
		hold.nextPulse = nowMs + int64(skill.Abnormal.Pulse)
		return nil
	}
	if nowMs < hold.nextPulse {
		return nil
	}
	hold.nextPulse = nowMs + int64(skill.Abnormal.Pulse)
	var out []simulation.DivisionFrames
	for _, c := range rt.structureAuraBeneficiaries(division, record, structure, skill, nowMs) {
		var frame wire.Frame
		healed := rt.deps.UpdateDeferred(c, "fortress-aura-heal", func() bool {
			hp, mp, ok := rt.skillHealAmounts(division, c, nil, skill, healAura)
			if !ok {
				return false
			}
			var applied bool
			frame, applied = rt.applySkillRecovery(division, c, hp, mp)
			return applied
		})
		if !healed || frame.Opcode == 0 {
			continue
		}
		out = append(out, simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: c.ID, Frames: simFrames([]wire.Frame{frame})})
	}
	return out
}

/*
================
retireStructureAura

A hold ends: every tracked resident loses its instance of the skill.
================
*/
func (rt *Runtime) retireStructureAura(division string, hold *structureAuraHold, nowMs int64) {
	rt.retireStructureAuraRecipients(division, hold.skillID, hold.recipients, nowMs)
	hold.recipients = nil
}

/*
================
retireStructureAuraRecipients

The field lane's retirement, addressed by the aura's skill: the
instance the scan installed is the recipient's live instance of the
skill, whoever the registry credits it to.
================
*/
func (rt *Runtime) retireStructureAuraRecipients(division string, skillID uint32, recipients []skillobject.FieldRecipient, nowMs int64) {
	object := skillobject.Object{Division: division, Program: skillobject.Program{SkillID: skillID}}
	rt.retireFieldRecipients(object, recipients, nowMs)
}
