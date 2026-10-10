/*
===========================================================================

screammask.go - the Warlock's Scream Mask stuns whoever strikes the masked
member

Scream Mask (SKILL_EU_WARLOCK_SOULA_STUNLINK_A) links the Warlock to a party
member: lnks dura abnb {range} st {..} reqi. ApplyBuffModifiersToActor
(594EB9) installs the recipient half in the member's ParamKeeper+200
(statuseffect.ScreamLink). On every hit the member takes,
CSkillManager_ProcessDamageEffects (5A14D8..5A15B6) checks that slot: an
attacker on the member's plane strictly inside abnb's range rolls the
row's status blocks through SkillCombat_RollAbnormalStatus (590680) with
the member as caster (ESI: its level and getv modifiers) and the attacker
as target (ECX), against the attacker's current cast's wall context
(+C0C -> RefSkill+2B4); 59EC90 applies the records to the attacker's own
state (+A30). No hit record is sent: the attacker's status frames are the
whole publication.

INFERENCE: the roll runs once per attack that lands on the member, after
the attack's door, as damage return's attacker half does (5A14D8 sits once
in the hit's ProcessDamageEffects pass, after the fence and quota shares).

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
screamMaskSource

The member's live Scream Mask link and its row, when the attacker at to
stands strictly inside the link's range (5A1557..5A157E) on the member's
plane (430CE0).
================
*/
func (rt *Runtime) screamMaskSource(division string, member *enterworld.Character, to monster.Pose, now int64) (*enterworld.Character, enterworld.SkillRow, bool) {
	snapshot := rt.characterSnapshot(division, member)
	if snapshot == nil || rt.effects == nil || rt.deps.SkillData() == nil {
		return nil, enterworld.SkillRow{}, false
	}
	link, ok := rt.effects.ScreamLink(division, snapshot.Name, now)
	if !ok {
		return nil, enterworld.SkillRow{}, false
	}
	row, known := rt.deps.SkillData().SkillByID(link.SkillID)
	if !known || !row.Abnormal.Present() {
		return nil, enterworld.SkillRow{}, false
	}
	at := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	if !worldgeom.SamePlane(at.RegionID, to.RegionID) {
		return nil, enterworld.SkillRow{}, false
	}
	from := monster.Pose{RegionID: at.RegionID, X: at.X, Y: at.Y, Z: at.Z}
	if float64(monster.NativeActorDistance(from, to)) >= float64(link.ScreamRange) {
		return nil, enterworld.SkillRow{}, false
	}
	return snapshot, row, true
}

/*
================
screamMaskMonster

The monster branch: the records land on the attacking monster through a
zero-damage, uncredited plan that carries only them; a monster casts
behind no wall.
================
*/
func (rt *Runtime) screamMaskMonster(division string, member *enterworld.Character, attacker monster.Instance, attackerPose monster.Pose, now int64) OpResult {
	snapshot, row, ok := rt.screamMaskSource(division, member, attackerPose, now)
	if !ok {
		return OpResult{}
	}
	live, exists := rt.Monsters.Get(division, attacker.Gid)
	if !exists || live.CurrentHP == 0 {
		return OpResult{}
	}
	records, err := rt.rollPlayerOnMonster(division, snapshot, &row.Abnormal, live)
	if err != nil || len(records) == 0 {
		return OpResult{}
	}
	plan := simulation.MonsterDamagePlan{GID: live.Gid, ExpectedHP: live.CurrentHP, Abnormal: records,
		AbnormalSources: rt.Monsters.PrepareAbnormalSources(division, records)}
	results, applied := rt.Monsters.ApplyDamageBatch(division, []simulation.MonsterDamagePlan{plan})
	if !applied {
		return OpResult{}
	}
	return OpResult{Broadcast: rt.monsterImpactAbnormalFrames(division, live.Gid, results)}
}

/*
================
screamMaskPlayer

The player branch: the records land on the attacking player through a
zero-damage strike in its door, with the member as the killer of record;
the attacker's standing wall is its current cast's (590680's wall
argument). The strike is a linked one, so the attacker's own links do not
run on it.
================
*/
func (rt *Runtime) screamMaskPlayer(division string, member, attacker *enterworld.Character, now int64) OpResult {
	view := rt.characterSnapshot(division, attacker)
	if view == nil || !enterworld.CharacterAlive(view) {
		return OpResult{}
	}
	at := rt.liveSpawn(simulation.WorldKey(division, view.Name), view, now)
	snapshot, row, ok := rt.screamMaskSource(division, member, monster.Pose{RegionID: at.RegionID, X: at.X, Y: at.Y, Z: at.Z}, now)
	if !ok {
		return OpResult{}
	}
	casterStats, _, err := rt.playerCombatStats(division, snapshot)
	if err != nil {
		return OpResult{}
	}
	defender, _, err := rt.playerCombatStats(division, view)
	if err != nil {
		return OpResult{}
	}
	strikeRow := row
	strikeRow.StatusCast = true
	strike := playerStrike{division: division, victim: attacker, killer: deathKiller{player: snapshot}, skill: strikeRow,
		impacts: 1, linkedShare: true, now: now}
	if !rt.planPlayerStrike(&strike,
		func(*enterworld.SkillWall) (combat.WallOutcome, error) {
			return combat.WallOutcome{Defender: combat.Result{ResultFlags: 1}}, nil
		},
		func(wall *enterworld.SkillWall, _ combat.Result) ([]abnormal.Record, error) {
			return rt.rollPlayerOnPlayer(division, snapshot, casterStats, &row.Abnormal, view, defender, wall)
		}) || len(strike.records) == 0 {
		return OpResult{}
	}
	var struck playerStruck
	if !rt.deps.Update(attacker, "scream-mask", func() bool {
		if attacker.DeletePending || !enterworld.CharacterAlive(attacker) {
			return false
		}
		struck = rt.strikePlayerInDoor(strike)
		return len(struck.impacts) > 0
	}) {
		return OpResult{}
	}
	public, private := rt.playerStruckFrames(division, attacker, struck, now)
	result := OpResult{Broadcast: public}
	if len(private) != 0 {
		result.Recipients = []RecipientFrames{{CharacterID: attacker.ID, Frames: private}}
	}
	return result
}
