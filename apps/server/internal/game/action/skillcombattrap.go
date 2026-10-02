/*
===========================================================================

skillcombattrap.go - planted hostile traps (the Wizard's Fire Trap)

A prepared, untargeted cast plants a stationary skill object at the caster.
The skill-object tick retires it on expiry, owner absence or distance, and
explodes it on the first living monster inside its trigger radius. The
explosion resolves with the owner's live stats against every victim around
the trap and settles through the ordinary monster damage and reward doors.

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/skillobject"
)

/*
================
acceptCombatTrap

Admission and preparation follow the timed self-effect owner. The release
debits the prepared cost and plants the object; a planter keeps at most the
authored number of live traps per link group, retiring the oldest first.
================
*/
func (rt *Runtime) acceptCombatTrap(division string, c, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64, pending *pendingProjectileCast) (OpResult, skillCastDecision) {
	trap := skill.CombatTrap
	if !trap.Pinned {
		return OpResult{DiagnosticRefusal: "combat-trap-admission-refused"}, skillCastRefused
	}
	if out, decision, done := rt.beginUntargetedCast(division, c, snapshot, cast, skill, now, pending, func(p *pendingProjectileCast) { p.trap = true }); done {
		return out, decision
	}
	rt.clearCurrentSkillCommand(division, c.Name)
	lease, present := rt.EntryPopulationLease(division, c.Name)
	if !present {
		return OpResult{DiagnosticRefusal: "combat-trap-population"}, skillCastRefused
	}
	gid := enterworld.ObjectIDForCharacter(c)
	at := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	var refusal uint16
	var effects []wire.Frame
	effectToken := atomic.AddUint32(&rt.castTokenCounter, 1)
	if !rt.deps.Update(c, "release-combat-trap", func() bool {
		if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(division, c, skill, now, pending)
		refusal = code
		if code != 0 {
			return false
		}
		object, err := rt.SkillObjects.Create(skillobject.Object{
			Division: division, Population: lease, OwnerGID: gid, OwnerName: c.Name, CreatedMs: now, OwnerEffect: effectToken,
			Program: skillobject.Program{SkillID: skill.ID, DurationMs: trap.DurationMs, ScanMs: enterworld.CombatTrapScanMs,
				Radius: trap.TriggerRadius, Combat: true, Hidden: trap.Hidden, OwnerDistance: trap.OwnerDistance, LinkGroup: trap.LinkGroup},
			Spawn: wire.SkillObjectSpawn{Region: at.RegionID, X: float32(at.X), Y: float32(at.Y), Z: float32(at.Z), Heading: at.Angle},
		})
		if err != nil {
			return false
		}
		// lnks' board word shows the live trap on the planter's buff board.
		var installed bool
		effects, installed = rt.commitCharacterEffect(division, c, skill, effectToken, statuseffect.StateActive, false, EffectPresentation{Phase: 1}, now)
		if !installed {
			rt.SkillObjects.Remove(object.Spawn.GID)
			return false
		}
		rt.commitOffensivePhaseCost(division, c, skill, cost, now, true)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
		return OpResult{DiagnosticRefusal: "combat-trap-release-commit-refused"}, skillCastRefused
	}
	rt.retireExcessCombatTraps(division, c, trap, now)
	vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceSkillRecovery, rt.publishedVitals(division, c))}
	released := wire.SkillCastReleaseFrame(pending.token, 0)
	// The release does not install an effect on the caster, so nothing else
	// closes the bracket: without the closing B505 the client keeps the
	// casting aura on the ground indefinitely.
	rt.queueDetachedCastClose(division, c.Name, gid, pending.token, now+int64(skill.ActionDurationMs))
	public := append([]wire.Frame{released}, effects...)
	return OpResult{Frames: append([]wire.Frame{vitals}, public...), Broadcast: public, ActorPrivate: []wire.Frame{vitals}}, skillCastAccepted
}

/*
================
retireExcessCombatTraps

lnks' third word caps the planter's live traps in one link group. Snapshot
order is creation order, so the newest traps survive.
================
*/
func (rt *Runtime) retireExcessCombatTraps(division string, c *enterworld.Character, trap enterworld.SkillCombatTrap, now int64) {
	owner := enterworld.ObjectIDForCharacter(c)
	var live []skillobject.Object
	for _, object := range rt.SkillObjects.Snapshot() {
		if object.OwnerGID == owner && object.Program.Combat && object.Program.LinkGroup == trap.LinkGroup {
			live = append(live, object)
		}
	}
	for len(live) > int(trap.MaxLive) {
		if rt.SkillObjects.Remove(live[0].Spawn.GID) {
			rt.retireCombatTrapEffect(division, c, live[0], now)
		}
		live = live[1:]
	}
}

/*
================
retireCombatTrapEffect

The planter's buff ends with its trap. Publication runs outside every
character door (finishEndedEffects reads the store).
================
*/
func (rt *Runtime) retireCombatTrapEffect(division string, c *enterworld.Character, object skillobject.Object, now int64) {
	if c == nil || object.OwnerEffect == 0 {
		return
	}
	rt.publishEndedEffects(division, c, rt.effects.RetireInstances(division, c.Name, []uint32{object.OwnerEffect}), now)
}

/*
================
combatTrapOwnerNear

lnks' second word: the planter must stay within this planar distance.
================
*/
func combatTrapOwnerNear(object skillobject.Object, owner simulation.Spawn) bool {
	from := worldgeom.RegionXZ{RegionID: object.Spawn.Region, X: float64(object.Spawn.X), Z: float64(object.Spawn.Z)}
	to := worldgeom.RegionXZ{RegionID: owner.RegionID, X: owner.X, Z: owner.Z}
	return worldgeom.SamePlane(from.RegionID, to.RegionID) && worldgeom.Distance(from, to) <= float64(object.Program.OwnerDistance)
}

/*
================
combatTrapVictims

The triggering monster first, then living monsters in the explosion radius
around the trap (efr shape 1 centred on the object), up to the authored cap.
================
*/
func (rt *Runtime) combatTrapVictims(object skillobject.Object, lease instance.Lease, primary monster.Instance, area enterworld.SkillOffensiveArea, now int64) []monster.Instance {
	from := simulation.Spawn{RegionID: object.Spawn.Region, X: float64(object.Spawn.X), Y: float64(object.Spawn.Y), Z: float64(object.Spawn.Z)}
	out := []monster.Instance{primary}
	for _, candidate := range rt.Monsters.CombatCandidatesInPopulation(object.Division, lease, from, float64(area.Radius), now, true) {
		if len(out) >= int(area.MaxTargets) {
			break
		}
		if candidate.Gid == primary.Gid || candidate.CurrentHP == 0 {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

/*
================
explodeCombatTrap

Each victim takes the trap's att once; damage falls by the area's reduction
per victim in selection order, as the cast-owned area does. The trap object
is not an actor on the wire: the result is a B3C6 pulse from the planter,
who owns credit, rewards and hostility.
================
*/
func (rt *Runtime) explodeCombatTrap(object skillobject.Object, c, snapshot *enterworld.Character, skill enterworld.SkillRow, primary monster.Instance, lease instance.Lease, now int64) []simulation.DivisionFrames {
	trap := skill.CombatTrap
	attacker, _, err := rt.playerCombatStats(object.Division, snapshot)
	if err != nil {
		return nil
	}
	strike := skill
	strike.Attack = trap.Attack
	victims := rt.combatTrapVictims(object, lease, primary, trap.Area, now)
	plans := make([]areaVictimPlan, 0, len(victims))
	sequences := make([][]simulation.MonsterDamagePlan, 0, len(victims))
	percent := uint64(100)
	for _, target := range victims {
		defender, err := combat.MonsterInstanceStats(target)
		if err != nil {
			return nil
		}
		defender.MotionState = target.Motion.StateAt(now)
		formula, err := rt.resolvePlayerImpact(object.Division, snapshot.Name, strike, attacker, defender, now, false)
		if err != nil {
			return nil
		}
		formula.Damage = uint32(uint64(formula.Damage) * percent / 100)
		percent = percent * uint64(100-trap.Area.ReductionPercent) / 100
		mover, ok := rt.Monsters.Mover(object.Division, target.Gid)
		if !ok {
			return nil
		}
		plan := areaVictimPlan{target: target, formulas: []combat.Result{formula}, pose: mover.LivePoseAt(now, nil)}
		impacts, ok := rt.planMonsterImpacts(object.Division, snapshot, strike, target, plan.formulas, now)
		if !ok {
			return nil
		}
		plans = append(plans, plan)
		sequences = append(sequences, impacts)
	}
	var committed [][]simulation.MonsterDamageResult
	var progression []wire.Frame
	var settlements monsterSettlement
	var drops []grounditem.Item
	roster := rt.monsterRewardRoster(object.Division, c, now)
	if !rt.deps.UpdateMany(roster.characters, "combat-trap-explosion", func() bool {
		var ok bool
		if committed, ok = rt.Monsters.ApplyDamageSequences(object.Division, sequences); !ok {
			return false
		}
		for index, impacts := range committed {
			impact := impacts[len(impacts)-1]
			if !impact.Fatal {
				continue
			}
			s := rt.settleMonsterInsideDoor(object.Division, c, roster, impact, plans[index].pose, now)
			progression = append(progression, s.actorFrames...)
			drops = append(drops, s.drops...)
			settlements.public = append(settlements.public, s.public...)
			settlements.otherPublic = append(settlements.otherPublic, s.otherPublic...)
			settlements.others = append(settlements.others, s.others...)
		}
		return true
	}) {
		return nil
	}
	owner := enterworld.ObjectIDForCharacter(snapshot)
	var after []wire.Frame
	var targets []wire.SkillAreaTarget
	for index, impacts := range committed {
		final := impacts[len(impacts)-1]
		gid := final.Instance.Gid
		records := []wire.SkillCastTargetImpact{committedSkillImpact(plans[index].formulas[0], impacts[0])}
		targets = append(targets, wire.SkillAreaTarget{GID: gid, Impacts: records})
		if final.Fatal {
			rt.queueMonsterDefeat(object.Division, gid, now+monsterDeathPresentationRetention.Milliseconds())
			after = append(after, monsterLifeDeadFrame(gid))
			continue
		}
		after = append(after, rt.monsterImpactAbnormalFrames(object.Division, gid, impacts)...)
		rt.commitSkillHostility(object.Division, owner, gid, strike, impacts, now)
	}
	// A planted object has no casting motion: like a linked pulse (59B220),
	// the explosion is a B3C6 result batch credited to its planter, which the
	// client presents at once, damage text included, without an action bracket.
	success := wire.SkillPulseFrame(owner, skill.ID, targets)
	public := append([]wire.Frame{success}, after...)
	public = append(public, rt.groundReferences(drops)...)
	for _, drop := range drops {
		public = append(public, wire.DropBroadcastFrames(drop.SpawnRow(true))...)
	}
	public = append(public, settlements.public...)
	out := []simulation.DivisionFrames{{DivisionID: object.Division, SourceGID: owner, Frames: simFrames(public)}}
	private := append(wire.ProgressionPrivateFrames(progression), settlements.otherPublic...)
	if len(private) > 0 {
		out = append(out, simulation.DivisionFrames{DivisionID: object.Division, OnlyCharacterID: c.ID, Frames: simFrames(private)})
	}
	return append(out, recipientDivisionFrames(object.Division, settlements.others)...)
}
