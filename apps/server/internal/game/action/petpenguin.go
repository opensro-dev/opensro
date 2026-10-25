/*
===========================================================================
petpenguin.go - port-only frost caster, bounded storms and spell progress.

All damage/statuses use the existing pet strike and credited reward doors.
Repeated storm pulses and echoes never feed their own counters.
===========================================================================
*/
package action

import (
	"strings"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
penguinRuntime
================
*/
type penguinRuntime struct {
	stacks    [2]uint8
	until     [2]int64
	hourglass uint8
	storms    []penguinStorm
}

/*
================
penguinStorm
================
*/
type penguinStorm struct {
	skill    enterworld.SkillRow
	center   simulation.Spawn
	next     int64
	left     uint8
	book     uint8
	extended bool
	stayed   map[uint32]bool
}

/*
================
penguinMagicGrant
================
*/
type penguinMagicGrant struct {
	percent float64
	until   int64
}

/*
================
penguinCompanion

Shares the wolf's PvE/range/lifecycle gate without requiring a wolf skill.
================
*/
func (rt *Runtime) penguinCompanion(division string, owner *enterworld.Character, now int64) (petCombatStep, bool) {
	return rt.elementalCompanion(division, owner, 0, "Penguin", now)
}

/*
================
penguinPlan
================
*/
func penguinPlan(step petCombatStep, skill *enterworld.SkillRow, ts *petTrainedStrike) {
	if companion.PetKind(step.pet.Codename) != "Penguin" {
		return
	}
	for _, use := range ts.uses {
		if use.skill.Row < 2 {
			if s, _, ok := companion.Passive(step.pet, "Frozen Hourglass", ts.mods); ok && step.state.penguin.hourglass == 5 {
				ts.hourglass = true
				ts.coldExtension = s.Option == 3
			}
			if s, _, ok := companion.Passive(step.pet, "Arcane Resonance", ts.mods); ok && s.Option == 3 && step.state.penguin.stacks[0] == 5 {
				ts.echo = true
			}
		}
	}
}

/*
================
penguinReserve

Called inside the committed primary hit transaction, after the shared row starts.
================
*/
func penguinReserve(step petCombatStep, live *enterworld.CharacterCOS, ts *petTrainedStrike) {
	if ts.hourglass {
		step.state.penguin.hourglass = 0
		for _, use := range ts.uses {
			live.Training.Rows[use.row].ReadyAtMs = max(step.nowMs, live.Training.Rows[use.row].ReadyAtMs-2000)
		}
	}
	if ts.echo {
		step.state.penguin.stacks[0] = 0
		live.ArcaneMagicPercent = 0
	}
}

/*
================
penguinProgress

One successful action, not one victim, impact, pulse or proc.
================
*/
func (rt *Runtime) penguinProgress(step petCombatStep, actor int) {
	run := &step.state.penguin
	mods := companion.ModifiersFor(step.pet)
	if s, _, ok := companion.Passive(step.pet, "Frozen Hourglass", mods); ok && (actor == 0 || s.Option >= 2) {
		run.hourglass = min(5, run.hourglass+1)
	}
	if s, _, ok := companion.Passive(step.pet, "Arcane Resonance", mods); ok && (actor == 0 || s.Option >= 2) {
		if run.until[actor] <= step.nowMs {
			run.stacks[actor] = 0
		}
		run.stacks[actor] = min(5, run.stacks[actor]+1)
		run.until[actor] = step.nowMs + 8000
	}
	rt.publishPenguinMagic(step)
}

/*
================
publishPenguinMagic

Detached keeper projection avoids authority reads inside character transactions.
================
*/
func (rt *Runtime) publishPenguinMagic(step petCombatStep) {
	run := &step.state.penguin
	pct := [2]float64{}
	if s, level, ok := companion.Passive(step.pet, "Arcane Resonance", companion.ModifiersFor(step.pet)); ok {
		for actor := range pct {
			if run.until[actor] > step.nowMs && (actor == 0 || s.Option >= 2) {
				pct[actor] = float64(run.stacks[actor]) * s.ValueAt(level)
			}
		}
	}
	if step.pet.ArcaneMagicPercent != pct[0] {
		rt.deps.Update(step.state.character, "penguin-magic", func() bool {
			if live := step.state.character.CompanionByGID(step.pet.GID); live != nil {
				live.ArcaneMagicPercent = pct[0]
			}
			return true
		})
		rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: step.key.division, OnlyCharacterID: step.state.character.ID, Frames: simFrames(rt.petTrainingStateFrames(step.state.character.CompanionByGID(step.pet.GID), step.nowMs))}})
	}
	rt.wolfHowls.Lock()
	old := rt.wolfHowls.arcane[step.key]
	if rt.wolfHowls.arcane == nil {
		rt.wolfHowls.arcane = make(map[petOwnerKey]penguinMagicGrant)
	}
	if pct[1] == 0 {
		delete(rt.wolfHowls.arcane, step.key)
	} else {
		rt.wolfHowls.arcane[step.key] = penguinMagicGrant{pct[1], run.until[1]}
	}
	rt.wolfHowls.Unlock()
	if old.percent != pct[1] {
		rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: step.key.division, OnlyCharacterID: step.state.character.ID, Frames: simFrames(rt.gaugeDropFrames(step.key.division, step.state.character, false, false, true))}})
	}
}

/*
================
penguinMagicWrites
================
*/
func (rt *Runtime) penguinMagicWrites(division string, c *enterworld.Character) []paramkeeper.Write {
	if !rt.AttackPetExpansion || c == nil || !enterworld.CharacterAlive(c) || rt.inTown(division, c) {
		return nil
	}
	now, pct := rt.Now().UnixMilli(), float64(0)
	rt.wolfHowls.Lock()
	for _, pet := range c.Companions() {
		if !pet.Summoned || pet.CurrentHP == 0 || !rt.petExpansionAllowed(c, pet) {
			continue
		}
		g := rt.wolfHowls.arcane[petOwnerKey{division: division, name: strings.ToLower(c.Name), gid: pet.GID}]
		if g.until > now {
			pct = max(pct, g.percent)
		}
	}
	rt.wolfHowls.Unlock()
	if pct == 0 {
		return nil
	}
	return []paramkeeper.Write{{Parameter: 15, Channel: paramkeeper.PercentProduct, Source: 0x7ffffb00, Value: float32(pct)}, {Parameter: 16, Channel: paramkeeper.PercentProduct, Source: 0x7ffffb00, Value: float32(pct)}}
}

/*
================
penguinShards

Frozen kills release one bounded burst. Proc damage does not re-enter this hook.
================
*/
func (rt *Runtime) penguinShards(step petCombatStep, mob monster.Instance, damage uint32) {
	s, _, ok := companion.Passive(step.pet, "Shatter", companion.ModifiersFor(step.pet))
	if !ok || s.Option != 3 || mob.Abnormal == nil || !mob.Abnormal.Has(abnormal.Freeze) || damage == 0 {
		return
	}
	lease, ok := rt.casterPopulation(step.key.division, step.snapshot)
	if !ok {
		return
	}
	center := simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}
	if mover, ok := rt.Monsters.Mover(step.key.division, mob.Gid); ok {
		at := mover.LivePoseAt(step.nowMs, nil)
		center = simulation.Spawn{RegionID: at.RegionID, X: at.X, Y: at.Y, Z: at.Z}
	}
	count := 0
	for _, candidate := range rt.Monsters.CombatCandidatesInPopulation(step.key.division, lease, center, 50, step.nowMs, false) {
		target, valid := rt.resolvePetCombatTarget(step, candidate.Gid)
		if !valid || target.monster == nil || target.gid == mob.Gid || !samePlaneAdjacent(center, target.at) || areaDistance(center, target.at) > 50 {
			continue
		}
		world, _ := step.state.follower.Presentation()
		if rt.LineOfSight != nil && !rt.LineOfSight(center, world.LiveOwnerAt(step.nowMs), target.at) {
			continue
		}
		rt.wolfEmote(step.key.division, target.gid, 0xd6)
		rt.wolfBonusHit(step, *target.monster, damage/2, "penguin-shatter")
		count++
		if count == 5 {
			break
		}
	}
}

/*
================
penguinAfterSpell
================
*/
func (rt *Runtime) penguinAfterSpell(step petCombatStep, primary petCombatTarget, skill enterworld.SkillRow, victims []petCombatTarget, strikes []monsterStrike) []simulation.Frame {
	if companion.PetKind(step.pet.Codename) != "Penguin" || step.trained == nil {
		return nil
	}
	damaged := false
	for _, hit := range strikes {
		if petStrikeDamaged(hit) {
			damaged = true
		}
		if hit.impacts[len(hit.impacts)-1].Fatal {
			for _, victim := range victims {
				if victim.monster != nil && hit.gid == victim.gid {
					var amount uint32
					for _, impact := range hit.impacts {
						amount += impact.Damage
					}
					rt.penguinShards(step, *victim.monster, amount)
				}
			}
		}
	}
	// Echo kills still qualify for Shatter, but never refill spell counters.
	if !damaged || step.trained.penguinRepeat {
		return nil
	}
	ts := step.trained
	rt.penguinProgress(step, 0)
	if len(ts.uses) == 0 {
		return nil
	}
	use := ts.uses[0]
	if use.skill.EffectName() == "Blizzard" {
		rt.wolfEmote(step.key.division, primary.gid, 0xd0+use.skill.ID-31)
	} else {
		for _, hit := range strikes {
			if petStrikeDamaged(hit) {
				rt.wolfEmote(step.key.division, hit.gid, 0xd0+use.skill.ID-31)
			}
		}
	}
	if use.skill.EffectName() == "Blizzard" {
		if len(step.state.penguin.storms) >= 4 {
			step.state.penguin.storms = step.state.penguin.storms[1:]
		}
		stayed := make(map[uint32]bool)
		for _, hit := range strikes {
			stayed[hit.gid] = true
		}
		step.state.penguin.storms = append(step.state.penguin.storms, penguinStorm{skill: skill, center: primary.at, next: step.nowMs + 1000, left: 3, book: use.skill.ID, extended: ts.coldExtension, stayed: stayed})
	}
	var echoFrames []simulation.Frame
	if ts.echo {
		copy := *ts
		copy.uses = nil
		copy.reserved = true
		copy.penguinRepeat = true
		copy.echo = false
		step.trained = &copy
		if live, ok := rt.resolvePetCombatTarget(step, primary.gid); ok {
			if hit, committed := rt.strikePetTargets(step, live, skill); committed {
				echoFrames = hit.frames
			}
		}
		if use.skill.EffectName() == "Blizzard" && len(step.state.penguin.storms) > 0 {
			clone := step.state.penguin.storms[len(step.state.penguin.storms)-1]
			if len(step.state.penguin.storms) < 4 {
				step.state.penguin.storms = append(step.state.penguin.storms, clone)
			}
		}
	}
	return echoFrames
}

/*
================
advancePenguin

At most four four-pulse storms exist; a missed simulation tick never catches up
with a damage burst. Death, separation, unslot and logout discard the storms.
================
*/
func (rt *Runtime) advancePenguin(key petOwnerKey, now int64) (output []simulation.Frame) {
	state := rt.petSessionFor(key.division, key.name, key.gid)
	if state == nil {
		return
	}
	pet := state.character.CompanionByGID(key.gid)
	if pet == nil || companion.PetKind(pet.Codename) != "Penguin" {
		return
	}
	step, eligible := rt.penguinCompanion(key.division, state.character, now)
	if !eligible {
		state.penguin = penguinRuntime{}
		step = petCombatStep{key: key, state: state, pet: state.character.CompanionByGID(key.gid), nowMs: now}
		if step.pet != nil {
			rt.publishPenguinMagic(step)
		}
		return
	}
	run := &state.penguin
	for actor := range run.stacks {
		if run.until[actor] <= now {
			run.stacks[actor] = 0
		}
	}
	if _, _, ok := companion.Passive(step.pet, "Frozen Hourglass", companion.ModifiersFor(step.pet)); !ok {
		run.hourglass = 0
	}
	if _, _, ok := companion.Passive(step.pet, "Arcane Resonance", companion.ModifiersFor(step.pet)); !ok {
		run.stacks = [2]uint8{}
		run.until = [2]int64{}
	}
	rt.publishPenguinMagic(step)
	var keep []penguinStorm
	for _, storm := range run.storms {
		if storm.left == 0 {
			continue
		}
		if now < storm.next {
			keep = append(keep, storm)
			continue
		}
		def, ok := companion.PetSkill(step.pet, storm.book)
		if !ok || companion.Level(step.pet, def) == 0 {
			continue
		}
		lease, ok := rt.casterPopulation(key.division, step.snapshot)
		if !ok {
			continue
		}
		present := make(map[uint32]bool)
		radius := float64(storm.skill.ActionArea.Radius)
		count := 0
		for _, mob := range rt.Monsters.CombatCandidatesInPopulation(key.division, lease, storm.center, radius, now, false) {
			target, valid := rt.resolvePetCombatTarget(step, mob.Gid)
			if !valid || target.monster == nil || !samePlaneAdjacent(storm.center, target.at) || areaDistance(storm.center, target.at) > radius {
				continue
			}
			world, _ := state.follower.Presentation()
			if rt.LineOfSight != nil && !rt.LineOfSight(storm.center, world.LiveOwnerAt(now), target.at) {
				continue
			}
			present[target.gid] = true
			skill := storm.skill
			if storm.left == 1 && def.Option == 3 && storm.stayed[target.gid] {
				setPetStatus(&skill, tagFreeze, [6]uint32{uint32(step.pet.Level) + 20, 100})
			}
			pulse := step
			pulse.percent = fullAreaPercent
			pulse.trained = &petTrainedStrike{mods: companion.ModifiersFor(step.pet), reserved: true, passive: -1, penguinRepeat: true, coldExtension: storm.extended}
			if s, level, ok := companion.Passive(step.pet, "Shatter", pulse.trained.mods); ok {
				pulse.trained.shatter = s.ValueAt(level)
			}
			if hit, ok := rt.strikePetTarget(pulse, target, skill); ok {
				public := hit.strike.public
				if current, found := rt.Monsters.Get(key.division, target.gid); found {
					public = append(public, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(target.gid, 0, current.CurrentHP)})
				}
				// The public audience excludes the owner, who needs the same pulse updates.
				state.public = append(state.public, public...)
				output = append(output, simFrames(public)...)
				output = append(output, simFrames(hit.actor)...)
				// Periodic damage uses native yellow text without replaying the spell cast.
				for _, impact := range hit.strike.impacts {
					if impact.Damage > 0 {
						output = append(output, simFrames([]wire.Frame{abnormalDamageFrame(target.gid, impact.Damage)})...)
					}
				}
				if hit.fatal {
					var dmg uint32
					for _, i := range hit.strike.impacts {
						dmg += i.Damage
					}
					rt.penguinShards(step, *target.monster, dmg)
				}
			}
			count++
			if count == 8 {
				break
			}
		}
		for gid := range storm.stayed {
			if !present[gid] {
				delete(storm.stayed, gid)
			}
		}
		storm.left--
		storm.next = now + 1000
		if storm.left > 0 {
			keep = append(keep, storm)
		}
	}
	run.storms = keep
	return output
}

/*
================
penguinOwnerShatter
================
*/
func (rt *Runtime) penguinOwnerShatter(division string, c *enterworld.Character, target monster.Instance, hits []combat.Result, now int64) {
	step, ok := rt.penguinCompanion(division, c, now)
	if !ok || !petTargetChilled(target) {
		return
	}
	s, level, ok := companion.Passive(step.pet, "Shatter", companion.ModifiersFor(step.pet))
	if !ok || s.Option < 2 {
		return
	}
	for i := range hits {
		wolfBonusDamage(&hits[i], s.ValueAt(level))
	}
}

/*
================
penguinOwnerHit
================
*/
func (rt *Runtime) penguinOwnerHit(division string, c *enterworld.Character, skill enterworld.SkillRow, victims []monster.Instance, hits []combat.Result, now int64) {
	step, ok := rt.penguinCompanion(division, c, now)
	if !ok || len(victims) == 0 {
		return
	}
	damaged := false
	for _, hit := range hits {
		if !hit.Blocked && hit.Damage > 0 {
			damaged = true
		}
	}
	if !damaged {
		return
	}
	if skill.Attack.Flags&8 != 0 {
		rt.penguinProgress(step, 1)
	}
	per := len(hits) / len(victims)
	for i, mob := range victims {
		if live, found := rt.Monsters.Get(division, mob.Gid); found && live.CurrentHP == 0 {
			var damage uint32
			for _, hit := range hits[i*per : (i+1)*per] {
				if !hit.Blocked {
					damage += hit.Damage
				}
			}
			rt.penguinShards(step, mob, damage)
		}
	}
}
