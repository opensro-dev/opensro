/*
===========================================================================

discordwave.go - the Bard's Discord Wave lowers monster hostility

Discord Wave (SKILL_EU_BARD_FORGETA_AGGRO_A, efr(1,2,100,4,0,16) ovl2(34)
dtnt(flat,0) mwdt(850) getv(BDMD)) is an instant cast on a friendly target
(Self, Ally, Party; enterworld/skillthreatdecrease.go). "Removes Monsters' hostility toward their target by creating a big wave of
discord around the target": up to efr's four monsters within efr's radius
of that target receive one hate event sourced at the target, whose amounts
are negative (dtnt's flat word plus the mwdt weapon term, and its percent
word), through the same ledger update every hit uses.

Owner's rule: Discord Wave lowers the hostility (dtnt) of up to 4 monsters
around the target so that they let go of whom they were attacking.

Inferred, recorded deliberately:
  - efr select 16 is the non-character objects of 58A020 (monsters), the
    word the Warrior's taunts select their victims with; shape 2 centres
    the area on the selected target, measured as offense areas measure it
    (caster and candidate body radii, server 58ab6d..58ac23).
  - mwdt is the magical weapon term of the pwtt/mwtt, pwdt/mwdt, mwhh/mwmh
    family (tooltip "Weapon Magical Attack Power <v>% Reflect"); like
    mwhh/mwmh it adds 411080's term to the amount, here dtnt's flat cut.
  - the cut applies to the record of each monster's current target, the
    monster's own "target" of the description, whoever that is.

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
acceptDiscordWave

Resolve the friendly target as a timed target buff does (the caster when
none is selected, walking into range first), admit and charge the cast,
then lower the hostility of the monsters around the target.
================
*/
func (rt *Runtime) acceptDiscordWave(division string, c, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64) OpResult {
	if !cast.HasTarget && !cast.HasGroundTarget && skill.Targets.Self {
		cast.HasTarget, cast.TargetGid = true, enterworld.ObjectIDForCharacter(snapshot)
	}
	if !skill.Threat.Decrease || rt.Monsters == nil || !cast.HasTarget || cast.HasGroundTarget || cast.TargetGid == 0 ||
		!enterworld.CharacterAlive(snapshot) || !enterworld.SkillLearned(snapshot, skill.ID) {
		return OpResult{DiagnosticRefusal: "discord-admission-refused"}
	}
	target := rt.findCharacterByGid(division, cast.TargetGid)
	view := rt.characterSnapshot(division, target)
	if view == nil || view.DeletePending || !enterworld.CharacterAlive(view) {
		return offensiveRefusal(0x3006)
	}
	to := rt.liveSpawn(simulation.WorldKey(division, view.Name), view, now)
	spacing, pinned, ok := rt.supportTargetSpacing(snapshot, view, skill)
	if !ok {
		return OpResult{DiagnosticRefusal: "discord-spacing-unavailable"}
	}
	from := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	if pinned && !spacing.Contains(from, to) {
		return rt.beginSupportApproach(division, c, snapshot, cast, spacing, from, to, now)
	}
	if rt.skillCastPostureBlocked(division, snapshot, now) || rt.hasOpenSkillCast(division, snapshot.Name) {
		return OpResult{DiagnosticRefusal: "discord-action-busy"}
	}
	if code := rt.skillAdmission(division, snapshot, skill, now, &admitTarget{at: to, player: view}, nil, admitExecution); code != 0 {
		return offensiveRefusal(code)
	}
	cut, ok := rt.threatDecreaseCut(division, snapshot, skill)
	if !ok {
		return OpResult{DiagnosticRefusal: "discord-weapon-unavailable"}
	}
	var refusal uint16
	if !rt.deps.Update(c, "discord-wave", func() bool {
		if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
			return false
		}
		charge, code := rt.offensivePhaseCost(division, c, skill, now, nil)
		if refusal = code; code != 0 {
			return false
		}
		rt.startSkillCast(division, c, skill, now)
		rt.commitOffensivePhaseCost(division, c, skill, charge, now, false)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal)
		}
		return OpResult{DiagnosticRefusal: "discord-commit-refused"}
	}
	// SkillCombat_ApplyResultRecipients (593D62..593E7B): with dtnt the hate
	// event's source is the cast's target, and its amounts are negative: the
	// flat word plus the weapon term, and the percent word. It runs the
	// ordinary ledger update (5473C0), which clamps at zero.
	decrease := simulation.HostilityEvent{Attacker: cast.TargetGid, Aggression: -int32(cut), Percent: -int32(skill.Threat.DecreasePercent)}
	for _, victim := range rt.discordVictims(division, snapshot, view, to, skill.Threat.Area, now) {
		rt.recordSkillHostility(division, victim.Gid, []simulation.HostilityEvent{decrease}, now)
	}

	casterGID := enterworld.ObjectIDForCharacter(snapshot)
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	lifetime, _ := skill.ActionLifecycleMs()
	rt.queueSkillFinalize(division, snapshot.Name, casterGID, now+int64(lifetime), wire.SkillCastFinalizeFrame(token))
	open := wire.SkillCastAtTargetFrame(wire.SkillCastSuccess{
		SkillId: skill.ID, CasterGid: casterGID, InstanceToken: token, OwnerOrTargetGid: cast.TargetGid,
	})
	vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(casterGID, simulation.VitalsSourceSkillRecovery, rt.publishedVitals(division, c))}
	return OpResult{Frames: []wire.Frame{open, vitals}, Broadcast: []wire.Frame{open}, ActorPrivate: []wire.Frame{vitals}}
}

/*
================
threatDecreaseCut

dtnt's flat word plus the mwdt weapon term (593DFE..593E49): the hate the
event takes away before its percent word.
================
*/
func (rt *Runtime) threatDecreaseCut(division string, snapshot *enterworld.Character, skill enterworld.SkillRow) (uint32, bool) {
	weapon, ok := rt.casterMagicalWeapon(division, snapshot)
	if !ok {
		return 0, false
	}
	cut := skill.Threat.DecreaseFlat
	if weapon.armed && skill.Threat.DecreaseWeaponPercent != 0 {
		cut += uint32(max(0, combat.WeaponHealBonus(weapon.low, weapon.high, weapon.ratio, skill.Threat.DecreaseWeaponPercent)))
	}
	return cut, true
}

/*
================
discordVictims

The first efr MaxTargets living monsters of the target's world within efr
radius of the target, plus the caster's and each candidate's body radius
(the shape-2 measure of areaVictims), in the registry's GID order.
================
*/
func (rt *Runtime) discordVictims(division string, caster, target *enterworld.Character, center simulation.Spawn, area enterworld.SkillOffensiveArea, now int64) []monster.Instance {
	radius, ok := rt.deps.CharacterBodyRadius(caster)
	if !ok || area.MaxTargets == 0 {
		return nil
	}
	world := instance.ID(domain.CharacterWorldInstance(target))
	var out []monster.Instance
	for _, candidate := range rt.Monsters.CombatCandidatesInWorld(division, world, center, float64(area.Radius)+radius, now, false) {
		if _, visible := rt.characterMonster(division, target, candidate.Gid); !visible || candidate.CurrentHP == 0 {
			continue
		}
		out = append(out, candidate)
		if len(out) == int(area.MaxTargets) {
			break
		}
	}
	return out
}
