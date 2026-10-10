/*
===========================================================================

skillcombattrap.go - planted hostile traps (the Wizard's Fire Trap, the
Rogue's Poison Trap)

A prepared, untargeted cast plants a stationary skill object at the caster.
The skill-object tick retires it on expiry, owner absence or distance, and
explodes it on the first living monster inside its trigger radius. The
explosion resolves with the owner's live stats against every victim around
the trap and settles through the ordinary monster damage and reward doors.
A trap field (skilltrapfield.go) plants the same way and pulses instead.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
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
A trap field has no lnks: no board instance and no live-trap cap.
================
*/
func (rt *Runtime) acceptCombatTrap(division string, c, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64, pending *pendingProjectileCast) (OpResult, skillCastDecision) {
	trap := skill.CombatTrap
	program, planted := plantedTrapProgram(skill)
	if !planted {
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
			Division: division, Population: lease, OwnerGID: gid, OwnerName: c.Name, CreatedMs: now,
			Program: program,
			Spawn:   wire.SkillObjectSpawn{Region: at.RegionID, X: float32(at.X), Y: float32(at.Y), Z: float32(at.Z), Heading: at.Angle},
		})
		if err != nil {
			return false
		}
		// lnks' board word shows the live trap on the planter's buff board.
		if object.OwnerEffect != 0 {
			var installed bool
			effects, installed = rt.commitCharacterEffect(division, c, skill, object.OwnerEffect, statuseffect.StateActive, false, EffectPresentation{Phase: 1}, now)
			if !installed {
				rt.SkillObjects.Remove(object.Spawn.GID)
				return false
			}
		}
		rt.commitOffensivePhaseCost(division, c, skill, cost, now, true)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
		return OpResult{DiagnosticRefusal: "combat-trap-release-commit-refused"}, skillCastRefused
	}
	if trap.Pinned {
		rt.retireExcessCombatTraps(division, c, trap, now)
	}
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
plantedTrapProgram

The object a trap cast plants: a combat trap (mode 1 with trap) watches
for its trigger, a trap field (mode 1 without) pulses on its period.
================
*/
func plantedTrapProgram(skill enterworld.SkillRow) (skillobject.Program, bool) {
	if trap := skill.CombatTrap; trap.Pinned {
		return skillobject.Program{SkillID: skill.ID, DurationMs: trap.DurationMs, ScanMs: enterworld.CombatTrapScanMs,
			Radius: trap.TriggerRadius, Combat: true, Hidden: trap.Hidden, OwnerDistance: trap.OwnerDistance, LinkGroup: trap.LinkGroup}, true
	}
	if field := skill.TrapField; field.Pinned {
		return skillobject.Program{SkillID: skill.ID, DurationMs: field.DurationMs, ScanMs: enterworld.CombatTrapScanMs,
			Radius: field.Radius, Select: uint32(field.Select), MaxTargets: field.MaxTargets, Pulse: true, PulseMs: field.PulseMs}, true
	}
	return skillobject.Program{}, false
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
		if object.Division == division && object.OwnerGID == owner && object.Program.Combat && object.Program.LinkGroup == trap.LinkGroup {
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

lnks' second word bounds the full 3D separation (5849F1..584A08).
NativeActorDistance preserves the float stores in 430BA0/405270/405250.
================
*/
func combatTrapOwnerNear(object skillobject.Object, owner simulation.Spawn) bool {
	from := monster.Pose{RegionID: object.Spawn.Region, X: float64(object.Spawn.X), Y: float64(object.Spawn.Y), Z: float64(object.Spawn.Z)}
	to := monster.Pose{RegionID: owner.RegionID, X: owner.X, Y: owner.Y, Z: owner.Z}
	return monster.NativeActorDistance(from, to) <= float32(object.Program.OwnerDistance)
}

/*
================
combatTrapVictims

The triggering object first, then the living monsters and attackable
players in the explosion radius around the trap (efr shape 1 centred on
the object), up to the authored cap.
================
*/
func (rt *Runtime) combatTrapVictims(object skillobject.Object, planter *enterworld.Character, skill enterworld.SkillRow, lease instance.Lease, primary combatTarget, area enterworld.SkillOffensiveArea, now int64) []combatTarget {
	from := simulation.Spawn{RegionID: object.Spawn.Region, X: float64(object.Spawn.X), Y: float64(object.Spawn.Y), Z: float64(object.Spawn.Z)}
	out := []combatTarget{primary}
	q := areaQuery{selects: area.Select, division: object.Division, caster: planter, skill: skill, lease: lease, center: from,
		reach: float64(area.Radius), nearest: true, now: now}
	for _, candidate := range rt.areaCandidates(q) {
		if len(out) >= int(area.MaxTargets) {
			break
		}
		if candidate.target.gid == primary.gid {
			continue
		}
		out = append(out, candidate.target)
	}
	return out
}

/*
================
explodeCombatTrap

Each victim takes the trap's att once; damage falls by the area's reduction
per victim in selection order, as the cast-owned area does. The trap object
is not an actor on the wire: 59B2A0 sends the trap identity, not its
planter, who owns credit, rewards and hostility.
================
*/
func (rt *Runtime) explodeCombatTrap(object skillobject.Object, c, snapshot *enterworld.Character, skill enterworld.SkillRow, primary combatTarget, lease instance.Lease, now int64) []simulation.DivisionFrames {
	trap := skill.CombatTrap
	strike := skill
	strike.Attack = trap.Attack
	return rt.strikeFromObject(objectStrike{object: object, c: c, snapshot: snapshot, skill: strike,
		victims: rt.combatTrapVictims(object, snapshot, strike, lease, primary, trap.Area, now), reduction: trap.Area.ReductionPercent,
		result: func(targets []wire.SkillAreaTarget) wire.Frame {
			return wire.SkillTrapResultsFrame(object.Spawn.GID, targets)
		}}, now)
}

/*
================
objectStrike

One strike a planted object resolves for its planter: the strike row, its
victims in selection order, the per-victim reduction and the result frame
that names the strike on the wire.
================
*/
type objectStrike struct {
	object      skillobject.Object
	c, snapshot *enterworld.Character
	skill       enterworld.SkillRow
	victims     []combatTarget
	reduction   uint8
	result      func([]wire.SkillAreaTarget) wire.Frame
}

/*
================
strikeFromObject

Resolves with the planter's live stats and settles through the ordinary
area doors. The public batch carries the object as its source, so scope
publication keeps the object available until the result is consumed.
================
*/
func (rt *Runtime) strikeFromObject(s objectStrike, now int64) []simulation.DivisionFrames {
	division := s.object.Division
	attacker, _, err := rt.playerCombatStats(division, s.snapshot)
	if err != nil || len(s.victims) == 0 {
		return nil
	}
	// One impact a victim, every victim's pose kept: the strike settles
	// each kill where the monster stood.
	plan, planned := rt.planAreaVictims(areaPlanInput{
		division: division, caster: s.c, snapshot: s.snapshot, skill: s.skill, attacker: attacker, victims: s.victims,
		reduction: s.reduction, impacts: 1, poseAll: true, now: now,
	})
	if !planned {
		return nil
	}
	var commit areaCommit
	roster := rt.monsterRewardRoster(division, s.c, now)
	if !rt.deps.UpdateMany(roster.characters, "planted-object-strike", func() bool {
		var ok bool
		commit, ok = rt.commitAreaInDoor(division, s.c, roster, &plan, now)
		return ok
	}) {
		return nil
	}
	published := rt.publishArea(division, s.snapshot, s.skill, 1, plan, commit, now)
	if len(published.targets) == 0 {
		return nil
	}
	public := append([]wire.Frame{s.result(published.targets)}, published.after...)
	public = append(public, commit.settlements.public...)
	public = append(public, published.returned.Broadcast...)
	out := []simulation.DivisionFrames{{DivisionID: division, SourceGID: s.object.Spawn.GID, Frames: simFrames(public)}}
	private := append(wire.ProgressionPrivateFrames(commit.progression), commit.settlements.otherPublic...)
	private = append(private, commit.playerActor...)
	private = append(private, published.returned.ActorPrivate...)
	if len(private) > 0 {
		out = append(out, simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: s.c.ID, Frames: simFrames(private)})
	}
	recipients := append(commit.settlements.others, published.recipients...)
	recipients = append(recipients, published.returned.Recipients...)
	return append(out, recipientDivisionFrames(division, recipients)...)
}
