/*
===========================================================================

projectilecast.go - prepared casts waiting for release (projectiles and support casts)

===========================================================================
*/

package action

import (
	log "github.com/sirupsen/logrus"
	"math"
	"sync/atomic"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
pendingProjectileCast

Preparing -> released (existing finalize queue) OR cancelled. The division
operation lock serializes release, cancel and teardown; the queue mutex only
protects ownership snapshots. No goroutine/timer or second damage authority.
Native projectile handler: B245 starts without results for positive casting time;
it generates results and charges at release, and retains the
bow-shot result for its distance/speed flight timer. Never charge at start.
Self recovery shares this preparation/cancellation owner, but skips monster
resolution and projectile flight. Its gameplay mutation is release-owned too.
==================
*/
type pendingProjectileCast struct {
	executionCost skillCharge // immutable prepared snapshot; never recomputed at release
	// verdictCost and verdictGroup snapshot the cast skill's admission
	// price shape at prepare time, beside executionCost: the waiting
	// verdict prices resources against them instead of resolving the
	// skill row again every tick (#2139). An unpriced cast (Pinned
	// false) was prepared without the snapshot and resolves its row
	// in the verdict, exactly as every cast did before.
	verdictCost               enterworld.SkillConsumption
	verdictGroup              uint32
	supportCast               bool // shares prepare/cancel ownership; has no monster target or flight
	selfEffect                bool // category-three recipient installation at release
	trap                      bool // untargeted; the release plants a combat trap (skillcombattrap.go)
	statusArea                bool // untargeted; the release rolls a caster-centred status area
	threatDecrease            bool // untargeted; the release lowers hostility around the caster (mirage.go)
	divisionID, characterName string
	// ownerKey is simulation.WorldKey(divisionID, characterName), computed
	// once at prepare time; see pendingSkillFinalize.ownerKey (#1820 D).
	ownerKey    string
	characterID int64
	cast        wire.SkillAction
	token       uint32
	rootID      uint32
	releaseAtMs int64
	// stealthStrike is command flag 0x10: issued in body mode 6.
	// The release reads it, not the live body, which the cast's own event
	// retirement has already ended.
	stealthStrike bool
}

/*
================
newPendingProjectileCast

newPendingProjectileCast starts one prepared cast for an owner. The
cached ownerKey is derived here, from the division and character the
caller names, so no literal at a prepare site can leave the key empty
and strand the cast outside the tick's owner scans (#1820 D).
================
*/
func newPendingProjectileCast(divisionID, characterName string) pendingProjectileCast {
	return pendingProjectileCast{
		divisionID:    divisionID,
		characterName: characterName,
		ownerKey:      simulation.WorldKey(divisionID, characterName),
	}
}

/*
================
prepareSupportCast
================
*/
func (rt *Runtime) prepareSupportCast(division string, c *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64) OpResult {
	return rt.prepareSelfCast(division, c, cast, skill, now, false)
}

/*
================
prepareSelfEffectCast
================
*/
func (rt *Runtime) prepareSelfEffectCast(division string, c *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64) OpResult {
	return rt.prepareSelfCast(division, c, cast, skill, now, true)
}

/*
================
prepareSelfCast
================
*/
func (rt *Runtime) prepareSelfCast(division string, c *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64, effect bool) OpResult {
	cost, err := rt.preparedExecutionMPCost(division, c, skill)
	if err != nil {
		return offensiveRefusal(0x3003)
	}
	charge := skillCharge{mp: cost, hp: rt.preparedExecutionHPCost(division, c, skill)}
	p := newPendingProjectileCast(division, c.Name)
	p.executionCost = charge
	p.verdictCost, p.verdictGroup = skill.Consumption, skill.Group
	p.supportCast = !effect
	p.selfEffect = effect
	p.characterID = c.ID
	p.cast = cast
	p.token = atomic.AddUint32(&rt.castTokenCounter, 1)
	p.releaseAtMs = now + int64(skill.ActionCastingTimeMs) + 1
	rt.pendingSkillFinalizesMu.Lock()
	rt.pendingProjectileCasts = append(rt.pendingProjectileCasts, p)
	rt.installCurrentSkillCommandLocked(division, c, p.token, skill)
	rt.pendingSkillFinalizesMu.Unlock()
	open := wire.SkillCastSelfFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: enterworld.ObjectIDForCharacter(c), InstanceToken: p.token})
	return OpResult{Frames: []wire.Frame{open}, Broadcast: []wire.Frame{open}}
}

/*
================
prepareProjectileCast
================
*/
func (rt *Runtime) prepareProjectileCast(division string, c *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64, rootID uint32) OpResult {
	cost, err := rt.preparedExecutionMPCost(division, c, skill)
	if err != nil {
		return offensiveRefusal(0x3003)
	}
	charge := skillCharge{mp: cost, hp: rt.preparedExecutionHPCost(division, c, skill)}
	pending := newPendingProjectileCast(division, c.Name)
	pending.executionCost = charge
	pending.verdictCost, pending.verdictGroup = skill.Consumption, skill.Group
	pending.characterID = c.ID
	pending.cast = cast
	pending.rootID = rootID
	pending.token = atomic.AddUint32(&rt.castTokenCounter, 1)
	pending.releaseAtMs = now + int64(skill.ActionCastingTimeMs) + 1
	pending.stealthStrike = c.NativeBodyStatus == 6
	rt.pendingSkillFinalizesMu.Lock()
	rt.pendingProjectileCasts = append(rt.pendingProjectileCasts, pending)
	rt.installCurrentSkillCommandLocked(division, c, pending.token, skill)
	rt.pendingSkillFinalizesMu.Unlock()
	start := wire.SkillCastUntargetedFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: enterworld.ObjectIDForCharacter(c), InstanceToken: pending.token, OwnerOrTargetGid: cast.TargetGid})
	return OpResult{Frames: []wire.Frame{start}, Broadcast: []wire.Frame{start}}
}

/*
================
projectileFlightMs

The same native 3D release sample determines the timer throughout flight;
target movement later never extends it. Source column 16 is speed, not delay.
================
*/
func projectileFlightMs(from, to simulation.Spawn, speed uint32) int64 {
	if speed == 0 {
		return 0
	}
	distance := simulation.WorldDistance2D(from, to)
	dy := to.Y - from.Y
	return int64(math.Sqrt(float64(float32(distance*distance+dy*dy))) * 1000 / float64(speed))
}

/*
================
takePreparingProjectile
================
*/
func (rt *Runtime) takePreparingProjectile(token uint32) bool {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()
	for i, p := range rt.pendingProjectileCasts {
		if p.token == token {
			rt.pendingProjectileCasts = append(rt.pendingProjectileCasts[:i], rt.pendingProjectileCasts[i+1:]...)
			delete(rt.castWaitVerdicts, token)
			return true
		}
	}
	return false
}

/*
================
cancelPreparingProjectile
================
*/
func (rt *Runtime) cancelPreparingProjectile(division, name string) []wire.Frame {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()
	var frames []wire.Frame
	ownerKey := simulation.WorldKey(division, name)
	kept := rt.pendingProjectileCasts[:0]
	for _, p := range rt.pendingProjectileCasts {
		if p.ownerKey == ownerKey {
			frames = append(frames, wire.SkillCastFinalizeFrame(p.token))
			delete(rt.castWaitVerdicts, p.token)
		} else {
			kept = append(kept, p)
		}
	}
	rt.pendingProjectileCasts = kept
	delete(rt.currentSkillCommands, ownerKey)
	return frames
}

/*
================
advanceProjectileCasts
================
*/
func (rt *Runtime) advanceProjectileCasts(now int64) []simulation.DivisionFrames {
	rt.pendingSkillFinalizesMu.Lock()
	pending := append([]pendingProjectileCast(nil), rt.pendingProjectileCasts...)
	// Verdicts of casts that left the queue without take or cancel
	// (an owner's teardown filter) are swept here, the one place the
	// whole queue is in hand; the map stays within a small multiple
	// of the queue between sweeps.
	if len(rt.castWaitVerdicts) > 4*len(pending)+32 {
		live := make(map[uint32]struct{}, len(pending))
		for _, p := range pending {
			live[p.token] = struct{}{}
		}
		for token := range rt.castWaitVerdicts {
			if _, ok := live[token]; !ok {
				delete(rt.castWaitVerdicts, token)
			}
		}
	}
	rt.pendingSkillFinalizesMu.Unlock()
	var out []simulation.DivisionFrames
	for _, p := range pending {
		out = append(out, rt.advanceProjectileCastInDivision(p, now)...)
	}
	return out
}

/*
================
advanceProjectileCastInDivision
================
*/
func (rt *Runtime) advanceProjectileCastInDivision(p pendingProjectileCast, now int64) []simulation.DivisionFrames {
	unlock := rt.lockDivision(p.divisionID)
	defer unlock()
	c := rt.findCharacter(p.divisionID, p.characterName)
	// A cast that is not due first faces the copy-free wait verdict
	// (projectileWaitingValid). Judged there, a valid cast waits on
	// and an invalid one refuses below: the tail reads its snapshot
	// only when the cast is valid, and a valid cast here is never
	// due, so no release can consume the missing copy. Unjudged (a
	// cold stats memo), the cast copy judges exactly as before and
	// its build memoises for the next tick.
	valid, judged := false, false
	if now < p.releaseAtMs && c != nil && c.ID == p.characterID {
		valid, judged = rt.projectileWaitingValid(p, c)
		if judged && valid {
			return nil
		}
	}
	var snapshot *enterworld.Character
	if !judged {
		if c != nil && c.ID == p.characterID {
			snapshot = rt.characterSnapshotCast(p.divisionID, c)
		}
		valid = snapshot != nil && !snapshot.DeletePending && enterworld.CharacterAlive(snapshot)
		if !p.supportCast && !p.selfEffect && !p.trap && !p.statusArea && !p.threatDecrease {
			valid = valid && rt.combatTargetExists(p.divisionID, snapshot, p.cast.TargetGid)
		}
		// The native instant handler runs mask 0x1C before reading the
		// timer; the projectile handler likewise.
		// Its resource bit must also
		// invalidate a waiting cast; checking only at release lets a
		// temporary shortage disappear and resurrect an invalid command.
		// Do not recheck the cooldown this preparation already installed,
		// or recompute its immutable execution cost. Linked stages own fresh
		// execution contexts and must retain the same resource check.
		if valid && !p.selfEffect {
			source := rt.deps.SkillData()
			if source == nil {
				valid = false
			} else if skill, exists := source.SkillByID(p.cast.ActionId); !exists {
				valid = false
			} else {
				_, refusal := rt.offensiveResourceCost(p.divisionID, snapshot, skill)
				valid = refusal == 0
			}
		}
		if valid && now < p.releaseAtMs {
			return nil
		}
	}
	// Cancellation/forget may have won after the snapshot. Re-check the
	// identity under the operation lock before publishing or touching HP.
	if !rt.takePreparingProjectile(p.token) {
		return nil
	}
	result, decision := OpResult{}, skillCastRefused
	if valid {
		if p.selfEffect {
			if skill, ok := rt.deps.SkillData().SkillByID(p.cast.ActionId); ok {
				result, decision = rt.acceptTimedSelfEffect(p.divisionID, c, snapshot, p.cast, skill, now, &p)
			}
		} else if p.statusArea {
			if skill, ok := rt.deps.SkillData().SkillByID(p.cast.ActionId); ok {
				result, decision = rt.acceptUntargetedStatusCast(p.divisionID, c, snapshot, p.cast, skill, now, &p, rt.tickDoors())
			}
		} else if p.threatDecrease {
			if skill, ok := rt.deps.SkillData().SkillByID(p.cast.ActionId); ok {
				result, decision = rt.acceptMirage(p.divisionID, c, snapshot, p.cast, skill, now, &p)
			}
		} else if p.trap {
			if skill, ok := rt.deps.SkillData().SkillByID(p.cast.ActionId); ok {
				result, decision = rt.acceptCombatTrap(p.divisionID, c, snapshot, p.cast, skill, now, &p)
			}
		} else if p.supportCast {
			if skill, ok := rt.deps.SkillData().SkillByID(p.cast.ActionId); ok {
				result, decision = rt.acceptSupportSkillPhase(p.divisionID, c, snapshot, p.cast, skill, now, &p)
			}
		} else {
			result, decision = rt.acceptSkillStagePhaseAt(p.divisionID, c, snapshot, p.cast, now, p.rootID, &p, rt.tickDoors())
		}
	}
	if decision != skillCastAccepted {
		if result.DiagnosticRefusal != "" {
			log.WithFields(log.Fields{"skill": p.cast.ActionId, "token": p.token, "reason": result.DiagnosticRefusal}).Debug("prepared skill release refused")
		}
		close := wire.SkillCastFinalizeFrame(p.token)
		result.Broadcast = []wire.Frame{close}
		result.ActorPrivate = result.Frames
	}
	// Each lifecycle owns its validation order. Persistent self effects
	// clear preparation before recipient validation; ensure every
	// release/refusal also clears it independently of in-flight ownership.
	rt.clearCurrentSkillCommand(p.divisionID, p.characterName)
	var out []simulation.DivisionFrames
	if len(result.Broadcast) > 0 {
		batch := simulation.DivisionFrames{DivisionID: p.divisionID, SourceGID: simulation.PlayerObjectID(p.characterID)}
		for _, f := range result.Broadcast {
			batch.Frames = append(batch.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
		}
		out = append(out, batch)
	}
	out = append(out, observedDivisionFrames(p.divisionID, result.Around)...)
	if len(result.ActorPrivate) > 0 && c != nil {
		batch := simulation.DivisionFrames{DivisionID: p.divisionID, OnlyCharacterID: c.ID}
		for _, f := range result.ActorPrivate {
			batch.Frames = append(batch.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
		}
		out = append(out, batch)
	}
	out = append(out, recipientDivisionFrames(p.divisionID, result.Recipients)...)
	return out
}

/*
================
projectileWaitVerdict

One waiting cast's judged wait, cached by cast token (#3101). The
priced leg's HP and MP amounts are fixed numbers once the caster's
stats and keeper maxima are read, so the entry stores those amounts
beside the abnormal block they were judged under: while the same
block stands, the resource gate replays from the caster's current
vitals alone and the stats graph is not re-priced. The life and
target gates are never cached - they read the live facts on every
tick, exactly as before.
================
*/
type projectileWaitVerdict struct {
	abnormalKey string
	block       *abnormal.Block
	hpCost      int64
	mpCost      int64
}

/*
================
castWaitVerdict
================
*/
func (rt *Runtime) castWaitVerdict(token uint32) (projectileWaitVerdict, bool) {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()
	verdict, ok := rt.castWaitVerdicts[token]
	return verdict, ok
}

/*
================
storeCastWaitVerdict
================
*/
func (rt *Runtime) storeCastWaitVerdict(token uint32, verdict projectileWaitVerdict) {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()
	if rt.castWaitVerdicts == nil {
		rt.castWaitVerdicts = make(map[uint32]projectileWaitVerdict)
	}
	rt.castWaitVerdicts[token] = verdict
}

/*
================
projectileWaitCosts

The HP and MP amounts the wait's priced leg judges against, in
resourceCostVerdictFor's own arithmetic (hpCostRefusal, then
resourceCostAt on the keeper maximum at the stats' consumption
rate). An unpayable price shape (resourceCostAt's 0x3003) caches as
an amount no vitals can meet, so the replay refuses identically.
================
*/
func projectileWaitCosts(maxHP, maxMP int64, stats combat.Stats, consumption enterworld.SkillConsumption, group uint32) (hpCost, mpCost int64) {
	if !consumption.Pinned || group == 0 {
		return math.MaxInt64, math.MaxInt64
	}
	hpCost = int64(consumption.HP) + vitalPercent(maxHP, uint32(consumption.HPPercent))
	cost := int64(consumption.MP) + vitalPercent(maxMP, uint32(consumption.MPPercent))
	rate := float32(combat.FullMPConsumptionRate)
	if consumption.MP != 0 || consumption.MPPercent != 0 {
		rate = combat.MPConsumptionRate(stats)
	}
	return hpCost, crtFtol(float64(rate) / 100 * float64(int32(cost)))
}

/*
================
projectileWaitingValid

The wait verdict of a prepared cast that is not due, judged without
detaching the caster (#2139 item 2, castwait item 5): the release
verdict's life, target and resource gates in the same order. The
life and target gates read the live record and monster state on
every tick, so a cast is still refused the moment its target dies.
The resource gate is the expensive one - it prices the caster
through the stats memo - and its inputs can only move with the
caster's vitals or abnormal block, so its judgement is cached by
token (projectileWaitVerdict): a tick where neither moved replays
the priced amounts against the current vitals instead of pricing
again. The native timing this keeps is the charge's: B245 charges
at release, and the wait's refusal still lands on the tick the
resources actually run out, because the vitals are read live.
judged is false only when the memo cannot serve (a cold or moved
caster): the caller then judges on the cast copy exactly as before,
and that build memoises for the next tick.
================
*/
func (rt *Runtime) projectileWaitingValid(p pendingProjectileCast, c *enterworld.Character) (valid, judged bool) {
	verdict, cached := rt.castWaitVerdict(p.token)
	abnormalKey := verdict.abnormalKey
	if !cached {
		abnormalKey = playerAbnormalKey(p.divisionID, c.Name)
	}
	block := rt.playerAbnormalKeyed(abnormalKey)

	var alive bool
	var world uint32
	var currentHP, currentMP int64
	rt.deps.Read(p.divisionID, func() {
		alive = !c.DeletePending && enterworld.CharacterAlive(c)
		world = domain.CharacterWorldInstance(c)
		currentHP, currentMP = enterworld.CurrentHP(c), enterworld.CurrentMP(c)
	})
	if !alive {
		return false, true
	}
	if !p.supportCast && !p.selfEffect && !p.trap && !p.statusArea && !p.threatDecrease {
		if !rt.combatTargetExistsFor(p.divisionID, c.ID, c.Name, world, p.cast.TargetGid) {
			return false, true
		}
	}
	if p.selfEffect {
		return true, true
	}
	// The priced gate replays while the block it was judged under
	// stands: the amounts are compared against the vitals just read.
	if cached && verdict.block == block {
		return currentHP >= verdict.hpCost && currentMP >= verdict.mpCost, true
	}
	source := rt.deps.SkillData()
	if source == nil {
		return false, true
	}
	// A priced cast judges against its prepared snapshot; resolving
	// the skill row again every waiting tick bought nothing, as the
	// row is static data the preparation already read (#2139). An
	// unpriced cast resolves its row exactly as before. The resource
	// leg's installed contributions come from their lifecycle
	// owners, never under the store door (#568), in the same order
	// playerCombatStats reads them.
	var skill enterworld.SkillRow
	consumption, group := p.verdictCost, p.verdictGroup
	if !consumption.Pinned {
		var exists bool
		skill, exists = source.SkillByID(p.cast.ActionId)
		if !exists {
			return false, true
		}
		consumption, group = skill.Consumption, skill.Group
	}
	writes := rt.statModifierWrites(p.divisionID, c)
	catalogs := rt.statCatalogs()
	var stats combat.Stats
	var statsErr error
	var statsHit bool
	var maxHP, maxMP int64
	rt.deps.Read(p.divisionID, func() {
		stats, _, statsErr, statsHit = combat.LookupMemoizedPlayerStats(c, catalogs, writes, block)
		if statsHit {
			if statsErr == nil && block != nil {
				stats.AbnormalMask = block.Mask
			}
			maxHP, maxMP, currentHP, currentMP = keeperVitalsFrom(c, stats, statsErr)
		}
	})
	if !statsHit {
		return false, false
	}
	var refusal uint16
	if p.verdictCost.Pinned {
		_, refusal = resourceCostVerdictFor(maxHP, maxMP, currentHP, currentMP, stats, statsErr, consumption, group)
	} else {
		_, refusal = resourceCostVerdict(maxHP, maxMP, currentHP, currentMP, stats, statsErr, skill)
	}
	if statsErr == nil {
		hpCost, mpCost := projectileWaitCosts(maxHP, maxMP, stats, consumption, group)
		rt.storeCastWaitVerdict(p.token, projectileWaitVerdict{
			abnormalKey: abnormalKey, block: block, hpCost: hpCost, mpCost: mpCost,
		})
	}
	return refusal == 0, true
}
