/*
===========================================================================

linkeddamage.go - fence and Pain Quota links taking part of a member's hit

CSkillManager_ProcessDamageEffects (5A0B80) runs on the recipient of every
hit before the hit lands. The Warrior's Physical and Magical Fence (lkdr,
the recipient's +2C8 list, 5A0F01) move part of the hit to the Warrior;
Pain Quota (lkdd, +20C, 5A11BF) keeps part and divides the rest among the
recipient's party members within quotaMemberRange. Both run before dgmp
(5A13FE). combat.FenceShare and combat.QuotaShare own the arithmetic.

The moved shares register on a queued area context the attacker's manager
drains (tagAreaEffectContext_Register), so each taker receives its share as
the attacker's hit, published as a pulse result like returned damage.
INFERENCE: a moved share does not run its taker's own links again; the
queued context is not a hit outcome, as damagereturn.go reasons for dmgr.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// quotaMemberRange is 5A131C's B45C68: the distance within which a party
// member takes a Pain Quota share.
const quotaMemberRange = 1000.0

/*
================
linkedMove

One share a link moved off a recipient's hit: who takes it and how much.
================
*/
type linkedMove struct {
	gid    uint32
	amount uint32
}

/*
================
planQuotaMembers

5A1257..5A1337 before the victim's door (the members' positions are read
through their own doors): the victim's party members other than the
victim, alive, within quotaMemberRange of the victim. Empty when the
victim holds no Pain Quota link.
================
*/
func (rt *Runtime) planQuotaMembers(division string, victim *enterworld.Character, now int64) []uint32 {
	if _, ok := rt.effects.QuotaLink(division, victim.Name, now); !ok {
		return nil
	}
	snapshot := rt.characterSnapshot(division, victim)
	if snapshot == nil {
		return nil
	}
	at := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	from := monster.Pose{RegionID: at.RegionID, X: at.X, Y: at.Y, Z: at.Z}
	self := enterworld.ObjectIDForCharacter(snapshot)
	var out []uint32
	for gid := range rt.auraParty(division, snapshot) {
		if gid == self {
			continue
		}
		member := rt.characterSnapshot(division, rt.findCharacterByGid(division, gid))
		if member == nil || !enterworld.CharacterAlive(member) {
			continue
		}
		to := rt.liveSpawn(simulation.WorldKey(division, member.Name), member, now)
		if float64(monster.NativeActorDistance(from, monster.Pose{RegionID: to.RegionID, X: to.X, Y: to.Y, Z: to.Z})) > quotaMemberRange {
			continue
		}
		out = append(out, gid)
	}
	return out
}

/*
================
shareLinkedDamageInDoor

5A0F01 then 5A11BF for one impact inside the victim's door: every fence
link moves its lanes to its source and counts the hit, then the Pain Quota
link divides what is left among the planned members. Returns the hit the
victim keeps; the moves are appended to out.
================
*/
func (rt *Runtime) shareLinkedDamageInDoor(s playerStrike, formula combat.Result, out *playerStruck) combat.Result {
	for _, link := range rt.effects.FenceLinks(s.division, s.victim.Name, s.now) {
		var moved uint32
		formula, moved = combat.FenceShare(link.FenceMask, link.FencePercent, formula)
		if moved == 0 {
			continue
		}
		out.linkMoves = append(out.linkMoves, linkedMove{gid: link.SourceGID, amount: moved})
		rt.effects.CountFenceHit(s.division, link.SourceToken)
	}
	if link, ok := rt.effects.QuotaLink(s.division, s.victim.Name, s.now); ok && len(s.quotaMembers) != 0 {
		var share uint32
		formula, share = combat.QuotaShare(link.QuotaPercent, formula, len(s.quotaMembers))
		if share != 0 {
			for _, gid := range s.quotaMembers {
				out.linkMoves = append(out.linkMoves, linkedMove{gid: gid, amount: share})
			}
		}
	}
	return formula
}

/*
================
strikeLinkedShares

After the victim's door: each moved share lands on its taker as the
attacker's hit (source, skill), through the shared player strike with the
taker's own links off, and is published as a pulse result with the
taker's own struck frames.
================
*/
func (rt *Runtime) strikeLinkedShares(division string, source uint32, killer deathKiller, skill enterworld.SkillRow, moves []linkedMove, now int64) OpResult {
	var result OpResult
	for _, move := range moves {
		taker := rt.findCharacterByGid(division, move.gid)
		if taker == nil {
			continue
		}
		strike := playerStrike{division: division, victim: taker, killer: killer, skill: skill, impacts: 1, linkedShare: true, now: now}
		formula := combat.Result{Damage: min(move.amount, wire.MaxSkillActionDamage), ResultFlags: 1}
		if !rt.planPlayerStrike(&strike,
			func(*enterworld.SkillWall) (combat.WallOutcome, error) {
				return combat.WallOutcome{Defender: formula}, nil
			},
			func(*enterworld.SkillWall, combat.Result) ([]abnormal.Record, error) { return nil, nil }) {
			continue
		}
		var struck playerStruck
		if !rt.deps.Update(taker, "linked-damage-share", func() bool {
			if taker.DeletePending || !enterworld.CharacterAlive(taker) {
				return false
			}
			struck = rt.strikePlayerInDoor(strike)
			return len(struck.impacts) > 0
		}) {
			continue
		}
		if struck.fatal {
			rt.bindResidentRegion(simulation.WorldKey(division, taker.Name), now)
		}
		public, private := rt.playerStruckFrames(division, taker, struck, now)
		pulse := wire.SkillPulseFrame(source, skill.ID, []wire.SkillAreaTarget{
			{GID: move.gid, Impacts: struck.impacts},
		})
		result.Broadcast = append(append(result.Broadcast, pulse), public...)
		if len(private) != 0 {
			result.Recipients = append(result.Recipients, RecipientFrames{CharacterID: taker.ID, Frames: private})
		}
	}
	return result
}
