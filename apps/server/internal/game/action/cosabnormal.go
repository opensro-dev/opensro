/*
===========================================================================

cosabnormal.go - abnormal-state blocks of pets (COS)

The division owns each pet's working block. Source facts are captured before
the owner's character transaction so status callbacks cannot reenter the
authority store while its write lock is held.

===========================================================================
*/

package action

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/internal/vitals"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
cosAbnormalEntry

49D240 cures the COS block at +0x34C, independently of its owner's statuses.
================
*/
type cosAbnormalEntry struct {
	division string
	name     string
	gid      uint32
	block    *abnormal.Block
}

/*
================
cosAbnormalStore

Keep immutable pet blocks separate from the character's player block.
================
*/
type cosAbnormalStore struct {
	mu     sync.Mutex
	blocks map[string]cosAbnormalEntry
}

/*
================
cosAbnormalKey

Include the summoned object's identity so a replacement pet inherits no status.
================
*/
func cosAbnormalKey(division, name string, gid uint32) string {
	return strings.ToLower(division) + "\x00" + strings.ToLower(name) + "\x00" + fmt.Sprint(gid)
}

/*
================
cosAbnormal

Return the committed immutable block for one summoned pet.
================
*/
func (rt *Runtime) cosAbnormal(division, name string, gid uint32) *abnormal.Block {
	rt.cosAbnormals.mu.Lock()
	defer rt.cosAbnormals.mu.Unlock()
	return rt.cosAbnormals.blocks[cosAbnormalKey(division, name, gid)].block
}

/*
================
storeCosAbnormal

Copy on publication so a later callback cannot mutate a committed block.
================
*/
func (rt *Runtime) storeCosAbnormal(division, name string, gid uint32, block *abnormal.Block) {
	rt.cosAbnormals.mu.Lock()
	defer rt.cosAbnormals.mu.Unlock()
	key := cosAbnormalKey(division, name, gid)
	if block == nil || (block.Mask == 0 && !block.Active()) {
		delete(rt.cosAbnormals.blocks, key)
		return
	}
	if rt.cosAbnormals.blocks == nil {
		rt.cosAbnormals.blocks = map[string]cosAbnormalEntry{}
	}
	copied := *block
	rt.cosAbnormals.blocks[key] = cosAbnormalEntry{division: division, name: name, gid: gid, block: &copied}
}

/*
================
cosAbnormalCandidates

Release the block registry before entering any character transaction.
================
*/
func (rt *Runtime) cosAbnormalCandidates() []cosAbnormalEntry {
	rt.cosAbnormals.mu.Lock()
	defer rt.cosAbnormals.mu.Unlock()
	out := make([]cosAbnormalEntry, 0, len(rt.cosAbnormals.blocks))
	for _, entry := range rt.cosAbnormals.blocks {
		out = append(out, entry)
	}
	return out
}

/*
================
cosAbnormalOwner

Adapt a pet's working block without reading the authority store from callbacks.
================
*/
type cosAbnormalOwner struct {
	pet                                *enterworld.CharacterCOS
	rt                                 *Runtime
	division                           string
	c                                  *enterworld.Character
	block                              *abnormal.Block
	now                                int64
	sources                            map[uint32]abnormalSourceState
	changed                            bool
	fatal                              bool
	aliveBefore                        bool
	died                               bool
	ref                                *enterworld.CharacterRef
	hpChanged, mpChanged, speedChanged bool
	private                            []wire.Frame
	public                             []wire.Frame
	hits                               []abnormalHit
	detonations                        []abnormal.Slot
}

/*
================
newCosAbnormalOwner

Copy the active pet's block; source facts are admitted before the write lock.
================
*/
func (rt *Runtime) newCosAbnormalOwner(division string, c *enterworld.Character, now int64) *cosAbnormalOwner {
	var pet *enterworld.CharacterCOS
	if c != nil {
		pet = c.ActiveCOS
	}
	return rt.newCosAbnormalOwnerForPet(division, c, pet, now)
}

/*
================
newCosAbnormalOwnerForPet

The authority transaction supplies the selected canonical companion. Source
snapshots and write callbacks therefore never change a sister companion.
================
*/
func (rt *Runtime) newCosAbnormalOwnerForPet(division string, c *enterworld.Character, pet *enterworld.CharacterCOS, now int64) *cosAbnormalOwner {
	o := &cosAbnormalOwner{rt: rt, division: division, c: c, pet: pet, now: now, block: &abnormal.Block{}}
	if c == nil || pet == nil {
		return o
	}
	o.aliveBefore = pet.CurrentHP > 0
	if ref, valid := rt.cosReference(pet); valid {
		o.ref = ref
	}
	if current := rt.cosAbnormal(division, c.Name, pet.GID); current != nil {
		copied := *current
		o.block = &copied
	}
	return o
}

/*
================
commit

Retire dead pets' statuses before publishing their replacement snapshot.
================
*/
func (o *cosAbnormalOwner) commit() {
	if o.c == nil || o.pet == nil {
		return
	}
	if o.fatal || o.pet.CurrentHP == 0 {
		pet := o.pet
		if o.aliveBefore && !o.died {
			o.died = true
			o.rt.cancelCompanionCasts(o.division, o.c, pet.GID)
			o.StopMove()
			// The port's persisted summon flag must agree with native LIFE=2
			// (529DE0 -> 490210), or the revival item rejects the dead pet.
			pet.StateFlags &^= 1
			o.private = append(o.private, companionItemStateFrames(o.c, pet)...)
			if pet.Mounted {
				pet.Mounted = false
				o.public = append(o.public, wire.Frame{Opcode: wire.OpCosRideState,
					Payload: wire.EncodeCosRideState(enterworld.ObjectIDForCharacter(o.c), false, pet.GID)})
				o.public = append(o.public, o.rt.refreshMovementEffects(o.division, o.c, o.now)...)
			}
		}
		o.changed = o.block.ClearAll(o) || o.changed
		o.fatal = true
	}
	o.rt.storeCosAbnormal(o.division, o.c.Name, o.pet.GID, o.block)
}

/*
================
Alive

A missing or depleted pet cannot admit a new status.
================
*/
func (o *cosAbnormalOwner) Alive() bool {
	return o.c != nil && o.pet != nil && o.pet.CurrentHP > 0
}

/*
================
IsPlayer

Pet statuses do not use the player's private snapshot channel.
================
*/
func (o *cosAbnormalOwner) IsPlayer() bool { return false }

/*
================
IsMonster

A summoned pet must not dispatch monster AI callbacks.
================
*/
func (o *cosAbnormalOwner) IsMonster() bool { return false }

/*
================
CurrentHP

Expose the pet's vitals, never its owning character's HP.
================
*/
func (o *cosAbnormalOwner) CurrentHP() uint32 {
	if !o.Alive() {
		return 0
	}
	return o.pet.CurrentHP
}

/*
================
MaxHP

Read the effective authored maximum, including status modifiers.
================
*/
func (o *cosAbnormalOwner) MaxHP() uint32 {
	return uint32(o.Param(abnormalMaxHPParam))
}

/*
================
MaxMP

Read the effective authored maximum, not the pet's depleted current gauge.
================
*/
func (o *cosAbnormalOwner) MaxMP() uint32 {
	return uint32(o.Param(abnormalMaxMPParam))
}

/*
================
Param

Evaluate the pet's own RefObjChar keeper and independent abnormal writes.
================
*/
func (o *cosAbnormalOwner) Param(id uint16) float32 {
	return cosParameter(o.ref, o.pet, o.block, id)
}

/*
================
SourceExists

Use values admitted before the authority write, including absent sources.
================
*/
func (o *cosAbnormalOwner) SourceExists(gid uint32) bool {
	return o.sources[gid].exists
}

/*
================
SourceDead

Read the admitted life state without acquiring another store lock.
================
*/
func (o *cosAbnormalOwner) SourceDead(gid uint32) bool {
	return o.sources[gid].dead
}

/*
================
Roll

Pet effects use the owner's deterministic effect stream.
================
*/
func (o *cosAbnormalOwner) Roll(key uint32, chance int32) bool {
	if chance <= 0 || o.c == nil {
		return false
	}
	proc, err := o.rt.effectOutcome(criticalActor{division: o.division, character: o.c.Name}, key, uint32(chance))
	return err == nil && proc
}

/*
================
Now

All callbacks share the admitted simulation timestamp.
================
*/
func (o *cosAbnormalOwner) Now() int64 { return o.now }

/*
================
ParamsChanged

Clamp gauges when a status changes a maximum; defer speed publication until
all callbacks commit. A COS never emits the player's private stat packet.
================
*/
func (o *cosAbnormalOwner) ParamsChanged(speed bool) {
	o.speedChanged = o.speedChanged || speed
	if !o.Alive() || o.ref == nil {
		return
	}
	pet := o.pet
	hp, mp := min(pet.CurrentHP, o.MaxHP()), min(pet.CurrentMP, o.MaxMP())
	o.hpChanged = o.hpChanged || hp != pet.CurrentHP
	o.mpChanged = o.mpChanged || mp != pet.CurrentMP
	pet.CurrentHP, pet.CurrentMP = hp, mp
}

/*
================
SetMotion

Pet status publication has no independent motion carrier in this adapter.
================
*/
func (o *cosAbnormalOwner) SetMotion(uint8, uint8, float32) {}

/*
================
CancelActions

4AA340 stops the summoned mover before canceling its pending command.
================
*/
func (o *cosAbnormalOwner) CancelActions(bool) { o.StopMove() }

/*
================
StopMove

Collection COS own a follower segment; a mounted transport shares its rider's
movement owner. Both paths settle once and collect the native correction.
================
*/
func (o *cosAbnormalOwner) StopMove() {
	o.public = append(o.public, o.rt.stopCompanionMovement(o.division, o.c, o.pet, o.now)...)
}

/*
================
AIEvent

Monster-only status events do not apply to COS owners.
================
*/
func (o *cosAbnormalOwner) AIEvent(uint8, uint8, uint32) {}

/*
================
Hit

Commit periodic damage to the COS, never its owner. The block's callback owns
poison's nonlethal rule; lethal burn/bleeding use the common pet death commit.
================
*/
func (o *cosAbnormalOwner) Hit(source uint32, credited bool, damage uint32, reason uint8, _ abnormal.Status) {
	if !o.Alive() || damage == 0 {
		return
	}
	pet := o.pet
	if credited && source == pet.GID {
		return
	}
	// 52A1E0 enters the rider's battle state for credited periodic hits too.
	// A vanished/uncredited source is passed as null and skips that branch.
	if credited && enterworld.CharacterAlive(o.c) {
		o.public = append(o.public, o.rt.enterBattleState(o.division, o.c, o.now)...)
	}
	pet.CurrentHP -= vitals.HitDebit(pet.CurrentHP, damage)
	o.hits = append(o.hits, abnormalHit{source: source, credited: credited, damage: damage, reason: reason})
	o.hpChanged = true
	o.fatal = pet.CurrentHP == 0
}

/*
================
ConsumeResources

The resource-debit carrier preserves one HP and saturates MP at zero.
================
*/
func (o *cosAbnormalOwner) ConsumeResources(hp, mp int32, _ uint8) {
	if !o.Alive() {
		return
	}
	pet := o.pet
	if hp > 0 {
		pet.CurrentHP = uint32(max(int64(pet.CurrentHP)-int64(hp), 1))
		o.hpChanged = true
	}
	if mp > 0 {
		pet.CurrentMP = uint32(max(int64(pet.CurrentMP)-int64(mp), 0))
		o.mpChanged = true
	}
}

/*
================
Detonate

59B300 keeps authored explosion damage for the packet even on overkill.
================
*/
func (o *cosAbnormalOwner) Detonate(slot abnormal.Slot) {
	o.detonations = append(o.detonations, slot)
	o.Hit(slot.SourceGID, true, slot.Damage1C, abnormalBombReason, abnormal.TimeBomb)
}

/*
================
cosAbnormalPublication

Publish the pet's shared abnormal mask without a player-only private snapshot.
================
*/
func (rt *Runtime) cosAbnormalPublication(gid uint32, o *cosAbnormalOwner) []wire.Frame {
	if o == nil || o.c == nil || o.pet == nil {
		return nil
	}
	// 4A5C60 sends the state snapshot (server 30D2, v1.150 0x36C7) only when
	// vfunc +1C reports a player; the client applies 0x36C7 to the local
	// player (77C110). A COS publishes its mask through the shared vitals
	// channel (dirty bit 0x100 -> 33A6) alone.
	frames := append([]wire.Frame(nil), o.public...)
	if o.hpChanged || o.mpChanged {
		pet := o.pet
		frames = append(frames, wire.Frame{Opcode: simulation.OpVitalsUpdate,
			Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceFlags(abnormalStatusVitalsSource),
				simulation.Vitals{CurrentHP: pet.CurrentHP, CurrentMP: pet.CurrentMP})})
	}
	for _, slot := range o.detonations {
		frames = append(frames, timeBombFrame(gid, slot.Damage1C, o.fatal))
	}
	if o.speedChanged {
		frames = append(frames, rt.refreshCosAbnormalSpeed(o)...)
	}
	if o.died {
		life := beginFatalLifePublication(gid)
		baseline, dead := life.publishDeathBaseline(), life.publishDead()
		frames = append(frames, wire.Frame{Opcode: baseline.opcode, Payload: baseline.payload},
			wire.Frame{Opcode: dead.opcode, Payload: dead.payload})
	}
	if o.changed {
		block := rt.cosAbnormal(o.division, o.c.Name, gid)
		frames = append(frames, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: abnormalVitalsPayload(gid, block)})
	}
	return frames
}

/*
================
characterByCosGID

Resolve a currently summoned pet to its owning character before mutation.
================
*/
func (rt *Runtime) characterByCosGID(division string, gid uint32) *enterworld.Character {
	if gid == 0 || rt.deps == nil {
		return nil
	}
	for _, character := range rt.deps.CharactersForDivision(division) {
		if character != nil && character.CompanionByGID(gid) != nil {
			return character
		}
	}
	return nil
}

/*
================
advanceCosAbnormals

Tick a detached candidate list so registry and authority locks never nest.
================
*/
func (rt *Runtime) advanceCosAbnormals(now int64) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, entry := range rt.cosAbnormalCandidates() {
		out = append(out, rt.advanceOneCosAbnormal(entry, now)...)
	}
	return out
}

/*
================
advanceOneCosAbnormal

Admit source facts before the character write, then publish the committed mask.
================
*/
func (rt *Runtime) advanceOneCosAbnormal(entry cosAbnormalEntry, now int64) []simulation.DivisionFrames {
	unlock := rt.lockDivision(entry.division)
	defer unlock()
	c := rt.findCharacter(entry.division, entry.name)
	if c == nil || c.CompanionByGID(entry.gid) == nil {
		rt.storeCosAbnormal(entry.division, entry.name, entry.gid, nil)
		return nil
	}
	owner := rt.newCosAbnormalOwnerForPet(entry.division, c, c.CompanionByGID(entry.gid), now)
	owner.sources = rt.captureAbnormalSources(entry.division, owner.block, nil)
	committed := rt.deps.Update(c, "cos-abnormal", func() bool {
		if owner.pet.CurrentHP == 0 {
			owner.changed = owner.block.ClearAll(owner)
			owner.fatal = true
		} else if result := owner.block.Update(owner, now); result.Changed {
			owner.changed = true
		}
		owner.commit()
		return owner.changed || owner.hpChanged || owner.mpChanged || owner.speedChanged || len(owner.public) != 0
	})
	if !committed {
		return nil
	}
	frames := rt.cosAbnormalPublication(entry.gid, owner)
	publication := playerAbnormalFrames{actor: append(append([]wire.Frame(nil), frames...), owner.private...), public: frames}
	// 52A1E0 delegates COS hits to the NPC/character hit owner. A player
	// source receives its private echo; the pet's rider is not the victim PC.
	for _, hit := range owner.hits {
		if !hit.credited || hit.reason != abnormalDamageOverTimeReason {
			continue
		}
		if source := rt.findCharacterByGid(entry.division, hit.source); source != nil {
			publication.sources = append(publication.sources, privateFrames{
				source.ID, []wire.Frame{abnormalDamageFrame(entry.gid, hit.damage)},
			})
		}
	}
	return playerAbnormalDivisionFrames(entry.division, c.ID, publication)
}

/*
==================
monsterHitSummonedCOS

monsterHitSummonedCOS applies a monster skill impact to the substituted
COS (529929 -> PC+0x1CD8). The independent COS reference, keeper and movement
owner supply admission; the rider's equipment cannot shield or weaken a pet.
==================
*/
func (rt *Runtime) monsterHitSummonedCOS(divisionID string, instance monster.Instance, recipient monsterCastRecipient, skillID uint32, nowMs int64, release *pendingMonsterCast) (result simulation.MonsterAttackResult) {
	owner := recipient.character
	pet := owner.CompanionByGID(recipient.gid)
	if pet == nil || !pet.Summoned || pet.CurrentHP == 0 || skillID == 0 {
		return result
	}
	skill, ok := rt.deps.SkillData().SkillByID(skillID)
	actionLifecycleMs, actionLifecyclePinned := skill.ActionLifecycleMs()
	attackShape := ok && skill.CombatPinned && skill.Attack.Present && skill.TargetRequired
	timed := actionLifecyclePinned && actionLifecycleMs != 0
	ranged := skill.ActionRangePinned && skill.ActionRange > 0
	if !attackShape || !timed || !ranged {
		return result
	}
	snapshot := rt.characterSnapshot(divisionID, owner)
	if snapshot == nil || snapshot.CompanionByGID(pet.GID) == nil || snapshot.DeletePending {
		return result
	}
	if _, sameWorld := rt.characterMonster(divisionID, snapshot, instance.Gid); !sameWorld {
		return result
	}
	result.TargetAlive = true
	ref, validRef := rt.cosReference(snapshot.CompanionByGID(pet.GID))
	if !validRef {
		return result
	}
	mover, exists := rt.Monsters.Mover(divisionID, instance.Gid)
	if !exists {
		return result
	}
	monsterPose := mover.LivePoseAt(nowMs, nil)
	petPose := rt.companionLiveSpawn(divisionID, snapshot, snapshot.CompanionByGID(recipient.gid), nowMs)
	spacing := simulation.CombatSpacing{ActorBodyRadius: simulation.BodyRadius(instance.Ref.BodyRadius),
		TargetBodyRadius: simulation.BodyRadius(ref.Parameters.BodyRadius), ActionReach: rt.monsterActionReach(instance, skill)}
	if !spacing.Valid() || simulation.IsDungeonRegion(monsterPose.RegionID) != simulation.IsDungeonRegion(petPose.RegionID) {
		return result
	}
	if release == nil && !spacing.Contains(simulation.Spawn{RegionID: monsterPose.RegionID, X: monsterPose.X, Y: monsterPose.Y, Z: monsterPose.Z}, petPose) {
		result.Refusal = simulation.MonsterAttackApproachRequired
		return result
	}
	attacker, err := combat.MonsterInstanceStats(instance)
	if err != nil {
		return result
	}
	ownerBlock := rt.newCosAbnormalOwnerForPet(divisionID, owner, pet, nowMs)
	defender, err := cosCombatStats(ref, pet, ownerBlock.block)
	if err != nil {
		return result
	}
	if release == nil && skill.ActionCastingTimeMs > 0 {
		return rt.prepareMonsterCast(divisionID, instance, monsterCastRecipient{snapshot, pet.GID}, skill, nowMs)
	}
	var records []abnormal.Record
	var formulas []combat.Result
	for range skill.Attack.ImpactCount {
		formula, resolveErr := rt.resolveCombat(criticalActor{division: divisionID, monster: instance.Gid}, skill, attacker, defender)
		if resolveErr != nil || formula.Damage == 0 && !formula.Blocked {
			return result
		}
		formulas = append(formulas, formula)
		if formula.Blocked {
			continue // 5905FB: no damage and no status roll
		}
		rolled, rollErr := rt.rollMonsterOnCOS(cosAbnormalRoll{division: divisionID, caster: instance,
			params: &skill.Abnormal, target: ownerBlock})
		if rollErr != nil {
			return result
		}
		records = append(records, rolled...)
	}
	ownerBlock.sources = rt.captureAbnormalSources(divisionID, ownerBlock.block, records)
	var fatal bool
	var impacts []wire.SkillCastTargetImpact
	var battleFrames []wire.Frame
	hitContext := abnormal.HitContext{Attack: skill.ReplacementPinned && skill.Replacement.MatchesExecutionSelector}
	committed := rt.deps.Update(owner, "monster-cos-hit", func() bool {
		live := owner.CompanionByGID(pet.GID)
		if live == nil || live.GID != pet.GID || live.CurrentHP == 0 {
			return false
		}
		// CGObjCOS_ProcessNormalHit (52A1E0): the owner (COS+0x1CD8) enters
		// battle (4E1DF0) before the pet takes the hit, fatal or not.
		if enterworld.CharacterAlive(owner) {
			battleFrames = rt.enterBattleState(divisionID, owner, nowMs)
		}
		for _, formula := range formulas {
			hitContext.Magical = hitContext.Magical || formula.MagicalDamage != 0
			live.CurrentHP -= vitals.HitDebit(live.CurrentHP, formula.Damage)
			fatal = live.CurrentHP == 0
			impacts = append(impacts, wire.SkillCastTargetImpact{Damage: formula.Damage,
				Fatal: fatal, Blocked: formula.Blocked, ResultFlags: formula.ResultFlags})
			if fatal {
				break
			}
		}
		if fatal {
			ownerBlock.changed = ownerBlock.block.ClearAll(ownerBlock)
			ownerBlock.fatal = true
		} else {
			if ownerBlock.block.Mask != 0 {
				ownerBlock.changed = ownerBlock.block.BreakOnHit(ownerBlock, hitContext) || ownerBlock.changed
			}
			for _, record := range records {
				if ownerBlock.block.Apply(ownerBlock, record, nowMs) {
					ownerBlock.changed = true
				}
			}
		}
		ownerBlock.commit()
		return true
	})
	if !committed {
		return result
	}
	token := uint32(0)
	if release != nil {
		token = release.token
	} else {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
	}
	wireResult := wire.NewStationarySkillCastSingleTargetResult(
		wire.SkillCastSuccess{SkillId: skillID, CasterGid: instance.Gid, InstanceToken: token},
		pet.GID,
		impacts,
	)
	frame := wire.SkillCastSingleTargetResultFrame(wireResult)
	flight := projectileFlightMs(simulation.Spawn{RegionID: monsterPose.RegionID, X: monsterPose.X, Y: monsterPose.Y, Z: monsterPose.Z}, petPose, skill.ProjectileSpeed)
	closeAt := nowMs + max(int64(actionLifecycleMs), flight+1)
	if release == nil {
		rt.queueSkillFinalize(divisionID, monsterCastOwner(instance.Gid), instance.Gid, nowMs, wire.SkillCastReleaseFrame(token, pet.GID))
	} else {
		frame = wire.SkillCastReleaseResultFrame(wireResult)
		closeAt = nowMs + max(int64(skill.ActionDurationMs), flight+1)
	}
	rt.queueSkillFinalize(divisionID, monsterCastOwner(instance.Gid), instance.Gid, closeAt, wire.SkillCastFinalizeFrame(token))
	result.Frames = []simulation.Frame{{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope}}
	result.Frames = append(result.Frames, simulation.Frame{
		Opcode:  simulation.OpVitalsUpdate,
		Payload: simulation.HPRefreshPayload(pet.GID, 0, pet.CurrentHP),
	})
	for _, published := range rt.cosAbnormalPublication(pet.GID, ownerBlock) {
		result.Frames = append(result.Frames, simulation.Frame{Opcode: published.Opcode, Payload: published.Payload})
	}
	for _, f := range battleFrames {
		result.Frames = append(result.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload})
	}
	for _, frame := range ownerBlock.private {
		result.TargetFrames = append(result.TargetFrames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
	}
	result.Accepted = true
	result.TargetAlive = !fatal
	return result
}

/*
================
rollMonsterOnCOS

Roll the monster's authored status rows against the pet identity and level.
================
*/
func (rt *Runtime) rollMonsterOnCOS(input cosAbnormalRoll) ([]abnormal.Record, error) {
	if input.params == nil || !input.params.Present() || input.target.ref == nil {
		return nil, nil
	}
	pet := input.target.pet
	in := abnormal.RollInput{
		Params:      input.params,
		TargetLevel: pet.Level,
		CasterLevel: input.caster.Ref.Level,
		SourceGID:   input.caster.Gid,
		TargetGID:   pet.GID,
		TargetBonus: input.target.Param(abnormalDiseaseBonusParam),
		// 59069B calls 482840: attack COS (band 1) are status-immune.
		TargetImmune: input.target.ref.TidWord&0x7fe == 0x1c6 && input.target.ref.TidWord>>11 == 1,
	}
	for index := range in.TargetResist {
		in.TargetResist[index] = input.target.Param(abnormalElementResistBase + uint16(index))
		in.TargetFlat[index] = input.target.Param(abnormalFlatResistanceBase + uint16(index))
	}
	random := &abnormalRandom{rt: rt, actor: criticalActor{division: input.division, monster: input.caster.Gid}}
	return abnormal.Roll(in, random), random.err
}

/*
================
cosAbnormalRoll

Detached caster and authoritative target inputs for the shared native roll.
================
*/
type cosAbnormalRoll struct {
	division string
	caster   monster.Instance
	params   *abnormal.SkillParams
	target   *cosAbnormalOwner
}

/*
================
ridingCOS

The COS a hostile single-target skill aimed at this player strikes instead.
CSkillManager_InitiateSkillCast (59B5F3) retargets such a skill, from any
caster but the skill system's AutoMob, when the target PC is mounted
(CGObjPC_IsMountedOnCOS, slot 0x540) on a vehicle, riding horse or attack
COS (slots 0x30/0x34/0x38): the ride (PC+0x1D18) takes the hit. The swap
happens once, at cast admission; a cast already admitted on the rider keeps
its target. Zero when the player is not riding a live COS.
================
*/
func ridingCOS(c *enterworld.Character) uint32 {
	if c == nil || c.ActiveCOS == nil {
		return 0
	}
	ride := c.ActiveCOS
	if !ride.Summoned || !ride.Mounted || ride.CurrentHP == 0 {
		return 0
	}
	return ride.GID
}
