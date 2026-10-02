/*
===========================================================================

replacement_transaction.go - atomic effect replacement and retirement

Admission and installation share metadata and registry state under one lock.
Retired instances remain queued until their teardown is published.

===========================================================================
*/
package statuseffect

import "strings"

// ReplacementApplication supplies immutable reference metadata and the action
// controller's current-command words. Active bits are always read under the
// registry lock, never accepted from a caller's stale snapshot. Missing metadata
// fails closed. This transaction does not itself establish cast admission,
// release timing, costs or current-command ownership.
/*
================
ReplacementApplication
================
*/
type ReplacementApplication struct {
	Effect                     Effect
	Descriptors                map[uint32]ReplacementDescriptor
	CasterIsRecipient          bool
	CurrentPacked, CurrentOvl2 uint32
}

// ApplyReplacement applies 59D870's decision and appends a fresh effect under
// one lock. Retired rows remain until the update drains them. Consequently even
// replacement needs a free row and a new token: reusing the old token before its
// ended-instance publication would let that packet destroy the new effect.
/*
================
ApplyReplacement
================
*/
func (r *Registry) ApplyReplacement(a ReplacementApplication) bool {
	return r.applyReplacement(a, true)
}

// RequestReplacement is the native validation phase (59D870, called by
// 58E2F4 for untargeted unlinked skills). It may request old-instance retirement
// but does not allocate, append or install the new effect. Later release or
// installation failure does not roll this native side effect back.
/*
================
RequestReplacement
================
*/
func (r *Registry) RequestReplacement(a ReplacementApplication) bool {
	return r.applyReplacement(a, false)
}

/*
================
applyReplacement
================
*/
func (r *Registry) applyReplacement(a ReplacementApplication, install bool) bool {
	e := a.Effect
	if r == nil || e.DivisionID == "" || e.CharacterName == "" || e.SkillID == 0 || e.LinkToken != 0 || e.StopRequested || e.MovementKind > MovementIndependent || install && e.InstanceToken == 0 {
		return false
	}
	d, ok := a.Descriptors[e.SkillID]
	if !ok || d.Group != e.SkillGroup {
		return false
	}
	if install && e.State != StatePending && e.State != StateActive {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := ownerKey(e.DivisionID, e.CharacterName)
	rows := r.byOwner[key]
	if install && len(rows) >= MaxAttachedEffectsPerCharacter {
		return false
	}
	if install {
		for _, ownerRows := range r.byOwner {
			for _, old := range ownerRows {
				if strings.EqualFold(old.DivisionID, e.DivisionID) && old.InstanceToken == e.InstanceToken {
					return false
				}
			}
		}
	}
	type peer struct {
		key   string
		index int
	}
	peers := make([]peer, len(rows))
	candidates := make([]ReplacementCandidate, len(rows))
	for i, old := range rows {
		metadata, found := a.Descriptors[old.SkillID]
		if !found || metadata.Group != old.SkillGroup {
			return false
		}
		candidates[i] = ReplacementCandidate{Descriptor: metadata, Mode: old.Phase}
		if old.AreaSourceGID == 0 || old.AreaSourceName == "" {
			continue
		}
		candidates[i].HasAreaLink = true
		peerKey := ownerKey(e.DivisionID, old.AreaSourceName)
		for j, other := range r.byOwner[peerKey] {
			// 59D9F5/59EF90: exact old skill ID, optional token zero;
			// a previously requested stop still participates in list order.
			if other.OwnerGID == old.AreaSourceGID && other.SkillID == old.SkillID && (other.State == StatePending || other.State == StateActive) {
				candidates[i].LinkedPeerFound = true
				peers[i] = peer{peerKey, j}
				break
			}
		}
	}
	conflicts := r.castingStates[key]
	conflicts.CurrentPacked, conflicts.CurrentOvl2 = a.CurrentPacked, a.CurrentOvl2
	decision := DecideReplacement(d, candidates, a.CasterIsRecipient, conflicts)
	if !decision.Allowed {
		return false
	}
	if install && e.Imbue {
		for _, old := range rows {
			if old.Imbue && !old.StopRequested {
				return false
			}
		}
	}
	if install && !r.bindModifiersLocked(&e) {
		return false
	}
	// No failing operation follows this point. 59D870 clears the live flag
	// directly; nbuf's voluntary-cancellation protection is not consulted.
	if decision.RetireIndex >= 0 {
		r.requestReplacementStopLocked(key, decision.RetireIndex)
		if decision.RetireLinkedPeer {
			p := peers[decision.RetireIndex]
			r.requestReplacementStopLocked(p.key, p.index)
		}
	}
	if install {
		prepareMovement(r.byOwner[key], -1, &e)
		r.replaceForcedTargetLocked(key, e)
		r.byOwner[key] = append(r.byOwner[key], e)
		r.changeEffectStatesLocked(key, e, false)
	}
	return true
}

/*
================
requestReplacementStopLocked
================
*/
func (r *Registry) requestReplacementStopLocked(key string, index int) {
	r.byOwner[key][index].StopRequested = true
	if !r.pendingSet[key] {
		r.pendingSet[key] = true
		r.pendingOwners = append(r.pendingOwners, key)
	}
}
