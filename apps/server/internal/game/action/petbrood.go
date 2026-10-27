/*
===========================================================================
petbrood.go - port-only destructible crow eggs and temporary basic-attack clones.

The character owns transient actors; the normal companion combat/visibility
lanes own their targets, damage and rewards. No summon survives logout.
===========================================================================
*/
package action

import (
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
const broodWindOpcode uint16 = 0x3FBA
const broodDeathMultiplier = 5

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
	snapshot := rt.characterSnapshot(key.division, state.character)
	source := snapshot.CompanionByGID(key.gid)
	if source == nil {
		for _, candidate := range snapshot.Companions() {
			if candidate.GID == key.gid && candidate.BroodStage == 0 && companion.PetKind(candidate.Codename) == "Crow" {
				source = candidate
				break
			}
		}
	}
	if source != nil && companion.PetKind(source.Codename) != "Crow" || key.gid >= broodGIDBase {
		return nil
	}
	valid := source != nil && rt.AttackPetExpansion && source.Summoned && source.CurrentHP > 0 && enterworld.CharacterAlive(snapshot) && !rt.inTown(key.division, snapshot)
	var output []wire.Frame
	var reuse []wire.Frame
	rt.deps.UpdateDeferred(state.character, "crow-brood-recharge", func() bool {
		live := state.character.CompanionByGID(key.gid)
		if live == nil {
			return false
		}
		row := companion.AdvanceCharges(live.Training.Rows[1], now)
		for _, def := range companion.SkillsForPet(live) {
			if def.EffectName() == companion.BroodSkill && companion.Level(live, def) > 0 {
				row = companion.BroodRow(row, def, companion.EffectiveLevel(live, def, companion.ModifiersFor(live)), now, companion.ModifiersFor(live))
			}
		}
		if row == live.Training.Rows[1] {
			return false
		}
		live.Training.Rows[1] = row
		reuse = rt.petTrainingStateFrames(live, now)
		return true
	})
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
			rt.deps.Update(state.character, "crow-brood-remove", func() bool {
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
			rt.petMu.Unlock()
			continue
		}
		if hatch {
			var clone *domain.CharacterCOS
			rt.deps.Update(state.character, "crow-brood-hatch", func() bool {
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
				rt.orderPetAttack(key.division, state.character, rt.characterSnapshot(key.division, state.character), pet.GID, state.combat.target, now)
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
			if current, found := rt.Monsters.Get(key.division, target.gid); found {
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
