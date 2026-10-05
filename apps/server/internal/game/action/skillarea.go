/*
===========================================================================

skillarea.go - area skills: victim selection around a centre

Plan all victims at release and commit their impacts together. Victims
are monsters and attackable players alike (areacandidates.go); a monster
takes the population's damage sequence, a player pvpstrike.go's hit, in
one door holding every character of the division. The primary target's
pre-impact pose also owns the projectile's retained flight time.

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
areaVictimPlan

One victim's release-time target and formulas until the whole area
commits. sequence indexes a monster's damage sequence, player a player's
hit; the other is -1.
================
*/
type areaVictimPlan struct {
	target   combatTarget
	formulas []combat.Result
	pose     monster.Pose
	sequence int
	player   int
}

/*
================
areaStrikePlan

Every victim in selection order, with the monsters' damage sequences and
the players' hits they index.
================
*/
type areaStrikePlan struct {
	victims   []areaVictimPlan
	sequences [][]simulation.MonsterDamagePlan
	players   []playerHit
}

/*
==================
areaPlanInput

What one area strike's victim planning varies by. caster is the striker's
record (a player victim's killer). impacts is the records each victim
takes; chained marks every victim after the first as a chained one (an
imbue's spread); posePrimary keeps the first victim's pose (a projectile's
flight), poseAll every victim's (a trap's explosion).
==================
*/
type areaPlanInput struct {
	division  string
	caster    *enterworld.Character
	snapshot  *enterworld.Character
	skill     enterworld.SkillRow
	attacker  combat.Stats
	victims   []combatTarget
	reduction uint8
	// lifeStealBase is an lfst row's 40F750 base (skilllifesteal.go): its
	// victims take the life-steal record at their running percent.
	lifeStealBase        int64
	impacts              int
	chained              bool
	posePrimary, poseAll bool
	now                  int64
}

/*
==================
planAreaVictims

SkillCombat_CalculateHitOutcome (58E5F0) per victim in selection order:
every impact's formula at the area's running percent, which falls by the
reduction after each victim, cumulatively. A monster the planned damage
kills keeps its live pose for the reward settlement; a player is planned
by planPlayerHit. ok is false when a victim cannot be planned.
==================
*/
func (rt *Runtime) planAreaVictims(in areaPlanInput) (areaStrikePlan, bool) {
	var out areaStrikePlan
	percent := uint64(fullAreaPercent)
	for index, victim := range in.victims {
		plan := areaVictimPlan{target: victim, sequence: -1, player: -1}
		if victim.player != nil {
			hit, ok := rt.planPlayerHit(playerHitInput{division: in.division, caster: in.caster, snapshot: in.snapshot,
				attacker: in.attacker, skill: in.skill, target: victim, impacts: in.impacts, percent: percent,
				chained: in.chained && index > 0, lifeStealBase: in.lifeStealBase, now: in.now})
			if !ok {
				return areaStrikePlan{}, false
			}
			plan.formulas = hit.strike.formulas
			plan.pose = monster.Pose{RegionID: victim.at.RegionID, X: victim.at.X, Y: victim.at.Y, Z: victim.at.Z}
			plan.player = len(out.players)
			out.players = append(out.players, hit)
			out.victims = append(out.victims, plan)
			percent = percent * uint64(100-in.reduction) / fullAreaPercent
			continue
		}
		target := *victim.monster
		defender, err := combat.MonsterInstanceStats(target)
		if err != nil {
			return areaStrikePlan{}, false
		}
		defender.MotionState = target.Motion.StateAt(in.now)
		total := uint64(0)
		for range in.impacts {
			if in.skill.LifeSteal.Present {
				// 58F4B5: the percent rides into 40F750, after its HP cap.
				formula := lifeStealResult(in.lifeStealBase, in.attacker, defender, target.CurrentHP, uint32(percent))
				total += uint64(formula.Damage)
				plan.formulas = append(plan.formulas, formula)
				continue
			}
			formula, err := rt.resolvePlayerImpact(in.division, in.snapshot.Name, in.skill, in.attacker, defender, in.now, in.chained && index > 0)
			if err != nil {
				return areaStrikePlan{}, false
			}
			formula.Damage = uint32(uint64(formula.Damage) * percent / fullAreaPercent)
			total += uint64(formula.Damage)
			plan.formulas = append(plan.formulas, formula)
		}
		percent = percent * uint64(100-in.reduction) / fullAreaPercent
		if in.poseAll || total >= uint64(target.CurrentHP) || index == 0 && in.posePrimary {
			mover, found := rt.Monsters.Mover(in.division, target.Gid)
			if !found {
				return areaStrikePlan{}, false
			}
			plan.pose = mover.LivePoseAt(in.now, nil)
		}
		impacts, planned := rt.planMonsterImpacts(in.division, in.snapshot, in.skill, target, plan.formulas, in.now)
		if !planned {
			return areaStrikePlan{}, false
		}
		plan.sequence = len(out.sequences)
		out.sequences = append(out.sequences, impacts)
		out.victims = append(out.victims, plan)
	}
	return out, true
}

/*
==================
areaCommit

What an area's door did: the monsters' committed impacts (by sequence),
their settlements, and the striker's side of its player hits.
==================
*/
type areaCommit struct {
	committed    [][]simulation.MonsterDamageResult
	progression  []wire.Frame
	drops        []grounditem.Item
	settlements  monsterSettlement
	playerActor  []wire.Frame
	playerPublic []wire.Frame
	shares       []jobKillShare
}

/*
==================
commitAreaInDoor

Inside the roster's door (every character of the division): the monster
sequences, each player victim, then the monsters' fatal settlements.
False when nothing landed.
==================
*/
func (rt *Runtime) commitAreaInDoor(division string, caster *enterworld.Character, roster rewardRoster, plan *areaStrikePlan, now int64) (areaCommit, bool) {
	var out areaCommit
	if len(plan.sequences) > 0 {
		var ok bool
		if out.committed, ok = rt.Monsters.ApplyDamageSequences(division, plan.sequences); !ok {
			return areaCommit{}, false
		}
	}
	landed := len(out.committed) > 0
	for i := range plan.players {
		commit := rt.commitPlayerHitInDoor(division, caster, &plan.players[i], now)
		out.playerActor = append(out.playerActor, commit.actor...)
		out.playerPublic = append(out.playerPublic, commit.public...)
		out.shares = append(out.shares, commit.shares...)
		landed = landed || len(plan.players[i].struck.impacts) > 0
	}
	if !landed {
		return areaCommit{}, false
	}
	out.progression, out.drops, out.settlements = rt.settleAreaFatalities(division, caster, roster, out.committed, plan, now)
	return out, true
}

/*
==================
areaDrains

Every committed record of the area in the drain owners' form, for an lfst
row's recovery.
==================
*/
func areaDrains(plan areaStrikePlan, commit areaCommit) [][]simulation.MonsterDamageResult {
	out := append([][]simulation.MonsterDamageResult(nil), commit.committed...)
	for _, hit := range plan.players {
		out = append(out, struckDamageResults(hit.struck))
	}
	return out
}

/*
==================
settleAreaFatalities

Inside the roster's door: every monster whose last committed impact was
fatal settles through the shared reward door at the pose its plan kept.
==================
*/
func (rt *Runtime) settleAreaFatalities(division string, c *enterworld.Character, roster rewardRoster, committed [][]simulation.MonsterDamageResult, plan *areaStrikePlan, now int64) (progression []wire.Frame, drops []grounditem.Item, settlements monsterSettlement) {
	poses := make([]monster.Pose, len(plan.sequences))
	for _, victim := range plan.victims {
		if victim.sequence >= 0 {
			poses[victim.sequence] = victim.pose
		}
	}
	for index, impacts := range committed {
		impact := impacts[len(impacts)-1]
		if !impact.Fatal {
			continue
		}
		s := rt.settleMonsterInsideDoor(division, c, roster, impact, poses[index], now)
		progression = append(progression, s.actorFrames...)
		drops = append(drops, s.drops...)
		settlements.public = append(settlements.public, s.public...)
		settlements.otherPublic = append(settlements.otherPublic, s.otherPublic...)
		settlements.others = append(settlements.others, s.others...)
	}
	return progression, drops, settlements
}

/*
==================
areaPublication

An area's wire after its door: the target records in selection order (a
walled player's absorb group follows it, 58EC9F), the victims' follow-ups
(cast interrupts, statuses, deaths, drops, a player's publication), the
recipients' frames and the returned hits.
==================
*/
type areaPublication struct {
	targets    []wire.SkillAreaTarget
	after      []wire.Frame
	recipients []RecipientFrames
	returned   OpResult
}

/*
==================
publishArea

The striker's skill row owns the hostility of its monster victims; player
victims recorded their aggression in the door.
==================
*/
func (rt *Runtime) publishArea(division string, caster *enterworld.Character, skill enterworld.SkillRow, impactCount int, plan areaStrikePlan, commit areaCommit, now int64) areaPublication {
	var out areaPublication
	source := enterworld.ObjectIDForCharacter(caster)
	var deaths []wire.Frame
	for _, victim := range plan.victims {
		if victim.player >= 0 {
			hit := plan.players[victim.player]
			if len(hit.struck.impacts) == 0 {
				continue
			}
			out.targets = append(out.targets, wire.SkillAreaTarget{GID: victim.target.gid, Impacts: padAreaImpacts(hit.struck.impacts, impactCount)})
			if hit.struck.absorb != nil {
				out.targets = append(out.targets, wire.SkillAreaTarget{GID: victim.target.gid, Impacts: padAreaImpacts(hit.struck.absorb, impactCount)})
			}
			public, private := rt.publishPlayerHit(division, hit, now)
			out.after = append(out.after, public...)
			out.recipients = append(out.recipients, private)
			returned := rt.returnDamageToPlayer(division, victim.target.player, caster, skill.ID, hit.defender, hit.strike.formulas[:len(hit.struck.impacts)], now)
			out.returned = mergeOpResults(out.returned, returned)
			continue
		}
		if victim.sequence >= len(commit.committed) {
			continue
		}
		impacts := commit.committed[victim.sequence]
		final := impacts[len(impacts)-1]
		gid := final.Instance.Gid
		for _, impact := range impacts {
			if impact.Knockdown != nil || impact.Knockback != nil {
				out.after = append(out.after, rt.interruptMonsterCast(division, gid)...)
				break
			}
		}
		if !final.Fatal {
			out.after = append(out.after, rt.monsterImpactAbnormalFrames(division, gid, impacts)...)
		}
		records := make([]wire.SkillCastTargetImpact, 0, impactCount)
		for i, impact := range impacts {
			records = append(records, committedSkillImpact(victim.formulas[i], impact))
		}
		out.targets = append(out.targets, wire.SkillAreaTarget{GID: gid, Impacts: padAreaImpacts(records, impactCount)})
		if final.Fatal {
			rt.queueMonsterDefeat(division, gid, now+monsterDeathPresentationRetention.Milliseconds())
			// 4A9C80 publishes LIFE for every alive-to-dead transition, area
			// victims included; without it the client keeps moving the corpse.
			deaths = append(deaths, monsterLifeDeadFrame(gid))
		}
		// A fatal victim records no aggression; its damage still feeds a
		// Mana Switch link.
		rt.commitSkillHostility(division, source, gid, skill, impacts, now)
	}
	out.after = append(out.after, deaths...)
	out.after = append(out.after, rt.groundReferences(commit.drops)...)
	for _, drop := range commit.drops {
		out.after = append(out.after, wire.DropBroadcastFrames(drop.SpawnRow(true))...)
	}
	out.after = append(out.after, commit.playerPublic...)
	out.recipients = append(out.recipients, rt.payJobKillShares(commit.shares)...)
	return out
}

/*
==================
padAreaImpacts

Once an impact is fatal the remaining records are bare type 8 (58EE1B).
==================
*/
func padAreaImpacts(records []wire.SkillCastTargetImpact, count int) []wire.SkillCastTargetImpact {
	out := append([]wire.SkillCastTargetImpact(nil), records...)
	for len(out) < count {
		out = append(out, wire.SkillCastTargetImpact{Skipped: true})
	}
	return out
}

/*
==================
areaVictims

areaVictims samples live positions without materializing additional nests.
Shape-2 uses the primary target's position, caster + candidate body radii,
and a three-dimensional distance (server 58a81c / 58ab6d..58ac23).
Monsters and attackable players are candidates alike (areacandidates.go),
in the port's deterministic order. Shapes 3 and 4 select along a line
(skillarea_directional.go); reach is the action's base range they measure
with.
==================
*/
func (rt *Runtime) areaVictims(division string, c *enterworld.Character, skill enterworld.SkillRow, primary combatTarget, area enterworld.SkillOffensiveArea, reach float32, nowMs int64) []combatTarget {
	validShape := area.Shape >= 1 && area.Shape <= 4 || area.Shape == 6
	if area.MaxTargets == 0 || !validShape {
		return nil
	}
	if _, ok := rt.resolveCombatTarget(division, c, primary.gid, nowMs); !ok {
		return nil
	}
	lease, exists := rt.areaPopulation(division, c, primary)
	if !exists {
		return nil
	}
	radius, ok := rt.deps.CharacterBodyRadius(c)
	if !ok {
		return nil
	}
	q := areaQuery{division: division, caster: c, skill: skill, lease: lease, center: primary.at, now: nowMs}
	if area.Shape == 3 || area.Shape == 4 {
		caster := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, nowMs)
		shape := areaShape{shape: area.Shape, width: area.Radius, maxTargets: area.MaxTargets}
		return rt.directionalVictims(q, caster, radius, primary, shape, reach)
	}
	if area.Shape == 1 {
		// Native 58A088..58A0D3 copies the caster position, whereas 58A831
		// copies the selected target. Sample the authority's current movement
		// leg, not the character's last persisted position.
		q.center = rt.liveSpawn(simulation.WorldKey(division, c.Name), c, nowMs)
	}
	out := []combatTarget{primary}
	if area.MaxTargets == 1 {
		return out
	}
	if area.Shape == 6 {
		q.reach, q.nearest = float64(area.Radius), true
	} else {
		q.reach = float64(area.Radius) + radius
	}
	for _, candidate := range rt.areaCandidates(q) {
		if candidate.target.gid == primary.gid {
			continue
		}
		out = append(out, candidate.target)
		if len(out) == int(area.MaxTargets) {
			break
		}
	}
	return out
}

/*
================
areaBaseRange

58B29B..58B2A9 uses the row's range word or the truncated attack-range
keeper. Pursuit bonuses do not widen this directional selection geometry.
================
*/
func areaBaseRange(skill enterworld.SkillRow, attacker combat.Stats) float32 {
	if skill.ActionRange > 0 {
		return float32(uint16(skill.ActionRange))
	}
	param, _ := attacker.Param(combat.AttackRangeParameter)
	return float32(uint16(int32(param)))
}

/*
==================
acceptSkillAreaAt

One area strike. A projectile area (the Chinese bow's pierce and special
shots) selects and resolves its victims at release like any area
(SkillAction_Projectile 585A1F -> TargetSelection_DispatchByShape, then
SkillCombat_CalculateHitOutcome), spends its arrow in the same commit, and
keeps the token open for the flight to the primary target (5860D2).

area is the row's own efr or, when chained is set, the active imbue's
(skillcombat.go): every victim after the primary is then a chained one.
charged is false for a basic attack spread by an imbue: like its single-
target hit, it pays no phase cost.
Each victim takes every authored impact; once one is fatal its remaining
records are bare type 8 (58EE1B, var_e5 set by 58FA53).
==================
*/
func (rt *Runtime) acceptSkillAreaAt(division string, character, snapshot *enterworld.Character, skill enterworld.SkillRow, area enterworld.SkillOffensiveArea, chained, charged bool, primary combatTarget, attacker combat.Stats, loadout combat.Loadout, consumeAmmo bool, nowMs int64, rootID uint32, release *pendingProjectileCast) (OpResult, skillCastDecision) {
	if skill.Attack.ImpactCount == 0 {
		return OpResult{}, skillCastRefused
	}
	victims := rt.areaVictims(division, snapshot, skill, primary, area, areaBaseRange(skill, attacker), nowMs)
	if len(victims) == 0 {
		return OpResult{}, skillCastRefused
	}
	stealBase, stealOK := int64(0), true
	if skill.LifeSteal.Present {
		stealBase, stealOK = rt.lifeStealBase(division, snapshot, skill.LifeSteal, attacker)
	}
	if !stealOK {
		return OpResult{}, skillCastRefused
	}
	plan, planned := rt.planAreaVictims(areaPlanInput{
		division: division, caster: character, snapshot: snapshot, skill: skill, attacker: attacker, victims: victims,
		reduction: area.ReductionPercent, impacts: int(skill.Attack.ImpactCount), chained: chained,
		lifeStealBase: stealBase, posePrimary: skill.ProjectileSpeed != 0, now: nowMs,
	})
	if !planned {
		return OpResult{}, skillCastRefused
	}
	var commit areaCommit
	var battleFrames []wire.Frame
	var refusal uint16
	roster := rt.monsterRewardRoster(division, character, nowMs)
	var ammo ammunitionResult
	var stolen wire.Frame
	if !rt.deps.UpdateMany(roster.characters, "player-area-attack", func() bool {
		var cost skillCharge
		if charged && rootID == 0 {
			cost, refusal = rt.offensivePhaseCost(division, character, skill, nowMs, release)
			if refusal != 0 {
				return false
			}
		}
		debit, admitted := ammunitionDebit{index: -1}, true
		if consumeAmmo {
			debit, admitted = rt.planEquippedAmmunition(character, loadout.WeaponKind, ammunitionSpent(skill, charged))
		}
		if !admitted {
			refusal = 0x300e
			return false
		}
		var ok bool
		if commit, ok = rt.commitAreaInDoor(division, character, roster, &plan, nowMs); !ok {
			return false
		}
		if release == nil {
			rt.startSkillCast(division, character, skill, nowMs)
		}
		battleFrames = rt.enterBattleState(division, character, nowMs)
		if consumeAmmo {
			ammo = applyAmmunitionDebit(character, debit)
		}
		if charged && rootID == 0 {
			rt.commitOffensivePhaseCost(division, character, skill, cost, nowMs, release != nil)
		} else if release == nil {
			rt.registerPlayerSkillCooldown(division, character, skill, nowMs)
		}
		if skill.LifeSteal.Present {
			stolen = rt.commitLifeSteal(division, character, areaDrains(plan, commit)...)
		}
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
		return OpResult{}, skillCastRefused
	}
	published := rt.publishArea(division, snapshot, skill, int(skill.Attack.ImpactCount), plan, commit, nowMs)
	if len(published.targets) == 0 || published.targets[0].GID != primary.gid {
		// The primary's record always leads (SkillCastAreaFrame): a primary
		// that left the door unstruck still steers with bare records.
		published.targets = append([]wire.SkillAreaTarget{{GID: primary.gid,
			Impacts: padAreaImpacts(nil, int(skill.Attack.ImpactCount))}}, published.targets...)
	}
	caster := enterworld.ObjectIDForCharacter(snapshot)
	var token uint32
	if release != nil {
		token = release.token
	} else {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
	}
	success := wire.SkillCastAreaFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: caster, InstanceToken: token}, primary.gid, published.targets)
	lifetime, _ := skill.ActionLifecycleMs()
	if release == nil {
		rt.queueSkillFinalize(division, snapshot.Name, caster, nowMs+int64(skill.ActionCastingTimeMs), wire.SkillCastReleaseFrame(token, primary.gid))
	} else {
		success = wire.SkillCastAreaReleaseFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: caster, InstanceToken: token}, primary.gid, published.targets)
		lifetime = uint64(skill.ActionDurationMs)
	}
	if skill.ProjectileSpeed != 0 {
		// The original target sample determines flight, even when impact
		// displaces or kills it. Zero-preparation area shots retain it too.
		pose := plan.victims[0].pose
		flight := projectileFlightMs(rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, nowMs), simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}, skill.ProjectileSpeed)
		lifetime = uint64(max(int64(lifetime), flight+1))
	}
	rt.queueSkillCastClose(division, snapshot.Name, caster, token, skill, rootID, nowMs+int64(lifetime))
	public := append([]wire.Frame{success}, published.after...)
	if stolen.Opcode != 0 {
		public = append(public, stolen)
	}
	actor := append([]wire.Frame{}, public...)
	private := wire.ProgressionPrivateFrames(commit.progression)
	if rootID == 0 {
		vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshPayload(enterworld.ObjectIDForCharacter(character), rt.publishedVitals(division, character))}
		actor = append(actor, vitals)
		private = append(private, vitals)
	}
	if consumeAmmo {
		frames := ammunitionFrames(ammo)
		actor = append(actor, frames...)
		private = append(frames, private...)
	}
	// 593832: every victim's unblocked attempts wear the attacker's weapon,
	// rolled once for the execution.
	var tally wearTally
	for _, victim := range plan.victims {
		for _, formula := range victim.formulas {
			tally.note(formula.Blocked, true)
		}
	}
	wear := rt.applyEquipmentWear(division, character, tally)
	actor = append(actor, wear.actor...)
	private = append(private, wear.actor...)
	public = append(public, wear.public...)
	actor = append(actor, commit.progression...)
	actor = append(actor, commit.playerActor...)
	private = append(private, wire.ProgressionPrivateFrames(commit.playerActor)...)
	public = append(public, commit.settlements.public...)
	actor = append(actor, commit.settlements.otherPublic...)
	// Entering battle (4E27C0) follows the hit's burst.
	actor = append(actor, battleFrames...)
	public = append(public, battleFrames...)
	out := OpResult{Frames: actor, Broadcast: public, ActorPrivate: private,
		Recipients: append(commit.settlements.others, published.recipients...)}
	return mergeOpResults(out, published.returned), skillCastAccepted
}
