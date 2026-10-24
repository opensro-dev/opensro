/*
===========================================================================

session_scene.go - scene revisions, visibility and private receipt admission

Queue admission checks captured revisions atomically. Visibility requires
game-ready; private authority receipts may follow a bootstrap while loading.

===========================================================================
*/
package transport

import (
	"sort"
)

// PublishedObjects returns only scope changes admitted to this scene's queue.
// A replacement between the caller's scene capture and this read invalidates
// the snapshot rather than adopting the replacement's objects.
/*
================
PublishedObjects

Every tick reads each session's set several times (the scene snapshot and
the view hooks) while it changes far less often, so the sorted order is
kept and rebuilt only after a change. The caller gets its own copy, never
nil while current: readers take nil as "not current".
================
*/
func (s *Session) PublishedObjects(revision uint64) ([]uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.sceneLoading || s.sceneRevision != revision {
		return nil, false
	}
	if s.publishedStale {
		s.publishedSorted = s.publishedSorted[:0]
		for gid := range s.observedObjects {
			s.publishedSorted = append(s.publishedSorted, gid)
		}
		sort.Slice(s.publishedSorted, func(i, j int) bool { return s.publishedSorted[i] < s.publishedSorted[j] })
		s.publishedStale = false
	}
	return append(make([]uint32, 0, len(s.publishedSorted)), s.publishedSorted...), true
}

/*
================
clearObservedLocked

A scene reset forgets every published object. Caller holds s.mu.
================
*/
func (s *Session) clearObservedLocked() {
	clear(s.observedObjects)
	s.publishedStale = true
}

// A transport survives scene replacement. Its visibility producers must not.
// Revision checks and queue publication share the session lock, so a tick
// captured before reset cannot enqueue actor creation after the new bootstrap.
/*
================
SceneRevision
================
*/
func (s *Session) SceneRevision() (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sceneRevision, !s.sceneLoading && !s.closed
}

/*
================
SceneReceiptRevision

Private receipts remain eligible during loading, after bootstrap enqueue.
Closed sessions are not capture targets; enqueue rechecks later closure.
================
*/
func (s *Session) SceneReceiptRevision() (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sceneRevision, !s.closed
}

// BeginSceneAdmission suspends visibility before bootstrap construction can
// publish a character's division binding. SendSceneReset later publishes the
// complete bootstrap; only game-ready releases scene producers again.
/*
================
BeginSceneAdmission
================
*/
func (s *Session) BeginSceneAdmission() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sceneRevision++
	s.sceneLoading = true
	s.clearObservedLocked()
}

// ObjectScopeChange accompanies the object's complete creation or removal
// batch. Membership becomes visible to other producers only after that batch
// has been admitted to the reliable queue under the same session lock.
/*
================
ObjectScopeChange
================
*/
type ObjectScopeChange struct {
	GID     uint32
	Visible bool
}

// ScopeChanges carries a game lane's scope records across the transport
// boundary without transport knowing the lane's type.
/*
================
ScopeChanges
================
*/
func ScopeChanges[T ~struct {
	GID     uint32
	Visible bool
}](in []T) []ObjectScopeChange {
	if len(in) == 0 {
		return nil
	}
	out := make([]ObjectScopeChange, len(in))
	for i, change := range in {
		out[i] = ObjectScopeChange(change)
	}
	return out
}

/*
================
PublishSceneObjects
================
*/
func (s *Session) PublishSceneObjects(revision uint64, changes []ObjectScopeChange, frames []Frame) error {
	return s.sendSceneObjectBatch(frames, sceneBatchOptions{revision: &revision, changes: changes})
}

// SendObservedBatch cannot race a despawn between recipient selection and
// enqueue. A stale scene capture cannot target a replacement scene either.
/*
================
SendObservedBatch
================
*/
func (s *Session) SendObservedBatch(revision uint64, gid uint32, frames []Frame) error {
	if gid == 0 {
		return nil
	}
	return s.sendSceneObjectBatch(frames, sceneBatchOptions{revision: &revision, observedGID: gid})
}

/*
================
SendUnobservedBatch

Introduces gid to a live scene that does not show it yet. The membership
check and enqueue share the queue lock, so a creation another producer
admitted first is never sent twice.
================
*/
func (s *Session) SendUnobservedBatch(revision uint64, gid uint32, frames []Frame) error {
	if gid == 0 {
		return nil
	}
	return s.sendSceneObjectBatch(frames, sceneBatchOptions{revision: &revision, unobservedGID: gid})
}

// BroadcastObserved queues one public transaction only for admitted viewers
// of its source. SendObservedBatch rechecks membership under the queue lock.
/*
================
BroadcastObserved
================
*/
func (h *Hub) BroadcastObserved(division string, exceptSession uint64, gid uint32, frames []Frame) {
	for _, viewer := range h.SessionsInDivision(division) {
		if viewer.ID == exceptSession {
			continue
		}
		revision, active := viewer.SceneRevision()
		if active {
			_ = viewer.SendObservedBatch(revision, gid, frames)
		}
	}
}

/*
================
SendSceneReset
================
*/
func (s *Session) SendSceneReset(frames []Frame) error { return s.sendSceneBatch(frames, nil, true) }

/*
================
SendSceneBatch
================
*/
func (s *Session) SendSceneBatch(revision uint64, frames []Frame) error {
	return s.sendSceneBatch(frames, &revision, false)
}

/*
================
SendSceneReceiptBatch

Private authority receipts committed after a bootstrap must follow it even
before game-ready. A replaced scene still rejects the captured revision.
The revision check and enqueue share the queue lock; this does not relax
the active-scene requirement for SendSceneBatch or visibility publication.
================
*/
func (s *Session) SendSceneReceiptBatch(revision uint64, frames []Frame) error {
	return s.sendSceneObjectBatch(frames, sceneBatchOptions{revision: &revision, allowLoading: true})
}

/*
================
SendSceneUnreliableKeyed
================
*/
func (s *Session) SendSceneUnreliableKeyed(revision uint64, opcode uint16, key uint64, payload []byte) error {
	return s.sendSceneUnreliableKeyed(opcode, key, payload, &revision)
}

// Initial admission still owns WorldBound hooks; scene-ready only releases
// existing world visibility, preserving session-owned effects and companions.
/*
================
FinishSceneReentry
================
*/
func (s *Session) FinishSceneReentry() bool {
	s.mu.Lock()
	if !s.sceneLoading || s.closed {
		s.mu.Unlock()
		return false
	}
	s.sceneLoading = false
	hooks := s.sceneReadyHooks
	s.sceneReadyHooks = nil
	s.mu.Unlock()
	for _, hook := range hooks {
		hook()
	}
	return true
}

/*
================
OnSceneReady

Registers fn to run, in registration order, when the scene currently
loading finishes (FinishSceneReentry). The hooks are not retained past
that completion; an owner holding work during later scene loads
registers again.
================
*/
func (s *Session) OnSceneReady(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sceneReadyHooks = append(s.sceneReadyHooks, fn)
}
