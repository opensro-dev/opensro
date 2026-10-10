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
Native 5857B0: B245 starts without results for positive casting time;
585F69/593540 generates results and charges at release; 5860D2 retains the
bow-shot result for its distance/speed flight timer. Never charge at start.
Self recovery shares this preparation/cancellation owner, but skips monster
resolution and projectile flight. Its gameplay mutation is release-owned too.
==================
*/
type pendingProjectileCast struct {
	executionCost             skillCharge // immutable prepared snapshot; never recomputed at release
	supportCast               bool        // shares prepare/cancel ownership; has no monster target or flight
	selfEffect                bool        // category-three recipient installation at release
	trap                      bool        // untargeted; the release plants a combat trap (skillcombattrap.go)
	statusArea                bool        // untargeted; the release rolls a caster-centred status area
	threatDecrease            bool        // untargeted; the release lowers hostility around the caster (mirage.go)
	divisionID, characterName string
	characterID               int64
	cast                      wire.SkillAction
	token                     uint32
	rootID                    uint32
	releaseAtMs               int64
	// stealthStrike is command flag 0x10 (4ACEAD): issued in body mode 6.
	// The release reads it, not the live body, which the cast's own event
	// retirement has already ended (58EDD7).
	stealthStrike bool
}

func (rt *Runtime) prepareSupportCast(division string, c *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64) OpResult {
	return rt.prepareSelfCast(division, c, cast, skill, now, false)
}

func (rt *Runtime) prepareSelfEffectCast(division string, c *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64) OpResult {
	return rt.prepareSelfCast(division, c, cast, skill, now, true)
}

func (rt *Runtime) prepareSelfCast(division string, c *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64, effect bool) OpResult {
	cost, err := rt.preparedExecutionMPCost(division, c, skill)
	if err != nil {
		return offensiveRefusal(0x3003)
	}
	charge := skillCharge{mp: cost, hp: rt.preparedExecutionHPCost(division, c, skill)}
	p := pendingProjectileCast{executionCost: charge, supportCast: !effect, selfEffect: effect, divisionID: division, characterName: c.Name, characterID: c.ID, cast: cast, token: atomic.AddUint32(&rt.castTokenCounter, 1), releaseAtMs: now + int64(skill.ActionCastingTimeMs) + 1}
	rt.pendingSkillFinalizesMu.Lock()
	rt.pendingProjectileCasts = append(rt.pendingProjectileCasts, p)
	rt.installCurrentSkillCommandLocked(division, c, p.token, skill)
	rt.pendingSkillFinalizesMu.Unlock()
	open := wire.SkillCastSelfFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: enterworld.ObjectIDForCharacter(c), InstanceToken: p.token})
	return OpResult{Frames: []wire.Frame{open}, Broadcast: []wire.Frame{open}}
}

func (rt *Runtime) prepareProjectileCast(division string, c *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64, rootID uint32) OpResult {
	cost, err := rt.preparedExecutionMPCost(division, c, skill)
	if err != nil {
		return offensiveRefusal(0x3003)
	}
	charge := skillCharge{mp: cost, hp: rt.preparedExecutionHPCost(division, c, skill)}
	pending := pendingProjectileCast{executionCost: charge, divisionID: division, characterName: c.Name, characterID: c.ID, cast: cast,
		rootID: rootID, token: atomic.AddUint32(&rt.castTokenCounter, 1), releaseAtMs: now + int64(skill.ActionCastingTimeMs) + 1,
		stealthStrike: c.NativeBodyStatus == 6}
	rt.pendingSkillFinalizesMu.Lock()
	rt.pendingProjectileCasts = append(rt.pendingProjectileCasts, pending)
	rt.installCurrentSkillCommandLocked(division, c, pending.token, skill)
	rt.pendingSkillFinalizesMu.Unlock()
	start := wire.SkillCastUntargetedFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: enterworld.ObjectIDForCharacter(c), InstanceToken: pending.token, OwnerOrTargetGid: cast.TargetGid})
	return OpResult{Frames: []wire.Frame{start}, Broadcast: []wire.Frame{start}}
}

// The same native 3D release sample determines the timer throughout flight;
// target movement later never extends it. Source column 16 is speed, not delay.
func projectileFlightMs(from, to simulation.Spawn, speed uint32) int64 {
	if speed == 0 {
		return 0
	}
	distance := simulation.WorldDistance2D(from, to)
	dy := to.Y - from.Y
	return int64(math.Sqrt(float64(float32(distance*distance+dy*dy))) * 1000 / float64(speed))
}

func (rt *Runtime) takePreparingProjectile(token uint32) bool {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()
	for i, p := range rt.pendingProjectileCasts {
		if p.token == token {
			rt.pendingProjectileCasts = append(rt.pendingProjectileCasts[:i], rt.pendingProjectileCasts[i+1:]...)
			return true
		}
	}
	return false
}

func (rt *Runtime) cancelPreparingProjectile(division, name string) []wire.Frame {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()
	var frames []wire.Frame
	kept := rt.pendingProjectileCasts[:0]
	for _, p := range rt.pendingProjectileCasts {
		if simulation.WorldKey(p.divisionID, p.characterName) == simulation.WorldKey(division, name) {
			frames = append(frames, wire.SkillCastFinalizeFrame(p.token))
		} else {
			kept = append(kept, p)
		}
	}
	rt.pendingProjectileCasts = kept
	delete(rt.currentSkillCommands, simulation.WorldKey(division, name))
	return frames
}

func (rt *Runtime) advanceProjectileCasts(now int64) []simulation.DivisionFrames {
	rt.pendingSkillFinalizesMu.Lock()
	pending := append([]pendingProjectileCast(nil), rt.pendingProjectileCasts...)
	rt.pendingSkillFinalizesMu.Unlock()
	var out []simulation.DivisionFrames
	for _, p := range pending {
		unlock := rt.lockDivision(p.divisionID)
		c := rt.findCharacter(p.divisionID, p.characterName)
		var snapshot *enterworld.Character
		if c != nil && c.ID == p.characterID {
			snapshot = rt.characterSnapshot(p.divisionID, c)
		}
		valid := snapshot != nil && !snapshot.DeletePending && enterworld.CharacterAlive(snapshot)
		if !p.supportCast && !p.selfEffect && !p.trap && !p.statusArea && !p.threatDecrease {
			_, exists := rt.resolveCombatTarget(p.divisionID, snapshot, p.cast.TargetGid, now)
			valid = valid && exists
		}
		// Instant 586C8D..586CA8 runs mask 0x1C before reading the
		// timer at 586CE3; projectile 585C67 precedes 585CA9 likewise.
		// Its resource bit (58E1B0..58E2BB) must also
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
			unlock()
			continue
		}
		// Cancellation/forget may have won after the snapshot. Re-check the
		// identity under the operation lock before publishing or touching HP.
		if !rt.takePreparingProjectile(p.token) {
			unlock()
			continue
		}
		result, decision := OpResult{}, skillCastRefused
		if valid {
			if p.selfEffect {
				if skill, ok := rt.deps.SkillData().SkillByID(p.cast.ActionId); ok {
					result, decision = rt.acceptTimedSelfEffect(p.divisionID, c, snapshot, p.cast, skill, now, &p)
				}
			} else if p.statusArea {
				if skill, ok := rt.deps.SkillData().SkillByID(p.cast.ActionId); ok {
					result, decision = rt.acceptUntargetedStatusCast(p.divisionID, c, snapshot, p.cast, skill, now, &p)
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
				result, decision = rt.acceptSkillStagePhaseAt(p.divisionID, c, snapshot, p.cast, now, p.rootID, &p)
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
		// clear preparation before recipient validation (5835EF); ensure every
		// release/refusal also clears it independently of in-flight ownership.
		rt.clearCurrentSkillCommand(p.divisionID, p.characterName)
		if len(result.Broadcast) > 0 {
			batch := simulation.DivisionFrames{DivisionID: p.divisionID, SourceGID: simulation.PlayerObjectID(p.characterID)}
			for _, f := range result.Broadcast {
				batch.Frames = append(batch.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
			}
			out = append(out, batch)
		}
		if len(result.ActorPrivate) > 0 && c != nil {
			batch := simulation.DivisionFrames{DivisionID: p.divisionID, OnlyCharacterID: c.ID}
			for _, f := range result.ActorPrivate {
				batch.Frames = append(batch.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
			}
			out = append(out, batch)
		}
		out = append(out, recipientDivisionFrames(p.divisionID, result.Recipients)...)
		unlock()
	}
	return out
}
