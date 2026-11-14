/*
===========================================================================
petcrowkit.go - port-only Raven's Refuge and recipient buff publication.

Admit sources before the character door; HP, healing and the persisted
recipient cooldown commit together. Several crows never stack protection.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const crowPartyBookOption = 3
const ravenRefugeHealingBookID = 59

const ravenRefugeFamily = "Raven's Refuge"
const ravenRefugeReduction = 15
const ravenRefugeHealPercent = 10

/*
================
crowKit / ravenRefugeGrant
================
*/
type crowKit struct{ views map[uint32][8]byte }
type ravenRefugeGrant struct {
	threshold float64
	cooldown  int64
	book      uint8
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
planRavenRefuge

Only live, nearby, expansion-admitted crows may protect this recipient.
================
*/
func (rt *Runtime) planRavenRefuge(division string, c *enterworld.Character, now int64) ravenRefugeGrant {
	var best ravenRefugeGrant
	for _, step := range rt.crowSources(division, c, now) {
		skill, level, ok := companion.Passive(step.pet, ravenRefugeFamily, companion.ModifiersFor(step.pet))
		if !ok || skill.Option < crowPartyBookOption && enterworld.ObjectIDForCharacter(c) != enterworld.ObjectIDForCharacter(step.snapshot) {
			continue
		}
		value := skill.ValueAt(level)
		if value > best.threshold || value == best.threshold && skill.ID > best.book {
			best = ravenRefugeGrant{threshold: value, cooldown: int64(skill.CooldownAt(level) * 1000), book: skill.ID}
		}
	}
	return best
}

/*
================
ravenRefugeDamage / ravenRefugeRecovery

Protection requires HP already below the threshold. Recovery requires a
survived downward crossing: it never resurrects or spends cooldown on death.
================
*/
func ravenRefugeDamage(grant ravenRefugeGrant, hp, maximum int64, damage uint32) uint32 {
	if grant.threshold > 0 && float64(hp)*100 < float64(maximum)*grant.threshold {
		return uint32(uint64(damage) * (100 - ravenRefugeReduction) / 100)
	}
	return damage
}
func ravenRefugeRecovery(grant ravenRefugeGrant, c *enterworld.Character, before, after, maximum, now int64) int64 {
	threshold := float64(maximum) * grant.threshold
	if grant.book < ravenRefugeHealingBookID || after <= 0 || after >= before || float64(before)*100 < threshold || float64(after)*100 >= threshold || c.RavenRefugeReadyAtMs > now {
		return 0
	}
	heal := min(maximum-after, maximum*ravenRefugeHealPercent/100)
	if heal <= 0 {
		return 0
	}
	c.RavenRefugeReadyAtMs = now + grant.cooldown
	return heal
}

/*
================
advanceCrowKit / advanceCrowKitSession

Retire departed recipients and publish only when their conditional buff changes.
================
*/
func (rt *Runtime) advanceCrowKit(key petOwnerKey, now int64) {
	rt.advanceCrowKitSession(key, rt.petSessionFor(key.division, key.name, key.gid), now)
}
func (rt *Runtime) advanceCrowKitSession(key petOwnerKey, state *petSession, now int64) {
	if state == nil {
		return
	}
	q := elementalQuery{gid: key.gid, kind: "Crow"}
	if len(state.crow.views) == 0 {
		q.passive = ravenRefugeFamily
	}
	step, ok := rt.elementalCompanionMatching(key.division, state.character, q, now)
	members := map[uint32]*enterworld.Character{}
	if ok {
		skill, _, learned := companion.Passive(step.pet, ravenRefugeFamily, companion.ModifiersFor(step.pet))
		if learned {
			members = rt.crowSupportRecipients(step, skill.Option == crowPartyBookOption, false)
		}
	}
	if state.crow.views == nil && len(members) > 0 {
		state.crow.views = make(map[uint32][8]byte)
	}
	for gid := range members {
		if _, found := state.crow.views[gid]; !found {
			state.crow.views[gid] = [8]byte{}
		}
	}
	for gid, previous := range state.crow.views {
		view := rt.crowPrecisionView(key.division, gid, now)
		if view != previous {
			rt.publishCrowPrecision(key.division, gid, now)
			state.crow.views[gid] = view
		}
		if members[gid] == nil {
			delete(state.crow.views, gid)
		}
	}
}

/*
================
crowPrecisionView / publishCrowPrecision

Keep the existing eight-byte recipient-buff wire contract. The former stack
slot now carries one while Refuge protects; percent is damage reduction.
================
*/
func (rt *Runtime) crowPrecisionView(division string, gid uint32, now int64) [8]byte {
	c := rt.findCharacterByGid(division, gid)
	var book, active uint8
	var percent uint16
	if c != nil {
		grant := rt.planRavenRefuge(division, c, now)
		maximum, _, hp, _ := rt.playerKeeperVitals(division, c)
		if grant.book != 0 && hp > 0 && float64(hp)*100 < float64(maximum)*grant.threshold {
			book, active, percent = grant.book, 1, ravenRefugeReduction*10
		}
	}
	var out [8]byte
	copy(out[:], wire.NewWriter(8).U32(gid).U8(book).U8(active).U16(percent).Payload())
	return out
}
func (rt *Runtime) publishCrowPrecision(division string, gid uint32, now int64) {
	if rt.findCharacterByGid(division, gid) == nil {
		return
	}
	view := rt.crowPrecisionView(division, gid, now)
	rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: division, SourceGID: gid, Frames: simFrames([]wire.Frame{{Opcode: 0x3fb9, Payload: view[:]}})}})
}
