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

lkdh's MP for one batch of impacts: percent of each impact's HP debit,
truncated, each capped at the link's ceiling. Inferred: "per hit" is per
impact record, so a multi-impact skill caps every impact on its own, and
the damage is the HP the impact took, as Tuning drains it.
================
*/
func linkedManaShare(impacts []simulation.MonsterDamageResult, percent, ceiling uint32) int64 {
	var mp int64
	for _, impact := range impacts {
		share := uint64(impact.Applied) * uint64(percent) / fullDamagePercent
		mp += int64(min(share, uint64(ceiling)))
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
	for _, link := range rt.effects.ManaLinks(division, c.Name, now) {
		mp := linkedManaShare(impacts, link.ManaPercent, link.ManaCap)
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
