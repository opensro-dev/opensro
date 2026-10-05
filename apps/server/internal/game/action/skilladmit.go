/*
===========================================================================

skilladmit.go - may this caster use this skill right now?

Skill_ValidatePrerequisitesAndCost (58D8F0). Every skill kind goes through
here, each native call site with its own phase mask.

Named in the binary but not ported yet:

	command +0xC == 0x20  indirect skill (59B933); skips the caster gates
	manager +0x214        set by stns (+0x448); no shipped row  0x3009
	ao / pw               motions 0x12, 8 (players never), 0xF  0x3009
	char +0xC0C           the active pw shield instance       0x3009
	                      (59369F): tele, tel2, tel3 refused;
	                      pw shields are not ported
	manager +0x1DC        only ever stored as 0 (59A5EE, 582DCE, 585351)

Masks 0x02 and 0x20 are applied by the action owners, not here.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
admitMask

The phase argument of 58D8F0:

	0x37    CGCharAutoCommandActor_ProcessCommand 4ACED4    command accepted
	0x17    CGCharAutoCommandActor_Handler_SkillCast 4AE6BD cast begins
	0x91    CGCharAutoCommandActor_Handler_SkillCast 4AEA18 release
	0xFFFF  SkillAction_* through Skill_ValidateCast 58D8D0  execution

==================
*/
type admitMask uint16

const (
	admitCooldown       admitMask = 0x01 // 58E08C -> 0x3005
	admitReplace        admitMask = 0x02 // 58E2BC -> 0x300C
	admitEquipment      admitMask = 0x04 // 58E115 -> 58D480
	admitTargets        admitMask = 0x08 // 58E13E -> 58CC70
	admitResources      admitMask = 0x10 // 58E1AC: HP 0x3013, MP 0x3004
	admitAmmo           admitMask = 0x20 // 58E32D -> 0x300E, cnsm +0x2A0
	admitRange          admitMask = 0x40 // 58E3C7: a clear line to each target
	admitActionRecovery admitMask = 0x80 // 64C1A0: the common action-recovery timer

	admitCommand   admitMask = 0x37
	admitExecution admitMask = 0xffff
)

/*
==================
admitTarget

The action target as 58D8F0 reads it: its motion state (GetMotionState
4AA590) for reqc bit 0, its position (object +0x7C) for the phase 0x40
line, and for a player the record TargetValidation_ValidateAllTargets
checks. A player target never reports motion 8 in this port.
==================
*/
type admitTarget struct {
	motion uint8
	at     simulation.Spawn
	player *enterworld.Character
}

/*
===============================================================================

ADMISSION

===============================================================================
*/

// cooldownGraceMs is how early a skill press may arrive and still cast: one
// that reaches the server within this of the skill becoming ready waits in
// the command queue until it is ready, instead of being refused with 0x3005.
// A deliberate deviation from retail, which refuses it: the browser client
// fires queued presses on its estimate of the server's clock (one delivery
// ahead of the cooldown's end), and the estimate's error must never become a
// refusal the player sees.
const cooldownGraceMs = 150

/*
==================
skillActionRecoveryApplies

64C1A0 checks the common action-recovery timer only for an unchained skill
with a cooldown and an authored action (activity 2).
==================
*/
func skillActionRecoveryApplies(skill enterworld.SkillRow) bool {
	return skill.CoolTimeMs != 0 && skill.ActionKind == 2 && skill.ActionDurationMs > 0 && skill.ChainNext == 0
}

/*
==================
skillReadyAtMs

When c may next cast skill as far as time is concerned: the later of its
cooldown entry and the action-recovery timer, where each applies.
==================
*/
func skillReadyAtMs(c *enterworld.Character, skill enterworld.SkillRow) int64 {
	ready := skillCooldownDeadline(c, skill)
	if skillActionRecoveryApplies(skill) {
		ready = max(ready, c.SkillActionRecoveryUntilMs)
	}
	return ready
}

/*
==================
skillAdmission

The checks this port owns, in native address order; the first failure is
the refusal the client sees. target is nil when the action has none. A
prepared cast is not refused by the cooldown its own preparation installed.

Returns 0 to admit.
==================
*/
func (rt *Runtime) skillAdmission(division string, c *enterworld.Character, skill enterworld.SkillRow, now int64, target *admitTarget, prepared *pendingProjectileCast, mask admitMask) uint16 {
	return rt.contextSkillAdmission(division, c, skill, now, target, prepared, mask, admitContext{})
}

/*
================
admitContext

The extra action-context fields read by 58D8F0 beyond the selected target.
================
*/
type admitContext struct {
	// Indirect is context kind 0x20 (BeginIndirectSkill: an item's skill).
	// 58DA30 skips the frozen/asleep/stunned gates for it.
	Indirect bool
	// TransformRef is +0x20, the RefObj msch word 1 reads.
	TransformRef uint32
}

/*
================
contextSkillAdmission

Preserve native prerequisite order across direct and item-owned skill paths.
================
*/
func (rt *Runtime) contextSkillAdmission(division string, c *enterworld.Character, skill enterworld.SkillRow, now int64, target *admitTarget, prepared *pendingProjectileCast, mask admitMask, context admitContext) uint16 {
	// 58DA3A / 58DAEF: frozen, asleep or stunned, unless the skill has nmf.
	// The codename exemptions (MOB_RM_SEALSTONE, MSKILL_SD_SETH_ATTACK10)
	// are monster-only.
	if !context.Indirect && !skill.CastGate.Nmf && rt.casterDisabled(division, c) {
		return 0x3009
	}

	// 58DB22: rpkt while the skill manager holds an rpkt buff (+0x1F8).
	if skill.CastGate.Rpkt && rt.casterHasRpktBuff(division, c) {
		return 0x3009
	}

	// 58DB38: qest wants a clear area around the caster.
	if skill.CastGate.Qest {
		if code := rt.qestRefusal(division, c, skill.CastGate.Efr3Radius, now); code != 0 {
			return code
		}
	}

	// 58DE1E
	if skill.CastGate.MschPresent {
		if code := rt.mschRefusal(division, c, skill.CastGate.MschMode, context.TransformRef, now); code != 0 {
			return code
		}
	}

	// 58DF20: berserk blocks hiding unless the row is a trap; stealth and
	// invisibility (words 1 and 2) are refused in battle.
	if skill.CastGate.HideGatePresent {
		if !skill.CastGate.TrapPresent && c.NativeBodyStatus == 1 {
			return 0x3031
		}
		mode := skill.CastGate.HideGateMode
		if (mode == 1 || mode == 2) && inBattleState(c, now) {
			return 0x3028
		}
	}

	// 58DF8C: reqc bit 2 keeps the skill for a caster at or below 30 % HP;
	// the product is taken as a float.
	if skill.Reqc.LowHP && rt.casterAboveLowHP(division, c) {
		return 0x3036
	}

	// 58DFE0: reqc bit 0x10 needs command flag 0x10, which 4ACEAD sets when
	// the command is issued in body mode 6 (stealth).
	issued := c.NativeBodyStatus == 6
	if prepared != nil {
		issued = prepared.stealthStrike
	}
	if skill.Reqc.Flag16 && !issued {
		return 0x3034
	}

	// 58DFF4: reqc bit 5 needs skill-manager selector bit 0.
	if skill.Reqc.Dance && !rt.danceSelectorActive(division, c) {
		return 0x3032
	}

	// 58E010: rooted (+0xD34 bit 0x80) with tele or tel3.
	teleports := skill.CastGate.Tele || skill.CastGate.Tel3
	if teleports && rt.casterRooted(division, c) {
		return 0x3009
	}

	if mask&admitCooldown != 0 && prepared == nil && skillCoolingDown(c, skill, now) {
		return 0x3005
	}
	if mask&(admitCooldown|admitActionRecovery) == admitCooldown|admitActionRecovery && prepared == nil &&
		skillActionRecoveryApplies(skill) && now < c.SkillActionRecoveryUntilMs {
		return 0x3005
	}

	// 58E0BF: ao or pw while seated (motion 4), behind a wall (motion 0x11,
	// set at 593690) or riding (state+0xE 1). Motions 0x12, 8 and 0xF are
	// not modelled for players.
	needsFooting := skill.CastGate.Ao || skill.CastGate.Pw
	riding := c.ActiveCOS != nil && c.ActiveCOS.Mounted
	if needsFooting && (riding || rt.casterSitting(division, c) || rt.wallStanding(division, c.Name)) {
		return 0x3009
	}

	if mask&admitEquipment != 0 {
		if code := skillEquipmentRefusal(c, rt.statCatalogs().Items, skill); code != 0 {
			return code
		}
	}

	// 58E13E -> 58CC70: the player target's own checks, then reqc bit 0,
	// which wants a knocked-down target (motion 8, 58D199).
	if mask&admitTargets != 0 && target != nil {
		if forced := rt.effects.ForcedTarget(division, c.Name, now); forced != 0 {
			// 58CF7F compares context+28 for every targeted action, including
			// support skills. A monster cannot be the source of this program.
			if target.player == nil || enterworld.ObjectIDForCharacter(target.player) != forced {
				return 0x3006
			}
		}
		if target.player != nil {
			if code := rt.playerSkillTarget(division, c, target.player, skill, now); code != 0 {
				return code
			}
		}
		if skill.Reqc.KnockedDown && target.motion != 8 {
			return 0x3006
		}
	}

	// Unparsed consumption has no cost to compare; its action owner refuses
	// that row itself.
	if mask&admitResources != 0 && skill.Consumption.Pinned {
		if _, code := rt.offensiveResourceCost(division, c, skill); code != 0 {
			return code
		}
	}

	// 58E490: a blocked line (region manager vfunc +0x78) refuses 0x3010.
	// Distance never does; the command actor walked into reach already.
	if mask&admitRange != 0 && target != nil && rt.LineOfSight != nil {
		from, owner := rt.liveNav(simulation.WorldKey(division, c.Name), c, now)
		if !rt.LineOfSight(from, owner, target.at) {
			return 0x3010
		}
	}

	return 0
}

/*
===============================================================================

EQUIPMENT

===============================================================================
*/

/*
==================
skillEquipmentRefusal

Skill_ValidateEquipmentRequirements (58D480). With reqi pairs only
reqiRefusal decides. Without them the weapon kinds (+0xC7 / +0xC8) are
compared with the primary weapon's TID4, bare hand counting as 1, and
0xFF / 0xFF admits anything.
==================
*/
func skillEquipmentRefusal(c *enterworld.Character, items enterworld.ItemRefSource, skill enterworld.SkillRow) uint16 {
	if skill.Reqi.Present {
		return combat.ReqiRefusal(c, items, skill.Reqi)
	}
	kinds := skill.RequiredWeaponKinds
	if kinds == [2]uint8{0xff, 0xff} {
		return 0
	}

	eq := combat.ReqiEquipment{C: c, Items: items}
	tid4 := uint8(eq.WeaponTID(6) >> 11 & 0x1f)
	if tid4 == 0 {
		tid4 = 1
	}

	var code uint16
	switch {
	case tid4 != kinds[0] && tid4 != kinds[1]:
		code = 0x300d
	case tid4 == 0x10:
		// Skill_ValidateFortressSiegeEquipment (4F1130) needs a siege
		// fortress context (CGObjPC+0x72F*4). Nothing in this port creates
		// one, so a fortress weapon is always refused.
		code = 0x3047
	}

	// 58D719: a broken main weapon is 0x300F whatever the kind said.
	if !eq.SlotUsable(6) {
		return 0x300f
	}
	return code
}

/*
==================
wearsJobSuit

CGItem_IsJobSuit (483280) on equip slot 8 (vfunc +0x578): TID 3/1/7 with
TID4 1..3, the trader, thief and hunter suits. A broken suit still counts.
==================
*/
func wearsJobSuit(c *enterworld.Character, items enterworld.ItemRefSource) bool {
	ref, _, ok := combat.ReqiEquipment{C: c, Items: items}.Item(8)
	if !ok {
		return false
	}
	tid := ref.TypeFlags()
	tid4 := tid >> 11
	return tid&2 == 0 && tid&0x1c == 0xc && tid&0x60 == 0x20 && tid&0x780 == 0x380 &&
		tid4 >= 1 && tid4 <= 3
}

/*
===============================================================================

CASTER STATE

===============================================================================
*/

/*
================
casterDisabled

58DAEF checks freeze, sleep and stun before ordinary skill admission.
================
*/
func (rt *Runtime) casterDisabled(division string, c *enterworld.Character) bool {
	block := rt.playerAbnormal(division, c.Name)
	disabling := abnormal.Freeze.Bit() | abnormal.Sleep.Bit() | abnormal.Stun.Bit()
	return block != nil && block.Mask&disabling != 0
}

/*
================
casterRooted

58E010 checks the native root bit independently of incapacitation.
================
*/
func (rt *Runtime) casterRooted(division string, c *enterworld.Character) bool {
	block := rt.playerAbnormal(division, c.Name)
	return block != nil && block.Mask&abnormal.Root.Bit() != 0
}

/*
================
casterAboveLowHP

58DF8C compares current HP against maximum HP times the widened float 0.3f.
================
*/
func (rt *Runtime) casterAboveLowHP(division string, c *enterworld.Character) bool {
	maxHP, _, currentHP, _ := rt.playerKeeperVitals(division, c)
	return float64(maxHP)*float64(float32(0.3)) < float64(currentHP)
}

/*
================
casterSitting

58E0BF reads sitting from the current world motion owner.
================
*/
func (rt *Runtime) casterSitting(division string, c *enterworld.Character) bool {
	if rt.Worlds == nil {
		return false
	}
	world := rt.Worlds.Snapshot(simulation.WorldKey(division, c.Name), func() simulation.WorldState {
		return simulation.SeedWorldState(c)
	})
	return world.Sitting
}

/*
================
playerMotionState

The player's native motion byte (state+0x2, GetMotionState 4AA590): the
world plane's moving, seated, posture-change and abnormal hold, then the
standing wall (0x11, set at 593690), which the skill owner keeps.
================
*/
func (rt *Runtime) playerMotionState(division string, c *enterworld.Character, nowMs int64) uint8 {
	if rt.Worlds == nil {
		return simulation.MotionNone
	}
	world := rt.Worlds.Snapshot(simulation.WorldKey(division, c.Name), func() simulation.WorldState {
		return simulation.SeedWorldState(c)
	})
	if state := world.MotionStateAt(nowMs); state != simulation.MotionNone {
		return state
	}
	if rt.wallStanding(division, c.Name) {
		return simulation.MotionWall
	}
	return simulation.MotionNone
}

/*
==================
casterHasRpktBuff

CSkillManager+0x1F8, which CSkillManager_ApplyBuffModifiersToActor (594D22)
sets while an installed buff's row carries rpkt.
==================
*/
func (rt *Runtime) casterHasRpktBuff(division string, c *enterworld.Character) bool {
	skills := rt.deps.SkillData()
	if skills == nil {
		return false
	}
	for _, effect := range rt.effects.Snapshot(division, c.Name) {
		if row, ok := skills.SkillByID(effect.SkillID); ok && row.CastGate.Rpkt {
			return true
		}
	}
	return false
}

/*
==================
qestRefusal

58DB38 collects everything within five times the efr kind-3 word around
the caster and refuses on the first object that stands too close:

	skill object (vfunc +0xC0) nearer than the word    0x3037
	monster (vfunc +0x28) nearer than five times it    0x3038

Distances are three-dimensional; the product is taken in 32 bits. Both
object families must be in the caster's current population lifetime.
==================
*/
func (rt *Runtime) qestRefusal(division string, c *enterworld.Character, radius uint32, now int64) uint16 {
	from := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	lease, admitted := rt.EntryPopulationLease(division, c.Name)
	for _, object := range rt.SkillObjects.Snapshot() {
		if !admitted || object.Division != division || object.Population != lease {
			continue
		}
		at := simulation.Spawn{RegionID: object.Spawn.Region, X: float64(object.Spawn.X),
			Y: float64(object.Spawn.Y), Z: float64(object.Spawn.Z)}
		if simulation.IsDungeonRegion(from.RegionID) == simulation.IsDungeonRegion(at.RegionID) &&
			distance3D(from, at) < float64(radius) {
			return 0x3037
		}
	}
	if rt.Monsters == nil {
		return 0
	}
	limit := float64(radius * 5)
	candidates := rt.Monsters.CombatCandidatesForChain(division, from, limit, now)
	if admitted {
		candidates = rt.Monsters.CombatCandidatesInPopulation(division, lease, from, limit, now, true)
	}
	for _, candidate := range candidates {
		mover, ok := rt.Monsters.Mover(division, candidate.Gid)
		if !ok {
			continue
		}
		pose := mover.LivePoseAt(now, nil)
		at := simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
		if distance3D(from, at) < limit {
			return 0x3038
		}
	}
	return 0
}

/*
==================
mschRefusal

58DE1E. Body mode 1 (berserk) refuses every word with 0x3031, then:

	word 1  transformRefusal: the RefObj the context carries
	word 2  no job suit in slot 8 and no horse or fellow (0x3039)

Only a monster mask's indirect cast carries a RefObj; a pressed skill has
none, so word 1 refuses it with 0x3006.
==================
*/
func (rt *Runtime) mschRefusal(division string, c *enterworld.Character, mode, transformRef uint32, now int64) uint16 {
	if c.NativeBodyStatus == 1 {
		return 0x3031
	}
	switch mode {
	case 1:
		return rt.transformRefusal(division, c, transformRef, now)
	case 2:
		riding := c.ActiveCOS != nil && c.ActiveCOS.Mounted
		if riding || wearsJobSuit(c, rt.statCatalogs().Items) {
			return 0x3039
		}
	}
	return 0
}

/*
================
reqiRefusal

Delegate the native reqi walk to the shared equipment contract.
================
*/
func reqiRefusal(c *enterworld.Character, items enterworld.ItemRefSource, req enterworld.SkillReqi) uint16 {
	return combat.ReqiRefusal(c, items, req)
}
