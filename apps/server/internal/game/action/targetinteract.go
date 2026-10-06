/*
===========================================================================

targetinteract.go - the 0x72CD target-interact dispatcher

One request opcode multiplexes pickups, basic attacks, skill commands,
buff cancellation and structure actions; this file classifies each and
hands it to the lane that owns it.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
HandleTargetInteract

HandleTargetInteract is the single semantic owner of the multiplexed native
0x72CD opcode. Classification happens before strict decoding so one command
family can never fall through into another family's response conversation.
==================
*/
func (rt *Runtime) HandleTargetInteract(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) OpResult {
	unlock := rt.lockDivision(divisionID)
	defer unlock()
	if queued, accepted := rt.queueObjectAction(divisionID, character, payload); accepted {
		return queued
	}
	previous := rt.captureActionSession(divisionID, character)
	result := rt.handleTargetInteractLocked(divisionID, character, payload)
	switch wire.ClassifyTargetActionLane(payload) {
	case wire.TargetActionBasicAttack, wire.TargetActionFollow, wire.TargetActionSkill:
		return rt.publishActionSession(divisionID, character, result, previous)
	}
	return result
}

/*
================
handleTargetInteractLocked

Keep admission, continuation replacement and the private action-state reply
inside the same division transaction.
================
*/
func (rt *Runtime) handleTargetInteractLocked(divisionID string, character *enterworld.Character, payload []byte) OpResult {
	switch wire.ClassifyTargetActionLane(payload) {
	case wire.TargetActionFollow:
		request, err := wire.DecodeFollowTarget(payload)
		if err != nil {
			return OpResult{DiagnosticRefusal: "follow-malformed"}
		}
		return rt.beginFollow(divisionID, character, request, rt.Now().UnixMilli())

	case wire.TargetActionBasicAttack:
		// 692CB0 emits family 1 for an attack; family 3 belongs to Trace.
		// A malformed attack must not answer or release the pickup latch.
		engage, err := wire.DecodeBasicAttackEngage(payload)
		if err != nil {
			return OpResult{DiagnosticRefusal: "basic-attack-malformed"}
		}

		snapshot := rt.characterSnapshot(divisionID, character)
		if snapshot == nil || snapshot.DeletePending || snapshot.NativeTeleportMode == 1 {
			return OpResult{DiagnosticRefusal: "character-unavailable"}
		}
		if mountedOnCOS(snapshot) {
			return mountedCommandRefusal()
		}
		nowMs := rt.Now().UnixMilli()
		if stand, seated := rt.standForSeatedCommand(divisionID, character, nowMs); seated {
			return stand
		}

		return rt.beginBasicAttack(divisionID, character, engage, nowMs)

	case wire.TargetActionSkill:
		// 6FCD50 flags 00/01 and 878100 flag 02 share this
		// discriminator. Malformed 0x04-family bytes remain skill-owned and
		// fail closed without emitting pickup frames.
		cast, err := wire.DecodeSkillAction(payload)
		if err != nil {
			return OpResult{}
		}

		snapshot := rt.characterSnapshot(divisionID, character)
		if snapshot == nil || snapshot.DeletePending || snapshot.NativeTeleportMode == 1 {
			return OpResult{}
		}
		if mountedOnCOS(snapshot) {
			return mountedCommandRefusal()
		}
		if stand, seated := rt.standForSeatedCommand(divisionID, character, rt.Now().UnixMilli()); seated {
			return stand
		}

		// 4AE601: a masked player (msch 1) has only its basic attack; every
		// skill command, instant ones included (4AD870 runs state 0 too),
		// answers 0x3030 once the skill itself resolves (0x3003).
		if snapshot.TransformMode == 1 {
			if source := rt.deps.SkillData(); source != nil {
				if _, ok := source.SkillByID(cast.ActionId); !ok {
					return offensiveRefusal(0x3003)
				}
			}
			return offensiveRefusal(0x3030)
		}

		// 4AD870 dispatches non-attack activity skills before touching the
		// command queue. Instant imbues preserve the current attack/approach.
		if source := rt.deps.SkillData(); source != nil {
			if skill, ok := source.SkillByID(cast.ActionId); ok &&
				(skill.InstantSelfEffectPinned || skill.Imbue.Pinned) {
				return rt.acceptInstantSelfEffect(divisionID, character, snapshot, cast, skill)
			}
		}

		// CGCharAutoCommandActor_ProcessCommand (4ACC40, call at 4ACED4) admits
		// a skill command with mask 0x37 (cooldown, replacement, equipment,
		// resources, ammunition) before it touches the queue, busy or not: a
		// refused press answers its error and the running attack, its swing and
		// its continuation stay untouched. Auto-attack is always mid-swing, so
		// skipping this while an action is open dropped the attack whenever a
		// cooling-down skill was pressed.
		if source := rt.deps.SkillData(); source != nil {
			if skill, ok := source.SkillByID(cast.ActionId); ok &&
				!isPinnedBaseAttack(snapshot, skill.Codename) {
				if code := rt.skillAdmission(
					divisionID,
					snapshot,
					skill,
					rt.Now().UnixMilli(),
					nil,
					nil,
					admitCommand,
				); code != 0 {
					return offensiveRefusal(code)
				}
			}
		}

		return rt.dispatchRetainingAttack(divisionID, character, snapshot, cast)

	case wire.TargetActionFortressStructure:
		// 692CB0's CICATStruct [02][01][01][gid] form is retail-valid,
		// but this gateway does not yet own spawned siege-structure HP/action
		// state. Keep it isolated and silent rather than aliasing bare cancel
		// or pickup. Decode here so the exact wire remains exercised.
		if _, err := wire.DecodeFortressStructureInteract(payload); err != nil {
			return OpResult{}
		}

		return OpResult{}

	case wire.TargetActionCancelActiveEffect:
		// CIFMagicStateBoard and CIFDelayInfo both enter this generic effect
		// session. 4AE566 passes reference+4 (skill ID), not reference+8
		// (group), to 59EF90's active-command+8 comparison. The optional token
		// narrows that exact skill match; zero is a token wildcard only.
		request, err := wire.DecodeCancelActiveEffectRequest(payload)
		if err != nil || character == nil {
			return OpResult{}
		}

		snapshot := rt.characterSnapshot(divisionID, character)
		if snapshot == nil || snapshot.DeletePending {
			return OpResult{}
		}

		skills := rt.deps.SkillData()
		if skills != nil {
			if row, known := skills.SkillByID(request.EffectID); known {
				rt.effects.RequestVoluntaryStop(
					divisionID, snapshot.Name, row.ID, request.InstanceToken,
				)
			}
		}

		// This command completes independently of the retained combat queue.
		// 4AD270 reports that queue's actual count; effect teardown later emits
		// B6A0. Zero here would make an unrelated buff cancel hide auto-attack.
		state := wire.ReleaseActionState()
		state.State = rt.actionQueueCount(divisionID, snapshot.Name)
		return OpResult{
			Frames: []wire.Frame{
				{
					Opcode:  wire.OpActionState,
					Payload: state.Encode(),
				},
			},
		}

	case wire.TargetActionCancel, wire.TargetActionGroundItemPickup:
		// Continue below in the pickup/approach owner.
	default:
		// Unknown 0x72CD bytes have no conversation owner. Emitting B2CD/B06D
		// here would falsely mutate the pickup lifecycle.
		return OpResult{}
	}

	request, err := wire.DecodeTargetInteract(payload)
	if err != nil {
		return pickupRefusal(wire.ErrCodeInvalidRequest)
	}

	if character == nil {
		return pickupRefusal(wire.ErrCodeInvalidRequest)
	}

	snapshot := rt.characterSnapshot(divisionID, character)
	if snapshot == nil || snapshot.DeletePending || snapshot.NativeTeleportMode == 1 {
		return pickupRefusal(wire.ErrCodeInvalidRequest)
	}

	pendingKey := grounditem.PendingKey(divisionID, character.Name)

	if request.Cancel {
		rt.Pending.Clear(pendingKey)
		return rt.cancelObjectAction(divisionID, character, snapshot)
	}

	groundItem, ok := rt.characterGround(divisionID, snapshot, request.Gid)
	if !ok {
		// Already picked / despawned: native "Cannot find target" (01:03).
		rt.Pending.Clear(pendingKey)
		return pickupRefusal(wire.ErrCodeTargetGone)
	}

	if groundItem.OwnerJID != 0 &&
		groundItem.OwnerJID != enterworld.ObjectIDForCharacter(snapshot) &&
		(rt.CanPickupOwnedDrop == nil || !rt.CanPickupOwnedDrop(divisionID, snapshot.Name, groundItem.OwnerJID)) {
		rt.Pending.Clear(pendingKey)
		return pickupRefusal(wire.ErrCodeCannotBePicked)
	}

	now := rt.Now()
	worldKey := simulation.WorldKey(divisionID, character.Name)

	if pending, armed := rt.Pending.Peek(pendingKey); armed && pending.ItemGid == request.Gid {
		matured, remaining := rt.Pending.TakeMatured(pendingKey, request.Gid, now)
		if !matured {
			// A duplicate interact while the server-owned travel is in flight is
			// silent. The simulation tick, not another client request, owns maturity.
			return OpResult{
				Pending: &PendingPickup{
					ItemGid: request.Gid,
					Eta:     remaining,
				},
			}
		}
	}

	// A matured timer is not proof of arrival. Re-read the authoritative live
	// position before every execute attempt so clipping, restarts, or any future
	// movement correction cannot grant remote loot.
	live := rt.liveSpawn(worldKey, snapshot, now.UnixMilli())
	from := grounditem.Point{
		RegionID: live.RegionID,
		X:        float32(live.X),
		Z:        float32(live.Z),
	}

	if !grounditem.SameWorld(from, groundItem.Position) {
		rt.Pending.Clear(pendingKey)
		return pickupRefusal(wire.ErrCodeCannotBePicked)
	}

	// A valid pickup replaces pursuit. Its own approach is now the sole
	// movement owner; the next action tick must not steer back to a player.
	stopped := rt.stopFollowMovement(divisionID, character, snapshot)
	rt.ClearCombatIntent(divisionID, character.Name)
	// Pickup already owns its arm/release replies. Transfer publication as
	// well as pursuit so combat retirement cannot release an active pickup.
	rt.actionSessions.Delete(worldKey)

	worldSnapshot := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState {
		return simulation.SeedWorldState(snapshot)
	})
	approach := grounditem.PlanApproach(from, groundItem.Position, worldSnapshot.MovementMode)
	if !approach.InRange {
		return prependOpResult(stopped, rt.armApproach(divisionID, worldKey, pendingKey, character, groundItem, approach, now))
	}

	// In range: a stale pending for another gid is superseded.
	rt.Pending.Clear(pendingKey)

	return prependOpResult(stopped, rt.grantPickup(divisionID, worldKey, character, snapshot, groundItem))
}

/*
================
dispatchSkillCommand

The admitted skill command's owner. Each owner may still refuse (busy
caster, posture, target): dispatchRetainingAttack restores the replaced
attack when it does.
================
*/
func (rt *Runtime) dispatchSkillCommand(divisionID string, character, snapshot *enterworld.Character, cast wire.SkillAction) OpResult {
	if source := rt.deps.SkillData(); source != nil {
		if skill, ok := source.SkillByID(cast.ActionId); ok &&
			!isPinnedBaseAttack(snapshot, skill.Codename) {

			if skill.Duplicate.Pinned {
				return rt.acceptDuplicate(divisionID, character, snapshot, cast, skill, rt.Now().UnixMilli())
			}

			if skill.MonsterCapture.Pinned {
				return rt.acceptMonsterCapture(divisionID, character, snapshot, cast, skill)
			}

			if skill.PositionEffect.Pinned {
				return rt.acceptPositionSkill(divisionID, character, snapshot, cast, skill)
			}
			// Discord Wave: a friendly-targeted hostility cut
			// (discordwave.go).
			if skill.Threat.Decrease {
				return rt.acceptDiscordWave(divisionID, character, snapshot, cast, skill, rt.Now().UnixMilli())
			}
			if skill.Threat.Only && !skill.TargetRequired {
				return rt.acceptUntargetedTaunt(tauntCast{division: divisionID, character: character, snapshot: snapshot, skill: skill}, cast)
			}

			// Untargeted party-area heals, heals over time and
			// resurrections: their action vector is the party
			// selection (58BEF0).
			if skill.Recovery.PartyResurrectPinned || skill.Recovery.PartyHealPinned || skill.Recovery.LowestHealPinned ||
				skill.Recovery.HealOverTimePinned {
				return rt.acceptSupportSkill(divisionID, character, snapshot, cast, skill)
			}

			if skill.Recovery.SelfFlatPinned ||
				(skill.Heal.Present || skill.Abnormal.AdmitDeadParty) && skill.TargetRequired ||
				skill.Abnormal.CurePresent() {
				return rt.acceptSupportSkill(divisionID, character, snapshot, cast, skill)
			}

			if skill.TimedEffect.Pinned && skill.TimedEffect.Targeted {
				return rt.acceptTimedTargetEffect(divisionID, character, snapshot, cast, skill, rt.Now().UnixMilli())
			}

			if skill.TimedEffect.Pinned || skill.Concealment.Pinned {
				result, _ := rt.acceptTimedSelfEffect(
					divisionID,
					character,
					snapshot,
					cast,
					skill,
					rt.Now().UnixMilli(),
					nil,
				)
				return result
			}

			if skill.Aura.Present && (skill.BuffModifiers.Present() || skill.Aura.Eshp) {
				return rt.acceptPartyBuff(divisionID, character, snapshot, cast, skill)
			}

			if skill.Wall.Pinned {
				return rt.acceptWall(divisionID, character, snapshot, cast, skill)
			}

			if skill.StatusCast && !skill.TargetRequired {
				result, _ := rt.acceptUntargetedStatusCast(divisionID, character, snapshot, cast, skill, rt.Now().UnixMilli(), nil)
				return result
			}

			if skill.AreaBurst && !skill.TargetRequired {
				return rt.acceptAreaBurst(divisionID, character, snapshot, cast, skill, rt.Now().UnixMilli())
			}

			if skill.CombatTrap.Pinned {
				result, _ := rt.acceptCombatTrap(divisionID, character, snapshot, cast, skill, rt.Now().UnixMilli(), nil)
				return result
			}

			return rt.beginOffensiveSkill(divisionID, character, snapshot, cast)
		}
	}

	return rt.acceptSkillCast(divisionID, character, snapshot, cast)
}

/*
================
dispatchRetainingAttack

4ACC40 never lets a refused command touch the queue: the attack it would
have replaced keeps swinging, its continuation keeps its place. Owners here
refuse after the intent is cleared (an open action, a seated or masked
caster, a vanished target), so a refused or empty answer that installed no
command of its own puts the running attack and its queued continuation back.
Only movement and an accepted command end auto-attack.
================
*/
func (rt *Runtime) dispatchRetainingAttack(divisionID string, character, snapshot *enterworld.Character, cast wire.SkillAction) OpResult {
	key := simulation.WorldKey(divisionID, snapshot.Name)
	prior, attacking := rt.combatIntentFor(divisionID, snapshot.Name)
	session, published := rt.actionSessions.Load(key)
	rt.ClearCombatIntent(divisionID, snapshot.Name)
	result := rt.dispatchSkillCommand(divisionID, character, snapshot, cast)
	if !attacking || !commandRefused(result) {
		return result
	}
	if _, replaced := rt.combatIntentFor(divisionID, snapshot.Name); replaced {
		return result
	}
	rt.setCombatIntent(prior)
	if published {
		rt.actionSessions.Store(key, session)
	}
	return result
}

/*
================
commandRefused

A refusal installs nothing: a diagnostic, a lone skill error (B245 [2, code])
or no answer at all. Anything published to others or a pending pickup is an
accepted command.
================
*/
func commandRefused(result OpResult) bool {
	if result.DiagnosticRefusal != "" {
		return true
	}
	if len(result.Broadcast) != 0 || len(result.Recipients) != 0 || result.Pending != nil {
		return false
	}
	for _, frame := range result.Frames {
		if frame.Opcode != wire.OpSkillCastResult || len(frame.Payload) == 0 || frame.Payload[0] != 2 {
			return false
		}
	}
	return true
}
