/*
===========================================================================
petbrood.go - port-only destructible crow eggs and temporary basic-attack clones.

The character owns transient actors; the normal companion combat/visibility
lanes own their targets, damage and rewards. No summon survives logout.
===========================================================================
*/
package action

import (
	"encoding/binary"
	"math"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const broodGIDBase uint32 = 0x08000000
const broodHatchMs int64 = 30000
const broodLifeMs int64 = 600000
const broodWindOpcode uint16 = 0x3FBE
const broodDeathMultiplier = 5

// The 0x33A6 vitals body is [u32 gid][u16 source][u8 updateMask]...; mask
// bit 0x01 means the body carries HP (simulation.HPRefreshPayload).
const vitalsUpdateMaskOffset = 6
const vitalsUpdateMaskHP uint8 = 0x01

/*
================
broodObjectID

Disjoint from native entities and the port's mercenary range.
================
*/
func broodObjectID(owner int64, slot int) (uint32, bool) {
	if owner <= 0 || owner > 0x3fffff || slot < 0 || slot >= companion.BroodLimit {
		return 0, false
	}
	return broodGIDBase + uint32(owner)*companion.BroodLimit + uint32(slot), true
}

/*
================
castCrowBrood
================
*/
func (rt *Runtime) castCrowBrood(division string, owner *enterworld.Character, gid uint32, skill companion.SkillDef, now int64) OpResult {
	step, ok := rt.elementalCompanion(division, owner, gid, "Crow", now)
	if !ok {
		return petTrainingReceipt("Stormbrood requires a living nearby crow outside town.")
	}
	mods := companion.ModifiersFor(step.pet)
	level := companion.EffectiveLevel(step.pet, skill, mods)
	if level == 0 {
		return petTrainingReceipt("Learn this ability first.")
	}
	var egg *domain.CharacterCOS
	var private []wire.Frame
	if !rt.deps.Update(owner, "crow-brood-cast", func() bool {
		source := owner.CompanionByGID(gid)
		if source == nil || len(owner.Brood) >= companion.BroodLimit || owner.BroodGeneration == ^uint64(0) {
			return false
		}
		row := companion.BroodRow(source.Training.Rows[skill.Row], skill, level, now, mods)
		if row.Charges == 0 {
			return false
		}
		var id uint32
		for slot := 0; slot < companion.BroodLimit; slot++ {
			candidate, valid := broodObjectID(owner.ID, slot)
			if valid && owner.CompanionByGID(candidate) == nil {
				id = candidate
				break
			}
		}
		if id == 0 {
			return false
		}
		owner.BroodGeneration++
		hp := max(uint32(1), step.ref.MaxHP/4)
		egg = &domain.CharacterCOS{GID: id, RefObjID: source.RefObjID, Codename: source.Codename, Name: "Stormbrood Egg",
			Level: source.Level, CurrentHP: hp, CurrentMP: source.CurrentMP, Summoned: true, StateFlags: 3, Satiety: initialCompanionSatiety,
			SummonGeneration: owner.BroodGeneration, NativeBodyStatus: domain.InitialCOSBodyStatus(owner.NativeBodyStatus),
			BroodStage: 1, BroodSource: gid, BroodUntil: now + broodHatchMs, BroodMaxHP: hp, BroodHatchHP: step.ref.MaxHP, BroodBook: uint8(skill.Option), BroodRank: level}
		// Stats are copied, never learned abilities, timers or support procs.
		egg.Training.Gear = source.Training.Gear
		owner.Brood = append(owner.Brood, egg)
		source.Training.Rows[skill.Row] = row
		companion.StartRow(&source.Training, skill.Row, skill, level, now, mods)
		source.Training.Queued = 0
		private = rt.petTrainingStateFrames(source, now)
		return true
	}) {
		return petTrainingReceipt("No stored cast or the live egg/clone limit is reached.")
	}
	state := rt.bindCompanionSession(division, step.state, egg)
	at := step.state.follower.Position(now)
	state.follower = simulation.NewPetFollower(egg.GID, at)
	state.broodAt = at
	state.summonedAtMs = now
	spawn := rt.broodSpawnFrame(division, state, egg)
	cue := crowVisualFrame(crowVisual{gid: gid, kind: crowEggVisual, book: uint8(skill.Option), at: at})
	return OpResult{Frames: append(private, spawn, cue), Broadcast: []wire.Frame{spawn, cue}}
}

/*
================
broodSpawnFrame

Peer-only creation leaves the owner's single attack-pet record untouched.
================
*/
func (rt *Runtime) broodSpawnFrame(division string, state *petSession, pet *domain.CharacterCOS) wire.Frame {
	peer := rt.companionPresentation(division, state, pet)
	if peer == nil {
		return wire.Frame{}
	}
	row := peer.Row
	at := state.broodAt
	row.Position = wire.Position{RegionID: at.RegionID, X: float32(at.X), Y: float32(at.Y), Z: float32(at.Z), Heading: at.Angle}
	row.BodyStatus = pet.NativeBodyStatus
	// Owned birth/hatch cues replace the native blue summon column.
	row.State = 0
	return wire.Frame{Opcode: wire.OpSingleObjectSpawn, Payload: wire.EncodeCosSpawnBand2(row)}
}

/*
================
crowBroodSource

The companion the brood lifecycle answers for: the summoned record
under this gid, else a brood-stage crow still waiting in the roster.
Admission and the advance itself resolve through here so the two can
never disagree about which pet a gid names.
================
*/
func crowBroodSource(c *enterworld.Character, gid uint32) *domain.CharacterCOS {
	if source := c.CompanionByGID(gid); source != nil {
		return source
	}
	var found *domain.CharacterCOS
	c.VisitCompanions(func(candidate *domain.CharacterCOS) bool {
		if candidate.GID == gid && candidate.BroodStage == 0 && companion.PetKind(candidate.Codename) == "Crow" {
			found = candidate
		}
		return found == nil
	})
	return found
}

/*
================
hasBroodFrom

Whether any brood pet of c was laid by the crow gid.
================
*/
func hasBroodFrom(c *enterworld.Character, gid uint32) bool {
	for _, pet := range c.Brood {
		if pet.BroodSource == gid {
			return true
		}
	}
	return false
}

/*
================
crowRechargeRow

The crow's recharge row at now, and whether it moves the stored row.
Pure on the live pet, so a tick judges it under the read door and
only a moving row opens the update door (#2139).
================
*/
func crowRechargeRow(live *enterworld.CharacterCOS, now int64) (domain.PetRowCooldown, bool) {
	row := companion.AdvanceCharges(live.Training.Rows[1], now)
	for _, def := range companion.SkillsForPet(live) {
		if def.EffectName() == companion.BroodSkill && companion.Level(live, def) > 0 {
			// One modifiers read answers both the level and the row:
			// ModifiersFor is pure on the live record, so the second
			// walk the two call sites used to pay bought nothing.
			mods := companion.ModifiersFor(live)
			row = companion.BroodRow(row, def, companion.EffectiveLevel(live, def, mods), now, mods)
		}
	}
	return row, row != live.Training.Rows[1]
}

/*
================
petBroodRechargeMemo

One crow session's last recharge verdict and the exact record
fields it was derived from (#2033): crowRechargeRow is a pure
function of the stored row, the record fields the modifiers
graph reads (the learned levels, the slotted passives, the worn
gear, the pet's own level and stage) and the clock, so an idle
crow tick whose fields still stand answers the memoised verdict
instead of walking the skill table and the modifiers graph
again. The verdict can only turn when a charge boundary crosses
- the row's own recharge deadline, kept as untilMs - or when a
compared field moves; either re-derives the full row. Only a
still verdict is memoised: a moving one is re-derived under the
update door, and its write is what the next tick compares.
================
*/
type petBroodRechargeMemo struct {
	set         bool
	row         domain.PetRowCooldown
	untilMs     int64
	codename    string
	level       uint8
	broodStage  uint8
	treeVersion uint8
	levels      [15]uint8
	slotted     [3]uint8
	gear        [3]domain.PetGear
}

/*
================
petBroodRechargeMemo.capture

Record a still verdict and the fields it was derived from.
================
*/
func (memo *petBroodRechargeMemo) capture(live *enterworld.CharacterCOS, row domain.PetRowCooldown, untilMs int64) {
	*memo = petBroodRechargeMemo{
		set: true, row: row, untilMs: untilMs,
		codename: live.Codename, level: live.Level, broodStage: live.BroodStage,
		treeVersion: live.Training.TreeVersion, levels: live.Training.Levels,
		slotted: live.Training.Slotted, gear: live.Training.Gear,
	}
}

/*
================
petBroodRechargeMemo.matches

Whether the memoised still verdict still answers for this
record at now: the stored row, every field the verdict was
derived from, and the recharge deadline still ahead.
================
*/
func (memo *petBroodRechargeMemo) matches(live *enterworld.CharacterCOS, row domain.PetRowCooldown, now int64) bool {
	return memo.set && memo.row == row && now < memo.untilMs &&
		memo.codename == live.Codename && memo.level == live.Level &&
		memo.broodStage == live.BroodStage && memo.treeVersion == live.Training.TreeVersion &&
		memo.levels == live.Training.Levels && memo.slotted == live.Training.Slotted &&
		memo.gear == live.Training.Gear
}

/*
================
broodRechargeStillUntil

The earliest tick a still recharge verdict could turn: with the
row and its inputs standing, AdvanceCharges is the verdict's
only clock, and it is still before the armed recharge deadline
- or forever, when no charge is counting down. A verdict judged
at the deadline itself already moved, so a still verdict's
deadline is always ahead of the tick that judged it.
================
*/
func broodRechargeStillUntil(row domain.PetRowCooldown) int64 {
	if row.Capacity == 0 || row.Charges >= row.Capacity || row.ReadyAtMs <= 0 {
		return math.MaxInt64
	}
	return row.ReadyAtMs
}

/*
================
petSession.broodRechargeVerdict

The crow's recharge verdict for one tick (crowBroodRecharge's
question): the session memo's standing answer while the row and
every field it was derived from still stand and the recharge
deadline is ahead; the full crowRechargeRow otherwise.
================
*/
func (state *petSession) broodRechargeVerdict(live *enterworld.CharacterCOS, now int64) (domain.PetRowCooldown, bool) {
	stored := live.Training.Rows[1]
	if state.broodRecharge.matches(live, stored, now) {
		return stored, false
	}
	row, moved := crowRechargeRow(live, now)
	if moved {
		state.broodRecharge.set = false
		return row, true
	}
	state.broodRecharge.capture(live, stored, broodRechargeStillUntil(stored))
	return row, false
}

/*
================
crowBroodPetFacts

One brood pet's judge inputs, read off the live record under the
character door: the values the lifecycle's remove and hatch verdicts
weigh (stage, deadline, life). The child-session half of those
verdicts (a lost session removes) is answered outside the door.
================
*/
type crowBroodPetFacts struct {
	gid   uint32
	stage uint8
	until int64
	hp    uint32
}

/*
================
crowBroodFacts

What one crow-brood tick decides, read off the live record
(#2030): whether this gid runs the lifecycle at all, whether any
brood event (a removal, a hatch) fires this tick, and the stage-2
clones the combat-order step answers for. The events themselves
re-derive under their doors on the copy path; the facts only keep
a tick that fires nothing off the owner copy.
================
*/
type crowBroodFacts struct {
	crow   bool
	events bool
	stage2 []uint32
}

/*
================
crowBroodJudge

advanceCrowBrood's facts door (#2030): the lifecycle below runs on
the companion copy, but most ticks fire nothing - the eggs wait on
their timers, the clones live out theirs - so the tick first judges
on the live record whether anything can move. The door read
collects the source verdict and each brood pet's timer facts; the
child-session facts are session state, answered by petSessionFor
outside the character door as the copy path answers them (a lost
session removes; a follower child's position feeds its broodAt).
An event, or the forced-copy seam, abstains to the copy path, whose
doors re-derive every event from the live record as before.
================
*/
func (rt *Runtime) crowBroodJudge(key petOwnerKey, state *petSession, now int64) (crowBroodFacts, bool) {
	if rt.companionCopy != nil {
		return crowBroodFacts{}, false
	}
	// A brood-space gid (an egg, a clone) never runs the lifecycle
	// under its own key - the crow verdict's first operand refuses
	// it, and the copy path refuses it the same way - so the key's
	// own arithmetic answers before any door read is taken. The
	// brood answers to the crow's key, never to its own.
	if key.gid >= broodGIDBase {
		return crowBroodFacts{}, true
	}
	var facts crowBroodFacts
	// The brood facts ride a stack array, not a grown slice: the
	// record's Brood never exceeds BroodLimit rows (castCrowBrood
	// refuses the cast that would), so the judge allocates nothing
	// on the door (#2030).
	var pets [companion.BroodLimit]crowBroodPetFacts
	petCount := 0
	valid := false
	judged := false
	guard := rt.deps.AcquireRead(key.division)
	judged = true
	live := state.character
	source := crowBroodSource(live, key.gid)
	facts.crow = key.gid < broodGIDBase && (source == nil || companion.PetKind(source.Codename) == "Crow")
	if facts.crow {
		for _, pet := range live.Brood {
			if pet.BroodSource != key.gid {
				continue
			}
			pets[petCount] = crowBroodPetFacts{gid: pet.GID, stage: pet.BroodStage, until: pet.BroodUntil, hp: pet.CurrentHP}
			petCount++
		}
		// The validity verdict (life, town) only feeds the removal
		// half of the pet loop below; a crow with no brood out has
		// nothing to remove, so the town read is only paid by a
		// tick that could actually retire a brood pet.
		if petCount > 0 {
			valid = source != nil && rt.AttackPetExpansion && source.Summoned && source.CurrentHP > 0 &&
				enterworld.CharacterAlive(live) && !rt.inTown(key.division, live)
		}
	}
	guard.Release()
	if !judged || !facts.crow {
		return facts, judged
	}
	for _, pet := range pets[:petCount] {
		child := rt.petSessionFor(key.division, key.name, pet.gid)
		if child != nil && child.follower != nil {
			child.broodAt = child.follower.Position(now)
		}
		remove := !valid || child == nil || pet.hp == 0 || pet.stage == 2 && now >= pet.until
		hatch := !remove && pet.stage == 1 && now >= pet.until
		if remove || hatch {
			facts.events = true
		}
		if pet.stage == 2 {
			facts.stage2 = append(facts.stage2, pet.gid)
		}
	}
	return facts, true
}

/*
================
crowBroodRecharge

The crow's recharge, judged and committed on the live pet: the row
is pure on the live record, so a tick reads its verdict under the
stack read guard (Deps.AcquireRead - the callback door boxed a
closure and its captures on every crow tick, #2033) and opens the
update door only on a tick whose row actually moves (#2139); the
door re-derives the same row. Returns its frames.
================
*/
func (rt *Runtime) crowBroodRecharge(key petOwnerKey, state *petSession, now int64) []wire.Frame {
	guard := rt.deps.AcquireRead(key.division)
	recharges := false
	if live := state.character.CompanionByGID(key.gid); live != nil {
		_, recharges = state.broodRechargeVerdict(live, now)
	}
	guard.Release()
	if !recharges {
		return nil
	}
	var reuse []wire.Frame
	rt.deps.UpdateDeferred(state.character, "crow-brood-recharge", func() bool {
		live := state.character.CompanionByGID(key.gid)
		if live == nil {
			return false
		}
		row, changed := crowRechargeRow(live, now)
		if !changed {
			return false
		}
		live.Training.Rows[1] = row
		reuse = rt.petTrainingStateFrames(live, now)
		return true
	})
	return reuse
}

/*
================
advanceCrowBrood

Only the original crow runs the lifecycle. Removal never grants back a cast.
Administrative removal (town, owner/source loss, relocation) never explodes.
================
*/
func (rt *Runtime) advanceCrowBrood(key petOwnerKey, now int64) []simulation.Frame {
	state := rt.petSessionFor(key.division, key.name, key.gid)
	if state == nil {
		return nil
	}
	// The tick judges on door-read facts first (#2030): a tick that
	// fires no brood event takes no owner copy at all - the recharge
	// and the stage-2 combat orders run on the facts, in the copy
	// path's own order. An event or an abstention falls through to
	// the copy path below, unchanged.
	if facts, judged := rt.crowBroodJudge(key, state, now); judged {
		if !facts.crow {
			return nil
		}
		if !facts.events {
			reuse := rt.crowBroodRecharge(key, state, now)
			var output []wire.Frame
			for _, gid := range facts.stage2 {
				child := rt.petSessionFor(key.division, key.name, gid)
				if child == nil {
					continue
				}
				childKey := petOwnerKey{division: key.division, name: key.name, gid: gid}
				if state.combat == nil {
					rt.cancelPetCombat(childKey, child, now)
				} else if child.combat == nil || child.combat.target != state.combat.target {
					rt.orderPetAttack(key.division, state.character, rt.characterSnapshot(key.division, state.character), gid, state.combat.target, now, rt.tickDoors())
				}
			}
			state.public = append(state.public, output...)
			return append(simFrames(output), simFrames(reuse)...)
		}
	}
	// Runs for every pet per tick: copy the owner only when the check
	// below would let this gid through - every other pet (and every
	// brood-space gid) is refused before any state moves (#1943). A crow
	// with no brood out needs no copy at all: only the recharge below
	// runs for it, and that reads the live record inside its own door.
	// The copy is the companion copy (#2030): every reader below - the
	// source and its brood, life, town, and the death storm's pet-combat
	// step - reads companions, equipment and world facts, never a plain
	// bag row or the avatar inventory.
	crow := false
	snapshot := rt.characterSnapshotCompanionAdmitted(key.division, state.character, func(c *enterworld.Character) bool {
		source := crowBroodSource(c, key.gid)
		crow = key.gid < broodGIDBase && (source == nil || companion.PetKind(source.Codename) == "Crow")
		return crow && hasBroodFrom(c, key.gid)
	})
	if !crow {
		return nil
	}
	valid := false
	if snapshot != nil {
		source := crowBroodSource(snapshot, key.gid)
		valid = source != nil && rt.AttackPetExpansion && source.Summoned && source.CurrentHP > 0 && enterworld.CharacterAlive(snapshot) && !rt.inTown(key.division, snapshot)
	}
	var output []wire.Frame
	reuse := rt.crowBroodRecharge(key, state, now)
	if snapshot == nil {
		return simFrames(reuse)
	}
	for _, pet := range snapshot.Brood {
		if pet.BroodSource != key.gid {
			continue
		}
		child := rt.petSessionFor(key.division, key.name, pet.GID)
		if child != nil && child.follower != nil {
			child.broodAt = child.follower.Position(now)
		}
		remove := !valid || child == nil || pet.CurrentHP == 0 || pet.BroodStage == 2 && now >= pet.BroodUntil
		hatch := !remove && pet.BroodStage == 1 && now >= pet.BroodUntil
		if remove {
			if valid && child != nil && pet.BroodStage == 2 && pet.BroodBook == 3 {
				rt.broodDeathStorm(key, child, snapshot, pet, now)
			}
			rt.deps.UpdateDeferred(state.character, "crow-brood-remove", func() bool {
				kept := state.character.Brood[:0]
				for _, live := range state.character.Brood {
					if live.GID != pet.GID {
						kept = append(kept, live)
					}
				}
				state.character.Brood = kept
				return true
			})
			output = append(output, rt.retireCosRuntime(key.division, state.character, pet.GID)...)
			output = append(output, wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: pet.GID}.Encode()})
			rt.petMu.Lock()
			delete(rt.petSessions, petOwnerKey{division: key.division, name: strings.ToLower(key.name), gid: pet.GID})
			rt.petSessionKeys = nil
			rt.petMu.Unlock()
			continue
		}
		if hatch {
			var clone *domain.CharacterCOS
			rt.deps.UpdateDeferred(state.character, "crow-brood-hatch", func() bool {
				live := state.character.CompanionByGID(pet.GID)
				if live == nil || live.CurrentHP == 0 || live.BroodStage != 1 {
					return false
				}
				live.BroodStage = 2
				live.BroodUntil = now + broodLifeMs
				live.BroodMaxHP = live.BroodHatchHP
				live.CurrentHP = live.BroodMaxHP
				live.Name = "Stormbrood Crow"
				clone = domain.CloneCOS(live)
				return true
			})
			if clone != nil {
				child.generation++
				output = append(output, wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: pet.GID}.Encode()}, rt.broodSpawnFrame(key.division, child, clone), crowVisualFrame(crowVisual{gid: clone.GID, kind: crowHatchVisual, book: clone.BroodBook, at: child.broodAt}))
			}
		}
		if pet.BroodStage == 2 && child != nil {
			childKey := petOwnerKey{division: key.division, name: key.name, gid: pet.GID}
			if state.combat == nil {
				rt.cancelPetCombat(childKey, child, now)
			} else if child.combat == nil || child.combat.target != state.combat.target {
				rt.orderPetAttack(key.division, state.character, rt.characterSnapshot(key.division, state.character), pet.GID, state.combat.target, now, rt.tickDoors())
			}
		}
	}
	state.public = append(state.public, output...)
	return append(simFrames(output), simFrames(reuse)...)
}

/*
================
broodDeathStorm

Port-only tuning: one 8 m wind burst, 500% of a normal clone strike, at most
8 hostile monsters. The shared strike door handles mitigation and rewards.
================
*/
func (rt *Runtime) broodDeathStorm(key petOwnerKey, state *petSession, snapshot *enterworld.Character, pet *domain.CharacterCOS, now int64) {
	// Position-bearing feedback survives the clone's same-tick despawn, even without victims.
	at := state.broodAt
	visual := wire.Frame{Opcode: broodWindOpcode, Payload: wire.NewWriter(18).U32(pet.GID).U16(at.RegionID).F32(float32(at.X)).F32(float32(at.Y)).F32(float32(at.Z)).Payload()}
	rt.queueMonsterLegFrames(rt.sealTickPrivate([]simulation.DivisionFrames{{DivisionID: key.division, SourceGID: key.gid, Frames: simFrames([]wire.Frame{visual})}}))
	ref, ok := rt.cosReference(pet)
	if !ok || rt.Monsters == nil {
		return
	}
	lease, ok := rt.casterPopulation(key.division, snapshot)
	if !ok {
		return
	}
	step := petCombatStep{key: key, state: state, snapshot: snapshot, pet: pet, ref: ref, nowMs: now, percent: fullAreaPercent}
	if state.follower == nil {
		state.follower = simulation.NewPetFollower(pet.GID, state.broodAt)
	}
	count := 0
	for _, mob := range rt.Monsters.CombatCandidatesInPopulation(key.division, lease, state.broodAt, 80, now, false) {
		target, valid := rt.resolvePetCombatTarget(step, mob.Gid)
		if !valid || target.monster == nil || !samePlaneAdjacent(state.broodAt, target.at) || areaDistance(state.broodAt, target.at) > 80 {
			continue
		}
		if rt.LineOfSight != nil && !rt.LineOfSight(state.broodAt, simulation.WorldState{Spawn: state.broodAt}.LiveOwnerAt(now), target.at) {
			continue
		}
		intent := petCombatIntent{}
		skill, found := rt.petAttackSkill(step, &intent, simulation.AttackTarget{})
		if !found {
			continue
		}
		skill, step.trained = rt.petTrainedDecide(step, skill, target)
		if step.trained != nil {
			step.trained.reserved = true
		}
		skill.Attack.Percent *= broodDeathMultiplier
		skill.ActionArea = enterworld.SkillOffensiveArea{}
		if hit, committed := rt.strikePetTarget(step, target, skill); committed {
			public := hit.strike.public
			// A target with an abnormal state already got its HP in the strike's
			// abnormal vitals frame (monsterImpactAbnormalFrames); send one refresh.
			if current, found := rt.Monsters.Get(key.division, target.gid); found && !hasVitalsFrameFor(public, target.gid) {
				public = append(public, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(target.gid, 0, current.CurrentHP)})
			}
			// The dying clone cannot publish a normal cast result after despawning.
			for _, impact := range hit.strike.impacts {
				if impact.Damage > 0 {
					public = append(public, abnormalDamageFrame(target.gid, impact.Damage))
				}
			}
			// The clone leaves observer scope this tick; its living source still has an audience.
			rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: key.division, SourceGID: key.gid, Frames: simFrames(public)},
				{DivisionID: key.division, OnlyCharacterID: state.character.ID, Frames: simFrames(hit.actor)}})
		}
		count++
		if count >= 8 {
			break
		}
	}
}

/*
================
hasVitalsFrameFor

Reports whether frames already carry gid's HP in a vitals update, so the
death burst never publishes the target's HP twice. A vitals update without
the HP bit (an abnormal- or MP-only body) does not count.
================
*/
func hasVitalsFrameFor(frames []wire.Frame, gid uint32) bool {
	for _, frame := range frames {
		if frame.Opcode != simulation.OpVitalsUpdate || len(frame.Payload) <= vitalsUpdateMaskOffset {
			continue
		}
		if binary.LittleEndian.Uint32(frame.Payload) == gid && frame.Payload[vitalsUpdateMaskOffset]&vitalsUpdateMaskHP != 0 {
			return true
		}
	}
	return false
}
