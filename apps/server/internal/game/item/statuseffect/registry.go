/*
===========================================================================

registry.go - character-owned effect installation and retirement

One registry owns the effect instances, modifier contributions and target
constraints. Consumers read snapshots; retirement publishes instance tokens.

===========================================================================
*/
package statuseffect

import (
	"strings"
	"sync"
)

// State is the server effect-row lifecycle observed by
// CSkillManager_FindActiveBuffBySkillID (v1.188
// SR_GameServer 0x59ef90). Only states 1 and 2 are eligible for the native
// client-cancellation action.
/*
================
State
================
*/
type State uint8

const (
	StatePending State = 1
	StateActive  State = 2
)

// Effect is one character-owned server gameplay effect. SkillGroup identifies
// the replacement family; voluntary cancellation uses the exact SkillID.
// InstanceToken optionally distinguishes parallel instances.
//
// ClientCancelable mirrors ActiveCharacterEffect_RequestStop(effect, false):
// the UI request is not a privileged purge and cannot remove an effect whose
// server descriptor forbids voluntary cancellation.
/*
================
Effect
================
*/
type Effect struct {
	ForcedTargetGID  uint32 // hitm context+28; zero means no target constraint.
	jobClock         relativeJobClock
	jobClockPresent  bool
	jobCheckpointDue bool
	jobUpdatedAtMs   int64
	Modifiers        Modifiers
	// InstalledStates records the state operations committed by the lifecycle
	// producer. It is not inferred from visible icons or reference-counted.
	InstalledStates  [2]uint32
	RetirementStates [2]uint32
	// Area source identity belongs to native context+6C, not the lnks pair.
	OwnerGID       uint32
	AreaSourceGID  uint32
	AreaSourceName string
	// AuraParentToken marks a party aura's child: the instance token of the
	// caster's persistent instance (action/skillparty.go) it was handed out
	// by. Zero for the caster's own instance and every other effect.
	AuraParentToken uint32
	// LinkToken identifies the shared source/target relationship. Only ApplyLink
	// may create these rows; ordinary Apply cannot replace half of a pair.
	LinkToken uint32
	// BodyStatusOwner identifies the application, independently of a wire
	// token that an exact replacement may reuse.
	BodyStatusOwner         uint64
	Imbue                   bool // the single active weapon-imbue category (native category 1)
	EventCancelMask         uint8
	Movement                bool
	MovementKind            MovementKind
	movementPhase           movementPhase
	movementSlot            bool
	movementAuthoredPercent uint32
	// MovementPercent is the installed contribution in snapshots. Producers
	// supply the authored value; the registry retains it separately for hst2
	// restoration when this visible buff temporarily contributes zero.
	MovementPercent uint32
	// HidePenaltyPercent and HideBonusPercent are the two PercentProduct
	// sources a hide installs on walk and run (5965D9, 596652): minus its
	// speed word under the instance's own key, plus STSP under key 6. They
	// leave with the instance, never through the haste slot.
	HidePenaltyPercent uint32
	HideBonusPercent   uint32
	// TransformRefObjID is the RefObj an msch 1 instance transformed its
	// owner into; retiring the instance ends the transform.
	TransformRefObjID uint32
	Persistent        bool
	DeathProtected    bool // authored cbuf; independent of the durable-job clock
	DivisionID        string
	CharacterName     string
	SkillID           uint32
	SkillGroup        uint32
	InstanceToken     uint32
	State             State
	Phase             uint8
	Rider             uint32
	ExpiresAtMs       int64
	// DurationPresent distinguishes dura,0 from an absent duration. In
	// particular, an application at timestamp zero may have a real zero deadline.
	DurationPresent  bool
	StartedAtMs      int64 // native duration origin; persistent jobs use their own clock
	ClientCancelable bool
	// StopRequested is the server-side live flag cleared by
	// ActiveCharacterEffect_RequestStop(effect, false). The request owner does
	// not erase the row or emit the ended-instance packet; the character-effect
	// update owns that second phase.
	StopRequested bool
}

// EndedBatch is one character update's retired effect set. Retail broadcasts
// one counted ended-instance packet per character update, not one global
// packet across every character in a division.
/*
================
EndedBatch
================
*/
type EndedBatch struct {
	DivisionID    string
	CharacterName string
	Effects       []Effect
}

// MaxAttachedEffectsPerCharacter is also the counted teardown packet's hard
// ownership limit. Retail serializes the retirement vector with a u8 count;
// admitting a 256th live row would make the later client teardown ambiguous.
const MaxAttachedEffectsPerCharacter = 0xff

// Registry is the sole runtime owner of cancellable character effects. It is
// deliberately independent from presentation packets: callers commit state
// here first, then serialize the resulting Effect through wire.
/*
================
Registry
================
*/
type Registry struct {
	nextModifierSource uint64
	mu                 sync.Mutex
	castingStates      map[string]CastingConflictSnapshot
	links              map[string]Link
	threatOwners       map[string]uint32
	byOwner            map[string][]Effect
	pendingOwners      []string
	pendingSet         map[string]bool
}

/*
================
NewRegistry
================
*/
func NewRegistry() *Registry {
	return &Registry{
		castingStates: make(map[string]CastingConflictSnapshot),
		byOwner:       make(map[string][]Effect),
		pendingSet:    make(map[string]bool),
		links:         make(map[string]Link),
		threatOwners:  make(map[string]uint32),
	}
}

/*
================
ownerKey
================
*/
func ownerKey(divisionID, characterName string) string {
	return strings.ToLower(divisionID) + "\x00" + strings.ToLower(characterName)
}

// Apply inserts or replaces the exact {group, instance} identity for one
// owner. A zero instance remains a real key: it represents a non-stacked
// effect family, not a wildcard at the storage boundary.
/*
================
Apply
================
*/
func (r *Registry) Apply(effect Effect) bool {
	if r == nil || effect.LinkToken != 0 || effect.SkillID == 0 || effect.SkillGroup == 0 ||
		effect.DivisionID == "" || effect.CharacterName == "" || effect.MovementKind > MovementIndependent {
		return false
	}
	if effect.State != StatePending && effect.State != StateActive {
		effect.State = StateActive
	}
	if effect.Persistent && !effect.jobClockPresent && effect.ExpiresAtMs > effect.StartedAtMs {
		effect.jobClock = relativeJobClock{remaining: uint32((effect.ExpiresAtMs - effect.StartedAtMs) / 1000), active: true}
		effect.jobClockPresent = true
		effect.jobUpdatedAtMs = effect.StartedAtMs
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	key := ownerKey(effect.DivisionID, effect.CharacterName)
	// B6A0 addresses tokens without a character GID. A published nonzero token
	// therefore has one owner throughout a division, including pending stops.
	if effect.InstanceToken != 0 {
		for existingKey, existing := range r.byOwner {
			for _, row := range existing {
				if strings.EqualFold(row.DivisionID, effect.DivisionID) && row.InstanceToken == effect.InstanceToken && (existingKey != key || row.SkillGroup != effect.SkillGroup) {
					return false
				}
			}
		}
	}
	rows := r.byOwner[key]
	if effect.Imbue {
		for _, old := range rows {
			if old.Imbue && !old.StopRequested && !(old.SkillGroup == effect.SkillGroup && old.InstanceToken == effect.InstanceToken) {
				return false
			}
		}
	}
	for index := range rows {
		if rows[index].SkillGroup == effect.SkillGroup &&
			rows[index].InstanceToken == effect.InstanceToken {
			if rows[index].LinkToken != 0 {
				return false
			}
			if !r.bindModifiersLocked(&effect) {
				return false
			}
			prepareMovement(rows, index, &effect)
			r.replaceForcedTargetLocked(key, effect)
			r.changeEffectStatesLocked(key, rows[index], true)
			rows[index] = effect
			r.changeEffectStatesLocked(key, effect, false)
			r.byOwner[key] = rows
			return true
		}
	}
	if len(rows) >= MaxAttachedEffectsPerCharacter {
		return false
	}
	if !r.bindModifiersLocked(&effect) {
		return false
	}
	prepareMovement(rows, -1, &effect)
	r.replaceForcedTargetLocked(key, effect)
	r.byOwner[key] = append(rows, effect)
	r.changeEffectStatesLocked(key, effect, false)
	return true
}

// RequestVoluntaryStop implements the exact cancel-active-effect lookup:
// exact skill-ID match, state 1/2 only, and instance match only when the
// request carries a nonzero token. It clears the matched effect's logical
// live flag and returns it, preserving native list order. Erasure and the
// ended-instance broadcast deliberately belong to DrainStopRequested.
/*
================
RequestVoluntaryStop
================
*/
func (r *Registry) RequestVoluntaryStop(
	divisionID, characterName string,
	skillID, optionalInstanceToken uint32,
) (Effect, bool) {
	if r == nil || skillID == 0 {
		return Effect{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	key := ownerKey(divisionID, characterName)
	rows := r.byOwner[key]
	for index, effect := range rows {
		if effect.SkillID != skillID ||
			(effect.State != StatePending && effect.State != StateActive) ||
			(optionalInstanceToken != 0 && effect.InstanceToken != optionalInstanceToken) {
			continue
		}
		// Native lookup stops at the first list-order identity match. The
		// permission decision belongs to RequestStop on that row; it must not
		// scan forward to a later, cancelable row when the first is protected.
		if !effect.ClientCancelable || effect.StopRequested {
			return effect, false
		}
		effect.StopRequested = true
		rows[index] = effect
		r.byOwner[key] = rows
		if !r.pendingSet[key] {
			r.pendingSet[key] = true
			r.pendingOwners = append(r.pendingOwners, key)
		}
		return effect, true
	}
	return Effect{}, false
}

// DrainStopRequested is the character-effect update/retirement phase. It
// removes only rows whose live flag was cleared and returns batches in request
// order so the simulation tick can serialize one counted packet per owner.
/*
================
DrainStopRequested
================
*/
func (r *Registry) DrainStopRequested() []EndedBatch {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	var batches []EndedBatch
	// A source teardown can enqueue its recipient. Process appended owners too;
	// a range loop would drop them when the queue is reset below.
	for cursor := 0; cursor < len(r.pendingOwners); cursor++ {
		key := r.pendingOwners[cursor]
		rows := r.byOwner[key]
		for i := range rows {
			if rows[i].StopRequested {
				retireMovement(rows, i)
			}
		}
		var kept []Effect
		var ended []Effect
		for _, effect := range rows {
			if effect.StopRequested {
				r.changeEffectStatesLocked(key, effect, true)
				if effect.LinkToken != 0 {
					r.retireLinkHalfLocked(effect)
				}
				if effect.jobClockPresent {
					// 582F93 -> embedded trigger -> 651480(reason=1).
					// This callback belongs to teardown, not the stop request.
					effect.jobClock.retire(1)
					effect.jobCheckpointDue = false
				}
				ended = append(ended, effect)
			} else {
				kept = append(kept, effect)
			}
		}
		delete(r.pendingSet, key)
		if len(ended) == 0 {
			continue
		}
		if len(kept) == 0 {
			delete(r.byOwner, key)
		} else {
			r.byOwner[key] = kept
		}
		batches = append(batches, EndedBatch{
			DivisionID:    ended[0].DivisionID,
			CharacterName: ended[0].CharacterName,
			Effects:       ended,
		})
	}
	r.pendingOwners = r.pendingOwners[:0]
	return batches
}

// Snapshot returns an owner-isolated copy for diagnostics and tests.
/*
================
Snapshot
================
*/
func (r *Registry) Snapshot(divisionID, characterName string) []Effect {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := r.byOwner[ownerKey(divisionID, characterName)]
	return append([]Effect(nil), rows...)
}

/*
================
Forget
================
*/
func (r *Registry) Forget(divisionID, characterName string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	key := ownerKey(divisionID, characterName)
	for _, e := range r.byOwner[key] {
		if e.LinkToken != 0 {
			r.retireLinkHalfLocked(e)
		}
	}
	delete(r.byOwner, key)
	delete(r.castingStates, key)
	delete(r.pendingSet, key)
	r.mu.Unlock()
}

// RetireBodyStatusesOnDeath removes every transient application, including
// modifier-only and presentation-only effects. Authored cbuf protection and the existing durable-job
// lifecycle are independent reasons to retain an application.
// The action owner holds the character mutation door while consuming this list.
/*
================
RetireBodyStatusesOnDeath
================
*/
func (r *Registry) RetireBodyStatusesOnDeath(divisionID, characterName string) []Effect {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := ownerKey(divisionID, characterName)
	var ended, kept []Effect
	rows := r.byOwner[key]
	for i := range rows {
		if !rows[i].DeathProtected && !rows[i].Persistent {
			retireMovement(rows, i)
		}
	}
	for _, effect := range rows {
		// 59FFD3 forces selection of every unprotected application. A
		// presentation-only buff (a planted trap's board entry) ends too.
		if RetirementSelected(true, 0, false, 0, false, false, effect.DeathProtected || effect.Persistent) {
			r.changeEffectStatesLocked(key, effect, true)
			if effect.LinkToken != 0 {
				r.retireLinkHalfLocked(effect)
			}
			ended = append(ended, effect)
		} else {
			kept = append(kept, effect)
		}
	}
	if len(kept) == 0 {
		delete(r.byOwner, key)
	} else {
		r.byOwner[key] = kept
	}
	return ended
}

// Expired is the shared retirement and consumer boundary. Ordinary duration
// effects use native 5830B0:5851E5..5851E7 (unsigned elapsed > duration+rider),
// including zero duration. Durable jobs use the native online-seconds clock.
// No caller should infer liveness from a positive remaining-duration display.
/*
================
Expired
================
*/
func (e Effect) Expired(nowMs int64) bool {
	if !e.DurationPresent && e.ExpiresAtMs <= 0 {
		return false
	}
	if e.Persistent {
		if e.jobClockPresent {
			e.advanceJob(nowMs)
			return !e.jobClock.active
		}
		return nowMs >= e.ExpiresAtMs
	}
	if e.DurationPresent {
		// Native installs and reads on one thread and one clock, so its
		// unsigned elapsed never starts below zero. Here a reader's clock can
		// trail an install made after it was sampled: that row has not
		// started yet, it has not wrapped.
		if nowMs < e.StartedAtMs {
			return false
		}
		return uint32(nowMs-e.StartedAtMs) > uint32(e.ExpiresAtMs-e.StartedAtMs)
	}
	return nowMs > e.ExpiresAtMs
}

// RemainingMs is presentation of the same clock, not an alternate liveness
// test: zero remaining is still live at a strict native deadline.
/*
================
RemainingMs
================
*/
func (e Effect) RemainingMs(nowMs int64) uint32 {
	if e.Expired(nowMs) {
		return 0
	}
	if e.DurationPresent && !e.Persistent {
		if nowMs < e.StartedAtMs {
			return uint32(e.ExpiresAtMs - e.StartedAtMs)
		}
		return uint32(e.ExpiresAtMs-e.StartedAtMs) - uint32(nowMs-e.StartedAtMs)
	}
	if e.ExpiresAtMs > nowMs {
		return uint32(e.ExpiresAtMs - nowMs)
	}
	return 0
}

/*
================
Expire

Expire uses the same retirement queue as explicit stops. Projection may omit
expired rows before the next tick, but only this owner removes live state.

The simulation tick samples its clock when it fires and runs this update
later, while operations install effects concurrently on their own clock. A
row whose origin lies after nowMs was installed after this pass's clock was
taken; Expired's uint32 elapsed would wrap and retire it at once. Native
installs and updates on one thread and one clock, so its update never sees
such a row: it waits for the next pass, whose clock has caught up.
================
*/
func (r *Registry) Expire(nowMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, rows := range r.byOwner {
		for i := range rows {
			if nowMs < rows[i].StartedAtMs {
				continue
			}
			if !rows[i].StopRequested {
				rows[i].advanceJob(nowMs)
			}
			if !rows[i].StopRequested && rows[i].Expired(nowMs) {
				rows[i].StopRequested = true
				if !r.pendingSet[key] {
					r.pendingSet[key] = true
					r.pendingOwners = append(r.pendingOwners, key)
				}
			}
		}
	}
}

// TakeJobCheckpoints advances each job's own clock and consumes its pending
// checkpoint. Jobs installed later do not inherit a global 300-second timer.
/*
================
TakeJobCheckpoints
================
*/
func (r *Registry) TakeJobCheckpoints(nowMs int64) []EndedBatch {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []EndedBatch
	for _, rows := range r.byOwner {
		var batch EndedBatch
		for i := range rows {
			e := &rows[i]
			if e.StopRequested {
				continue
			}
			e.advanceJob(nowMs)
			if e.Persistent && e.jobCheckpointDue {
				e.jobCheckpointDue = false
				batch.DivisionID = e.DivisionID
				batch.CharacterName = e.CharacterName
				batch.Effects = append(batch.Effects, *e)
			}
		}
		if len(batch.Effects) > 0 {
			out = append(out, batch)
		}
	}
	return out
}

// RetireEvent implements synchronous 5A16C0 event-mask teardown. Producers
// bind only descriptors with nonzero activity and execution context mode 1.
/*
================
RetireEvent
================
*/
func (r *Registry) RetireEvent(division, name string, mask uint8) []Effect {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := ownerKey(division, name)
	rows := r.byOwner[key]
	var ended, kept []Effect
	for i := range rows {
		if rows[i].EventCancelMask&mask != 0 {
			retireMovement(rows, i)
		}
	}
	for _, e := range rows {
		if e.EventCancelMask&mask != 0 {
			r.changeEffectStatesLocked(key, e, true)
			if e.LinkToken != 0 {
				r.retireLinkHalfLocked(e)
			}
			ended = append(ended, e)
		} else {
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 {
		delete(r.byOwner, key)
	} else {
		r.byOwner[key] = kept
	}
	return ended
}

// RetireInstances is the synchronous RequestRetirement of chosen instances
// (CSkillManager_ProcessDamageEffects 5A1691): the same teardown as
// RetireEvent, selected by instance token instead of event bit.
/*
================
RetireInstances
================
*/
func (r *Registry) RetireInstances(division, name string, tokens []uint32) []Effect {
	if len(tokens) == 0 {
		return nil
	}
	chosen := make(map[uint32]bool, len(tokens))
	for _, token := range tokens {
		chosen[token] = true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := ownerKey(division, name)
	rows := r.byOwner[key]
	var ended, kept []Effect
	for i := range rows {
		if chosen[rows[i].InstanceToken] {
			retireMovement(rows, i)
		}
	}
	for _, e := range rows {
		if chosen[e.InstanceToken] {
			r.changeEffectStatesLocked(key, e, true)
			if e.LinkToken != 0 {
				r.retireLinkHalfLocked(e)
			}
			ended = append(ended, e)
		} else {
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 {
		delete(r.byOwner, key)
	} else {
		r.byOwner[key] = kept
	}
	return ended
}
