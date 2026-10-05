/*
===========================================================================

skillpulsearea.go - Soul Chaos: the instance strikes the enemies around
its owner on a period

A pulse-area effect (enterworld.SkillPulseArea) installs through the
ordinary timed self-effect release. While its instance lives,
CastLifecycle_ProcessPersistent selects the area afresh each time puls has
elapsed since the last pulse (efr kind 2, centred on the owner, at most
its most-targets, each later victim's share cut by the reduction) and
strikes every victim with the pdmg record (40F5F0). The victims' results
leave in one B0BC (v1.150 B3C6 mode 2, wire.SkillPulseFrame). The hits
are the owner's credited hits; a fatal one settles rewards.

===========================================================================
*/

package action

import (
	"sync"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
pulseAreaClock

The owners whose pulse areas run, and how many pulses each instance has
struck (keyed by its effect token).
================
*/
type pulseAreaClock struct {
	mu     sync.Mutex
	owners map[petOwnerKey]map[uint32]int64
}

/*
================
trackPulseArea
================
*/
func (rt *Runtime) trackPulseArea(division, name string) {
	rt.pulseAreas.mu.Lock()
	defer rt.pulseAreas.mu.Unlock()
	if rt.pulseAreas.owners == nil {
		rt.pulseAreas.owners = map[petOwnerKey]map[uint32]int64{}
	}
	key := petOwnerKey{division: division, name: name}
	if rt.pulseAreas.owners[key] == nil {
		rt.pulseAreas.owners[key] = map[uint32]int64{}
	}
}

/*
================
advancePulseAreas
================
*/
func (rt *Runtime) advancePulseAreas(now int64) []simulation.DivisionFrames {
	rt.pulseAreas.mu.Lock()
	keys := make([]petOwnerKey, 0, len(rt.pulseAreas.owners))
	for key := range rt.pulseAreas.owners {
		keys = append(keys, key)
	}
	rt.pulseAreas.mu.Unlock()
	var out []simulation.DivisionFrames
	for _, key := range keys {
		out = append(out, rt.advancePulseAreaOwner(key, now)...)
	}
	return out
}

/*
================
advancePulseAreaOwner

Every live pulse-area instance of one owner strikes the pulses its age
has reached. An owner with none left is forgotten.
================
*/
func (rt *Runtime) advancePulseAreaOwner(key petOwnerKey, now int64) []simulation.DivisionFrames {
	unlock := rt.lockDivision(key.division)
	defer unlock()
	skills := rt.deps.SkillData()
	c := rt.findCharacter(key.division, key.name)
	var out []simulation.DivisionFrames
	live := map[uint32]bool{}
	if c != nil && skills != nil && rt.effects != nil {
		for _, e := range rt.effects.Snapshot(key.division, key.name) {
			if e.StopRequested || e.DurationPresent && now >= e.ExpiresAtMs {
				continue
			}
			row, ok := skills.SkillByID(e.SkillID)
			if !ok || !row.TimedEffect.PulseArea.Present {
				continue
			}
			live[e.InstanceToken] = true
			due := (now - e.StartedAtMs) / int64(row.TimedEffect.PulseArea.PeriodMs)
			rt.pulseAreas.mu.Lock()
			struck := rt.pulseAreas.owners[key][e.InstanceToken]
			if due > struck {
				rt.pulseAreas.owners[key][e.InstanceToken] = due
			}
			rt.pulseAreas.mu.Unlock()
			if due > struck {
				out = append(out, rt.pulseArea(key.division, c, row, now)...)
			}
		}
	}
	rt.pulseAreas.mu.Lock()
	defer rt.pulseAreas.mu.Unlock()
	for token := range rt.pulseAreas.owners[key] {
		if !live[token] {
			delete(rt.pulseAreas.owners[key], token)
		}
	}
	if len(live) == 0 {
		delete(rt.pulseAreas.owners, key)
	}
	return out
}

/*
================
pulseArea

One pulse: the living enemies around the owner, nearest the selection's
order first, each struck with the pdmg record at its share.
================
*/
func (rt *Runtime) pulseArea(division string, c *enterworld.Character, row enterworld.SkillRow, now int64) []simulation.DivisionFrames {
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil || !enterworld.CharacterAlive(snapshot) {
		return nil
	}
	pulse := row.TimedEffect.PulseArea
	strike := row
	strike.FixedDamage, strike.Attack.ImpactCount = pulse.Fixed, 1
	victims := rt.casterAreaVictims(division, snapshot, strike, pulse.Area, now)
	if len(victims) == 0 {
		return nil
	}
	attacker, _, err := rt.playerCombatStats(division, snapshot)
	if err != nil {
		return nil
	}
	plan, planned := rt.planAreaVictims(areaPlanInput{division: division, caster: c, snapshot: snapshot, skill: strike,
		attacker: attacker, victims: victims, reduction: pulse.Area.ReductionPercent, impacts: 1, now: now})
	if !planned {
		return nil
	}
	source := enterworld.ObjectIDForCharacter(snapshot)
	var targets []wire.SkillAreaTarget
	var after []wire.Frame
	var private []wire.Frame
	var recipients []RecipientFrames
	for _, victim := range plan.victims {
		var result OpResult
		if victim.player >= 0 {
			hit, credited, ok := rt.commitCreditedPlayerHit(division, c, plan.players[victim.player], now)
			if !ok {
				continue
			}
			targets = append(targets, wire.SkillAreaTarget{GID: victim.target.gid, Impacts: hit.struck.impacts})
			result = credited
		} else {
			target := *victim.target.monster
			hit, ok := rt.commitCreditedMonsterHit(division, c, snapshot, strike, target, victim.formulas[0], "pulse-area-kill", now)
			if !ok {
				continue
			}
			targets = append(targets, wire.SkillAreaTarget{GID: target.Gid,
				Impacts: []wire.SkillCastTargetImpact{committedSkillImpact(victim.formulas[0], hit.impacts[0])}})
			result = rt.creditedHitResult(division, target, hit, nil, now)
		}
		after = append(after, result.Broadcast...)
		private = append(private, result.ActorPrivate...)
		recipients = append(recipients, result.Recipients...)
	}
	if len(targets) == 0 {
		return nil
	}
	public := append([]wire.Frame{wire.SkillPulseFrame(source, row.ID, targets)}, after...)
	out := []simulation.DivisionFrames{hawkDivisionFrames(division, source, 0, public)}
	if len(private) != 0 {
		out = append(out, hawkDivisionFrames(division, 0, c.ID, private))
	}
	return append(out, recipientDivisionFrames(division, recipients)...)
}
