/*
===========================================================================

pettrained.go - attack-pet skills and gear in the pet's own combat

Port-only, not native (companion/training.go, gear.go). A trained strike
is decided once, before the shared area pipeline (petarea.go) runs: at most
one action per active row is picked, the native attack row is rewritten
(damage percent, area, abnormal parameters) and the wolf, bear, penguin or
crow keeps its own authored animation. Statuses use the native abnormal
engine with argument layouts copied from native skills (noted per
status). Row cooldowns, counters, leech and owner heals commit in the
transaction of the primary target's hit; a refused commit spends nothing.
Every bonus applies to ordinary-world monsters only.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// Area radii are game units (meters x 10); mask 24 selects hostiles.
	petAreaPerMeter     = 10
	petAreaHostile      = 24
	petAreaAroundPet    = 1
	petAreaAroundTarget = 2
	// Bleed: 6 s of 2 s pulses (Rending Bite).
	petBleedMs      = 6000
	petBleedPulseMs = 2000
	// petStatusGrade is the native grade every trained status rolls with.
	petStatusGrade = 3
	// Elemental blues add a status with this chance (percent).
	petElementChance = 5
	// A pet's Mend proc heals the owner this share of max HP.
	petProcMendPercent = 5
)

/*
================
petTrainedRuntime

The pet session's unsaved combat memory: the attack counter for every
Nth-attack passive, and the self buffs in force.
================
*/
type petTrainedRuntime struct {
	attacks       int
	frenzyUntil   int64
	frenzyPercent float64
	frenzyCrit    float64
	ironHideUntil int64
	ironHidePct   float64
	retribution   int64 // window end; 0 when not storing
	retribStored  float64
	retribPercent float64
	retribStun    bool
	procReadyAt   int64
}

/*
================
petRowUse
================
*/
type petRowUse struct {
	row   int
	skill companion.SkillDef
	level uint8
}

/*
================
petTrainedStrike

One strike's trained plan, shared by every target of the strike.
================
*/
type petTrainedStrike struct {
	codename string
	mods     companion.Modifiers
	uses     []petRowUse
	// ownerState is the owner's 0x3FB1 push after a row was spent.
	ownerState []wire.Frame
	reserved   bool
	procUsed   bool
	execute    float64 // kill non-bosses below this share of max HP
	execBleed  float64 // extra execute share against bleeding targets
	critBonus  float64
	critWound  float64 // Scent of Blood: crit against bleeding or wounded
	shatter    float64 // bonus damage share against frozen or frostbitten
	ownerHeal  float64 // owner heal (percent of max HP) at commit
	killHeal   float64 // owner heal on a kill
	petHeal    float64 // pet heal (percent of max HP) at commit
	passive    int     // passive row whose timer the commit restarts (-1)
	passiveMs  int64
	fixed      uint32 // Retribution's release: fixed damage per target
}

/*
================
petExpansionAllowed

Bonuses apply only to ordinary monster combat. No job suit, free battle or
instanced-world bonuses; attacks on players/COS never enter this path.
================
*/
func (rt *Runtime) petExpansionAllowed(c *enterworld.Character, pet *domain.CharacterCOS) bool {
	return rt.AttackPetExpansion && companion.PetKind(pet.Codename) != "" &&
		!rt.inFreeBattle(c) && !wearsJobSuit(c, rt.deps.ItemReferences()) && domain.CharacterWorldInstance(c) == domain.DefaultWorldInstance
}

/*
================
petChance

One percent roll from the combat RNG.
================
*/
func (rt *Runtime) petChance(percent float64) bool {
	if percent <= 0 || rt.CombatRoll == nil {
		return false
	}
	draw, err := rt.CombatRoll()
	return err == nil && float64(draw%10000) < percent*100
}

/*
================
setPetStatus

Adds one native status to the strike's abnormal parameters.
================
*/
func setPetStatus(skill *enterworld.SkillRow, tag uint32, args [6]uint32) {
	if index, ok := abnormal.SourceIndex(tag); ok {
		skill.Abnormal.Params[index] = abnormal.Param{Present: true, Args: args}
	}
}

// Status tags and their native argument layouts (from the cited skills).
const (
	tagFreeze    = 0x667a     // [level, chance]            SKILL_CH_COLD_GIGONGTA
	tagFrostbite = 0x6662     // [level, chance]            SKILL_CH_COLD_GIGONGTA
	tagShock     = 0x6573     // [level, chance, 50]        SKILL_CH_LIGHTNING_GIGONGTA
	tagBurn      = 0x6275     // [level, chance, damage]    SKILL_CH_FIRE_GIGONGTA
	tagPoison    = 0x7073     // [level, chance, damage]    MSKILL_CH_WATERGHOST_ATTACK02
	tagFear      = 0x6665     // [ms, chance, grade]        MSKILL_EU_SIREN_CLON_ATTACK02
	tagMyopia    = 0x6d79     // [ms, chance, grade, %]     SKILL_EU_WARLOCK_CONFUSIONA_RANGE
	tagBleed     = 0x626c     // [ms, chance, grade, dmg]   SKILL_EU_WARRIOR_DUALA_TWIST
	tagStun      = 0x7374     // [ms, chance, grade]        SKILL_CH_SPEAR_STUN
	tagDecay     = 0x63737064 // [ms, chance, grade, %pdef] SKILL_EU_WARLOCK_RAZEA_PHYSICAL
	tagWeaken    = 0x63736d64 // [ms, chance, grade, %mdef] SKILL_EU_WARLOCK_RAZEA_MAGICAL
)

/*
================
setPetArea
================
*/
func setPetArea(skill *enterworld.SkillRow, shape uint8, meters float64, targets int, reduction uint8) {
	skill.ActionArea = enterworld.SkillOffensiveArea{Shape: shape, Radius: uint32(meters * petAreaPerMeter),
		MaxTargets: uint8(max(2, min(targets, 255))), ReductionPercent: reduction, Select: petAreaHostile}
}

/*
================
petTrainedDecide

Rewrites the native attack row for one strike on a monster: the action
the owner queued (once its row is ready), the slotted passives, then the
gear's blues.
Returns the row unchanged and nil outside the expansion.
================
*/
func (rt *Runtime) petTrainedDecide(step petCombatStep, skill enterworld.SkillRow, target petCombatTarget) (enterworld.SkillRow, *petTrainedStrike) {
	pet := step.pet
	if target.monster == nil || !rt.petExpansionAllowed(step.snapshot, pet) {
		return skill, nil
	}
	ts := &petTrainedStrike{mods: companion.ModifiersFor(pet), passive: -1, codename: pet.Codename}
	if companion.Profile(pet.Codename).MagicPrimary {
		skill.Attack.Flags = skill.Attack.Flags&^4 | 8
	}
	run := &step.state.trained
	level := uint32(max(1, pet.Level))
	hpPercent := float64(pet.CurrentHP) * 100 / float64(max(1, step.ref.MaxHP))
	// Retribution releases what it stored once its window has closed.
	if run.retribution != 0 && step.nowMs >= run.retribution {
		ts.fixed = uint32(run.retribStored * run.retribPercent / 100)
		setPetArea(&skill, petAreaAroundPet, 5, 8, 0)
		if run.retribStun {
			setPetStatus(&skill, tagStun, [6]uint32{2000, 100, petStatusGrade})
		}
		run.retribution, run.retribStored = 0, 0
	}
	if s, ok := companion.PetSkill(pet, pet.Training.Queued); ok && s.Active && s.Live() &&
		companion.RowReady(&pet.Training, s.Row, step.nowMs) {
		if lvl := companion.EffectiveLevel(pet, s, ts.mods); lvl > 0 && rt.petApplyAction(step, &skill, s, lvl, ts) {
			ts.uses = append(ts.uses, petRowUse{row: s.Row, skill: s, level: lvl})
		}
	}
	rt.petApplyPassives(step, &skill, ts, level, hpPercent)
	m := ts.mods
	for _, e := range []struct {
		add float64
		tag uint32
	}{{m.FireAdd, tagBurn}, {m.ColdAdd, tagFrostbite}, {m.ShockAdd, tagShock}, {m.PoisonAdd, tagPoison}} {
		if e.add <= 0 {
			continue
		}
		args := [6]uint32{level, petElementChance}
		if e.tag == tagBurn || e.tag == tagPoison {
			args[2] = uint32(e.add)
		} else if e.tag == tagShock {
			args[2] = 50
		}
		setPetStatus(&skill, e.tag, args)
	}
	if step.nowMs >= run.procReadyAt && rt.petChance(m.ProcChain) {
		ts.procUsed = true
		setPetArea(&skill, petAreaAroundTarget, 8, 3+int(level)/20, 20)
		setPetStatus(&skill, tagShock, [6]uint32{level, 100, 50})
	}
	if step.nowMs >= run.procReadyAt && !ts.procUsed && rt.petChance(m.ProcFire) {
		ts.procUsed = true
		setPetArea(&skill, petAreaAroundTarget, 3, 5, 0)
		setPetStatus(&skill, tagBurn, [6]uint32{level, 100, level})
	}
	if step.nowMs >= run.procReadyAt && !ts.procUsed && rt.petChance(m.ProcMend) {
		ts.procUsed = true
		ts.ownerHeal += petProcMendPercent
	}
	// Native COS damage lives mainly in the attack block, not the actor's
	// base attack. Scaling only combat.Stats left equipment almost inert.
	physical, magical := companion.AttackScales(pet.Codename, m)
	scale := physical
	if skill.Attack.Flags&8 != 0 {
		scale = magical
	}
	skill.Attack.Min = int64(float64(skill.Attack.Min) * scale)
	skill.Attack.Max = int64(float64(skill.Attack.Max) * scale)
	return skill, ts
}

/*
================
petApplyAction

The queued action of an active row. Returns false when it cannot apply
now (the queue stays for the next strike).
================
*/
func (rt *Runtime) petApplyAction(step petCombatStep, skill *enterworld.SkillRow, s companion.SkillDef, lvl uint8, ts *petTrainedStrike) bool {
	v := s.ValueAt(lvl)
	rank2, rank3 := lvl >= companion.D.Rules.Rank2Level || s.Family != "" && s.Option > 1, lvl >= companion.D.Rules.Rank3Level || s.Family != "" && s.Option > 1
	level := uint32(max(1, step.pet.Level))
	run := &step.state.trained
	switch s.EffectName() {
	case "Terrifying Howl":
		radius := 6.0
		if rank2 {
			radius = 9
		}
		ms := uint32(3000)
		if rank3 {
			ms = 4000
		}
		setPetArea(skill, petAreaAroundPet, radius, int(v), 0)
		setPetStatus(skill, tagFear, [6]uint32{ms, 100, petStatusGrade})
	case "Hamstring":
		setPetStatus(skill, tagStun, [6]uint32{uint32(v * 1000), 100, petStatusGrade})
		if rank3 {
			setPetArea(skill, petAreaAroundTarget, 3, 3, 0)
		}
	case "Rending Bite":
		skill.Attack.Percent = skill.Attack.Percent * int64(v) / 100
		bleed := max(1, level*2)
		if rank2 {
			bleed *= 2
		}
		setPetStatus(skill, tagBleed, [6]uint32{petBleedMs, 100, 1, bleed})
		skill.Abnormal.Pulse, skill.Abnormal.PulsePresent = petBleedPulseMs, true
		if rank3 {
			ts.execBleed = max(ts.execBleed, 30)
		}
	case "Frenzy":
		run.frenzyUntil, run.frenzyPercent = step.nowMs+6000, v
		run.frenzyCrit = 0
		if rank2 {
			run.frenzyCrit = 15
		}
	case "Pounce":
		skill.Attack.Percent = skill.Attack.Percent * int64(v) / 100
		if rank2 {
			setPetStatus(skill, tagStun, [6]uint32{2000, 100, petStatusGrade})
		}
		if rank3 {
			setPetArea(skill, petAreaAroundTarget, 3, 5, 50)
		}
	case "Ground Slam":
		ms := uint32(1500)
		if rank2 {
			ms = 2500
		}
		setPetArea(skill, petAreaAroundPet, 4, int(v), 0)
		setPetStatus(skill, tagStun, [6]uint32{ms, 100, petStatusGrade})
	case "Retribution":
		if run.retribution != 0 {
			return false
		}
		run.retribution, run.retribStored, run.retribPercent, run.retribStun = step.nowMs+10000, 0, v, rank3
	case "Iron Hide":
		run.ironHideUntil, run.ironHidePct = step.nowMs+6000, v
	case "Frost Nova":
		radius := 5.0
		if rank2 {
			radius = 7
		}
		skill.Attack.Percent = skill.Attack.Percent * int64(v) / 100
		setPetArea(skill, petAreaAroundPet, radius, 8, 0)
		setPetStatus(skill, tagFrostbite, [6]uint32{level + 20, 100})
	case "Deep Freeze":
		setPetStatus(skill, tagFreeze, [6]uint32{level + uint32(v*10), 100})
		if rank2 {
			setPetArea(skill, petAreaAroundTarget, 3, 3, 0)
		}
	case "Mend":
		ts.ownerHeal += v
		if rank2 {
			ts.ownerHeal += v / 2
		}
	case "Mark Prey":
		pct := uint32(v)
		if rank2 {
			pct += 20
		}
		setPetStatus(skill, tagDecay, [6]uint32{8000, 100, petStatusGrade, pct})
		setPetStatus(skill, tagWeaken, [6]uint32{8000, 100, petStatusGrade, pct})
	case "Blinding Peck":
		setPetStatus(skill, tagMyopia, [6]uint32{5000, 100, petStatusGrade, uint32(v)})
		if rank2 {
			setPetArea(skill, petAreaAroundTarget, 3, 3, 0)
		}
	default:
		return false
	}
	return true
}

/*
================
petApplyPassives

The slotted passives that change this strike.
================
*/
func (rt *Runtime) petApplyPassives(step petCombatStep, skill *enterworld.SkillRow, ts *petTrainedStrike, level uint32, hpPercent float64) {
	pet := step.pet
	run := &step.state.trained
	for row := companion.ActiveRowCount; row < companion.RowCount; row++ {
		s, ok := companion.SlottedPassive(pet, row)
		if !ok {
			continue
		}
		lvl := companion.EffectiveLevel(pet, s, ts.mods)
		v := s.ValueAt(lvl)
		rank2, rank3 := lvl >= companion.D.Rules.Rank2Level || s.Family != "" && s.Option > 1, lvl >= companion.D.Rules.Rank3Level || s.Family != "" && s.Option > 1
		every := func(n, n2 int) bool {
			if rank2 {
				n = n2
			}
			return (run.attacks+1)%n == 0
		}
		switch s.EffectName() {
		case "Execute":
			ts.execute = v
			if rank2 {
				ts.execBleed = max(ts.execBleed, v+5)
			}
		case "Bloodlust":
			if every(10, 8) {
				skill.Attack.Percent = skill.Attack.Percent * int64(v) / 100
				if rank3 {
					setPetArea(skill, petAreaAroundTarget, 3, 5, 0)
				}
			}
		case "Scent of Blood":
			ts.critWound = v
		case "Second Wind":
			ready := pet.Training.PassiveReadyAtMs[row-companion.ActiveRowCount] <= step.nowMs
			if ready && hpPercent < 30 {
				ts.petHeal += v
				ts.passive, ts.passiveMs = row, step.nowMs+int64(s.CooldownAt(lvl)*1000)
			}
		case "Crushing Paw":
			if every(6, 5) {
				setPetStatus(skill, tagStun, [6]uint32{uint32(v * 1000), 100, petStatusGrade})
				if rank3 {
					setPetArea(skill, petAreaAroundTarget, 3, 5, 0)
				}
			}
		case "Shatter":
			ts.shatter = v
		case "Chilling Touch":
			setPetStatus(skill, tagFrostbite, [6]uint32{level, uint32(min(100, v))})
		case "Static Feathers":
			if rt.petChance(v) {
				jumps := 3
				if rank2 {
					jumps = 5
				}
				setPetArea(skill, petAreaAroundTarget, 8, jumps, 20)
				setPetStatus(skill, tagShock, [6]uint32{level, 100, 50})
			}
		case "Spell Echo":
			if every(5, 4) {
				skill.Attack.Percent = skill.Attack.Percent * int64(100+v) / 100
			}
		case "Murder of Crows":
			if every(10, 8) {
				skill.Attack.Percent = skill.Attack.Percent * int64(v) / 100
				setPetArea(skill, petAreaAroundPet, 4, 8, 0)
				if rank3 {
					setPetStatus(skill, tagMyopia, [6]uint32{2000, 100, petStatusGrade, 50})
				}
			}
		case "Carrion Feast":
			ts.killHeal = v
		}
	}
	if run.frenzyUntil > step.nowMs {
		ts.critBonus += run.frenzyCrit
	}
}

/*
================
petAttackSpeed

The share an attack pet's attack interval is divided by: attack speed
passives, blues and a running Frenzy.
================
*/
func (rt *Runtime) petAttackSpeed(step petCombatStep) float64 {
	if !rt.petExpansionAllowed(step.snapshot, step.pet) {
		return 1
	}
	pct := companion.ModifiersFor(step.pet).AttackSpeedPct
	if run := &step.state.trained; run.frenzyUntil > step.nowMs {
		pct += run.frenzyPercent
	}
	return 1 + max(0, pct)/100
}

/*
================
petTrainedStats

The attacker and defender projections of one target: attack and flat
elemental damage, critical chance, and pierce against the defender.
================
*/
func petTrainedStats(ts *petTrainedStrike, target monster.Instance, attacker, defender combat.Stats) (combat.Stats, combat.Stats) {
	m := ts.mods
	attack, magic := companion.AttackScales(ts.codename, m)
	flat := m.FireAdd + m.ColdAdd + m.ShockAdd + m.PoisonAdd
	attacker.PhysicalAttackMin = (attacker.PhysicalAttackMin+m.GearStats.PhysicalAttack)*attack + flat
	attacker.PhysicalAttackMax = (attacker.PhysicalAttackMax+m.GearStats.PhysicalAttack)*attack + flat
	attacker.MagicalAttackMin = (attacker.MagicalAttackMin+m.GearStats.MagicalAttack)*magic + flat
	attacker.MagicalAttackMax = (attacker.MagicalAttackMax+m.GearStats.MagicalAttack)*magic + flat
	attacker.HitRate += m.GearStats.HitRatio
	attacker.CriticalRate += m.CritPct + ts.critBonus
	if ts.critWound > 0 && (petTargetBleeds(target) || uint64(target.CurrentHP)*2 < uint64(target.Ref.MaxHP)) {
		attacker.CriticalRate += ts.critWound
	}
	pierce := 1 - min(m.PiercePct, 90)/100
	defender.PhysicalDefense *= pierce
	defender.MagicalDefense *= pierce
	return attacker, defender
}

/*
================
petTargetBleeds / petTargetChilled
================
*/
func petTargetBleeds(target monster.Instance) bool {
	return target.Abnormal != nil && target.Abnormal.Slots[abnormal.Bleeding].Active
}

func petTargetChilled(target monster.Instance) bool {
	return target.Abnormal != nil && (target.Abnormal.Slots[abnormal.Frostbite].Active || target.Abnormal.Slots[abnormal.Freeze].Active)
}

/*
================
petTrainedDamage

Adjusts the resolved hits of one target: Retribution's fixed damage,
Shatter, crushing blow and deadly strike, then Execute.
================
*/
func (rt *Runtime) petTrainedDamage(ts *petTrainedStrike, target monster.Instance, formulas []combat.Result) {
	unique := target.Rarity()&0x0f == 3 || target.Rarity()&0x0f == 8
	total := uint32(0)
	lastHit := -1
	for i := range formulas {
		f := &formulas[i]
		if f.Blocked {
			continue
		}
		lastHit = i
		if ts.fixed > 0 {
			f.Damage = max(f.Damage, ts.fixed)
		}
		if ts.shatter > 0 && petTargetChilled(target) {
			f.Damage += uint32(float64(f.Damage) * ts.shatter / 100)
		}
		if rt.petChance(ts.mods.DeadlyPct) {
			f.Damage *= 2
		}
		if rt.petChance(ts.mods.CrushPct) {
			share := uint32(10)
			if unique {
				share = 2
			}
			f.Damage += target.CurrentHP * share / 100
		}
		total += f.Damage
	}
	threshold := ts.execute
	if petTargetBleeds(target) {
		threshold = max(threshold, ts.execBleed)
	}
	if threshold <= 0 || unique || lastHit < 0 || total == 0 || total >= target.CurrentHP {
		return
	}
	left := float64(target.CurrentHP - total)
	if left*100 < float64(target.Ref.MaxHP)*threshold {
		formulas[lastHit].Damage += target.CurrentHP - total
	}
}

/*
================
petTrainedCommit

Inside the primary hit's transaction, on the live pet: reserves the used
rows (refusing the hit when a row was spent meanwhile), counts the attack
and a kill, and applies leech and the heals. Returns false to refuse.
================
*/
func (rt *Runtime) petTrainedCommit(step petCombatStep, owner *enterworld.Character, live *domain.CharacterCOS, ts *petTrainedStrike, damage uint32, fatal, check bool) ([]wire.Frame, bool) {
	if ts == nil || ts.reserved {
		return nil, true
	}
	if live == nil {
		return nil, false
	}
	if check {
		for _, u := range ts.uses {
			if !companion.RowReady(&live.Training, u.row, step.nowMs) {
				return nil, false
			}
		}
		return nil, true
	}
	ts.reserved = true
	previousRows := live.Training.Rows
	// Count the completed attack before starting a new recharge: the cast
	// itself must not pay one of the attacks required for its next use.
	if damage > 0 {
		step.state.trained.attacks++
		companion.CountEvent(&live.Training, companion.TriggerAttacks)
	}
	// A committed healing proc also spends its recovery when the hit is blocked.
	if ts.procUsed {
		step.state.trained.procReadyAt = step.nowMs + companion.D.Rules.ProcInternalCooldownMs
	}
	if fatal {
		companion.CountEvent(&live.Training, companion.TriggerKills)
	}
	for _, u := range ts.uses {
		companion.StartRow(&live.Training, u.row, u.skill, u.level, step.nowMs, ts.mods)
		live.Training.Queued = 0
	}
	if len(ts.uses) > 0 || live.Training.Rows != previousRows {
		// The owner's window shows the spent row and the cleared queue.
		ts.ownerState = rt.petTrainingStateFrames(live, step.nowMs)
	}
	if ts.passive >= 0 {
		live.Training.PassiveReadyAtMs[ts.passive-companion.ActiveRowCount] = ts.passiveMs
	}
	var frames []wire.Frame
	maxHP := uint64(max(1, step.ref.MaxHP))
	heal := uint64(float64(damage)*ts.mods.LeechPct/100) + uint64(float64(maxHP)*ts.petHeal/100)
	if heal > 0 && live.CurrentHP > 0 {
		live.CurrentHP = uint32(min(maxHP, uint64(live.CurrentHP)+heal))
		frames = append(frames, wire.Frame{Opcode: simulation.OpVitalsUpdate,
			Payload: simulation.VitalsRefreshWithSourcePayload(live.GID, simulation.VitalsSourceSkillRecovery,
				simulation.Vitals{CurrentHP: live.CurrentHP, CurrentMP: live.CurrentMP})})
	}
	share := ts.ownerHeal
	if fatal {
		share += ts.killHeal
	}
	if share > 0 {
		if frame, ok := rt.petOwnerHeal(step.key.division, owner, share); ok {
			frames = append(frames, frame)
		}
	}
	return frames, true
}

/*
================
petOwnerHeal

Heals the owner a share of its max HP inside the caller's transaction.
================
*/
func (rt *Runtime) petOwnerHeal(division string, owner *enterworld.Character, percent float64) (wire.Frame, bool) {
	stats, _, err := rt.playerCombatStats(division, owner)
	if err != nil {
		return wire.Frame{}, false
	}
	maxHP, _, _, _ := keeperVitalsFrom(owner, stats, nil)
	frame, ok := rt.applySkillRecovery(division, owner, int64(float64(maxHP)*percent/100), 0)
	return frame, ok && frame.Opcode != 0
}

//============================================================================

/*
================
petTrainingDefense

The pet's defense against a monster: its gear and passives, Last Stand
below 30% HP and a running Iron Hide.
================
*/
func (rt *Runtime) petTrainingDefense(owner *enterworld.Character, pet *domain.CharacterCOS, maxHP uint32, stats combat.Stats) combat.Stats {
	if !rt.petExpansionAllowed(owner, pet) {
		return stats
	}
	m := companion.ModifiersFor(pet)
	pct := m.DefensePct
	if s, lvl, ok := companion.Passive(pet, "Last Stand", m); ok && maxHP > 0 {
		threshold := uint64(30)
		if lvl >= companion.D.Rules.Rank2Level || s.Family != "" && s.Option > 1 {
			threshold = 40
		}
		if uint64(pet.CurrentHP)*100 < uint64(maxHP)*threshold {
			pct += s.ValueAt(lvl)
		}
	}
	scale := 1 + pct/100
	profile := companion.Profile(pet.Codename)
	// The reference already includes the species profile; apply it only to new gear.
	stats.PhysicalDefense = (stats.PhysicalDefense + m.GearStats.PhysicalDefense*profile.DefensePercent/100) * scale
	stats.MagicalDefense = (stats.MagicalDefense + m.GearStats.MagicalDefense*profile.MagicDefensePercent/100) * scale
	stats.EvasionRate += m.GearStats.ParryRatio
	stats.ParryRate += m.GearStats.ParryRatio
	return stats
}

/*
================
petTrainedDamageTaken

A monster's hit on a trained pet: Iron Hide reduces it, Retribution
stores it. Returns the damage the pet takes.
================
*/
func (rt *Runtime) petTrainedDamageTaken(division string, owner *enterworld.Character, pet *domain.CharacterCOS, damage uint32, nowMs int64) uint32 {
	if !rt.petExpansionAllowed(owner, pet) {
		return damage
	}
	state := rt.petSessionFor(division, owner.Name, pet.GID)
	if state == nil {
		return damage
	}
	run := &state.trained
	if run.ironHideUntil > nowMs {
		damage = uint32(float64(damage) * max(0, 100-run.ironHidePct) / 100)
	}
	if run.retribution > nowMs {
		run.retribStored += float64(min(damage, pet.CurrentHP))
	}
	return damage
}

/*
================
petTrainedHitTaken

Inside the hit's transaction on the live pet: one hit taken for the
counter rows.
================
*/
func (rt *Runtime) petTrainedHitTaken(owner *enterworld.Character, live *domain.CharacterCOS) {
	if rt.petExpansionAllowed(owner, live) {
		companion.CountEvent(&live.Training, companion.TriggerHits)
	}
}
