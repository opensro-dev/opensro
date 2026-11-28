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

A resident's summoned companion stands in the area beside its owner
(#4490): the structure skills' native target check admits a companion
recipient and judges it through its owner (the target's guild resolves
through the owner, with no recipient-class gate), so a holder-side pet
is a beneficiary too. A character holds the field lane's instance; a
companion holds the skill's program words in the pet word store
(coswords.go), where its stat projection reads them, and the healing
flag's pulse heals it by the same shares of its maxima.

The instance lifetime is inferred: the rows author no duration word,
so an installed instance is given a standing half minute and the scan
re-admits a holder whose instance has ended, while the tracked retire
on exit does the real work.

===========================================================================
*/

package action

import (
	"math"
	"sync/atomic"

	"opensro.online/server/internal/game/combat"
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
holding its instance (characters, and companions by owner name and pet
GID), and the lane's clocks. The scan clock serves the buff holds, the
pulse clock the healing flag's.
================
*/
type structureAuraHold struct {
	skillID    uint32
	recipients []skillobject.FieldRecipient
	companions []structureAuraCompanion
	nextScan   int64
	nextPulse  int64
}

/*
================
structureAuraCompanion

One companion holding a hold's words: the owner's character name (the
pet word store's key) and the pet's GID. The owner pointer is not kept:
the store outlives any one record read, and the advance pass clears a
gone pet's words itself.
================
*/
type structureAuraCompanion struct {
	owner string
	gid   uint32
}

/*
================
structureAuraBeneficiary

One admitted resident of a structure's area: a character, or a
companion standing beside its owner (companion set, gid the pet's).
================
*/
type structureAuraBeneficiary struct {
	owner     *enterworld.Character
	gid       uint32
	companion bool
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
				out = append(out, rt.retireStructureAura(division, hold, nowMs)...)
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
					out = append(out, rt.retireStructureAura(division, hold, nowMs)...)
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
				out = append(out, rt.scanStructureAura(division, record, structure, skill, hold, nowMs)...)
			}
		}
		for gid, hold := range holds {
			if !standing[gid] {
				out = append(out, rt.retireStructureAura(division, hold, nowMs)...)
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

The holder-side residents standing in the structure's area - each
player followed by its companions, in the candidate order the monster
scans walk - bounded by the area's authored cap, which counts
companions as the pulse's cap counts them. A guildless resident, an
enemy of the holder and one past the reach (the area's radius with
both body radii added, as the shape-1 selection measures) is refused,
as the structure skills' native target checks refuse them; a resident
in body modes 2..4 is refused, while a companion is judged by its
owner's standing, as the native check resolves a companion target
through its owner (the pulse reads a companion's own body status the
same way and admits it too). A rider's vehicle stands for its rider,
so the rider is admitted as the character it carries.
================
*/
func (rt *Runtime) structureAuraBeneficiaries(division string, record fortress.Record, structure monster.Instance, skill enterworld.SkillRow, nowMs int64) []structureAuraBeneficiary {
	area := skill.Abnormal.PersistentArea
	center := simulation.Spawn{RegionID: structure.Spawn.RegionID, X: structure.Spawn.X, Y: structure.Spawn.Y, Z: structure.Spawn.Z}
	in := monsterStrikeInput{division: division, instance: structure, skill: skill, now: nowMs}
	var out []structureAuraBeneficiary
	for _, candidate := range rt.monsterAreaCandidates(in) {
		if len(out) >= int(area.MaxTargets) {
			break
		}
		if candidate.owner == nil {
			continue
		}
		if !candidate.companion && candidate.hostility.NativeBodyStatus >= 2 && candidate.hostility.NativeBodyStatus <= 4 {
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
		// A candidate whose hostility names an owner is a summoned
		// companion; the rider's vehicle (no owner named) is the
		// character's own admission.
		out = append(out, structureAuraBeneficiary{owner: candidate.owner, gid: candidate.gid,
			companion: candidate.hostility.OwnerGid != 0})
	}
	return out
}

/*
================
scanStructureAura

One scan of a standing buff: the tracked residents that have left the
area lose their instance of the skill; the admitted ones that hold
none receive one. A character's install is the row with its timed
program claimed: the structure rows author none, so the installer
would otherwise file no recovery rates, and their modifier words
install from the row's own blocks. A companion's install is the same
program as words in the pet store (structureAuraCompanionWords), each
with its own token, published on the pet as an attached effect.
================
*/
func (rt *Runtime) scanStructureAura(division string, record fortress.Record, structure monster.Instance, skill enterworld.SkillRow, hold *structureAuraHold, nowMs int64) []simulation.DivisionFrames {
	admitted := rt.structureAuraBeneficiaries(division, record, structure, skill, nowMs)
	inside := map[*enterworld.Character]bool{}
	insidePets := map[uint32]bool{}
	for _, b := range admitted {
		if b.companion {
			insidePets[b.gid] = true
		} else {
			inside[b.owner] = true
		}
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
	for _, b := range admitted {
		if b.companion {
			continue
		}
		if _, holds := rt.fieldInstance(division, b.owner.Name, skill.ID); !holds {
			fresh = append(fresh, b.owner)
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

	var out []simulation.DivisionFrames
	var keptPets, leftPets []structureAuraCompanion
	for _, r := range hold.companions {
		if !insidePets[r.gid] {
			leftPets = append(leftPets, r)
			continue
		}
		if rt.companionHoldsAura(division, r, skill.ID, nowMs) {
			keptPets = append(keptPets, r)
		}
	}
	out = append(out, rt.retireStructureAuraCompanions(division, skill.ID, leftPets, nowMs)...)
	for _, b := range admitted {
		if !b.companion || rt.companionHoldsAura(division, structureAuraCompanion{owner: b.owner.Name, gid: b.gid}, skill.ID, nowMs) {
			continue
		}
		r := structureAuraCompanion{owner: b.owner.Name, gid: b.gid}
		if frames, ok := rt.installStructureAuraCompanion(division, b, skill, nowMs); ok {
			keptPets = append(keptPets, r)
			out = append(out, frames...)
		}
	}
	hold.companions = keptPets
	return out
}

/*
================
companionHoldsAura

Whether the pet still holds a live word of the aura's skill.
================
*/
func (rt *Runtime) companionHoldsAura(division string, r structureAuraCompanion, skillID uint32, nowMs int64) bool {
	for _, word := range rt.cosWordsHeld(division, r.owner, r.gid, nowMs) {
		if word.SkillID == skillID {
			return true
		}
	}
	return false
}

/*
================
structureAuraCompanionWords

The aura's program as pet words, one per block the row carries: the
damage words (dru), the absorption words (odar) and the recovery
rates (irgc), the same words the character installer files from the
row's own blocks. Words without a block are not filed.
================
*/
func structureAuraCompanionWords(skill enterworld.SkillRow, token func() uint32, nowMs int64) []monster.SelfEffect {
	m := skill.BuffModifiers
	var words []monster.SelfEffect
	add := func(tag, first, second uint32) {
		words = append(words, monster.SelfEffect{SkillID: skill.ID, Token: token(), Tag: tag, First: first, Second: second,
			StartedAtMs: nowMs, UntilMs: nowMs + auraInstanceDurationMs})
	}
	if m.Dru {
		add(cosWordDamageTag, m.DruWords[0], m.DruWords[1])
	}
	if m.Odar {
		add(cosWordAbsorbTag, m.OdarBits, m.OdarWord)
	}
	if skill.TimedEffect.Recovery.Present {
		add(cosWordRecoveryTag, skill.TimedEffect.Recovery.HP, skill.TimedEffect.Recovery.MP)
	}
	return words
}

/*
================
installStructureAuraCompanion

Install the aura's words on one companion and publish them: a replaced
word of the same program retires first, then each word lands as an
attached effect on the pet. A pet that has died or been dismissed
since the scan refuses silently.
================
*/
func (rt *Runtime) installStructureAuraCompanion(division string, b structureAuraBeneficiary, skill enterworld.SkillRow, nowMs int64) ([]simulation.DivisionFrames, bool) {
	pet := b.owner.CompanionByGID(b.gid)
	if pet == nil || !pet.Summoned || pet.CurrentHP == 0 {
		return nil, false
	}
	var frames []wire.Frame
	for _, word := range structureAuraCompanionWords(skill, func() uint32 { return atomic.AddUint32(&rt.castTokenCounter, 1) }, nowMs) {
		replaced, ok := rt.installCosWord(division, b.owner.Name, b.gid, word, nowMs)
		if !ok {
			continue
		}
		if replaced != 0 {
			ended, err := (wire.EndedEffectInstances{InstanceTokens: []uint32{replaced}}).Encode()
			if err != nil {
				panic(err)
			}
			frames = append(frames, wire.Frame{Opcode: wire.OpEndedEffectInstances, Payload: ended})
		}
		payload, err := (wire.AttachedEffect{GID: b.gid, SkillID: skill.ID, InstanceToken: word.Token, Phase: 2}).Encode(
			wire.AttachedEffectLayout{Status: skill.SpawnStatus, Rider: skill.EffectRider})
		if err != nil {
			panic(err)
		}
		frames = append(frames, wire.Frame{Opcode: wire.OpAttachedEffect, Payload: payload})
	}
	if len(frames) == 0 {
		return nil, false
	}
	return []simulation.DivisionFrames{hawkDivisionFrames(division, b.gid, 0, frames)}, true
}

/*
================
retireStructureAuraCompanions

The tracked retire of companions that left a hold: each loses the
hold's skill words, and the still-live tokens retire on the wire
around the pet, as the pet advance pass retires expired words.
================
*/
func (rt *Runtime) retireStructureAuraCompanions(division string, skillID uint32, left []structureAuraCompanion, nowMs int64) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, r := range left {
		tokens := rt.retireCosWords(division, r.owner, r.gid, skillID, nowMs)
		if len(tokens) == 0 {
			continue
		}
		ended, err := (wire.EndedEffectInstances{InstanceTokens: tokens}).Encode()
		if err != nil {
			panic(err)
		}
		out = append(out, hawkDivisionFrames(division, r.gid, 0,
			[]wire.Frame{{Opcode: wire.OpEndedEffectInstances, Payload: ended}}))
	}
	return out
}

/*
================
pulseStructureHeal

One pulse of the healing flag: every admitted holder-side resident in
its area recovers the skill's authored shares of maximum health and
mana, through the same heal recovery the auras' heals ride. A companion
recovers the same shares of its own maxima through the recovery door
the pet potions use, published as the pet's refreshed vitals.
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
	for _, b := range rt.structureAuraBeneficiaries(division, record, structure, skill, nowMs) {
		if b.companion {
			out = append(out, rt.pulseStructureHealCompanion(division, b, skill)...)
			continue
		}
		c := b.owner
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
pulseStructureHealCompanion

One companion's share of the healing flag's pulse: the authored
percents of its effective maxima, landed through combat.RecoverVital
under the pet's recovery reductions, as its potions land. The owner
receives the pet's refreshed vitals; viewers receive the health
refresh, the pair the pet's natural recovery publishes.
================
*/
func (rt *Runtime) pulseStructureHealCompanion(division string, b structureAuraBeneficiary, skill enterworld.SkillRow) []simulation.DivisionFrames {
	owner := b.owner
	var ownerFrame, publicFrame wire.Frame
	healed := rt.deps.UpdateDeferred(owner, "fortress-aura-heal", func() bool {
		pet := owner.CompanionByGID(b.gid)
		if pet == nil || !pet.Summoned || pet.CurrentHP == 0 {
			return false
		}
		ref, valid := rt.cosReference(pet)
		if !valid {
			return false
		}
		block := rt.cosAbnormal(division, owner.Name, b.gid)
		maxHP := int64(uint32(cosParameter(ref, pet, block, abnormalMaxHPParam)))
		maxMP := int64(uint32(cosParameter(ref, pet, block, abnormalMaxMPParam)))
		hp, mp := auraHealBase(skill.Heal, maxHP, maxMP)
		beforeHP, beforeMP := pet.CurrentHP, pet.CurrentMP
		pet.CurrentHP = uint32(combat.RecoverVital(int64(pet.CurrentHP), maxHP, hp,
			cosParameter(ref, pet, block, combat.HPRecoveryReductionParameter)))
		pet.CurrentMP = uint32(combat.RecoverVital(int64(pet.CurrentMP), maxMP, mp,
			cosParameter(ref, pet, block, combat.MPRecoveryReductionParameter)))
		if pet.CurrentHP == beforeHP && pet.CurrentMP == beforeMP {
			return false
		}
		ownerFrame = wire.Frame{Opcode: simulation.OpVitalsUpdate,
			Payload: simulation.VitalsRefreshPayload(b.gid, simulation.Vitals{CurrentHP: pet.CurrentHP, CurrentMP: pet.CurrentMP})}
		if pet.CurrentHP != beforeHP {
			publicFrame = wire.Frame{Opcode: simulation.OpVitalsUpdate,
				Payload: simulation.HPRefreshPayload(b.gid, 0, pet.CurrentHP)}
		}
		return true
	})
	if !healed || ownerFrame.Opcode == 0 {
		return nil
	}
	out := []simulation.DivisionFrames{{DivisionID: division, OnlyCharacterID: owner.ID, Frames: simFrames([]wire.Frame{ownerFrame})}}
	if publicFrame.Opcode != 0 {
		out = append(out, hawkDivisionFrames(division, b.gid, 0, []wire.Frame{publicFrame}))
	}
	return out
}

/*
================
retireStructureAura

A hold ends: every tracked resident loses its instance of the skill,
and every tracked companion its words.
================
*/
func (rt *Runtime) retireStructureAura(division string, hold *structureAuraHold, nowMs int64) []simulation.DivisionFrames {
	rt.retireStructureAuraRecipients(division, hold.skillID, hold.recipients, nowMs)
	hold.recipients = nil
	out := rt.retireStructureAuraCompanions(division, hold.skillID, hold.companions, nowMs)
	hold.companions = nil
	return out
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
