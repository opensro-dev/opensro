/*
===========================================================================
petbond.go - port-only Guardian's Bond admission, HP transfer and auto revival

Resolve other actors before a character write; debit the bear after it closes.
Transferred damage never re-enters combat counters or another damage link.
===========================================================================
*/
package action

import (
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/simulation"
)

const petBondRange = 120.0          // 12 metres.
const petBondOpcode uint16 = 0x3fb4 // [recipient u32, bear u32, kind u8: transfer=1/revive=2].

/*
================
petBondRuntime / petBondPlan
================
*/
type petBondRuntime struct {
	window int64
	spent  uint32
}
type petBondPlan struct {
	owner           *enterworld.Character
	state           *petSession
	division        string
	bear, recipient uint32
	percent         float64
	budget, moved   uint32
	now             int64
}

/*
================
petBondFrame
================
*/
func petBondFrame(recipient, bear uint32, kind uint8) wire.Frame {
	return wire.Frame{Opcode: petBondOpcode, Payload: wire.NewWriter(9).U32(recipient).U32(bear).U8(kind).Payload()}
}

/*
================
bondSource

The same opt-in, session, job, world and town gates protect transfer and revival.
Owner death is allowed here so a living bear can revive its own owner.
================
*/
func (rt *Runtime) bondSource(key petOwnerKey, state *petSession) (*enterworld.Character, *domain.CharacterCOS, companion.SkillDef, uint8, bool) {
	if !rt.AttackPetExpansion || state == nil || !state.ready || state.follower == nil {
		return nil, nil, companion.SkillDef{}, 0, false
	}
	// Runs for every bear source per bond plan: copy the owner only when
	// the record still holds a live summoned bear under this gid, and
	// only its equipment rows - no reader of this copy leaves the worn
	// equipment or reaches the avatar inventory (#1943).
	owner := rt.characterSnapshotEquipmentAdmitted(key.division, state.character, func(c *enterworld.Character) bool {
		pet := c.CompanionByGID(key.gid)
		return !c.DeletePending && pet != nil && pet.CurrentHP != 0 && companion.PetKind(pet.Codename) == "Bear"
	})
	if owner == nil || owner.DeletePending {
		return nil, nil, companion.SkillDef{}, 0, false
	}
	pet := owner.CompanionByGID(key.gid)
	if pet == nil || !pet.Summoned || pet.CurrentHP == 0 || companion.PetKind(pet.Codename) != "Bear" || !rt.petExpansionAllowed(owner, pet) || rt.inTown(key.division, owner) {
		return nil, nil, companion.SkillDef{}, 0, false
	}
	skill, level, ok := companion.Passive(pet, "Guardian's Bond", companion.ModifiersFor(pet))
	return owner, pet, skill, level, ok
}

/*
================
bondRecipient
================
*/
func (rt *Runtime) bondRecipient(key petOwnerKey, owner, recipient *enterworld.Character, pet *domain.CharacterCOS, skill companion.SkillDef, now int64) bool {
	if recipient == nil || recipient.DeletePending || !rt.petExpansionAllowed(recipient, pet) || rt.inTown(key.division, recipient) {
		return false
	}
	if recipient.Name != owner.Name && (skill.Option < 3 || !rt.auraParty(key.division, owner)[enterworld.ObjectIDForCharacter(recipient)]) {
		return false
	}
	session := rt.petSessionFor(key.division, recipient.Name, 0)
	if session == nil || !session.ready {
		return false
	}
	a := rt.companionLiveSpawn(key.division, owner, pet, now)
	b := rt.liveSpawn(simulation.WorldKey(key.division, recipient.Name), recipient, now)
	return samePlaneAdjacent(a, b) && areaDistance(a, b) <= petBondRange
}

/*
================
planPetBond

One strongest bond, never stacked. The party shares a 20%-of-bear-max-HP/sec
budget, and transferred HP costs cannot kill the bear. No nested store reads.
================
*/
func (rt *Runtime) planPetBond(division string, victim *enterworld.Character, now int64) *petBondPlan {
	if !rt.AttackPetExpansion || victim == nil {
		return nil
	}
	type source struct {
		key   petOwnerKey
		state *petSession
	}
	var sources []source
	rt.petMu.Lock()
	for key, state := range rt.petSessions {
		if key.division == division && key.gid != 0 && state.ready {
			sources = append(sources, source{key, state})
		}
	}
	rt.petMu.Unlock()
	sort.Slice(sources, func(i, j int) bool { return sources[i].key.gid < sources[j].key.gid })
	var best *petBondPlan
	for _, source := range sources {
		owner, pet, skill, level, ok := rt.bondSource(source.key, source.state)
		if !ok || skill.Option < 2 || !enterworld.CharacterAlive(owner) || !rt.bondRecipient(source.key, owner, victim, pet, skill, now) {
			continue
		}
		percent := min(float64(skill.Option)*10, skill.ValueAt(min(level, skill.MaxLevel)))
		ref, found := rt.cosReference(pet)
		if !found || pet.CurrentHP <= 1 {
			continue
		}
		spent := uint32(0)
		rt.petMu.Lock()
		if now-source.state.bond.window < 1000 {
			spent = source.state.bond.spent
		}
		rt.petMu.Unlock()
		cap := max(uint32(1), ref.MaxHP/5)
		if spent >= cap {
			continue
		}
		budget := min(cap-spent, pet.CurrentHP-1)
		if best == nil || percent > best.percent {
			best = &petBondPlan{owner: source.state.character, state: source.state, division: division, bear: pet.GID, recipient: enterworld.ObjectIDForCharacter(victim), percent: percent, budget: budget, now: now}
		}
	}
	return best
}

/*
================
sharePetBond

Called only for monster damage remaining after shields and mana redirection.
================
*/
func sharePetBond(plan *petBondPlan, damage uint32) uint32 {
	if plan == nil || plan.moved >= plan.budget {
		return damage
	}
	moved := min(uint32(float64(damage)*plan.percent/100), plan.budget-plan.moved)
	plan.moved += moved
	return damage - moved
}

/*
================
commitPetBond
================
*/
func (rt *Runtime) commitPetBond(plan *petBondPlan) []wire.Frame {
	if plan == nil || plan.moved == 0 {
		return nil
	}
	moved := plan.moved
	plan.moved = 0 // Publication may be requested more than once.
	// The bear's HP may have fallen since the plan: debit only what it
	// can still pay, and never to death - the booked spent and the
	// published debit are the clamped amount, not the planned share
	// (checklist item 5's fatal over-charge).
	var hp uint32
	paid := uint32(0)
	if !rt.deps.UpdateDeferred(plan.owner, "pet-bond-transfer", func() bool {
		pet := plan.owner.CompanionByGID(plan.bear)
		if pet == nil || !pet.Summoned || pet.CurrentHP <= 1 {
			return false
		}
		paid = min(moved, pet.CurrentHP-1)
		pet.CurrentHP -= paid
		hp = pet.CurrentHP
		return true
	}) {
		return nil
	}
	rt.petMu.Lock()
	if plan.now-plan.state.bond.window >= 1000 {
		plan.state.bond = petBondRuntime{window: plan.now}
	}
	plan.state.bond.spent += paid
	rt.petMu.Unlock()
	return []wire.Frame{{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(plan.bear, 0, hp)}, petBondFrame(plan.recipient, plan.bear, 1)}
}

/*
================
advancePetBond

Every tier revives the owner; III also revives party members. Tier-I ranks
scale HP/EXP recovery and the saved shared cooldown, surviving resummon.
================
*/
func (rt *Runtime) advancePetBond(key petOwnerKey, state *petSession, now int64) []simulation.Frame {
	owner, pet, skill, _, ok := rt.bondSource(key, state)
	if !ok {
		return nil
	}
	rank, cooldown := companion.GuardianRevival(pet)
	if rank == 0 || pet.Training.PassiveReadyAtMs[skill.Row-companion.ActiveRowCount] > now {
		return nil
	}
	gids := []uint32{enterworld.ObjectIDForCharacter(owner)}
	var party []uint32
	for gid := range rt.auraParty(key.division, owner) {
		if skill.Option == 3 && gid != gids[0] {
			party = append(party, gid)
		}
	}
	sort.Slice(party, func(i, j int) bool { return party[i] < party[j] })
	gids = append(gids, party...)
	for _, gid := range gids {
		target := rt.findCharacterByGid(key.division, gid)
		snapshot := rt.characterSnapshot(key.division, target)
		if snapshot == nil || enterworld.CharacterAlive(snapshot) || snapshot.DeathKind != uint8(pk.DeathMonster) || rewardLevel(snapshot) > int64(pet.Level)+10 || !rt.bondRecipient(key, owner, snapshot, pet, skill, now) {
			continue
		}
		stats, _, err := rt.playerCombatStats(key.division, snapshot)
		if err != nil {
			continue
		}
		maxHP, _, _, _ := keeperVitalsFrom(snapshot, stats, nil)
		actors := []*enterworld.Character{state.character}
		if target != state.character {
			actors = append(actors, target)
		}
		var revived revivedWhereDead
		if !rt.deps.UpdateManyDeferred(actors, "pet-bond-revival", func() bool {
			live := state.character.CompanionByGID(pet.GID)
			if live == nil || !live.Summoned || live.CurrentHP == 0 || live.Training.PassiveReadyAtMs[skill.Row-companion.ActiveRowCount] > now || target.DeletePending || enterworld.CharacterAlive(target) {
				return false
			}
			live.Training.PassiveReadyAtMs[skill.Row-companion.ActiveRowCount] = now + cooldown
			loss := max(int64(0), target.LastExpLoss)
			refund := loss/10*int64(rank) + loss%10*int64(rank)/10
			revived = rt.reviveWhereDead(key.division, target, resurrectionOffer{hp: max(int64(0), maxHP*int64(rank)/10-1), exp: refund}, now)
			return true
		}) {
			return nil
		}
		// A bond revival settles the target's death: any pending
		// resurrection offer for it is stale from here and must not
		// overwrite the revival's vitals/position (checklist item 4).
		rt.resurrections.drop(key.division, target.Name)
		actor, peers := rt.revivalFrames(key.division, target, revived, now)
		pulse := petBondFrame(gid, pet.GID, 2)
		actor = append(actor, pulse)
		if target.ID != state.character.ID {
			// Party revival: the revived member gets only its private
			// set plus the pulse; the peer set plus pulse broadcasts to
			// the bear's owner and every other observer exactly once
			// (the public lane would exclude the owner and double-send
			// to the revived member).
			state.routed = append(state.routed, simulation.DivisionFrames{
				DivisionID:        key.division,
				SourceGID:         pet.GID,
				ExceptCharacterID: target.ID,
				Frames:            simFrames(append(peers, pulse)),
			})
		} else {
			state.public = append(state.public, append(peers, pulse)...)
		}
		state.others = append(state.others, RecipientFrames{CharacterID: target.ID, Frames: actor})
		return simFrames(rt.petTrainingStateFrames(state.character.CompanionByGID(pet.GID), now))
	}
	return nil
}
