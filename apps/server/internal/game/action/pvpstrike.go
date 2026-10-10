/*
===========================================================================

pvpstrike.go - a player's hits on a player

A player struck by a player takes the same recipient path as one struck by
a monster (playervictim.go); this file owns the attacker's side. One unit,
playerHit, is planned before any door (58EC6C's hit behind the victim's
wall at the area's running percent, 590680's status rolls), committed
inside the door that holds both characters (593800's recipient side, the
aggression of ProcessNormalHit 4E25C0 / 4E1DF0, the kill's rewards) and
published after it. The single-target stage, the area victims, periodic
pulses, hawks and returned damage all strike players through it.

Admission is the native one: SkillCombat_ValidateTargets (58CC70) with the
attack permission CGObjChar_CheckTargetAttackable (5293A0), whatever the
row's execution selector, since only hostile rows reach this file.

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
offensiveStage

One admitted offensive stage at its release instant, as
acceptSkillStagePhaseAt resolved it before learning the target's kind.
================
*/
type offensiveStage struct {
	division            string
	character, snapshot *enterworld.Character
	cast                wire.SkillAction
	skill               enterworld.SkillRow
	basic, advanced     bool
	rootID              uint32
	release             *pendingProjectileCast
	attacker            combat.Stats
	loadout             combat.Loadout
	consumeAmmo         bool
	actionReach         simulation.ActionReach
	lifecycleMs         uint64
	now                 int64
}

/*
================
playerHitInput

What one player victim's strike varies by. percent is the area's running
share (100 for a single target); fixed is a credited hit's finished record
(a returned hit), resolved by no formula.
================
*/
type playerHitInput struct {
	division      string
	caster        *enterworld.Character
	snapshot      *enterworld.Character
	attacker      combat.Stats
	skill         enterworld.SkillRow
	target        combatTarget
	impacts       int
	percent       uint64
	chained       bool
	lifeStealBase int64
	fixed         *combat.Result
	now           int64
}

/*
================
playerHit

One planned player victim and, after its door, what the door did.
================
*/
type playerHit struct {
	target   combatTarget
	defender combat.Stats
	strike   playerStrike
	kill     playerKill
	struck   playerStruck
}

/*
================
playerHitCommit

The caster's side of a committed hit: its frames, the observers' frames
and the job shares its party still earns.
================
*/
type playerHitCommit struct {
	actor, public []wire.Frame
	shares        []jobKillShare
}

/*
================
hostilePlayerRefusal

58CC70 for a hostile row on a player: the target checks of
playerSkillTarget, then 5293A0 when the row's selector did not already
ask for it. Zero admits.
================
*/
func (rt *Runtime) hostilePlayerRefusal(division string, caster, target *enterworld.Character, skill enterworld.SkillRow, now int64) uint16 {
	if code := rt.playerSkillTarget(division, caster, target, skill, now); code != 0 {
		return code
	}
	if !skill.Replacement.MatchesExecutionSelector {
		return rt.playerAttackTargetRefusal(division, caster, target, now)
	}
	return 0
}

/*
================
planPlayerHit

58E5F0 for one player victim: every impact behind its wall, scaled by the
running percent (a drain's share instead rides into 40F750), then the
row's and the imbue's statuses. The commit classifies the kill under the
character door before death changes the victim.
================
*/
func (rt *Runtime) planPlayerHit(in playerHitInput) (playerHit, bool) {
	victim := in.target.snapshot
	defender, _, err := rt.playerCombatStats(in.division, victim)
	if err != nil {
		return playerHit{}, false
	}
	_, _, hp, _ := rt.playerKeeperVitals(in.division, victim)
	h := playerHit{target: in.target, defender: defender, kill: rt.classifyPlayerKill(in.division, in.caster, victim)}
	h.strike = playerStrike{division: in.division, victim: in.target.player, killer: deathKiller{player: in.caster},
		skill: in.skill, impacts: in.impacts, now: in.now}
	percent := in.percent
	if percent == 0 {
		percent = fullAreaPercent
	}
	resolve := func(wall *enterworld.SkillWall) (combat.WallOutcome, error) {
		switch {
		case in.fixed != nil:
			return combat.WallOutcome{Defender: *in.fixed}, nil
		case in.skill.LifeSteal.Present:
			// 58F4B5: the percent rides into 40F750, after its HP cap.
			steal := lifeStealResult(in.lifeStealBase, in.attacker, defender, uint32(max(hp, 0)), uint32(percent))
			return combat.WallOutcome{Defender: combat.FinishImpact(steal, combat.ImpactTail{PercentApplied: true, Attack: in.skill.Attack.Present})}, nil
		}
		out, err := rt.resolvePlayerImpactBehindWall(in.division, in.snapshot.Name, in.skill, in.attacker, defender, in.now, in.chained, wall)
		out.Defender = combat.FinishImpact(out.Defender, combat.ImpactTail{Percent: percent, Attack: in.skill.Attack.Present,
			Covered: out.Covered})
		out.Absorbed = uint32(uint64(out.Absorbed) * percent / fullAreaPercent)
		return out, err
	}
	roll := func(wall *enterworld.SkillWall, formula combat.Result) ([]abnormal.Record, error) {
		records, err := rt.rollPlayerOnPlayer(in.division, in.snapshot, in.attacker, &in.skill.Abnormal, victim, defender, wall)
		if err != nil {
			return nil, err
		}
		imbue, err := rt.rollPlayerOnPlayer(in.division, in.snapshot, in.attacker, &formula.Imbue, victim, defender, wall)
		return append(records, imbue...), err
	}
	if !rt.planPlayerStrike(&h.strike, resolve, roll) {
		return playerHit{}, false
	}
	if in.fixed == nil {
		from := rt.liveSpawn(simulation.WorldKey(in.division, in.snapshot.Name), in.snapshot, in.now)
		if err := rt.planStrikeDisplacement(&h.strike, criticalActor{division: in.division, character: in.snapshot.Name}, from, in.target.at); err != nil {
			return playerHit{}, false
		}
	}
	return h, true
}

/*
================
commitPlayerHitInDoor

Inside a door holding the caster and the victim: the recipient side, then
the aggression ProcessNormalHit records and a kill's rewards. Preserve
pre-death relation facts, and book the killer before its hostile-target
registration, as 4E27C0 does.
================
*/
func (rt *Runtime) commitPlayerHitInDoor(division string, caster *enterworld.Character, h *playerHit, now int64) playerHitCommit {
	var out playerHitCommit
	victim := h.target.player
	if victim.DeletePending || !enterworld.CharacterAlive(victim) || len(h.strike.formulas) == 0 {
		return out
	}
	// 52AA30 records the recipient's aggression before 52A240 runs the
	// killer callback. Capture the target under this door before death
	// relief changes its red state, including the last 200 penalty points.
	out.public = append(out.public, rt.registerPlayerAttacked(division, victim, caster, now)...)
	h.kill = h.kill.withVictim(caster, victim)
	beforeDeath := *victim
	if victim.PK != nil {
		record := *victim.PK
		beforeDeath.PK = &record
	}
	h.struck = rt.strikePlayerInDoor(h.strike)
	if len(h.struck.impacts) == 0 {
		return out
	}
	if h.struck.fatal {
		actor, public, shares := rt.payPlayerKillInDoor(division, caster, victim, h.kill, now)
		out.actor = append(out.actor, actor...)
		out.public = append(out.public, public...)
		out.shares = shares
	}
	// 4E27F4 books the kill before 4E280B registers the hostile target.
	attack := rt.registerPlayerAttack(division, caster, &beforeDeath, now)
	out.actor = append(out.actor, attack...)
	out.public = append(out.public, attack...)
	return out
}

/*
================
publishPlayerHit

The victim's publication after the door: public to every observer, the
victim's private frames to the victim alone.
================
*/
func (rt *Runtime) publishPlayerHit(division string, h playerHit, now int64) (public []wire.Frame, victim RecipientFrames) {
	if len(h.struck.impacts) == 0 {
		return nil, RecipientFrames{}
	}
	if h.struck.fatal {
		rt.bindResidentRegion(simulation.WorldKey(division, h.target.player.Name), now)
	}
	public, private := rt.playerStruckFrames(division, h.target.player, h.struck, now)
	return public, RecipientFrames{CharacterID: h.target.player.ID, Frames: private}
}

/*
================
struckDamageResults

A struck player's records in the drain owners' form (the dmgt and lfst
commits read each record's damage and the HP before it).
================
*/
func struckDamageResults(struck playerStruck) []simulation.MonsterDamageResult {
	out := make([]simulation.MonsterDamageResult, 0, len(struck.impacts))
	for i, impact := range struck.impacts {
		before := struck.before[i]
		out = append(out, simulation.MonsterDamageResult{BeforeHP: before, Damage: impact.Damage,
			Applied: min(before, impact.Damage), Fatal: impact.Fatal})
	}
	return out
}

/*
================
acceptPlayerTargetStage

acceptSkillStagePhaseAt for a player target: the same admission, range,
preparation, area and periodic hand-offs, then one playerHit committed in
a door holding the caster and the victim. Taunts act on monster hostility
only; their rows target monsters and 58D7A0 refuses them here.
================
*/
func (rt *Runtime) acceptPlayerTargetStage(st offensiveStage) (OpResult, skillCastDecision) {
	division, character, snapshot, skill, now := st.division, st.character, st.snapshot, st.skill, st.now
	target, ok := rt.resolveCombatTarget(division, snapshot, st.cast.TargetGid, now)
	if !ok || target.player == nil {
		return OpResult{}, skillCastRefused
	}
	mask := admitExecution
	if st.rootID != 0 {
		mask &^= admitCooldown
	}
	struck := &admitTarget{at: target.at, player: target.snapshot, motion: rt.playerTargetMotion(division, target.snapshot, now)}
	if code := rt.skillAdmission(division, snapshot, skill, now, struck, st.release, mask); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}
	if code := rt.hostilePlayerRefusal(division, snapshot, target.snapshot, skill, now); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}
	if skill.Threat.Only {
		return offensiveRefusal(0x3006), skillCastRefused
	}
	if st.advanced {
		if _, refusal := rt.stagePhaseCost(division, snapshot, skill, now, st.release, st.rootID); refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
	}
	casterAt := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	spacing, spacingOK := rt.combatTargetSpacing(snapshot, target, st.actionReach)
	if !spacingOK || simulation.IsDungeonRegion(casterAt.RegionID) != simulation.IsDungeonRegion(target.at.RegionID) {
		return OpResult{}, skillCastDeferred
	}
	if st.release == nil && !spacing.Contains(casterAt, target.at) {
		return OpResult{}, skillCastDeferred
	}
	if st.release == nil && st.rootID == 0 {
		rt.noteHawkOwnerAttack(division, snapshot, target.gid, now)
	}
	if skill.ActionCastingTimeMs != 0 && st.release == nil {
		return rt.prepareOffensiveCast(division, character, snapshot, st.cast, skill, st.advanced, st.rootID, now)
	}
	if skill.TimedEffect.Periodic.Pinned {
		return rt.installPeriodicCast(periodicCast{division: division, character: character, snapshot: snapshot,
			skill: skill, cast: st.cast, target: target, attacker: st.attacker, now: now, release: st.release})
	}
	if skill.OffensiveArea.Radius != 0 {
		return rt.acceptSkillAreaAt(division, character, snapshot, skill, skill.OffensiveArea, false, true, target, st.attacker, st.loadout, st.consumeAmmo, now, st.rootID, st.release)
	}
	if skill.Attack.Value5 != 0 && skill.ReplacementPinned && skill.Replacement.MatchesExecutionSelector {
		if imbue, _ := rt.activeWeaponImbue(division, snapshot.Name, now); imbue.Pinned && imbue.Area.Radius != 0 {
			return rt.acceptSkillAreaAt(division, character, snapshot, skill, imbue.Area, true, st.advanced, target, st.attacker, st.loadout, st.consumeAmmo, now, st.rootID, st.release)
		}
	}
	return rt.strikePlayerTarget(st, target, casterAt)
}

/*
================
strikePlayerTarget

The single-target strike: plan, one door for the caster and the victim
(cost, ammunition, the hit, a charge's travel, a drain's recovery), then
the B245 result and the victim's publication.
================
*/
func (rt *Runtime) strikePlayerTarget(st offensiveStage, target combatTarget, casterAt simulation.Spawn) (OpResult, skillCastDecision) {
	division, character, snapshot, skill, now := st.division, st.character, st.snapshot, st.skill, st.now
	stealBase, stealOK := int64(0), true
	if skill.LifeSteal.Present {
		stealBase, stealOK = rt.lifeStealBase(division, snapshot, skill.LifeSteal, st.attacker)
	}
	if !stealOK {
		return OpResult{}, skillCastRefused
	}
	hit, planned := rt.planPlayerHit(playerHitInput{division: division, caster: character, snapshot: snapshot,
		attacker: st.attacker, skill: skill, target: target, lifeStealBase: stealBase, now: now})
	if !planned {
		return OpResult{}, skillCastRefused
	}
	var travel skillTravelPlan
	if skill.PositionEffect.Charge {
		from, owner := rt.liveNav(simulation.WorldKey(division, snapshot.Name), snapshot, now)
		radius, valid := rt.deps.CharacterBodyRadius(snapshot)
		targetRadius, targetValid := rt.deps.CharacterBodyRadius(target.snapshot)
		goal, admitted := chargeSkillGoal(from, target.at, skill.PositionEffect.Range, radius+targetRadius)
		if !valid || !targetValid || !admitted {
			return OpResult{}, skillCastRefused
		}
		if travel, planned = rt.planSkillTravel(snapshot.Name, from, owner, goal); !planned {
			return OpResult{DiagnosticRefusal: "charge-navigation-refused"}, skillCastRefused
		}
	}
	var commit playerHitCommit
	var ammo ammunitionResult
	var battle []wire.Frame
	var tuning, stolen wire.Frame
	var refusal uint16
	if !rt.deps.UpdateMany([]*enterworld.Character{character, target.player}, "player-strike-player", func() bool {
		var cost skillCharge
		if st.advanced {
			if cost, refusal = rt.stagePhaseCost(division, character, skill, now, st.release, st.rootID); refusal != 0 {
				return false
			}
		}
		debit, admitted := ammunitionDebit{index: -1}, true
		if st.consumeAmmo {
			debit, admitted = rt.planEquippedAmmunition(character, st.loadout.WeaponKind, ammunitionSpent(skill, st.advanced))
		}
		if !admitted {
			refusal = 0x300e
			return false
		}
		commit = rt.commitPlayerHitInDoor(division, character, &hit, now)
		if len(hit.struck.impacts) == 0 {
			return false
		}
		if st.release == nil {
			rt.startSkillCast(division, character, skill, now)
		}
		battle = rt.enterBattleState(division, character, now)
		if st.consumeAmmo {
			ammo = applyAmmunitionDebit(character, debit)
		}
		if st.advanced {
			rt.commitOffensivePhaseCost(division, character, skill, cost, now, st.release != nil)
		} else if st.release == nil {
			rt.registerPlayerSkillCooldown(division, character, skill, now)
		}
		taken := struckDamageResults(hit.struck)
		if skill.FixedDamage.Present {
			tuning = rt.commitTuningMana(division, character, skill.FixedDamage, taken)
		}
		if skill.LifeSteal.Present {
			stolen = rt.commitLifeSteal(division, character, taken)
		}
		if skill.PositionEffect.Charge {
			rt.commitSkillTravel(simulation.WorldKey(division, character.Name), character, travel)
		}
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
		return OpResult{}, skillCastRefused
	}
	if skill.PositionEffect.Charge {
		rt.bindResidentRegion(simulation.WorldKey(division, character.Name), now)
	}
	caster := enterworld.ObjectIDForCharacter(snapshot)
	var token uint32
	if st.release != nil {
		token = st.release.token
	} else {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
	}
	success := wire.SkillCastSuccess{SkillId: st.cast.ActionId, CasterGid: caster, InstanceToken: token}
	result := wire.NewStationarySkillCastSingleTargetResult(success, target.gid, hit.struck.impacts)
	if skill.PositionEffect.Charge {
		result = wire.NewSkillCastSingleTargetResult(success, target.gid, hit.struck.impacts, travel.point)
	}
	if hit.struck.absorb != nil {
		result = result.WithAbsorb(hit.struck.absorb)
	}
	frame := wire.SkillCastSingleTargetResultFrame(result)
	closeAt := now + int64(st.lifecycleMs)
	if st.release == nil {
		rt.queueSkillFinalize(division, snapshot.Name, caster, now+int64(skill.ActionCastingTimeMs), wire.SkillCastReleaseFrame(token, target.gid))
	} else {
		frame = wire.SkillCastReleaseResultFrame(result)
		closeAt = now + int64(skill.ActionDurationMs)
	}
	if skill.ActionHandler == enterworld.SkillActionProjectile {
		closeAt = max(closeAt, now+projectileFlightMs(casterAt, target.at, skill.ProjectileSpeed)+1)
	}
	if !skill.PositionEffect.Charge {
		rt.queueSkillCastClose(division, snapshot.Name, caster, token, skill, st.rootID, closeAt)
	}
	victimPublic, victimPrivate := rt.publishPlayerHit(division, hit, now)
	actor := append([]wire.Frame{frame}, victimPublic...)
	public := append([]wire.Frame{frame}, victimPublic...)
	var private []wire.Frame
	if st.consumeAmmo {
		actor = append(actor, ammunitionFrames(ammo)...)
		private = append(private, ammunitionFrames(ammo)...)
	}
	// 593832: the attacker's unblocked attempts wear its weapon.
	var tally wearTally
	for _, formula := range hit.strike.formulas {
		tally.note(formula.Blocked, true)
	}
	wear := rt.applyEquipmentWear(division, character, tally)
	actor = append(actor, wear.actor...)
	private = append(private, wear.actor...)
	public = append(public, wear.public...)
	if st.advanced {
		vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshPayload(caster, rt.publishedVitals(division, character))}
		actor = append(actor, vitals)
		private = append(private, vitals)
	}
	if tuning.Opcode != 0 {
		actor = append(actor, tuning)
		private = append(private, tuning)
	}
	if stolen.Opcode != 0 {
		actor = append(actor, stolen)
		public = append(public, stolen)
	}
	actor = append(actor, commit.actor...)
	private = append(private, wire.ProgressionPrivateFrames(commit.actor)...)
	public = append(public, commit.public...)
	actor = append(actor, battle...)
	public = append(public, battle...)
	recipients := append([]RecipientFrames{victimPrivate}, rt.payJobKillShares(commit.shares)...)
	out := OpResult{Frames: actor, Broadcast: public, ActorPrivate: private, Recipients: recipients}
	// 5A0C2D ran inside the hit outcome; the victim's dmgr returns its share
	// to the attacker afterwards, as the victim's credited hit.
	returned := rt.returnDamageToPlayer(division, target.player, character, skill.ID, hit.defender, hit.strike.formulas[:len(hit.struck.impacts)], now)
	shared := rt.strikeLinkedShares(division, caster, hit.strike.killer, skill, hit.struck.linkMoves, now)
	// A Scream Mask on the victim rolls its statuses on the attacker (screammask.go).
	shared = mergeOpResults(shared, rt.screamMaskPlayer(division, target.player, character, now))
	return mergeOpResults(mergeOpResults(out, returned), shared), skillCastAccepted
}

/*
================
creditPlayerHit

A credited hit on a player outside a cast's own commit (a periodic pulse,
a pulse area's strike, a hawk, a returned hit): plan, one door for the
striker and the victim, then the victim's publication. False when nothing
landed.
================
*/
func (rt *Runtime) creditPlayerHit(in playerHitInput) (playerHit, OpResult, bool) {
	hit, planned := rt.planPlayerHit(in)
	if !planned {
		return playerHit{}, OpResult{}, false
	}
	return rt.commitCreditedPlayerHit(in.division, in.caster, hit, in.now)
}

/*
================
commitCreditedPlayerHit

A planned credited hit's door and publication.
================
*/
func (rt *Runtime) commitCreditedPlayerHit(division string, caster *enterworld.Character, hit playerHit, now int64) (playerHit, OpResult, bool) {
	var commit playerHitCommit
	if !rt.deps.UpdateMany([]*enterworld.Character{caster, hit.target.player}, "player-credited-hit", func() bool {
		if caster.DeletePending {
			return false
		}
		commit = rt.commitPlayerHitInDoor(division, caster, &hit, now)
		return len(hit.struck.impacts) > 0
	}) {
		return playerHit{}, OpResult{}, false
	}
	public, victim := rt.publishPlayerHit(division, hit, now)
	out := OpResult{Broadcast: append(public, commit.public...),
		ActorPrivate: commit.actor,
		Recipients:   append([]RecipientFrames{victim}, rt.payJobKillShares(commit.shares)...)}
	return hit, out, true
}

/*
================
mergeOpResults

b's frames after a's, for a result built from two owners.
================
*/
func mergeOpResults(a, b OpResult) OpResult {
	a.Frames = append(a.Frames, b.Broadcast...)
	a.Frames = append(a.Frames, b.ActorPrivate...)
	a.Broadcast = append(a.Broadcast, b.Broadcast...)
	a.ActorPrivate = append(a.ActorPrivate, b.ActorPrivate...)
	a.Recipients = append(a.Recipients, b.Recipients...)
	return a
}
