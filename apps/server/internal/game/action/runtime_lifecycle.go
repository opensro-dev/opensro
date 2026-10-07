/*
===========================================================================

runtime_lifecycle.go - the action runtime's tick hooks

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"strings"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
MonsterActionTickHook

MonsterActionTickHook settles accepted monster actions before the next AI
decision. A delayed coordinator tick can cross both release and Timer-10
expiry; the old post-AI-only hook then made a valid next command collide
with a stale pending cast. Player action ordering remains in TickHook.
==================
*/
func (rt *Runtime) MonsterActionTickHook() simulation.TickHook {
	return func(nowMs int64) []simulation.DivisionFrames {
		if rt.Monsters != nil {
			rt.Monsters.AdvanceSummons(nowMs)
		}
		out := rt.retireMonsterSelfEffects(nowMs)
		out = append(out, rt.advanceMonsterCasts(nowMs)...)
		return append(out, rt.drainSelectedSkillFinalizes(nowMs, true)...)
	}
}

/*
================
TickHook

Owns gameplay lifecycles on one simulation clock. Linked attack pulses settle
before abnormal-state updates, allowing their newly applied statuses to enter
the same actor update without another health authority.
Each step runs under rt.Steps, so a slow tick names the subsystem that
held it rather than only this hook.
================
*/
func (rt *Runtime) TickHook() simulation.TickHook {
	return func(nowMs int64) []simulation.DivisionFrames {
		step := rt.Steps.Time
		var out []simulation.DivisionFrames
		frames := func(name string, advance func(int64) []simulation.DivisionFrames) {
			step(name, func() { out = append(out, advance(nowMs)...) })
		}
		step("advanceResidentRegions", func() { rt.advanceResidentRegions(nowMs) })
		if rt.AdvanceQuestCalendar != nil {
			step("AdvanceQuestCalendar", func() { rt.AdvanceQuestCalendar(nowMs) })
		}
		step("advanceDepartures", func() { rt.advanceDepartures(nowMs) })
		step("advanceReturnScrolls", func() { rt.advanceReturnScrolls(nowMs) })
		step("advanceJobDresses", func() { rt.advanceJobDresses(nowMs) })
		step("advanceFortressPhases", func() { rt.advanceFortressPhases(nowMs) })
		frames("drainStructureDeaths", rt.drainStructureDeaths)
		step("advanceFortressStructures", func() { rt.advanceFortressStructures(nowMs) })
		step("advanceGuildVotes", func() { rt.advanceGuildVotes(nowMs) })
		// Retirement is presentation-only. Reward state was already committed
		// by the fatal hit, while the zero-HP source remains resolvable through
		// the authored death-animation completion.
		step("drainMonsterDefeats", func() { rt.drainMonsterDefeats(nowMs) })
		if rt.Monsters != nil {
			step("ExpireMonsterLifetimes", func() { rt.Monsters.ExpireMonsterLifetimes(nowMs) })
		}
		step("retireMonsterCriticals", rt.retireMonsterCriticals)
		if rt.Monsters != nil {
			step("AdvancePopulation", func() { rt.Monsters.AdvancePopulation(nowMs) })
		}
		// One tick is one action-owner transaction. Snapshot the actors that
		// own an open B245 bracket before closing due brackets, then exclude
		// those owners from repeat admission for the rest of this turn. This
		// makes B505 an observable release boundary: a staged native move can
		// flush and supersede the retained engage before a later tick admits
		// another B245. Closing and reopening in this same turn kept client
		// motion-state 2 continuously owned and could strand click movement.
		var openActionOwners map[string]bool
		step("openSkillCastOwnerSnapshot", func() { openActionOwners = rt.openSkillCastOwnerSnapshot() })
		frames("advanceBerserk", rt.advanceBerserk)
		frames("advancePlayerAggressions", rt.advancePlayerAggressions)
		frames("advanceBattleStates", rt.advanceBattleStates)
		frames("drainSkillFinalizes", rt.drainSkillFinalizes)
		frames("advanceProjectileCasts", rt.advanceProjectileCasts)
		// A cancel-active-effect request only clears the server effect's live
		// flag. Character-effect retirement and its counted instance teardown
		// broadcast are a distinct update phase in retail (v1.188 sub_5a0100 ->
		// sub_59ecd0 / B072; v1.150 client opcode B6A0).
		step("checkpointOnlineSkillJobs", func() { rt.checkpointOnlineSkillJobs(nowMs) })
		step("advanceLinkedEffects", func() { rt.advanceLinkedEffects(nowMs) })
		frames("advancePartyAuras", rt.advancePartyAuras)
		frames("advanceWalls", rt.advanceWalls)
		frames("advancePeriodicEffects", rt.advancePeriodicEffects)
		frames("advanceHawks", rt.advanceHawks)
		frames("advancePulseAreas", rt.advancePulseAreas)
		// A heal over time pulses before expiry: its last pulse lands on
		// the instant its effect's duration is reached.
		frames("advanceHealsOverTime", rt.advanceHealsOverTime)
		step("effects.Expire", func() { rt.effects.Expire(nowMs) })
		step("drainStoppedCharacterEffects", func() { out = append(out, rt.drainStoppedCharacterEffects()...) })
		// 4A4390 per actor: expiry, damage over time, detonation, mask.
		frames("advanceMonsterAbnormals", rt.advanceMonsterAbnormals)
		step("drainMonsterLegRecipients", func() { out = append(out, rt.drainMonsterLegRecipients()...) })
		frames("advancePlayerAbnormals", rt.advancePlayerAbnormals)
		frames("advanceCosAbnormals", rt.advanceCosAbnormals)
		frames("advanceQueuedActionSessions", rt.advanceQueuedActionSessions)
		step("advanceBasicAttackIntents", func() {
			out = append(out, rt.advanceBasicAttackIntents(nowMs, openActionOwners)...)
		})
		frames("advanceNaturalRecovery", rt.advanceNaturalRecovery)
		frames("advancePets", rt.advancePets)
		// 60C684 after the pets: a fired caravan reads the transport's
		// cargo and live position as this tick left them.
		step("advanceCaravans", func() { rt.advanceCaravans(nowMs) })
		step("advancePetSkillWindows", func() { rt.advancePetSkillWindows(nowMs) })
		step("advanceParamJobs", func() { rt.advanceParamJobs(nowMs) })
		step("advancePKRecords", rt.advancePKRecords)
		step("advanceMercenaryCooldowns", func() { rt.advanceMercenaryCooldowns(nowMs) })
		frames("advancePendingPickups", rt.advancePendingPickups)
		step("advanceBodyRestores", func() { rt.advanceBodyRestores(nowMs) })
		step("advanceCompoundJobs", func() { rt.advanceCompoundJobs(nowMs) })
		frames("ReleaseExpiredOwnership", rt.ReleaseExpiredOwnership)
		frames("SweepExpired", rt.SweepExpired)
		step("retireActionSessions", func() { out = append(out, rt.retireActionSessions()...) })
		return coalesceDivisionFrames(out)
	}
}

/*
================
pendingPickupDelivery

Packet delivery leaves the division operation lock before touching sessions.
================
*/
type pendingPickupDelivery struct {
	divisionID    string
	characterName string
	frames        []wire.Frame
	broadcast     []wire.Frame
}

/*
==================
advancePendingPickups

advancePendingPickups is the authoritative arrival half of a ground-item
click. Native object-action processing owns the walk and invokes inventory
pickup after reach succeeds; it does not require the client to replay the
original target packet. Each due entry is revalidated under the same
per-division operation lock as request-time pickup, then the actor and peer
packet halves are routed without duplicating the public frames to the actor.
==================
*/
func (rt *Runtime) advancePendingPickups(nowMs int64) []simulation.DivisionFrames {
	var recipients []simulation.DivisionFrames
	now := time.UnixMilli(nowMs)
	var deliveries []pendingPickupDelivery
	for _, pending := range rt.Pending.Due(now) {
		unlock := rt.lockDivision(pending.DivisionID)
		result, character := rt.completePendingPickup(pending, now)
		recipients = append(recipients, recipientDivisionFrames(pending.DivisionID, result.Recipients)...)
		unlock()
		if character == nil || (len(result.Frames) == 0 && len(result.Broadcast) == 0) {
			continue
		}
		deliveries = append(deliveries, pendingPickupDelivery{
			divisionID:    pending.DivisionID,
			characterName: character.Name,
			frames:        result.Frames,
			broadcast:     result.Broadcast,
		})
	}
	for _, delivery := range deliveries {
		if rt.PushCharacterFrames != nil && len(delivery.frames) > 0 {
			rt.PushCharacterFrames(delivery.divisionID, delivery.characterName, delivery.frames)
		}
		if rt.PushDivisionPeerFrames != nil && len(delivery.broadcast) > 0 {
			rt.PushDivisionPeerFrames(delivery.divisionID, delivery.characterName, delivery.broadcast)
		}
	}
	return recipients
}

/*
================
completePendingPickup

Revalidate ownership and live approach range before committing inventory.
================
*/
func (rt *Runtime) completePendingPickup(pending grounditem.Pending, now time.Time) (OpResult, *enterworld.Character) {
	character := rt.findCharacter(pending.DivisionID, pending.CharacterName)
	if character == nil {
		rt.Pending.Clear(pending.Key)
		return OpResult{}, nil
	}
	matured, _ := rt.Pending.TakeMatured(pending.Key, pending.ItemGid, now)
	if !matured {
		return OpResult{}, character
	}
	snapshot := rt.characterSnapshot(pending.DivisionID, character)
	if snapshot == nil || snapshot.DeletePending {
		return pickupRefusal(wire.ErrCodeInvalidRequest), character
	}
	groundItem, ok := rt.characterGround(pending.DivisionID, snapshot, pending.ItemGid)
	if !ok {
		return pickupRefusal(wire.ErrCodeTargetGone), character
	}
	if groundItem.OwnerJID != 0 &&
		groundItem.OwnerJID != enterworld.ObjectIDForCharacter(snapshot) &&
		(rt.CanPickupOwnedDrop == nil ||
			!rt.CanPickupOwnedDrop(pending.DivisionID, snapshot.Name, groundItem.OwnerJID)) {
		return pickupRefusal(wire.ErrCodeCannotBePicked), character
	}

	worldKey := simulation.WorldKey(pending.DivisionID, character.Name)
	live := rt.liveSpawn(worldKey, snapshot, now.UnixMilli())
	from := grounditem.Point{RegionID: live.RegionID, X: float32(live.X), Z: float32(live.Z)}
	if !grounditem.SameWorld(from, groundItem.Position) {
		return pickupRefusal(wire.ErrCodeCannotBePicked), character
	}
	worldSnapshot := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState {
		return simulation.SeedWorldState(snapshot)
	})
	approach := grounditem.PlanApproach(from, groundItem.Position, worldSnapshot.MovementMode)
	if !approach.InRange {
		return rt.armApproach(
			pending.DivisionID,
			worldKey,
			pending.Key,
			character,
			groundItem,
			approach,
			now,
		), character
	}
	return rt.grantPickup(pending.DivisionID, worldKey, character, snapshot, groundItem), character
}

/*
==================
coalesceDivisionFrames

coalesceDivisionFrames preserves authoritative phase order while joining
only ADJACENT batches with the exact same route. The old global key map
could move a later public batch ahead of an intervening actor-private tail:
public(A), private(A), public(B) became public(A+B), private(A). That broke
the native causal boundary this helper was meant to preserve.
==================
*/
func coalesceDivisionFrames(in []simulation.DivisionFrames) []simulation.DivisionFrames {
	out := make([]simulation.DivisionFrames, 0, len(in))
	for _, routed := range in {
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.DivisionID == routed.DivisionID &&
				last.SourceGID == routed.SourceGID &&
				last.ExceptSessionID == routed.ExceptSessionID &&
				last.OnlyCharacterID == routed.OnlyCharacterID {
				last.Frames = append(last.Frames, routed.Frames...)
				continue
			}
		}
		if len(routed.Frames) == 0 {
			continue
		}
		out = append(out, routed)
	}
	return out
}

// ---- Session-close cleanup ----

/*
==================
ForgetCharacter

ForgetCharacter is THE per-character cleanup path: it releases every
runtime plane keyed by division:name - the live world state (shared with
the movement lane), any pickup approach in flight, the open skill bracket,
combat intent, object selection, and active effects.
Division-scoped
state (the ground registry) deliberately
stays: drops outlive the dropper. The transport's session-close hook
must call this; without it every character who ever moved or dropped an
item leaks a world entry for the life of the process.

The division operation lock serializes cleanup against in-flight handlers
on this lane. Idempotent: a double close or never-seen character is a no-op.
==================
*/
func (rt *Runtime) ForgetCharacter(divisionID, characterName string) {
	unlock := rt.lockDivision(divisionID)
	defer unlock()
	rt.forgetCharacterLocked(divisionID, characterName)
}

/*
================
ForgetCharacterSession

Reject teardown from an owner displaced by a newer logical session. Socket
resume keeps the same session and never calls this.
================
*/
func (rt *Runtime) ForgetCharacterSession(divisionID, characterName string, session uint64) {
	unlock := rt.lockDivision(divisionID)
	defer unlock()
	if owner, exists := rt.characterAdmissions.Load(simulation.WorldKey(divisionID, characterName)); exists {
		if owner.(populationAdmission).session != session {
			return
		}
		rt.forgetCharacterLocked(divisionID, characterName)
		return
	}
	rt.petMu.Lock()
	state := rt.petSessions[petOwnerKey{division: divisionID, name: strings.ToLower(characterName)}]
	stale := state != nil && state.session != session
	rt.petMu.Unlock()
	if stale {
		return
	}
	rt.forgetCharacterLocked(divisionID, characterName)
}

/*
================
forgetCharacterLocked

Release actor-owned runtime state while the division operation lock is held.
================
*/
func (rt *Runtime) forgetCharacterLocked(divisionID, characterName string) {
	rt.periodicEffects.StopSource(divisionID, characterName)
	rt.returnCasts.Delete(simulation.WorldKey(divisionID, characterName))
	rt.playerDisplacements.Delete(simulation.WorldKey(divisionID, characterName))
	rt.jobDresses.Delete(simulation.WorldKey(divisionID, characterName))
	rt.jobActivations.Delete(simulation.WorldKey(divisionID, characterName))
	rt.berserkActors.Delete(simulation.WorldKey(divisionID, characterName))
	rt.battleActors.Delete(simulation.WorldKey(divisionID, characterName))
	rt.aggressionActors.Delete(simulation.WorldKey(divisionID, characterName))
	rt.leavePopulationSession(divisionID, characterName)
	rt.forgetCriticalCharacter(divisionID, characterName)
	var departedGID uint32
	if c := rt.findCharacter(divisionID, characterName); c != nil {
		departedGID = enterworld.ObjectIDForCharacter(c)
		rt.noteLastSeen(c)
		rt.releaseGuildStorage(divisionID, c.Name)
		rt.retirePremiumSpend(divisionID, c, rt.Now().UnixMilli())
	}
	if rt.Monsters != nil {
		rt.Monsters.ForgetAbnormalSource(divisionID, departedGID, characterName)
	}
	rt.forgetPlayerAbnormalSource(divisionID, characterName, departedGID)
	// Storage retains the character across sessions; native actor destruction
	// does not. Runtime body state must not survive that actor lifetime.
	if c := rt.findCharacter(divisionID, characterName); c != nil {
		rt.deps.Update(c, "forget-body-status", func() bool {
			changed := c.NativeTeleportMode != 0
			c.NativeTeleportMode = 0
			c.BerserkUntilMs = 0
			changed = len(c.Aggressions) != 0 || changed
			c.Aggressions = nil
			changed = c.TransitionBodyStatus(domain.BodyStatusTransition{}) || changed
			if rows := rt.effects.Snapshot(divisionID, characterName); len(rows) > 0 {
				changed = rt.checkpointSkillJobs(c, rows, rt.Now().UnixMilli()) || changed
			}
			for _, pet := range c.Companions() {
				if pet.NativeBodyStatus == 0 {
					continue
				}
				pet.NativeBodyStatus = 0
				changed = true
			}
			return changed
		})
	}

	rt.forgetPetSession(divisionID, characterName)
	rt.forgetRecoverySession(divisionID, characterName)
	rt.Worlds.Forget(simulation.WorldKey(divisionID, characterName))
	rt.Pending.Clear(grounditem.PendingKey(divisionID, characterName))
	rt.Selected.Clear(divisionID, characterName)
	rt.NpcDialogs.Clear(divisionID, characterName)
	rt.ClearCombatIntent(divisionID, characterName)
	rt.clearSkillFinalizes(divisionID, characterName)
	rt.effects.Forget(divisionID, characterName)
	rt.clearCompoundJob(compoundKey{divisionID, characterName})
}

/*
==================
GroundRefItemCodenames

GroundRefItemCodenames names the division's ground-drop codenames for the
bootstrap refItemSnapshot seam (Deps.ExtraRefItemCodenames): drops possibly
from other characters' rosters must resolve records on a fresh client too.
==================
*/
func (rt *Runtime) GroundRefItemCodenames(divisionID string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range rt.Ground.All(divisionID) {
		if item.Codename == "" || seen[item.Codename] {
			continue
		}
		seen[item.Codename] = true
		out = append(out, item.Codename)
	}
	return out
}

/*
==================
StaticRefItemCodenames

StaticRefItemCodenames names the item references every viewer needs whatever
the division holds: the small starter drop seed, the alchemy outputs, the
Magic Pop rewards and all three v1.150 Magic Pop ticket states. They are
published once in the content-addressed browser references
(Deps.StaticRefItemCodenames), not repeated in every login. Higher-level
loot is introduced by immutable reference deltas before live spawn, not by
preloading its catalog.
==================
*/
func (rt *Runtime) StaticRefItemCodenames() []string {
	candidates := loot.MonsterDropRefItemCodenames()
	candidates = append(candidates, rt.alchemyOutputCodenames()...)
	candidates = append(candidates, rt.GachaCatalog.RewardCodenames()...)
	if catalog := rt.GachaCatalog; catalog != nil {
		candidates = append(candidates, catalog.Card.Codename, catalog.WinCard.Codename, catalog.LoseCard.Codename)
	}
	seen := make(map[string]bool, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, codename := range candidates {
		if seen[codename] {
			continue
		}
		seen[codename] = true
		out = append(out, codename)
	}
	return out
}
