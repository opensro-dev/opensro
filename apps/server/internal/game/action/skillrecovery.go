/*
===========================================================================

skillrecovery.go - support casts: self, targeted and party heals, cures, resu

===========================================================================
*/

package action

import (
	"slices"
	"sort"
	"sync/atomic"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// fullHealPercent is the whole heal; a reduction word is taken from it.
	fullHealPercent = 100

	// healAreaKind and healSecondaryShape are efr words 0 and 1 of a
	// targeted heal that also reaches the primary's neighbours: the action
	// area (+0x28C) around the primary target.
	healAreaKind       = 1
	healSecondaryShape = 6
)

/*
==================
acceptSupportSkill

The action owner holds the division lock. Positive-time recovery publishes
only its opening bracket at acceptance. Healing and cost commit together at
release; a refused release closes that bracket without publishing vitals.
==================
*/
func (rt *Runtime) acceptSupportSkill(
	division string,
	character, snapshot *enterworld.Character,
	cast wire.SkillAction,
	skill enterworld.SkillRow,
) OpResult {
	result, _ := rt.acceptSupportSkillPhase(
		division,
		character,
		snapshot,
		cast,
		skill,
		rt.Now().UnixMilli(),
		nil,
	)
	return result
}

/*
==================
acceptSupportSkillPhase

A target out of reach defers the cast behind a support intent: the command
actor walks first, and target validation and the clear line (phases 0x08
and 0x40) belong to execution. A resu row proposes a revival to a dead
target (resurrection.go) and heals nobody.

A party-area row (enterworld SkillRecovery) runs its action vector,
TargetSelection_Party (58BEF0, skillCureVector), at execution: a party heal
applies 5A0850 to every entry, a party resurrection runs the per-target
5946C5 arm (proposeResurrection) on every entry. A targeted heal whose efr
is shape 6 also heals the primary's nearest party members by its reduction
word (secondaryHealTargets).

A heal over time (Mana Cycle, Mana Orbit) heals nobody at release: its
target, or its party vector, receives the row's effect and is healed every
puls by skillhealtime.go. A targeted one cast without a target lands on
the caster when the row admits Self (column 26), the rule 593F50 applies
to an empty cure vector (resolveSkillCureTargets).
==================
*/
func (rt *Runtime) acceptSupportSkillPhase(
	division string,
	character, snapshot *enterworld.Character,
	cast wire.SkillAction,
	skill enterworld.SkillRow,
	now int64,
	release *pendingProjectileCast,
) (OpResult, skillCastDecision) {
	cure := skill.Abnormal.CurePresent()
	resu := skill.Abnormal.AdmitDeadParty
	targeted := ((skill.Heal.Present && !skill.Aura.Eshp) || resu) && skill.TargetRequired
	partyHeal := skill.Recovery.PartyHealPinned
	lowestHeal := skill.Recovery.LowestHealPinned
	partyResu := skill.Recovery.PartyResurrectPinned
	overTime := skill.Recovery.HealOverTimePinned
	partyOverTime := overTime && !skill.TargetRequired
	supported := skill.Recovery.SelfFlatPinned || cure || targeted || partyHeal || lowestHeal || partyResu || partyOverTime
	selfHealOnly := skill.Recovery.SelfFlatPinned && !cure && !targeted
	casterReady := enterworld.SkillLearned(snapshot, skill.ID) &&
		enterworld.CharacterAlive(snapshot)

	if !supported || skill.ChainSub || cast.HasGroundTarget || !casterReady ||
		selfHealOnly && cast.HasTarget {
		return OpResult{DiagnosticRefusal: "recovery-admission-refused"}, skillCastRefused
	}

	recipient, recipientView := character, snapshot
	var target *admitTarget
	var primaryAt simulation.Spawn

	if overTime && targeted && !cast.HasTarget && skill.Targets.Self {
		cast.HasTarget, cast.TargetGid = true, enterworld.ObjectIDForCharacter(snapshot)
	}

	if targeted {
		if !cast.HasTarget || cast.TargetGid == 0 {
			return OpResult{DiagnosticRefusal: "recovery-admission-refused"}, skillCastRefused
		}

		recipient = rt.findCharacterByGid(division, cast.TargetGid)
		recipientView = rt.characterSnapshot(division, recipient)
		if recipientView == nil {
			return offensiveRefusal(0x3006), skillCastRefused
		}

		to := rt.liveSpawn(simulation.WorldKey(division, recipientView.Name), recipientView, now)
		if release == nil {
			spacing, pinned, ok := rt.supportTargetSpacing(snapshot, recipientView, skill)
			if !ok {
				return OpResult{DiagnosticRefusal: "recovery-spacing-unavailable"}, skillCastRefused
			}

			from := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
			if pinned && !spacing.Contains(from, to) {
				return rt.beginSupportApproach(division, character, snapshot, cast, spacing, from, to, now), skillCastDeferred
			}
		}

		target = &admitTarget{at: to, player: recipientView}
		primaryAt = to
	}

	if rt.skillCastPostureBlocked(division, snapshot, now) ||
		release == nil && rt.hasOpenSkillCast(division, snapshot.Name) {
		return OpResult{DiagnosticRefusal: "recovery-action-busy"}, skillCastRefused
	}

	if code := rt.skillAdmission(division, snapshot, skill, now, target, release, admitExecution); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}

	costForPhase := func(c *enterworld.Character) (skillCharge, uint16) {
		return rt.offensivePhaseCost(division, c, skill, now, release)
	}

	if _, code := costForPhase(snapshot); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}

	if release == nil && skill.ActionCastingTimeMs > 0 {
		var refusal uint16
		if !rt.deps.Update(character, "prepare-self-recovery", func() bool {
			if !enterworld.CharacterAlive(character) ||
				!enterworld.SkillLearned(character, skill.ID) {
				return false
			}

			_, refusal = rt.offensiveCost(division, character, skill, now)
			if refusal != 0 {
				return false
			}

			rt.startSkillCast(division, character, skill, now)
			rt.registerPlayerSkillCooldown(division, character, skill, now)
			return true
		}) {
			if refusal != 0 {
				return offensiveRefusal(refusal), skillCastRefused
			}

			return OpResult{DiagnosticRefusal: "recovery-prepare-commit-refused"}, skillCastRefused
		}

		return rt.prepareSupportCast(division, snapshot, cast, skill, now), skillCastAccepted
	}

	var party []uint32
	if partyHeal || partyResu || partyOverTime {
		party = rt.skillCureVector(division, snapshot, skill, cast, now)
	}
	// The cure vector reads the store; resolve it before the caster's door.
	var cureTargets []cureTarget
	if cure {
		cureTargets = rt.resolveSkillCureTargets(division, character, snapshot, skill, cast, now)
	}
	var secondary []uint32
	if targeted && !resu && !overTime {
		secondary = rt.secondaryHealTargets(division, snapshot, recipientView, primaryAt, skill.Abnormal.EffectArea, now)
	}
	// Healing Division: the lowest HP ratio of the party around the caster
	// takes the whole heal, the members nearest it the reduction's share.
	var lowest *enterworld.Character
	if lowestHeal {
		area := skill.Abnormal.EffectArea
		var set []*enterworld.Character
		for _, gid := range rt.partyCureTargets(division, snapshot, area.Radius, area.Select&1 != 0, false, now) {
			if member := rt.characterSnapshot(division, rt.findCharacterByGid(division, gid)); member != nil {
				set = append(set, member)
			}
		}
		if lowest = rt.lowestChainHealTarget(division, set); lowest == nil {
			return OpResult{DiagnosticRefusal: "recovery-admission-refused"}, skillCastRefused
		}
		at := rt.liveSpawn(simulation.WorldKey(division, lowest.Name), lowest, now)
		secondary = rt.secondaryHealTargets(division, snapshot, lowest, at, area, now)
	}
	casterGID := enterworld.ObjectIDForCharacter(snapshot)
	healCaster := !overTime && (skill.Recovery.SelfFlatPinned ||
		targeted && recipient == character ||
		partyHeal && slices.Contains(party, casterGID) ||
		lowest != nil && lowest.ID == snapshot.ID)

	var refusal uint16
	var vitals wire.Frame
	var cureActor, curePublic []wire.Frame
	var cureRecipients []RecipientFrames
	selfThreat := makeSkillHealingThreat(division, character, character, skill)

	if !rt.deps.Update(character, "skill-self-recovery", func() bool {
		if !enterworld.CharacterAlive(character) || !enterworld.SkillLearned(character, skill.ID) {
			return false
		}

		cost, code := costForPhase(character)
		refusal = code
		if code != 0 {
			return false
		}

		if release == nil {
			rt.startSkillCast(division, character, skill, now)
		}
		rt.commitOffensivePhaseCost(division, character, skill, cost, now, release != nil)
		if cure {
			cureActor, curePublic, cureRecipients = rt.applySkillCure(
				division,
				character,
				skill,
				cureTargets,
				now,
			)
		}

		// Only the party-area rows, the heals over time and the HP-cost
		// self heal (Rave Melody) publish a charge-only 0x33A6; the other
		// support rows keep the caster frames they always had
		// (supportCostVitals).
		chargeVitals := partyHeal || lowestHeal || partyResu || overTime ||
			skill.Recovery.SelfFlatPinned && cost.hp != 0
		if !healCaster {
			if chargeVitals {
				vitals = rt.supportCostVitals(division, character, cost)
			}
			return true
		}

		hp, mp, ok := rt.skillHealAmounts(division, character, character, skill, healCast)
		if !ok {
			return false
		}

		selfThreat.amount = hp + mp
		vitals, ok = rt.applySkillRecovery(division, character, hp, mp)
		if ok && vitals.Opcode == 0 && chargeVitals {
			vitals = rt.supportCostVitals(division, character, cost)
		}
		return ok
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}

		return OpResult{DiagnosticRefusal: "recovery-commit-refused"}, skillCastRefused
	}

	rt.publishSkillHealingThreat(selfThreat, now)
	switch {
	case overTime:
		recipients := []*enterworld.Character{recipient}
		if partyOverTime {
			recipients = recipients[:0]
			for _, gid := range party {
				recipients = append(recipients, rt.findCharacterByGid(division, gid))
			}
		}
		curePublic = append(curePublic, rt.installHealsOverTime(division, character, skill, recipients, now)...)
	case partyResu:
		cureRecipients = append(cureRecipients, rt.proposePartyResurrection(division, snapshot, skill, party, now)...)
	case partyHeal:
		cureRecipients = append(cureRecipients, rt.applyPartyHeal(partyHealRequest{
			division: division, caster: character, skill: skill, targets: party, percent: fullHealPercent, now: now})...)
	case lowestHeal:
		if lowest.ID != character.ID {
			cureRecipients = append(cureRecipients, rt.applyPartyHeal(partyHealRequest{
				division: division, caster: character, skill: skill,
				targets: []uint32{enterworld.ObjectIDForCharacter(lowest)}, percent: fullHealPercent, now: now})...)
		}
	case resu:
		if prompt := rt.proposeResurrection(division, snapshot, recipientView, skill, now); len(prompt) != 0 {
			cureRecipients = append(cureRecipients, RecipientFrames{
				CharacterID: recipientView.ID,
				Frames:      prompt,
			})
		}
	case targeted && recipient != character:
		var frame wire.Frame
		healingThreat := makeSkillHealingThreat(division, character, recipient, skill)
		if !rt.deps.Update(recipient, "skill-target-heal", func() bool {
			hp, mp, ok := rt.skillHealAmounts(division, recipient, character, skill, healCast)
			if !ok {
				return false
			}

			var applied bool
			healingThreat.amount = hp + mp
			frame, applied = rt.applySkillRecovery(division, recipient, hp, mp)
			return applied
		}) {
			return OpResult{DiagnosticRefusal: "recovery-commit-refused"}, skillCastRefused
		}

		rt.publishSkillHealingThreat(healingThreat, now)
		if frame.Opcode != 0 {
			cureRecipients = append(cureRecipients, RecipientFrames{
				CharacterID: recipient.ID,
				Frames:      []wire.Frame{frame},
			})
		}
	}
	if len(secondary) != 0 {
		share := fullHealPercent - skill.Abnormal.EffectArea.Reduction
		for _, gid := range secondary {
			cureRecipients = append(cureRecipients, rt.applyPartyHeal(partyHealRequest{
				division: division, caster: character, skill: skill, targets: []uint32{gid}, percent: share, now: now})...)
			share = share * (fullHealPercent - skill.Abnormal.EffectArea.Reduction) / fullHealPercent
		}
	}

	var token uint32
	if release != nil {
		token = release.token
	} else {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
	}

	open := wire.SkillCastSelfFrame(wire.SkillCastSuccess{
		SkillId:       skill.ID,
		CasterGid:     enterworld.ObjectIDForCharacter(snapshot),
		InstanceToken: token,
	})

	lifetime, _ := skill.ActionLifecycleMs()
	if release != nil {
		started := release.releaseAtMs - int64(skill.ActionCastingTimeMs) - 1
		rt.queueSkillFinalize(
			division,
			snapshot.Name,
			enterworld.ObjectIDForCharacter(snapshot),
			max(now+1, started+int64(lifetime)),
			wire.SkillCastFinalizeFrame(token),
		)

		control := wire.SkillCastReleaseFrame(token, enterworld.ObjectIDForCharacter(snapshot))
		return supportCastResult(
			control,
			vitals,
			cureActor,
			curePublic,
			cureRecipients,
		), skillCastAccepted
	}

	rt.queueSkillFinalize(
		division,
		snapshot.Name,
		enterworld.ObjectIDForCharacter(snapshot),
		now+int64(skill.ActionCastingTimeMs),
		wire.SkillCastReleaseFrame(token, enterworld.ObjectIDForCharacter(snapshot)),
	)
	rt.queueSkillFinalize(
		division,
		snapshot.Name,
		enterworld.ObjectIDForCharacter(snapshot),
		now+int64(lifetime),
		wire.SkillCastFinalizeFrame(token),
	)

	return supportCastResult(
		open,
		vitals,
		cureActor,
		curePublic,
		cureRecipients,
	), skillCastAccepted
}

/*
==================
supportCostVitals

The caster's 0x33A6 after the charge when its own recovery published
nothing, for the rows this owner admits beyond the shipped ones: Rave
Melody with a full MP gauge (its flat HP cost), the party-area heals
and resurrections (Group Reverse heals nobody but charges MP) and the
heals over time, which heal nobody at release. Inferred
from the other cost-paying owners (skillposition.go, statuscastarea.go),
not from a native address. Heals on another player, cures and targeted
resurrections keep the caster frames they had: whether native publishes
their charge at the cast is not established. A cast that charged nothing
publishes nothing.
==================
*/
func (rt *Runtime) supportCostVitals(division string, caster *enterworld.Character, cost skillCharge) wire.Frame {
	if cost.mp == 0 && cost.hp == 0 {
		return wire.Frame{}
	}
	gid := enterworld.ObjectIDForCharacter(caster)
	return wire.Frame{
		Opcode:  simulation.OpVitalsUpdate,
		Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceSkillRecovery, rt.publishedVitals(division, caster)),
	}
}

/*
================
partyHealRequest

All recipients share the cast's authoritative release instant.
================
*/
type partyHealRequest struct {
	division string
	caster   *enterworld.Character
	skill    enterworld.SkillRow
	targets  []uint32
	percent  uint32
	now      int64
}

/*
==================
applyPartyHeal

The 5A0850 heal for selected party entries. Ordinary party healing already
healed the caster inside its door; a chain can select the caster as a secondary
recipient. Each member is healed inside its own door and receives its
0x33A6 frame alone. Inferred: a member whose door refuses (gone, or its
stats unavailable) is skipped, since the caster has already paid and the
others are still healed.

percent is the share of the heal each entry receives: fullHealPercent for
a party heal, less for the secondary targets of a shape-6 heal
(secondaryHealTargets).
==================
*/
func (rt *Runtime) applyPartyHeal(request partyHealRequest) []RecipientFrames {
	division, caster, skill, percent := request.division, request.caster, request.skill, request.percent
	casterGID := enterworld.ObjectIDForCharacter(caster)
	var out []RecipientFrames
	for _, gid := range request.targets {
		if gid == casterGID && skill.Abnormal.EffectArea.Shape != healSecondaryShape {
			continue
		}
		member := rt.findCharacterByGid(division, gid)
		if member == nil {
			continue
		}

		var frame wire.Frame
		healingThreat := makeSkillHealingThreat(division, caster, member, skill)
		if !rt.deps.Update(member, "skill-party-heal", func() bool {
			hp, mp, ok := rt.skillHealShareAmounts(division, member, caster, skill, percent)
			if !ok {
				return false
			}

			var applied bool
			healingThreat.amount = hp + mp
			frame, applied = rt.applySkillRecovery(division, member, hp, mp)
			return applied
		}) {
			continue
		}
		rt.publishSkillHealingThreat(healingThreat, request.now)
		if frame.Opcode == 0 {
			continue
		}

		out = append(out, RecipientFrames{CharacterID: member.ID, Frames: []wire.Frame{frame}})
	}
	return out
}

/*
================
lowestChainHealTarget

58C660 sorts party candidates using 58C0E0's double-precision HP ratio.
Unlike the persistent aura's 584D95 scan, zero is not an unset sentinel.
Stable ties retain the party vector order (the party has at most eight).
================
*/
func (rt *Runtime) lowestChainHealTarget(division string, set []*enterworld.Character) *enterworld.Character {
	var who *enterworld.Character
	var lowest float64
	for _, one := range set {
		maxHP, _, currentHP, _ := rt.playerKeeperVitals(division, one)
		ratio := float64(currentHP) / float64(maxHP) * 100
		if who == nil || ratio < lowest {
			who, lowest = one, ratio
		}
	}
	return who
}

/*
==================
secondaryHealTargets

The secondary half of a targeted heal whose efr is shape 6 (Mana Wind,
efr(1,6,100,3,50,4)): up to MaxTargets - 1 other living members of the
caster's party within the radius of the primary target's position, the
nearest first (the offensive shape 6 orders its secondary victims by
centre distance from the primary, skillarea.go), ties in roster order. Rows
without a shape-6 party efr return nothing.

58C9C2..58CA28 assigns the current percentage to the recipient, then
multiplies it by (100 - reduction) / 100 with truncation for the next.
Select bit 0 includes the caster in the chain candidates (58C5E0).
==================
*/
func (rt *Runtime) secondaryHealTargets(division string, caster, primary *enterworld.Character, center simulation.Spawn, area abnormal.EffectArea, now int64) []uint32 {
	if !area.Present || area.Kind != healAreaKind || area.Shape != healSecondaryShape ||
		area.Select&enterworld.SelectParty == 0 || area.MaxTargets < 2 || area.Reduction >= fullHealPercent {
		return nil
	}
	primaryGID := enterworld.ObjectIDForCharacter(primary)
	type candidate struct {
		gid      uint32
		distance float64
	}
	var near []candidate
	if area.Select&1 != 0 && caster.ID != primary.ID {
		at := rt.liveSpawn(simulation.WorldKey(division, caster.Name), caster, now)
		if partyAreaReach(center, at, area.Radius) {
			near = append(near, candidate{gid: enterworld.ObjectIDForCharacter(caster), distance: distance3D(center, at)})
		}
	}
	for _, gid := range rt.partyMembersAround(division, caster, center, area.Radius, false, now) {
		other := rt.findCharacterByGid(division, gid)
		if gid == primaryGID || other == nil {
			continue
		}
		at := rt.liveSpawn(simulation.WorldKey(division, other.Name), other, now)
		near = append(near, candidate{gid: gid, distance: distance3D(center, at)})
	}
	sort.SliceStable(near, func(i, j int) bool { return near[i].distance < near[j].distance })

	out := make([]uint32, 0, len(near))
	for _, one := range near {
		if len(out) == int(area.MaxTargets)-1 {
			break
		}
		out = append(out, one.gid)
	}
	return out
}

/*
==================
proposePartyResurrection

The per-target 5946C5 arm for every party entry: proposeResurrection skips
the living, players above resu word 0 and players already answering a
proposal, so only dead members in range receive the 0x3393 prompt. As with
the targeted row, the heal block is the revival vitals and heals nobody now
(inferred from that shipped targeted rule, run once per vector entry).
==================
*/
func (rt *Runtime) proposePartyResurrection(division string, caster *enterworld.Character, skill enterworld.SkillRow, party []uint32, now int64) []RecipientFrames {
	var out []RecipientFrames
	for _, gid := range party {
		member := rt.characterSnapshot(division, rt.findCharacterByGid(division, gid))
		if member == nil || member.ID == caster.ID {
			continue
		}

		if prompt := rt.proposeResurrection(division, caster, member, skill, now); len(prompt) != 0 {
			out = append(out, RecipientFrames{CharacterID: member.ID, Frames: prompt})
		}
	}
	return out
}

/*
==================
supportCastResult

Frames is the caster's whole burst; ActorPrivate is only its private part
(the vitals and the actor frames), never the public frames: a prepared
release publishes Broadcast through the caster's observed scope, which
reaches the caster too (projectilecast.go), and a public frame in both
reached the caster twice. Live, a released Healing Orbit sent its caster
every 0xB419 twice and the client failed on the repeated buff identity.

An empty vitals frame (a heal that changed nothing and charged nothing) is
left out: a zero frame is opcode 0 on the wire, which the client refuses
and drops the session on. A flat self heal cast at full HP did exactly
that.
==================
*/
func supportCastResult(
	control, vitals wire.Frame,
	actor, public []wire.Frame,
	recipients []RecipientFrames,
) OpResult {
	var private []wire.Frame
	if vitals.Opcode != 0 {
		private = append(private, vitals)
	}
	private = append(private, actor...)

	frames := append([]wire.Frame{control}, private...)
	frames = append(frames, public...)

	return OpResult{
		Frames:       frames,
		Broadcast:    append([]wire.Frame{control}, public...),
		ActorPrivate: private,
		Recipients:   recipients,
	}
}
