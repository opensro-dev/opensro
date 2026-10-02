/*
===========================================================================

skillimbue.go - imbue and other zero-duration self skills

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"sync/atomic"
)

// A zero-action-duration skill still owns admission, MP, cooldown, an accepted
// action and an independently timed effect. Native 5830B0 dispatches category 3.
/*
================
acceptInstantSelfEffect
================
*/
func (rt *Runtime) acceptInstantSelfEffect(division string, character, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow) OpResult {
	instantShape := (skill.InstantSelfEffectPinned || skill.Imbue.Pinned) && !skill.ChainSub
	selfOnly := !cast.HasTarget && !cast.HasGroundTarget
	casterReady := enterworld.CharacterAlive(snapshot) && enterworld.SkillLearned(snapshot, skill.ID)
	if !instantShape || !selfOnly || !casterReady {
		return OpResult{DiagnosticRefusal: "instant-effect-admission-refused"}
	}
	now := rt.Now().UnixMilli()
	// 4AD870 executes activity != 2 through events 0 and 2 immediately,
	// before inspecting the ordinary command queue. An open attack is allowed.
	if rt.skillCastPostureBlocked(division, snapshot, now) {
		return OpResult{DiagnosticRefusal: "instant-effect-action-busy"}
	}
	if _, _, err := combat.PlayerStats(snapshot, rt.statCatalogs()); err != nil {
		return OpResult{DiagnosticRefusal: "instant-effect-loadout-invalid"}
	}
	if _, code := rt.offensiveCost(division, snapshot, skill, now); code != 0 {
		return offensiveRefusal(code)
	}
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	effectToken := atomic.AddUint32(&rt.castTokenCounter, 1)
	var effects []wire.Frame
	var refusal uint16
	label := "skill-instant-self-effect"
	if skill.Imbue.Pinned {
		label = "skill-weapon-imbue"
	}
	if !rt.deps.Update(character, label, func() bool {
		if !enterworld.CharacterAlive(character) || !enterworld.SkillLearned(character, skill.ID) {
			return false
		}
		cost, code := rt.offensiveCost(division, character, skill, now)
		refusal = code
		if code != 0 {
			return false
		}
		rt.startSkillCast(division, character, now)
		if !rt.requestSelfEffectReplacement(division, character, skill) {
			refusal = 0x300c
			return false
		}
		var ok bool
		effects, ok = rt.commitCharacterEffect(division, character, skill, effectToken, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, now)
		if !ok {
			return false
		}
		rt.commitOffensiveCost(division, character, skill, skillCharge{mp: cost, hp: rt.preparedExecutionHPCost(division, character, skill)}, now)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal)
		}
		return OpResult{DiagnosticRefusal: "instant-effect-category-occupied"}
	}
	// 583567 open -> 58405E MP -> 58408A action release -> 584297 attach.
	// The lasting effect has its own token; no animation-duration timer is invented.
	gid := enterworld.ObjectIDForCharacter(snapshot)
	open := wire.SkillCastSelfFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: gid, InstanceToken: token})
	vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceCombatDamage, rt.publishedVitals(division, character))}
	release := wire.SkillCastReleaseFrame(token, gid)
	close := wire.SkillCastFinalizeFrame(token)
	frames := append([]wire.Frame{open, vitals, release, close}, effects...)
	broadcast := append([]wire.Frame{open, release, close}, effects...)
	return OpResult{Frames: frames, Broadcast: broadcast, ActorPrivate: []wire.Frame{vitals}}
}

/*
================
activeWeaponImbue
================
*/
func (rt *Runtime) activeWeaponImbue(division, name string, nowMs int64) (enterworld.SkillImbue, abnormal.SkillParams) {
	if rt.effects == nil || rt.deps.SkillData() == nil {
		return enterworld.SkillImbue{}, abnormal.SkillParams{}
	}
	for _, effect := range rt.effects.Snapshot(division, name) {
		if !effect.Imbue || effect.StopRequested || effect.Expired(nowMs) {
			continue
		}
		if row, ok := rt.deps.SkillData().SkillByID(effect.SkillID); ok && row.Imbue.Pinned {
			return row.Imbue, row.Abnormal
		}
	}
	return enterworld.SkillImbue{}, abnormal.SkillParams{}
}

/*
==================
resolvePlayerImpact

All player impact paths (including linked stages, areas and release-time
projectiles) share the current effect snapshot. att value 5 scales the
separately evaluated imbue damage, not the primary physical attack.

A chained victim (target entry flag 1, pushed by an imbue's efr) loses the
physical lane of both (58F1C4, 58F2E4): it takes the magical lane and the
imbue damage only.
==================
*/
func (rt *Runtime) resolvePlayerImpact(division, name string, skill enterworld.SkillRow, attacker, defender combat.Stats, nowMs int64, chained bool) (combat.Result, error) {
	// A status cast has no att block: its record is a successful zero-damage
	// result with no critical, block or imbue roll (skillstatuscast.go).
	if skill.StatusCast {
		return combat.Result{ResultFlags: 1}, nil
	}
	actor := criticalActor{division: division, character: name}
	lanes := skill.Attack.Flags & 0xc
	if chained {
		lanes &^= 4
	}
	var result combat.Result
	var err error
	if chained && lanes == 0 {
		result = combat.Result{ResultFlags: 1}
	} else {
		split, resolveErr := rt.resolveCombatRequest(combatRequest{
			actor: actor, skill: skill, attacker: attacker, defender: defender, lanes: lanes,
		})
		result, err = split.Defender, resolveErr
	}
	// A blocked impact takes no imbue share (5905FB skips it).
	if err != nil || skill.Attack.Value5 == 0 || result.Blocked {
		return result, err
	}
	imbue, imbueAbnormal := rt.activeWeaponImbue(division, name, nowMs)
	if !imbue.Pinned {
		return result, nil
	}
	imbueLanes := imbue.Attack.Flags & 0xc
	if chained {
		imbueLanes &^= 4
	}
	var extra combat.Result
	if imbueLanes != 0 {
		extra, err = combat.ResolveCalculation(attacker, defender, combat.AttackCalculation{
			Attack: imbue.Attack, OriginalFlags: skill.Attack.Flags, Lanes: imbueLanes, Player: true,
		}, rt.CombatRoll)
	}
	if err != nil {
		return combat.Result{}, err
	}
	result.Damage = uint32(min(uint64(wire.MaxSkillActionDamage), uint64(result.Damage)+uint64(uint16(extra.Damage))*uint64(skill.Attack.Value5)/100))
	// 58F43C adds the imbue's magical word before scaling its damage share.
	result.MagicalDamage += uint32(uint16(extra.MagicalDamage))
	// The imbue's bu block is rolled with the hit's statuses in 590680.
	result.Imbue = imbueAbnormal
	return result, nil
}
