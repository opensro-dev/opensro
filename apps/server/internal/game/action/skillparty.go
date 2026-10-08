/*
===========================================================================

skillparty.go - persistent party auras

CastLifecycle_ProcessPersistent (5830B0) for efr kind 2 rows: bard dances,
the guard aura and the cleric recovery aura.

One update runs, in native order:

	1. caster checks      the caster's persistent instance must still live
	2. onff pulse         pay MP every PulseMs or retire (585262)
	3. leave walk         drop members who left (584C..)
	4. eshp heal          heal the lowest HP ratio (584D95 / 584E5B)
	5. join walk          add party members who came in range

Steps 3-5 are throttled by puls (+0x384) when the row has it.

The port runs that update in two passes over every open aura: first every
retirement (steps 1-3 and the two-Bard rule below), then every installation
(steps 4-5). A retired copy is removed at once, its 0xB6A0 queued ahead of
any 0xB419 of the same update, and each character whose stats moved gets
one 0x343C at the end. Native ends a member's copy synchronously inside its
own update; this port's auras share one tick, so a new aura installed
before an old one ended left the client a stale 0x343C, and an aura settled
away after its children joined ended tokens whose installation reached the
client afterwards.

Joining may also replace another source through its area link. A stopped
source cannot heal or join later in the pass; its remaining children retire
before the final stat publication.

The Bard's auras follow the owner's rules on top of that update (rules 1
and 4 of the Bard specification): a Bard keeps one instrument aura and one
dance at a time, the new cast replacing the old; an aura ends when its
caster runs out of MP, dies or goes through a loading screen; a member who
left the radius, or whose child ended for any other reason, joins again
when the walk finds it back in range. Rule 3 settles two Bards of one
party after every update (settleRivalInstruments).

===========================================================================
*/

package action

import (
	"sort"
	"sync/atomic"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
partyAura

One open area context. token is the caster's persistent instance: the aura
lives exactly as long as that effect. members maps a joined character to
its child instance token; the caster is never a member.
==================
*/
type partyAura struct {
	division   string
	casterName string
	skillID    uint32
	token      uint32
	radius     uint32
	nextPulse  int64
	nextScan   int64
	members    map[string]uint32
	scanDue    bool
}

/*
==================
auraUpdate

One pass of advancePartyAuras: the routed frames in delivery order, and
the characters owed one 0x343C once every retirement and installation of
the pass has landed.
==================
*/
type auraUpdate struct {
	frames []simulation.DivisionFrames
	stats  []auraStatsOwner
	owed   map[string]bool
}

// auraStatsOwner is one character owed a 0x343C.
/*
================
auraStatsOwner
================
*/
type auraStatsOwner struct {
	division string
	c        *enterworld.Character
}

// oweStats records that c's stats moved in this pass.
/*
================
oweStats
================
*/
func (u *auraUpdate) oweStats(division string, c *enterworld.Character) {
	key := simulation.WorldKey(division, c.Name)
	if u.owed[key] {
		return
	}
	if u.owed == nil {
		u.owed = map[string]bool{}
	}
	u.owed[key] = true
	u.stats = append(u.stats, auraStatsOwner{division: division, c: c})
}

/*
===============================================================================

CAST START

===============================================================================
*/

/*
==================
acceptPartyBuff

584115: charge the cast, install the caster's persistent instance and open
the area. Members are chosen by the update, never here. The caster's new
stats ride its own burst only; observers see the cast and the instance.
==================
*/
func (rt *Runtime) acceptPartyBuff(division string, c, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow) OpResult {
	now := rt.Now().UnixMilli()

	payload := skill.BuffModifiers.Present() || skill.Aura.Eshp
	if !skill.Aura.Present || !payload || cast.HasGroundTarget {
		return OpResult{DiagnosticRefusal: "party-buff-admission-refused"}
	}
	if !enterworld.CharacterAlive(snapshot) || !enterworld.SkillLearned(snapshot, skill.ID) {
		return OpResult{DiagnosticRefusal: "party-buff-admission-refused"}
	}
	if rt.hasOpenSkillCast(division, snapshot.Name) {
		return OpResult{DiagnosticRefusal: "party-buff-action-busy"}
	}
	if code := rt.skillAdmission(division, snapshot, skill, now, nil, nil, admitExecution); code != 0 {
		return offensiveRefusal(code)
	}
	// 58E2F4 validates untargeted source instances too. The area link lets
	// 59D9CE retire the previous source instead of opening another healer.
	// A Bard switching its own instrument or dance follows owner rule 1
	// instead (replacesOwnAura).
	if !rt.replacesOwnAura(division, snapshot, skill) && !rt.auraReplacementAllowed(division, snapshot, skill, true) {
		return offensiveRefusal(0x300c)
	}
	radius, ok := rt.auraRadius(division, snapshot, skill)
	if !ok {
		return offensiveRefusal(0x3003)
	}

	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	var frames []wire.Frame
	var refusal uint16
	committed := rt.deps.Update(c, "party-buff", func() bool {
		if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(division, c, skill, now, nil)
		if refusal = code; code != 0 {
			return false
		}
		rt.startSkillCast(division, c, skill, now)
		presentation := EffectPresentation{
			Phase: 1, AreaSourceGID: enterworld.ObjectIDForCharacter(c), AreaSourceName: c.Name,
		}
		installed, ok := rt.commitCharacterEffect(division, c, skill, token, statuseffect.StateActive, true, presentation, now)
		if !ok {
			return false
		}
		rt.replaceOwnAura(division, c, skill, token)
		success := wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: enterworld.ObjectIDForCharacter(c), InstanceToken: token}
		frames = append(frames, wire.SkillCastSelfFrame(success))
		frames = append(frames, installed...)
		rt.commitOffensivePhaseCost(division, c, skill, cost, now, false)
		return true
	})
	if !committed {
		if refusal != 0 {
			return offensiveRefusal(refusal)
		}
		return OpResult{DiagnosticRefusal: "party-buff-commit-refused"}
	}
	rt.queuePersistentRelease(division, c, token, skill, now)
	broadcast := append([]wire.Frame(nil), frames...)
	private := rt.auraStatsFrames(division, c, skill, token)
	frames = append(frames, private...)

	aura := partyAura{
		division:   division,
		casterName: c.Name,
		skillID:    skill.ID,
		token:      token,
		radius:     radius,
		nextPulse:  now + int64(skill.Aura.PulseMs),
		nextScan:   now,
		members:    map[string]uint32{},
	}
	rt.partyAuraMu.Lock()
	rt.partyAuras = append(rt.partyAuras, aura)
	rt.partyAuraMu.Unlock()
	return OpResult{Frames: frames, Broadcast: broadcast, ActorPrivate: private}
}

// auraRadius is the efr radius plus the caster's MUER (+0x544) and DSER
// (+0x54C) when the row asks for them.
/*
================
auraRadius
================
*/
func (rt *Runtime) auraRadius(division string, caster *enterworld.Character, skill enterworld.SkillRow) (uint32, bool) {
	stats, _, err := rt.playerCombatStats(division, caster)
	if err != nil {
		return 0, false
	}
	radius := skill.Aura.Radius
	for _, key := range [...]enterworld.SkillParameter{enterworld.ParameterMusicRange, enterworld.ParameterDanceRange} {
		if skill.Attack.Parameters.Has(key) {
			radius += stats.SkillParameters[key]
		}
	}
	return radius, true
}

/*
===============================================================================

UPDATE

===============================================================================
*/

/*
==================
advancePartyAuras

The 5830B0 update for every open aura in two passes (see the file
header): every retirement, including the owner's two-Bard rule, then every
heal and join on what stayed open, then the owed 0x343C frames. The list
lock is held for the whole update so a cast cannot add an aura mid-update.
==================
*/
func (rt *Runtime) advancePartyAuras(now int64) []simulation.DivisionFrames {
	rt.partyAuraMu.Lock()
	defer rt.partyAuraMu.Unlock()

	var u auraUpdate
	kept := rt.partyAuras[:0]
	for _, aura := range rt.partyAuras {
		if rt.retireAuraStep(&u, &aura, now) {
			kept = append(kept, aura)
		}
	}
	rt.partyAuras = kept
	rt.settleRivalInstruments(&u)

	for i := range rt.partyAuras {
		rt.installAuraStep(&u, &rt.partyAuras[i], now)
	}
	// A recipient replacement can retire another source during the join
	// walk (59DAA9/59DAB0). Finish its remaining children before stats are
	// published; installAuraStep prevents that source from running again.
	kept = rt.partyAuras[:0]
	for _, aura := range rt.partyAuras {
		if rt.auraInstanceLive(aura) {
			kept = append(kept, aura)
		} else {
			rt.retireAura(&u, aura)
		}
	}
	rt.partyAuras = kept
	for _, owner := range u.stats {
		stats, err := rt.PlayerBaseStats(owner.division, owner.c)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{"division": owner.division, "character": owner.c.Name}).Error("aura stat projection failed")
			continue
		}
		frame := simulation.Frame{Opcode: wire.OpBaseStats, Payload: stats.Encode()}
		u.frames = append(u.frames, simulation.DivisionFrames{DivisionID: owner.division, OnlyCharacterID: owner.c.ID, Frames: []simulation.Frame{frame}})
	}
	return u.frames
}

/*
==================
retireAuraStep

Steps 1-3 of one aura's update: the caster checks, the onff pulse and,
when the puls throttle lets the walks run, the leave walk. Returns false
once the aura has retired.
==================
*/
func (rt *Runtime) retireAuraStep(u *auraUpdate, aura *partyAura, now int64) bool {
	aura.scanDue = false
	caster := rt.findCharacter(aura.division, aura.casterName)
	skill, known := rt.deps.SkillData().SkillByID(aura.skillID)
	if caster == nil || !known || !enterworld.CharacterAlive(caster) || !rt.auraInstanceLive(*aura) {
		rt.retireAura(u, *aura)
		return false
	}

	if skill.Aura.PulseMs != 0 && now >= aura.nextPulse {
		paid, ok := rt.pulseAura(aura.division, caster, skill)
		if !ok {
			rt.retireAura(u, *aura)
			return false
		}
		u.frames = append(u.frames, paid...)
		aura.nextPulse = now + int64(skill.Aura.PulseMs)
	}

	if skill.Abnormal.PulsePresent {
		if now < aura.nextScan {
			return true
		}
		aura.nextScan = now + int64(skill.Abnormal.Pulse)
	}
	aura.scanDue = true
	rt.leaveAura(u, aura, caster, now)
	return true
}

/*
==================
installAuraStep

Steps 4-5 of one aura's update, run only when its retireAuraStep let the
walks run.
==================
*/
func (rt *Runtime) installAuraStep(u *auraUpdate, aura *partyAura, now int64) {
	if !aura.scanDue || !rt.auraInstanceLive(*aura) {
		return
	}
	caster := rt.findCharacter(aura.division, aura.casterName)
	skill, known := rt.deps.SkillData().SkillByID(aura.skillID)
	if caster == nil || !known {
		return
	}
	u.frames = append(u.frames, rt.healAura(aura, caster, skill, now)...)
	rt.joinAura(u, aura, caster, skill, now)
}

// auraInstanceLive reports the caster's persistent instance still installed
// and not asked to stop.
/*
================
auraInstanceLive
================
*/
func (rt *Runtime) auraInstanceLive(aura partyAura) bool {
	return rt.instanceLive(aura.division, aura.casterName, aura.skillID, aura.token)
}

// instanceLive reports one instance of skillID still installed on name and
// not asked to stop.
/*
================
instanceLive
================
*/
func (rt *Runtime) instanceLive(division, name string, skillID, token uint32) bool {
	for _, effect := range rt.effects.Snapshot(division, name) {
		if effect.SkillID == skillID && effect.InstanceToken == token {
			return !effect.StopRequested
		}
	}
	return false
}

/*
==================
pulseAura

CastLifecycle_ProcessPersistent 585277..585284: current MP below the raw onff
word 1 retires the aura (jl), before any cut; the charge that follows is the
word at the caster's 0x8D rate cut by BDMD and getv (auraPulseCost). A Bard
holding enough for the cut charge but not the raw word loses the aura, as
native does.
==================
*/
func (rt *Runtime) pulseAura(division string, caster *enterworld.Character, skill enterworld.SkillRow) ([]simulation.DivisionFrames, bool) {
	_, _, _, current := rt.playerKeeperVitals(division, caster)
	if current < int64(skill.Aura.PulseMP) {
		return nil, false
	}
	cost, ok := rt.auraPulseCost(division, caster, skill)
	if !ok {
		return nil, false
	}

	gid := enterworld.ObjectIDForCharacter(caster)
	var vitals []simulation.Frame
	paid := rt.deps.Update(caster, "aura-pulse", func() bool {
		rt.commitOffensiveResources(division, caster, skillCharge{mp: cost})
		payload := simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceCombatDamage, rt.publishedVitals(division, caster))
		vitals = []simulation.Frame{{Opcode: simulation.OpVitalsUpdate, Payload: payload}}
		return true
	})
	if !paid {
		return nil, false
	}
	return []simulation.DivisionFrames{{DivisionID: division, OnlyCharacterID: caster.ID, Frames: vitals}}, true
}

/*
==================
auraPulseCost

onff word 1 at the caster's MP consumption rate (parameter 0x8D, lowered
by Dancing of Mana's dcmp), cut by its BDMD (Music Life): what one pulse
charges.
==================
*/
func (rt *Runtime) auraPulseCost(division string, caster *enterworld.Character, skill enterworld.SkillRow) (int64, bool) {
	stats, _, err := rt.playerCombatStats(division, caster)
	if err != nil {
		return 0, false
	}
	// 583224..583262: the pulse's MP at the caster's 0x8D rate, then the
	// getv cuts (58327A..).
	cost := combat.PreparedCost(0, skill.Aura.PulseMP, 0, true, true, combat.MPConsumptionRate(stats))
	return int64(combat.ApplyMPDecrease(cost, skill.Attack.Parameters, stats.SkillParameters)), true
}

// retireAura ends the caster's instance and every child.
/*
================
retireAura
================
*/
func (rt *Runtime) retireAura(u *auraUpdate, aura partyAura) {
	rt.endAuraInstance(u, aura, aura.casterName, aura.token)
	for name, token := range aura.members {
		rt.endAuraInstance(u, aura, name, token)
	}
}

/*
==================
endAuraInstance

End one instance now, under its owner's door: the registry drops it, the
character-effect teardown runs (finishEndedEffects) and its 0xB6A0 joins
the pass's frames, so no later installation of the pass can reach a client
ahead of it. Its 0x343C is owed to the end of the pass. An owner whose
object is gone only has the instance asked to stop, for the next drain.
==================
*/
func (rt *Runtime) endAuraInstance(u *auraUpdate, aura partyAura, name string, token uint32) {
	owner := rt.findCharacter(aura.division, name)
	if owner == nil {
		rt.effects.RequestVoluntaryStop(aura.division, name, aura.skillID, token)
		return
	}
	var public, actor []wire.Frame
	stats := false
	rt.deps.Update(owner, "aura-retire", func() bool {
		ended := rt.effects.RetireInstances(aura.division, name, []uint32{token})
		for _, e := range ended {
			stats = stats || e.Modifiers.HasWrites()
		}
		public, actor = rt.finishEndedEffects(aura.division, owner, ended, rt.Now().UnixMilli())
		return len(ended) != 0
	})
	if len(public) != 0 {
		u.frames = append(u.frames, simulation.DivisionFrames{DivisionID: aura.division, Frames: simFrames(public)})
	}
	var private []wire.Frame
	for _, frame := range actor {
		if frame.Opcode != wire.OpBaseStats {
			private = append(private, frame)
		}
	}
	if len(private) != 0 {
		u.frames = append(u.frames, simulation.DivisionFrames{DivisionID: aura.division, OnlyCharacterID: owner.ID, Frames: simFrames(private)})
	}
	if stats {
		u.oweStats(aura.division, owner)
	}
}

/*
===============================================================================

MEMBERSHIP

===============================================================================
*/

/*
==================
leaveAura

The set walk at 584C... A member whose object is gone is erased (and, since
per-name effect state outlives a logged-out object here, its child is
stopped too). Everyone else is tested by memberLeaves.

A member whose child already ended is erased as well, so the join walk
right after can hand it a new one. Inferred: native erases a set entry when
the child's own retirement notifies the area (the child dies with the
member's death, a loading screen or an equipment re-check); this port has
no such callback, so the walk reads the child's liveness instead. Without
it the set kept the dead token and the member never joined again.
==================
*/
func (rt *Runtime) leaveAura(u *auraUpdate, aura *partyAura, caster *enterworld.Character, now int64) {
	party := rt.auraParty(aura.division, caster)
	from := rt.liveSpawn(simulation.WorldKey(aura.division, caster.Name), caster, now)

	for name, token := range aura.members {
		member := rt.findCharacter(aura.division, name)
		if member == nil {
			rt.effects.RequestVoluntaryStop(aura.division, name, aura.skillID, token)
			delete(aura.members, name)
			continue
		}
		if !rt.instanceLive(aura.division, name, aura.skillID, token) {
			// Finish a requested stop before the join walk can see the old
			// source link and replace the still-live parent through it.
			rt.endAuraInstance(u, *aura, name, token)
			delete(aura.members, name)
			continue
		}

		to := rt.liveSpawn(simulation.WorldKey(aura.division, name), member, now)
		if !memberLeaves(party, member, from, to, aura.radius) {
			continue
		}
		rt.endAuraInstance(u, *aura, name, token)
		delete(aura.members, name)
	}
}

/*
==================
memberLeaves

The native leave rule, in its order:

  - a member outside the caster's party leaves
  - another plane or sector stays: that branch jumps to the keep path
    (584D95) before distance is read
  - a 3D distance past the radius leaves

Death is never read by this walk.

Native skips the party test when the caster has no party. Owner's rule
(Bard specification, rule 1: the aura is the party's): a caster with no
party keeps the aura on itself only, so when its party dissolves every
ex-member leaves at the next walk. Live, an ex-member of a dissolved
two-member party kept its copy while in range.
==================
*/
func memberLeaves(party map[uint32]bool, member *enterworld.Character, from, to simulation.Spawn, radius uint32) bool {
	if !party[enterworld.ObjectIDForCharacter(member)] {
		return true
	}
	if !samePlaneAdjacent(from, to) {
		return false
	}
	return distance3D(from, to) > float64(radius)
}

/*
==================
joinAura

The party walk after the heal: every other party member not in the set,
alive, on the same plane, within the radius and admitted by buff
replacement gets a child instance under its own authority; its new stats
are owed to the end of the pass. There is no target cap.
==================
*/
func (rt *Runtime) joinAura(u *auraUpdate, aura *partyAura, caster *enterworld.Character, skill enterworld.SkillRow, now int64) {
	party := rt.auraParty(aura.division, caster)
	from := rt.liveSpawn(simulation.WorldKey(aura.division, caster.Name), caster, now)

	for gid := range party {
		member := rt.findCharacterByGid(aura.division, gid)
		if member == nil || member.Name == aura.casterName || aura.members[member.Name] != 0 {
			continue
		}
		if !enterworld.CharacterAlive(member) {
			continue
		}
		to := rt.liveSpawn(simulation.WorldKey(aura.division, member.Name), member, now)
		if !partyAreaReach(from, to, aura.radius) || !rt.auraReplacementAllowed(aura.division, member, skill, false) {
			continue
		}

		child := atomic.AddUint32(&rt.castTokenCounter, 1)
		var installed []wire.Frame
		joined := rt.deps.Update(member, "aura-join", func() bool {
			var ok bool
			// 5850C3/5850CE: recipient mode 2 retains the same source link.
			presentation := EffectPresentation{
				Phase: 2, AuraParent: aura.token,
				AreaSourceGID: enterworld.ObjectIDForCharacter(caster), AreaSourceName: aura.casterName,
			}
			installed, ok = rt.commitCharacterEffect(aura.division, member, skill, child, statuseffect.StateActive, true, presentation, now)
			return ok
		})
		if !joined {
			continue
		}
		aura.members[member.Name] = child
		u.frames = append(u.frames, simulation.DivisionFrames{DivisionID: aura.division, SourceGID: gid, Frames: simFrames(installed)})
		if rt.instanceWrites(aura.division, member, child) {
			u.oweStats(aura.division, member)
		}
	}
}

/*
==================
auraStatsFrames

The owner's private 0x343C after one aura instance was installed. The
frame and its private routing follow the timed self effect's release
(skilltimedeffect.go), which publishes unconditionally. The gate does not:
the frame is sent only when that instance writes parameters, the condition
under which its retirement republishes the block
(drainStoppedCharacterEffects), so an eshp-only aura adds no frame.
==================
*/
func (rt *Runtime) auraStatsFrames(division string, c *enterworld.Character, skill enterworld.SkillRow, token uint32) []wire.Frame {
	if !rt.instanceWrites(division, c, token) {
		return nil
	}
	stats, err := rt.PlayerBaseStats(division, c)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{"division": division, "character": c.Name, "skill": skill.ID}).Error("aura installation stat projection failed")
		return nil
	}
	return []wire.Frame{{Opcode: wire.OpBaseStats, Payload: stats.Encode()}}
}

// instanceWrites reports that c's instance token writes parameters.
/*
================
instanceWrites
================
*/
func (rt *Runtime) instanceWrites(division string, c *enterworld.Character, token uint32) bool {
	for _, effect := range rt.effects.Snapshot(division, c.Name) {
		if effect.InstanceToken == token {
			return effect.Modifiers.HasWrites()
		}
	}
	return false
}

// auraParty is the caster's party (actor+0x1CB8) as a gid set, empty when
// the caster has no party.
/*
================
auraParty
================
*/
func (rt *Runtime) auraParty(division string, caster *enterworld.Character) map[uint32]bool {
	out := map[uint32]bool{}
	if rt.RewardParties == nil {
		return out
	}
	casterGID := enterworld.ObjectIDForCharacter(caster)
	for _, group := range rt.RewardParties(division) {
		for _, gid := range group.Members {
			if gid != casterGID {
				continue
			}
			for _, member := range group.Members {
				out[member] = true
			}
			return out
		}
	}
	return out
}

// auraReplacementAllowed is CSkillManager_ValidateBuffReplacement on the
// member's own manager.
/*
================
auraReplacementAllowed
================
*/
func (rt *Runtime) auraReplacementAllowed(division string, member *enterworld.Character, skill enterworld.SkillRow, casterIsRecipient bool) bool {
	if !skill.ReplacementPinned || skill.Replacement.Lnks {
		return true
	}
	return rt.requestEffectReplacement(division, member, skill, effectReplacementContext{casterIsRecipient: casterIsRecipient})
}

/*
==================
endPartyAurasForLoading

Owner's rule 1: a teleport, a portal or a return to town (any loading
screen) ends the auras the character plays. Inferred: the children it holds
from other casters' auras end with the same loading; it is out of every
radius while it loads, and the join walk hands it a new child once it is
back in range. Both are retired now, before the re-entry packets are built,
so the rebuilt client never sees them; the aura's next update retires the
remaining children of an ended caster instance.
==================
*/
func (rt *Runtime) endPartyAurasForLoading(division string, c *enterworld.Character) {
	if rt.effects == nil {
		return
	}
	skills := rt.deps.SkillData()
	if skills == nil {
		return
	}
	rt.deps.Update(c, "aura-loading-end", func() bool {
		var tokens []uint32
		for _, effect := range rt.effects.Snapshot(division, c.Name) {
			if row, ok := skills.SkillByID(effect.SkillID); ok && row.Aura.Present {
				tokens = append(tokens, effect.InstanceToken)
			}
		}
		ended := rt.effects.RetireInstances(division, c.Name, tokens)
		rt.publishEndedEffects(division, c, ended, rt.Now().UnixMilli())
		return len(ended) != 0
	})
}

/*
===============================================================================

HEALING (eshp)

===============================================================================
*/

/*
==================
healAura

584E5B: heal the member pickHealTarget chose through
CSkillManager_ApplyHealRecovery (5A09F0).
==================
*/
func (rt *Runtime) healAura(aura *partyAura, caster *enterworld.Character, skill enterworld.SkillRow, now int64) []simulation.DivisionFrames {
	if !skill.Aura.Eshp {
		return nil
	}
	who := rt.pickHealTarget(aura, caster, skill)
	if who == nil {
		return nil
	}

	var frame wire.Frame
	healingThreat := makeSkillHealingThreat(aura.division, caster, who, skill)
	healed := rt.deps.Update(who, "aura-heal", func() bool {
		hp, mp, ok := rt.skillHealAmounts(aura.division, who, caster, skill, healAura)
		if !ok {
			return false
		}
		healingThreat.amount = hp + mp
		frame, ok = rt.applySkillRecovery(aura.division, who, hp, mp)
		return ok
	})
	if healed {
		rt.publishSkillHealingThreat(healingThreat, now)
	}
	if !healed || frame.Opcode == 0 {
		return nil
	}
	return []simulation.DivisionFrames{{DivisionID: aura.division, OnlyCharacterID: who.ID, Frames: simFrames([]wire.Frame{frame})}}
}

/*
==================
pickHealTarget

584D95 on the members the leave walk kept, in set (ascending gid) order,
plus the caster when select bit 0 put the caster in the set. Dead members
are scored too.

The choice itself is lowestHPRatio's.
==================
*/
func (rt *Runtime) pickHealTarget(aura *partyAura, caster *enterworld.Character, skill enterworld.SkillRow) *enterworld.Character {
	set := make([]*enterworld.Character, 0, len(aura.members)+1)
	if skill.Aura.Select&1 != 0 {
		set = append(set, caster)
	}
	for name := range aura.members {
		if member := rt.findCharacter(aura.division, name); member != nil {
			set = append(set, member)
		}
	}
	return rt.lowestHPRatio(aura.division, set)
}

/*
==================
lowestHPRatio

584D95's choice over set, in ascending gid order. The ratio is
float32(current / max * 100). The running minimum starts at 0 and 0 means
unset: the first scored actor is taken, a later one only when strictly
lower, and an actor at 0 is always replaced by the next.
==================
*/
func (rt *Runtime) lowestHPRatio(division string, set []*enterworld.Character) *enterworld.Character {
	sort.Slice(set, func(i, j int) bool {
		return enterworld.ObjectIDForCharacter(set[i]) < enterworld.ObjectIDForCharacter(set[j])
	})

	var who *enterworld.Character
	var lowest float32
	for _, one := range set {
		maxHP, _, currentHP, _ := rt.playerKeeperVitals(division, one)
		ratio := float32(float64(currentHP) / float64(maxHP) * 100)
		if lowest == 0 || ratio < lowest {
			who, lowest = one, ratio
		}
	}
	return who
}

/*
===============================================================================

HELPERS

===============================================================================
*/

// simFrames carries wire frames into the tick's routed form unchanged.
/*
================
simFrames
================
*/
func simFrames(in []wire.Frame) []simulation.Frame {
	out := make([]simulation.Frame, len(in))
	for i, frame := range in {
		out[i] = simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope}
	}
	return out
}
