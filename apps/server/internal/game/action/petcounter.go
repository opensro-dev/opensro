/*
===========================================================================
petcounter.go - port-only bear retaliation after ten incoming combat impacts

Runs after the victim's hit door; physical and magical hits share admission.
Shielded hits count, blocks and periodic damage do not. No player PvP effects.
===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const petCounterHits = 10
const petCounterRange = 8 * petAreaPerMeter
const petCounterPulseOpcode uint16 = 0x3fb3 // Port-only: [u32 hit-recipient gid], cosmetic ground pulse.

/*
================
petCounterHit
================
*/
type petCounterHit struct {
	division         string
	owner            *enterworld.Character
	victim, attacker uint32
	formulas         []combat.Result
	now              int64
}

/*
================
petCounterStun

Independent bear/owner counters; only the attacker is guaranteed an attempt.
Tier III rolls separately for other ordinary monsters around the hit recipient.
================
*/
func (rt *Runtime) petCounterStun(hit petCounterHit) []wire.Frame {
	if rt.Monsters == nil {
		return nil
	}
	// The counter needs a living owner holding a summoned bear; both
	// are record fields, so a dead or bear-less owner never pays for
	// its detached copy (#1943). The admitted copy feeds the gear,
	// town and session gates below, as the detached record did.
	owner := rt.characterSnapshotCompanionAdmitted(hit.division, hit.owner, func(live *enterworld.Character) bool {
		if !enterworld.CharacterAlive(live) {
			return false
		}
		for _, candidate := range live.Companions() {
			if candidate.Summoned && companion.PetKind(candidate.Codename) == "Bear" {
				return true
			}
		}
		return false
	})
	if owner == nil {
		return nil
	}
	if _, found := rt.characterMonster(hit.division, owner, hit.attacker); !found {
		return nil // Companion attackers share the hit door but cannot charge this PvE passive.
	}
	// Retail attack pets live on their summon items, independently of a transport.
	var pet *enterworld.CharacterCOS
	for _, candidate := range owner.Companions() {
		if candidate.Summoned && companion.PetKind(candidate.Codename) == "Bear" && owner.CompanionByGID(candidate.GID) == candidate {
			pet = candidate
			break
		}
	}
	if pet == nil || !pet.Summoned || pet.CurrentHP == 0 || !rt.petExpansionAllowed(owner, pet) {
		return nil
	}
	state := rt.petSessionFor(hit.division, owner.Name, pet.GID)
	if state == nil || state.follower == nil {
		return nil
	}
	ref, found := rt.cosReference(pet)
	if !found {
		return nil
	}
	skill, learned := companion.SlottedPassive(pet, companion.RowCount-1)
	index := 0
	if hit.victim != pet.GID {
		if hit.victim != enterworld.ObjectIDForCharacter(owner) {
			return nil
		}
		index = 1
	}
	if !learned || skill.EffectName() != "Crushing Paw" || index == 1 && skill.Option < 2 || rt.inTown(hit.division, owner) {
		state.trained.counterHits[index] = 0
		return nil
	}
	at := rt.companionLiveSpawn(hit.division, owner, pet, hit.now)
	if index == 1 {
		at = rt.liveSpawn(simulation.WorldKey(hit.division, owner.Name), owner, hit.now)
	}
	if townRegion(at.RegionID) {
		state.trained.counterHits[index] = 0
		return nil
	}
	level := companion.EffectiveLevel(pet, skill, companion.ModifiersFor(pet))
	row := enterworld.SkillRow{}
	// Grade zero derives stun strength from the bear's level, not a fixed level-30 grade.
	setPetStatus(&row, tagStun, [6]uint32{uint32(skill.ValueAt(level) * 1000), 100, 0})
	step := petCombatStep{key: petOwnerKey{division: hit.division}, snapshot: owner, pet: pet, ref: ref, state: state, nowMs: hit.now}
	var frames []wire.Frame
	for _, formula := range hit.formulas {
		if formula.Blocked || formula.Damage == 0 {
			continue
		}
		state.trained.counterHits[index]++
		if state.trained.counterHits[index] < petCounterHits {
			continue
		}
		state.trained.counterHits[index] = 0
		stunned := rt.applyPetCounterStun(step, row, hit.attacker)
		frames = append(frames, stunned...)
		nearby, areaStunned := 0, 0
		if skill.Option == 3 {
			frames = append(frames, wire.Frame{Opcode: petCounterPulseOpcode, Payload: wire.NewWriter(4).U32(hit.victim).Payload()})
			if lease, ok := rt.casterPopulation(hit.division, owner); ok {
				for _, mob := range rt.Monsters.CombatCandidatesInPopulation(hit.division, lease, at, petCounterRange, hit.now, false) {
					// Discovery includes body radius; the aura uses strict center distance.
					target, found := rt.resolvePetCombatTarget(step, mob.Gid)
					if !found || mob.Gid == hit.attacker || !samePlaneAdjacent(at, target.at) || areaDistance(at, target.at) > petCounterRange {
						continue
					}
					nearby++
					if rt.petChance(50) {
						extra := rt.applyPetCounterStun(step, row, mob.Gid)
						frames = append(frames, extra...)
						if len(extra) > 0 {
							areaStunned++
						}
					}
				}
			}
		}
		reason := "bear retaliation counter triggered"
		if index == 1 {
			reason = "owner retaliation counter triggered"
		}
		rt.petDiagnostic(hit.division, owner.Name, pet.GID, reason, log.Fields{
			"tier": skill.Option, "hits": petCounterHits, "victim": hit.victim, "attacker": hit.attacker,
			"stun_applied": len(stunned) > 0, "area_in_range": nearby, "area_stunned": areaStunned,
		})
	}
	return frames
}

/*
================
applyPetCounterStun

Reuse native status resolution/publication with a zero-damage plan, as Scream
Mask does. Unique monsters keep their immunity; retaliation deals no damage.
================
*/
func (rt *Runtime) applyPetCounterStun(step petCombatStep, row enterworld.SkillRow, gid uint32) []wire.Frame {
	target, ok := rt.resolvePetCombatTarget(step, gid)
	if !ok || target.monster == nil {
		return nil
	}
	mob := *target.monster
	if rarity := mob.Rarity() & 0x0f; rarity == 3 || rarity == 8 {
		return nil
	}
	actor := criticalActor{division: step.key.division, monster: step.pet.GID}
	records, err := rt.rollMonsterImpactStatus(monsterImpactCaster{division: step.key.division, actor: actor, level: step.pet.Level}, &row.Abnormal, mob)
	if err != nil || len(records) == 0 {
		return nil
	}
	plan := simulation.MonsterDamagePlan{GID: mob.Gid, ExpectedHP: mob.CurrentHP, Abnormal: records,
		AbnormalSources: rt.Monsters.PrepareAbnormalSources(step.key.division, records)}
	results, applied := rt.Monsters.ApplyDamageBatch(step.key.division, []simulation.MonsterDamagePlan{plan})
	if !applied {
		return nil
	}
	return rt.monsterImpactAbnormalFrames(step.key.division, mob.Gid, results)
}
