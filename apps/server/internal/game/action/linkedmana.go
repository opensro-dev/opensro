/*
===========================================================================

linkedmana.go - Mana Switch: a linked member's damage refills the Bard's MP

The Bard's Mana Switch is the lnks pair of skilllinkedeffect.go with lkdh
(enterworld.SkillEffectLink.Mana): while the link holds, every hit the
recipient lands on a monster hands the link's source a share of the damage
as MP. The share is fed where the attacker's committed damage is known,
the same place linked threat is split for Protect (commitSkillHostility).
Range, death, expiry and the Bard's harp are the link's own lifecycle
(linkedlifecycle.go, equipmentreqi.go): a retired link feeds nothing.

Owner's rule: a link to an ally (never the Bard), 20 s, range 700, harp
required; when the ally damages an enemy, 50 % of the damage, at most 1596
per hit (lkdh), becomes the Bard's MP.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
linkedManaShare

lkdh's MP for one batch of impacts, per impact as
CSkillManager_DistributeSharedDamage (5A04A0) runs per hit record: the HP
the impact took (SkillCombat_ApplyResultRecipients passes the damage, or
the remaining HP on a fatal hit) is skipped when it is 1 or less
(593BDD), divided by the links the attacker holds and stored as a float32
share; the MP is ftol(share * (word 1 / 100.0)), held at word 2.
================
*/
func linkedManaShare(impacts []simulation.MonsterDamageResult, percent, ceiling uint32, held int) int64 {
	if held <= 0 {
		return 0
	}
	var mp int64
	for _, impact := range impacts {
		if impact.Applied <= 1 {
			continue
		}
		share := float32(impact.Applied / uint32(held))
		mp += int64(min(uint32(crtFtol(float64(share)*(float64(percent)/fullDamagePercent))), ceiling))
	}
	return mp
}

/*
================
commitLinkedMana

Give every live Mana Switch source linked to attacker its share, inside
the source's door, and push the source its 0x33A6. Inferred: every Bard
linked to the attacker gets its own share (the threat link is a single
pointer, but nothing makes the MP links exclusive); an attacker that is
not a character (a tempted monster) feeds nothing, and a pet's hit, which
commits under its owner's gid, feeds its owner's links. The gain is a
skill recovery, as for Tuning (skilltuning.go).
================
*/
func (rt *Runtime) commitLinkedMana(division string, attacker uint32, impacts []simulation.MonsterDamageResult, now int64) {
	if rt.effects == nil || len(impacts) == 0 {
		return
	}
	c := rt.findCharacterByGid(division, attacker)
	if c == nil {
		return
	}
	links, held := rt.effects.ManaLinks(division, c.Name, now)
	for _, link := range links {
		mp := linkedManaShare(impacts, link.ManaPercent, link.ManaCap, held)
		source := rt.findCharacter(division, link.SourceName)
		if mp == 0 || source == nil || enterworld.ObjectIDForCharacter(source) != link.SourceGID {
			continue
		}
		var frame wire.Frame
		rt.deps.Update(source, "linked-damage-mana", func() bool {
			if source.DeletePending {
				return false
			}
			var ok bool
			frame, ok = rt.applySkillRecovery(division, source, 0, mp)
			return ok
		})
		if frame.Opcode != 0 && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(division, source.Name, []wire.Frame{frame})
		}
	}
}
