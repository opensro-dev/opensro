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
}

/*
===============================================================================

CAST START

===============================================================================
*/

/*
==================
acceptPartyBuff

583657: charge the cast, install the caster's persistent instance and open
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
		rt.startSkillCast(division, c, now)
		installed, ok := rt.commitCharacterEffect(division, c, skill, token, statuseffect.StateActive, true, EffectPresentation{Phase: 1}, now)
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

The 5830B0 update for every open aura, then the owner's two-Bard rule on
what stayed open. The list lock is held for the whole pass so a cast cannot
add an aura mid-update.
==================
*/
func (rt *Runtime) advancePartyAuras(now int64) []simulation.DivisionFrames {
	rt.partyAuraMu.Lock()
	defer rt.partyAuraMu.Unlock()

	var out []simulation.DivisionFrames
	kept := rt.partyAuras[:0]
	for _, aura := range rt.partyAuras {
		frames, open := rt.advanceAura(&aura, now)
		out = append(out, frames...)
		if open {
			kept = append(kept, aura)
		}
	}
	rt.partyAuras = kept
	rt.settleRivalInstruments()
	return out
}

/*
==================
advanceAura

One update of one aura. Returns false once the aura has retired.
==================
*/
func (rt *Runtime) advanceAura(aura *partyAura, now int64) ([]simulation.DivisionFrames, bool) {
	caster := rt.findCharacter(aura.division, aura.casterName)
	skill, known := rt.deps.SkillData().SkillByID(aura.skillID)
	if caster == nil || !known || !enterworld.CharacterAlive(caster) || !rt.auraInstanceLive(*aura) {
		rt.retireAura(*aura)
		return nil, false
	}

	var frames []simulation.DivisionFrames
	if skill.Aura.PulseMs != 0 && now >= aura.nextPulse {
		paid, ok := rt.pulseAura(aura.division, caster, skill)
		if !ok {
			rt.retireAura(*aura)
			return nil, false
		}
		frames = append(frames, paid...)
		aura.nextPulse = now + int64(skill.Aura.PulseMs)
	}

	if skill.Abnormal.PulsePresent {
		if now < aura.nextScan {
			return frames, true
		}
		aura.nextScan = now + int64(skill.Abnormal.Pulse)
	}

	rt.leaveAura(aura, caster, now)
	frames = append(frames, rt.healAura(aura, caster, skill)...)
	frames = append(frames, rt.joinAura(aura, caster, skill, now)...)
	return frames, true
}

// auraInstanceLive reports the caster's persistent instance still installed
// and not asked to stop.
func (rt *Runtime) auraInstanceLive(aura partyAura) bool {
	return rt.instanceLive(aura.division, aura.casterName, aura.skillID, aura.token)
}

// instanceLive reports one instance of skillID still installed on name and
// not asked to stop.
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

585262: current MP below onff word 1 retires the aura; otherwise that word,
cut by the caster's BDMD, is paid.
==================
*/
func (rt *Runtime) pulseAura(division string, caster *enterworld.Character, skill enterworld.SkillRow) ([]simulation.DivisionFrames, bool) {
	_, _, _, current := rt.playerKeeperVitals(division, caster)
	if current < int64(skill.Aura.PulseMP) {
		return nil, false
	}
	stats, _, err := rt.playerCombatStats(division, caster)
	if err != nil {
		return nil, false
	}
	cost := int64(combat.ApplyMPDecrease(int32(skill.Aura.PulseMP), skill.Attack.Parameters, stats.SkillParameters))

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

// retireAura stops the caster's instance and every child.
func (rt *Runtime) retireAura(aura partyAura) {
	rt.stopAuraInstance(aura, aura.casterName, aura.token)
	for name, token := range aura.members {
		rt.stopAuraInstance(aura, name, token)
	}
}

// stopAuraInstance asks one instance to stop, under its owner's door when
// the owner is online.
func (rt *Runtime) stopAuraInstance(aura partyAura, name string, token uint32) {
	request := func() bool {
		rt.effects.RequestVoluntaryStop(aura.division, name, aura.skillID, token)
		return true
	}
	if member := rt.findCharacter(aura.division, name); member != nil {
		rt.deps.Update(member, "aura-retire", request)
		return
	}
	request()
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
func (rt *Runtime) leaveAura(aura *partyAura, caster *enterworld.Character, now int64) {
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
			delete(aura.members, name)
			continue
		}

		to := rt.liveSpawn(simulation.WorldKey(aura.division, name), member, now)
		if !memberLeaves(party, member, from, to, aura.radius) {
			continue
		}
		rt.stopAuraInstance(*aura, name, token)
		delete(aura.members, name)
	}
}

/*
==================
memberLeaves

The native leave rule, in its order:

  - with the caster in a party, another party leaves
  - another plane or sector stays: that branch jumps to the keep path
    (584D95) before distance is read
  - a 3D distance past the radius leaves

Death is never read by this walk.
==================
*/
func memberLeaves(party map[uint32]bool, member *enterworld.Character, from, to simulation.Spawn, radius uint32) bool {
	if len(party) != 0 && !party[enterworld.ObjectIDForCharacter(member)] {
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
replacement gets a child instance under its own authority, and its new
stats privately. There is no target cap.
==================
*/
func (rt *Runtime) joinAura(aura *partyAura, caster *enterworld.Character, skill enterworld.SkillRow, now int64) []simulation.DivisionFrames {
	party := rt.auraParty(aura.division, caster)
	from := rt.liveSpawn(simulation.WorldKey(aura.division, caster.Name), caster, now)

	var out []simulation.DivisionFrames
	for gid := range party {
		member := rt.findCharacterByGid(aura.division, gid)
		if member == nil || member.Name == aura.casterName || aura.members[member.Name] != 0 {
			continue
		}
		if !enterworld.CharacterAlive(member) {
			continue
		}
		to := rt.liveSpawn(simulation.WorldKey(aura.division, member.Name), member, now)
		if !partyAreaReach(from, to, aura.radius) || !rt.auraReplacementAllowed(aura.division, member, skill) {
			continue
		}

		child := atomic.AddUint32(&rt.castTokenCounter, 1)
		var installed []wire.Frame
		joined := rt.deps.Update(member, "aura-join", func() bool {
			var ok bool
			installed, ok = rt.commitCharacterEffect(aura.division, member, skill, child, statuseffect.StateActive, true, EffectPresentation{Phase: 1, AuraParent: aura.token}, now)
			return ok
		})
		if !joined {
			continue
		}
		aura.members[member.Name] = child
		out = append(out, simulation.DivisionFrames{DivisionID: aura.division, SourceGID: gid, Frames: simFrames(installed)})
		if stats := rt.auraStatsFrames(aura.division, member, skill, child); len(stats) != 0 {
			out = append(out, simulation.DivisionFrames{DivisionID: aura.division, OnlyCharacterID: member.ID, Frames: simFrames(stats)})
		}
	}
	return out
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
	writes := false
	for _, effect := range rt.effects.Snapshot(division, c.Name) {
		if effect.InstanceToken == token {
			writes = effect.Modifiers.HasWrites()
			break
		}
	}
	if !writes {
		return nil
	}
	stats, err := rt.PlayerBaseStats(division, c)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{"division": division, "character": c.Name, "skill": skill.ID}).Error("aura installation stat projection failed")
		return nil
	}
	return []wire.Frame{{Opcode: wire.OpBaseStats, Payload: stats.Encode()}}
}

// auraParty is the caster's party (actor+0x1CB8) as a gid set, empty when
// the caster has no party.
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
func (rt *Runtime) auraReplacementAllowed(division string, member *enterworld.Character, skill enterworld.SkillRow) bool {
	if !skill.ReplacementPinned || skill.Replacement.Lnks {
		return true
	}
	return rt.requestSelfEffectReplacement(division, member, skill)
}

/*
===============================================================================

THE BARD'S RULES

===============================================================================
*/

/*
==================
replaceOwnAura

Owner's rule 1: a Bard plays one instrument aura (Guard Tambour, Mana
Tambour, Hit March, Clout March) at a time; casting another replaces the
one already playing. Inferred: a dance replaces the Bard's previous dance
the same way, since a Bard dances one Dancing at a time. Moving and Swing
March are timed buffs of no family and are never touched here.

The old caster instance is asked to stop, exactly as a client cancel does;
the next update finds it stopped and retires its children (advanceAura).
Only the caster's own instances are read: a child the Bard holds from
another Bard's aura has an AuraParentToken. The caller holds c's door.
==================
*/
func (rt *Runtime) replaceOwnAura(division string, c *enterworld.Character, skill enterworld.SkillRow, token uint32) {
	family := skill.AuraFamily()
	if family == enterworld.AuraFamilyNone {
		return
	}
	for _, effect := range rt.effects.Snapshot(division, c.Name) {
		if effect.InstanceToken == token || effect.AuraParentToken != 0 || effect.StopRequested {
			continue
		}
		row, ok := rt.deps.SkillData().SkillByID(effect.SkillID)
		if !ok || row.AuraFamily() != family {
			continue
		}
		rt.effects.RequestVoluntaryStop(division, c.Name, effect.SkillID, effect.InstanceToken)
	}
}

/*
==================
settleRivalInstruments

Owner's rule 3: when two Bards of one party play instrument auras, the
lower-level one is cancelled and the higher one stays; while a Dancing
plays in that party, two DIFFERENT instruments may both stay. Two auras of
one kind (the same ovl2 overlap word: both Guard Tambours, or Hit March
and Clout March) never share a party.

Inferred:
  - the level is the row's required mastery level (skilldata column 36),
    the one number that orders Guard Tambour against Mana Tambour; the
    tier inside a line does not compare across lines
  - the whole loser aura ends (its Bard's instance and every child), not
    one member's copy: the Bard is the one the rule cancels
  - on equal levels the newer cast stays, as the same Bard's new cast
    replaces its old one
  - the rule is settled after every update, so a dance that stops ends the
    coexistence at the next update

The caller holds partyAuraMu.
==================
*/
func (rt *Runtime) settleRivalInstruments() {
	for {
		loser := rt.rivalInstrumentLoser()
		if loser < 0 {
			return
		}
		rt.retireAura(rt.partyAuras[loser])
		rt.partyAuras = append(rt.partyAuras[:loser], rt.partyAuras[loser+1:]...)
	}
}

/*
==================
rivalInstrumentLoser

The index of the first aura rule 3 cancels, or -1. The list is in cast
order, so of two equal levels the earlier index is the older cast.
==================
*/
func (rt *Runtime) rivalInstrumentLoser() int {
	skills := rt.deps.SkillData()
	for i := range rt.partyAuras {
		older := rt.partyAuras[i]
		olderRow, ok := skills.SkillByID(older.skillID)
		if !ok || olderRow.AuraFamily() != enterworld.AuraFamilyInstrument {
			continue
		}
		caster := rt.findCharacter(older.division, older.casterName)
		if caster == nil {
			continue
		}
		party := rt.auraParty(older.division, caster)
		for j := i + 1; j < len(rt.partyAuras); j++ {
			newer := rt.partyAuras[j]
			newerRow, ok := skills.SkillByID(newer.skillID)
			if !ok || newerRow.AuraFamily() != enterworld.AuraFamilyInstrument ||
				newer.division != older.division || newer.casterName == older.casterName {
				continue
			}
			rival := rt.findCharacter(newer.division, newer.casterName)
			if rival == nil || !party[enterworld.ObjectIDForCharacter(rival)] {
				continue
			}
			sameKind := newerRow.Replacement.Ovl2 == olderRow.Replacement.Ovl2
			if !sameKind && rt.danceInParty(older.division, party) {
				continue
			}
			if auraLevel(olderRow) > auraLevel(newerRow) {
				return j
			}
			return i
		}
	}
	return -1
}

// danceInParty reports an open dance aura whose Bard is in party.
func (rt *Runtime) danceInParty(division string, party map[uint32]bool) bool {
	for _, aura := range rt.partyAuras {
		row, ok := rt.deps.SkillData().SkillByID(aura.skillID)
		if !ok || aura.division != division || row.AuraFamily() != enterworld.AuraFamilyDance {
			continue
		}
		if caster := rt.findCharacter(division, aura.casterName); caster != nil && party[enterworld.ObjectIDForCharacter(caster)] {
			return true
		}
	}
	return false
}

// auraLevel is the level rule 3 compares: the row's first required mastery
// level (see settleRivalInstruments).
func auraLevel(row enterworld.SkillRow) int64 {
	return row.Masteries[0].Level
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
func (rt *Runtime) healAura(aura *partyAura, caster *enterworld.Character, skill enterworld.SkillRow) []simulation.DivisionFrames {
	if !skill.Aura.Eshp {
		return nil
	}
	who := rt.pickHealTarget(aura, caster, skill)
	if who == nil {
		return nil
	}

	var frame wire.Frame
	healed := rt.deps.Update(who, "aura-heal", func() bool {
		hp, mp, ok := rt.skillHealAmounts(aura.division, who, caster, skill, healAura)
		if !ok {
			return false
		}
		frame, ok = rt.applySkillRecovery(aura.division, who, hp, mp)
		return ok
	})
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

The ratio is float32(current / max * 100). The running minimum starts at 0
and 0 means unset: the first scored actor is taken, a later one only when
strictly lower, and an actor at 0 is always replaced by the next.
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
	sort.Slice(set, func(i, j int) bool {
		return enterworld.ObjectIDForCharacter(set[i]) < enterworld.ObjectIDForCharacter(set[j])
	})

	var who *enterworld.Character
	var lowest float32
	for _, one := range set {
		maxHP, _, currentHP, _ := rt.playerKeeperVitals(aura.division, one)
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
func simFrames(in []wire.Frame) []simulation.Frame {
	out := make([]simulation.Frame, len(in))
	for i, frame := range in {
		out[i] = simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope}
	}
	return out
}
