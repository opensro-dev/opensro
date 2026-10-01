/*
===========================================================================

monsterstate_abnormal.go - monster abnormal plans and payloads

===========================================================================
*/

package simulation

import (
	"math"
	"sort"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

/*
==================
MonsterAbnormalContext

MonsterAbnormalContext supplies what MonsterState cannot own: caster
resolution (ObjMgr_FindByID and life state), the actor's CZoeZoeRnd stream
and parameter reads that include self effects (combat stats).
==================
*/
type MonsterAbnormalContext interface {
	SourceExists(division string, gid uint32, name string) bool
	SourceDead(division string, gid uint32, name string) bool
	Roll(division string, owner, key uint32, chance int32) bool
	Param(instance monster.Instance, id uint16) float32
	RetiresSkill(skillID uint32, all bool) bool
}

// SetAbnormalContext installs the runtime context before damage traffic.
/*
================
SetAbnormalContext
================
*/
func (s *MonsterState) SetAbnormalContext(ctx MonsterAbnormalContext) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.abnormalContext = ctx
}

// MonsterAbnormalHit is one vfunc 4FC damage request from a status tick
// (reason 2) or a time-bomb detonation (reason 1).
/*
================
MonsterAbnormalHit
================
*/
type MonsterAbnormalHit struct {
	Status     abnormal.Status
	SourceGID  uint32
	SourceName string
	Credited   bool
	Damage     uint32
	Reason     uint8
}

// MonsterAbnormalEffects are the outward consequences of one block
// transaction; MonsterState has already committed the state they describe.
/*
================
MonsterAbnormalEffects
================
*/
type MonsterAbnormalEffects struct {
	// MaskChanged asks the caller to publish the vitals mask (4A5C60 dirty bit).
	MaskChanged bool
	// CancelActions is vfunc 570: pending monster casts are withdrawn.
	CancelActions bool
	EndedSkills   []uint32
	// Halted is the live pose of a StopMove (B2F5 settle).
	Halted *monster.Pose
	// SpeedChanged publishes the effective movement pair (376F).
	SpeedChanged bool
	Hits         []MonsterAbnormalHit
	Detonations  []abnormal.Slot
}

// Merge folds another transaction's consequences into e.
/*
================
Merge
================
*/
func (e *MonsterAbnormalEffects) Merge(other MonsterAbnormalEffects) {
	e.MaskChanged = e.MaskChanged || other.MaskChanged
	e.CancelActions = e.CancelActions || other.CancelActions
	e.EndedSkills = append(e.EndedSkills, other.EndedSkills...)
	e.SpeedChanged = e.SpeedChanged || other.SpeedChanged
	if other.Halted != nil {
		e.Halted = other.Halted
	}
	e.Hits = append(e.Hits, other.Hits...)
	e.Detonations = append(e.Detonations, other.Detonations...)
}

/*
================
monsterAIEvent
================
*/
type monsterAIEvent struct {
	Event, Kind uint8
	Source      uint32
}

// monsterAbnormalOwner adapts one monster under the population lock. It
// mutates a private copy of the instance; the caller commits the copy.
/*
================
monsterAbnormalInput
================
*/
type monsterAbnormalInput struct {
	division string
	ctx      MonsterAbnormalContext
	state    *divisionMonsterState
	// ground re-grounds a halted pose (see groundedLivePose).
	ground   MonsterSpawnGroundResolver
	instance *monster.Instance
	now      int64
	sources  map[uint32]MonsterAbnormalSource
}

/*
================
monsterAbnormalOwner

Mutates one detached instance while the population door is held. Source
lookups are already resolved; callbacks cannot re-enter character authority.
================
*/
type monsterAbnormalOwner struct {
	monsterAbnormalInput
	block                 *abnormal.Block
	fx                    MonsterAbnormalEffects
	ai                    []monsterAIEvent
	names                 map[uint32]string
	walkBefore, runBefore float64
}

/*
================
newMonsterAbnormalOwner
================
*/
func newMonsterAbnormalOwner(input monsterAbnormalInput) *monsterAbnormalOwner {
	instance := input.instance
	var block abnormal.Block
	if instance.Abnormal != nil {
		block = *instance.Abnormal
	}
	o := &monsterAbnormalOwner{monsterAbnormalInput: input, block: &block, names: map[uint32]string{},
		walkBefore: instance.WalkSpeed(), runBefore: instance.RunSpeed()}
	instance.Abnormal = o.block
	for i := range block.Slots {
		if slot := block.Slots[i]; slot.Active {
			o.names[slot.SourceGID] = slot.SourceName
		}
	}
	return o
}

// finish stores the mutated block, or nil once nothing remains.
/*
================
finish
================
*/
func (o *monsterAbnormalOwner) finish() {
	if o.fx.SpeedChanged {
		o.refreshMovementSpeed()
	}
	if !o.block.Active() && o.block.Mask == 0 {
		o.instance.Abnormal = nil
	}
}

/*
================
refreshMovementSpeed

Retiming the full segment preserves its admitted surface path and cell spans.
Changing From would invalidate that ownership and could place the slowed actor
on a different floor. The live fraction stays continuous to millisecond precision.
================
*/
func (o *monsterAbnormalOwner) refreshMovementSpeed() {
	mover, exists := o.state.movers.lookup(o.instance.Gid)
	if !exists {
		return
	}
	before, after := o.walkBefore, o.instance.WalkSpeed()
	if mover.Channel == wire.MoveStateRun {
		before, after = o.runBefore, o.instance.RunSpeed()
	}
	if before <= 0 || after <= 0 || before == after {
		return
	}
	if mover.InFlight(o.now) {
		duration := max(int64(float64(mover.ArriveMs-mover.DepartMs)*before/after), 1)
		elapsed := int64(float64(o.now-mover.DepartMs) * before / after)
		mover.DepartMs = o.now - elapsed
		mover.ArriveMs = mover.DepartMs + duration
	}
	_, channel := mover.NavigationMotion()
	mover.SetNavigationMotion(after, channel)
	o.state.movers.set(o.instance.Gid, mover)
}

/*
================
Alive
================
*/
func (o *monsterAbnormalOwner) Alive() bool { return o.instance.CurrentHP > 0 }

/*
================
IsPlayer
================
*/
func (o *monsterAbnormalOwner) IsPlayer() bool { return false }

/*
================
IsMonster
================
*/
func (o *monsterAbnormalOwner) IsMonster() bool { return true }

/*
================
CurrentHP
================
*/
func (o *monsterAbnormalOwner) CurrentHP() uint32 { return o.instance.CurrentHP }

/*
================
MaxHP
================
*/
func (o *monsterAbnormalOwner) MaxHP() uint32 { return o.instance.EffectiveMaxHP() }

/*
================
MaxMP
================
*/
// 4C382A initializes keeper 4 from RefObjChar MP. All 5,986 enabled monster
// rows in the v1.150 projection have zero MP; do not invent a mana pool.
func (o *monsterAbnormalOwner) MaxMP() uint32 { return 0 }

/*
================
Now
================
*/
func (o *monsterAbnormalOwner) Now() int64 { return o.now }

/*
================
Param
================
*/
func (o *monsterAbnormalOwner) Param(id uint16) float32 {
	if o.ctx == nil {
		return 0
	}
	return o.ctx.Param(*o.instance, id)
}

/*
================
SourceExists
================
*/
func (o *monsterAbnormalOwner) SourceExists(gid uint32) bool {
	source, ok := o.sources[gid]
	return ok && source.Name == o.names[gid] && source.Exists
}

/*
================
SourceDead
================
*/
func (o *monsterAbnormalOwner) SourceDead(gid uint32) bool {
	source, ok := o.sources[gid]
	return ok && source.Name == o.names[gid] && source.Dead
}

/*
================
Roll
================
*/
func (o *monsterAbnormalOwner) Roll(key uint32, chance int32) bool {
	return chance > 0 && o.ctx != nil && o.ctx.Roll(o.division, o.instance.Gid, key, chance)
}

/*
================
ParamsChanged
================
*/
func (o *monsterAbnormalOwner) ParamsChanged(speed bool) {
	o.fx.SpeedChanged = o.fx.SpeedChanged || speed
}

// SetMotion ports vfunc 55C for the monster's motion hold. State zero falls
// back to any still-active freeze/stun/sleep (4AAB60).
/*
================
SetMotion
================
*/
func (o *monsterAbnormalOwner) SetMotion(state, next uint8, delay float32) {
	if state == 0 {
		switch {
		case o.block.Has(abnormal.Freeze):
			state = 0xa
		case o.block.Has(abnormal.Stun):
			state = 9
		case o.block.Has(abnormal.Sleep):
			state = 0x13
		}
	}
	if next == 0xff {
		if state == 0 {
			o.instance.Motion = monster.MotionHold{}
		} else {
			o.instance.Motion = monster.MotionHold{State: state, UntilMs: math.MaxInt64}
		}
		return
	}
	o.instance.Motion = monster.MotionHold{State: state, UntilMs: o.now + int64(float64(delay)*1000)}
}

/*
================
CancelActions

4AA340 first stops an in-flight mover, then retires pending commands. The
mask also gates later movement, but cannot settle a segment already issued.
================
*/
func (o *monsterAbnormalOwner) CancelActions(all bool) {
	o.StopMove()
	o.fx.CancelActions = true
	if o.ctx == nil {
		return
	}
	for index, effect := range o.instance.SelfEffects {
		if effect.Token != 0 && o.ctx.RetiresSkill(effect.SkillID, all) {
			o.fx.EndedSkills = append(o.fx.EndedSkills, effect.Token)
			o.instance.SelfEffects[index] = monster.SelfEffect{}
		}
	}
}

// StopMove ports 4A9430: only a moving actor stops, at its live pose.
/*
================
StopMove
================
*/
func (o *monsterAbnormalOwner) StopMove() {
	mover, ok := o.state.movers.lookup(o.instance.Gid)
	if !ok || !mover.InFlight(o.now) {
		return
	}
	mover.Pose = groundedLivePose(mover, o.now, o.ground)
	if err := mover.Transition(monster.MoverEventDisplaced, mover.TargetGID()); err != nil {
		panic(err)
	}
	o.state.movers.set(o.instance.Gid, mover)
	pose := mover.Pose
	o.fx.Halted = &pose
}

/*
================
AIEvent
================
*/
func (o *monsterAbnormalOwner) AIEvent(event, kind uint8, source uint32) {
	o.ai = append(o.ai, monsterAIEvent{event, kind, source})
}

/*
================
Hit
================
*/
func (o *monsterAbnormalOwner) Hit(source uint32, credited bool, damage uint32, reason uint8, status abnormal.Status) {
	// 52A240 ignores hits after LIFE becomes dead. A later periodic slot in
	// the same update must not add credit after an earlier slot killed it.
	if !o.Alive() || damage == 0 || credited && source == o.instance.Gid {
		return
	}
	hit := MonsterAbnormalHit{Status: status, SourceGID: source, SourceName: o.names[source], Credited: credited, Damage: damage, Reason: reason}
	o.fx.Hits = append(o.fx.Hits, hit)
	o.instance.CurrentHP -= min(o.instance.CurrentHP, damage)
}

/*
================
ConsumeResources
================
*/
func (o *monsterAbnormalOwner) ConsumeResources(int32, int32, uint8) {} // monsters carry no MP pool
// Detonate ports 59B300: a victim with more HP than the bomb takes the bomb
// damage (reason 1); otherwise it takes its remaining HP and dies (flag 80).
/*
================
Detonate
================
*/
func (o *monsterAbnormalOwner) Detonate(slot abnormal.Slot) {
	damage := slot.Damage1C
	if o.instance.CurrentHP <= damage {
		damage = o.instance.CurrentHP
	}
	// 59B300 debits remaining HP on a fatal hit, but its B0BC payload still
	// takes the authored damage word from the original slot.
	o.fx.Detonations = append(o.fx.Detonations, slot)
	o.Hit(slot.SourceGID, true, damage, 1, slot.Status)
}

// applyAbnormalLocked runs the damage consequences on a surviving monster:
// retire the status-specific hit consequences, then apply the hit's statuses.
/*
================
applyAbnormalLocked
================
*/
func (s *MonsterState) applyAbnormalLocked(input monsterAbnormalInput, hit abnormal.HitContext, records []abnormal.Record) MonsterAbnormalEffects {
	instance, state, now := input.instance, input.state, input.now
	if instance.CurrentHP == 0 || !hit.Magical && !hit.Attack && len(records) == 0 {
		return MonsterAbnormalEffects{}
	}
	o := newMonsterAbnormalOwner(input)
	before := o.block.Mask
	if o.block.BreakOnHit(o, hit) {
		o.fx.MaskChanged = true
	}
	for _, r := range records {
		o.names[r.SourceGID] = r.SourceName
		if o.block.Apply(o, r, now) {
			o.fx.MaskChanged = true
		}
	}
	o.fx.MaskChanged = o.fx.MaskChanged || before != o.block.Mask
	o.finish()
	state.queueAIEvents(instance.Gid, o.ai)
	state.trackAbnormal(instance.Gid, instance.Abnormal)
	return o.fx
}

/*
================
trackAbnormal
================
*/
func (state *divisionMonsterState) trackAbnormal(gid uint32, block *abnormal.Block) {
	if block == nil {
		delete(state.abnormalActive, gid)
		return
	}
	if state.abnormalActive == nil {
		state.abnormalActive = make(map[uint32]struct{})
	}
	state.abnormalActive[gid] = struct{}{}
}

/*
================
queueAIEvents
================
*/
func (state *divisionMonsterState) queueAIEvents(gid uint32, events []monsterAIEvent) {
	if len(events) == 0 {
		return
	}
	if state.aiEvents == nil {
		state.aiEvents = make(map[uint32][]monsterAIEvent)
	}
	state.aiEvents[gid] = append(state.aiEvents[gid], events...)
}

// takeAIEvents consumes the tactics events queued by fear and confusion.
/*
================
takeAIEvents
================
*/
func (s *MonsterState) takeAIEvents(division string, gid uint32) []monsterAIEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	events := state.aiEvents[gid]
	delete(state.aiEvents, gid)
	return events
}

// MonsterAbnormalCandidate is one monster with an active block.
/*
================
MonsterAbnormalCandidate
================
*/
type MonsterAbnormalCandidate struct {
	DivisionID string
	Instance   monster.Instance
}

// AbnormalCandidates lists monsters whose blocks need 4A4390 this tick.
/*
================
AbnormalCandidates
================
*/
func (s *MonsterState) AbnormalCandidates() []MonsterAbnormalCandidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []MonsterAbnormalCandidate
	for _, key := range s.populationKeys() {
		division, state := key.division, s.populationForLease(key.division, key.lease)
		for gid := range state.abnormalActive {
			instance, ok := state.instances.lookup(gid)
			if !ok || instance.Abnormal == nil {
				delete(state.abnormalActive, gid)
				continue
			}
			out = append(out, MonsterAbnormalCandidate{division, instance})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DivisionID != out[j].DivisionID {
			return out[i].DivisionID < out[j].DivisionID
		}
		return out[i].Instance.Gid < out[j].Instance.Gid
	})
	return out
}

// MonsterAbnormalPlan is a dry-run of 4A4390 on a detached copy. Commit
// applies it only while the live block and HP are still the planned inputs.
/*
================
MonsterAbnormalPlan
================
*/
type MonsterAbnormalPlan struct {
	Division string
	GID      uint32
	before   *abnormal.Block
	beforeHP uint32
	after    monster.Instance
	Effects  MonsterAbnormalEffects
	ai       []monsterAIEvent
}

// PlanAbnormalUpdate evaluates the update without committing anything.
/*
================
PlanAbnormalUpdate
================
*/
func (s *MonsterState) PlanAbnormalUpdate(division string, gid uint32, now int64) (MonsterAbnormalPlan, bool) {
	before, ok := s.Get(division, gid)
	if !ok || before.Abnormal == nil {
		return MonsterAbnormalPlan{}, false
	}
	var records []abnormal.Record
	for _, slot := range before.Abnormal.Slots {
		if slot.Active {
			records = append(records, abnormal.Record{SourceGID: slot.SourceGID, SourceName: slot.SourceName})
		}
	}
	sources := s.PrepareAbnormalSources(division, records)
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	instance, ok := state.instances.lookup(gid)
	// Resolving source facts released the population door. Reject an
	// intervening hit or status replacement before evaluating its callbacks.
	if !ok || instance.Abnormal != before.Abnormal || instance.CurrentHP != before.CurrentHP {
		return MonsterAbnormalPlan{}, false
	}
	plan := MonsterAbnormalPlan{Division: division, GID: gid, before: instance.Abnormal, beforeHP: instance.CurrentHP}
	copyInstance := instance
	// The dry run must not mutate the live mover; StopMove is a start-only
	// callback, so updates never reach it.
	o := newMonsterAbnormalOwner(monsterAbnormalInput{division: division, ctx: s.abnormalContext, state: state, ground: s.ground, instance: &copyInstance, now: now, sources: sources})
	if instance.CurrentHP == 0 {
		o.block.ClearAll(o)
		o.fx.MaskChanged = true
	} else if result := o.block.Update(o, now); result.Changed {
		o.fx.MaskChanged = true
	}
	o.finish()
	plan.after, plan.Effects, plan.ai = copyInstance, o.fx, o.ai
	return plan, true
}

/*
==================
CommitAbnormalUpdate

CommitAbnormalUpdate publishes a planned update: HP debits with their
contribution records (credited hits only), corpse settlement at zero HP,
the new block and motion hold.
==================
*/
func (s *MonsterState) CommitAbnormalUpdate(plan MonsterAbnormalPlan, now int64) (MonsterDamageResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(plan.Division, plan.GID)
	instance, ok := state.instances.lookup(plan.GID)
	if !ok || instance.Abnormal != plan.before || instance.CurrentHP != plan.beforeHP {
		return MonsterDamageResult{}, false
	}
	before := instance.CurrentHP
	next := instance
	next.Abnormal, next.Motion = plan.after.Abnormal, plan.after.Motion
	var applied uint32
	for _, hit := range plan.Effects.Hits {
		if next.CurrentHP == 0 {
			break
		}
		credit := uint32(0)
		if hit.Credited {
			credit = hit.SourceGID
		}
		state.recordContribution(plan.GID, credit, hit.Damage)
		debit := min(next.CurrentHP, hit.Damage)
		next = finishSummonAction(next, now)
		next.CurrentHP -= debit
		next.DamageSinceSummon += debit
		applied += debit
	}
	if before > 0 && next.CurrentHP == 0 {
		state.settleCorpseLocked(&next, now, s.ground)
	}
	state.instances.set(plan.GID, next)
	state.trackAbnormal(plan.GID, next.Abnormal)
	state.queueAIEvents(plan.GID, plan.ai)
	result := MonsterDamageResult{Population: state.lease, Instance: next, BeforeHP: before, CurrentHP: next.CurrentHP, Applied: applied, Fatal: before > 0 && next.CurrentHP == 0}
	if result.Fatal {
		result.Contributions = state.contributionSnapshot(plan.GID)
	}
	return result, true
}

/*
==================
MonsterAbnormalPayload

MonsterAbnormalPayload is the v1.150 vitals frame body for the mask:
33A6 {gid, source 0x100, flags 4, mask, grade bytes in ascending bit
order over 017FCFC0}.
==================
*/
func MonsterAbnormalPayload(instance monster.Instance) []byte {
	var mask uint32
	var grades []uint8
	if instance.CurrentHP > 0 && instance.Abnormal != nil {
		mask, grades = instance.Abnormal.Mask, instance.Abnormal.Grades()
	}
	w := wire.NewWriter(12).U32(instance.Gid).U16(0x100).U8(4).U32(mask)
	for _, g := range grades {
		w.U8(g)
	}
	return w.Payload()
}

// MonsterSpeedPayload is 376F (research 30D0): the effective movement pair.
/*
================
MonsterSpeedPayload
================
*/
func MonsterSpeedPayload(instance monster.Instance) []byte {
	return wire.NewWriter(12).U32(instance.Gid).F32(float32(instance.WalkSpeed())).F32(float32(instance.RunSpeed())).Payload()
}

// ForgetAbnormalSource detaches a departing caster from every block in the
// division: later ticks run uncredited, and nobody is cured.
/*
================
ForgetAbnormalSource
================
*/
func (s *MonsterState) ForgetAbnormalSource(division string, gid uint32, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range s.populationKeys() {
		if key.division != division {
			continue
		}
		state := s.populationForLease(key.division, key.lease)
		for monsterGID := range state.abnormalActive {
			instance, ok := state.instances.lookup(monsterGID)
			if !ok || instance.Abnormal == nil {
				continue
			}
			block := *instance.Abnormal
			if block.ForgetSource(gid, name) {
				instance.Abnormal = &block
				state.instances.set(monsterGID, instance)
			}
		}
	}
}

// MonsterCorrectionPayload is the B2F5 settle a StopMove publishes.
/*
================
MonsterCorrectionPayload
================
*/
func MonsterCorrectionPayload(gid uint32, pose monster.Pose) []byte {
	return correctionFrame(gid, pose).Payload
}
