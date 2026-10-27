/*
===========================================================================

session_scene.go - scene revisions, visibility and private receipt admission

Queue admission checks captured revisions atomically. Visibility requires
game-ready; private authority receipts may follow a bootstrap while loading.

===========================================================================
*/
package transport

import (
	"slices"
)

// PublishedObjects returns only scope changes admitted to this scene's queue.
// A replacement between the caller's scene capture and this read invalidates
// the snapshot rather than adopting the replacement's objects.
/*
================
PublishedObjects

The caller gets its own copy, never nil while current: readers take nil as
"not current". A reader that only reads takes PublishedObjectsShared.
================
*/
func (s *Session) PublishedObjects(revision uint64) ([]uint32, bool) {
	shared, current := s.PublishedObjectsShared(revision)
	if !current {
		return nil, false
	}
	return append(make([]uint32, 0, len(shared)), shared...), true
}

/*
================
PublishedObjectsShared

PublishedObjects without the copy, for readers that never write the set:
every tick reads each session's set several times (the scene snapshot and
the view hooks) while it changes far less often. The sorted order is kept
and rebuilt only after a change. Never nil while current.

The kept set is immutable once built: a change builds a NEW array rather
than refilling the old one, so a reader holding an earlier result never
sees a later change, and holding it needs no lock. The capacity is clipped
to the length, so a reader's append copies instead of writing into the
shared array. A reader must never write an element.
================
*/
func (s *Session) PublishedObjectsShared(revision uint64) ([]uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.sceneLoading || s.sceneRevision != revision {
		return nil, false
	}
	if s.publishedStale {
		sorted := make([]uint32, 0, len(s.observedObjects))
		for gid := range s.observedObjects {
			sorted = append(sorted, gid)
		}
		slices.Sort(sorted)
		s.publishedSorted = sorted
		s.publishedStale = false
	}
	if s.publishedSorted == nil {
		// Nothing was ever published: an empty, non-nil set (no allocation).
		return []uint32{}, true
	}
	return slices.Clip(s.publishedSorted), true
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

// SendBatch atomically queues one reliable application transaction. No other
// producer can interleave with the batch and the client can never receive a
// successful prefix followed by a queue-overflow disconnect. This is the
// admission path for the EnterWorld result and its native bootstrap stream.
//
// The batch remains bounded by both configured frame and byte limits. Payloads
// are copied before publication, exactly as in Send.
/*
================
SendBatch
================
*/
func (s *Session) SendBatch(frames []Frame) error { return s.sendSceneBatch(frames, nil, false) }

/*
================
sendSceneBatch
================
*/
func (s *Session) sendSceneBatch(frames []Frame, revision *uint64, reset bool) error {
	return s.sendSceneObjectBatch(frames, sceneBatchOptions{revision: revision, reset: reset})
}

/*
================
sceneBatchOptions

Private receipts may follow an already queued bootstrap while its scene is
loading. Visibility producers retain the active-scene requirement.
================
*/
type sceneBatchOptions struct {
	revision    *uint64
	reset       bool
	observedGID uint32
	// unobservedGID is the converse: send only while the scene does not
	// show that object yet.
	unobservedGID uint32
	changes       []ObjectScopeChange
	allowLoading  bool
}

/*
================
sendSceneObjectBatch
================
*/
func (s *Session) sendSceneObjectBatch(frames []Frame, options sceneBatchOptions) error {
	// Copy before appending: callers retain ownership of their metadata.
	changes := append([]ObjectScopeChange(nil), options.changes...)
	for _, frame := range frames {
		changes = append(changes, frame.Scope...)
	}
	// An empty frame's scope change still applies; only its bytes are
	// refused (emptyframe.go).
	frames = s.withoutEmptyFrames(frames)
	if len(frames) == 0 {
		return nil
	}
	if len(frames) > s.hub.cfg.OutboundQueue {
		s.hub.closeSessionFromSend(s, errOutboundBurstTooLarge)
		return errOutboundBurstTooLarge
	}
	batchBytes := 0
	for _, frame := range frames {
		if frame.Opcode > maxReservedOpcode && s.evicted.Load() {
			return ErrSessionEvicted
		}
		frameBytes := frame.EncodedLen()
		if frameBytes > MaxFrameBytes {
			s.hub.closeSessionFromSend(s, ErrFrameTooLarge)
			return ErrFrameTooLarge
		}
		if batchBytes > s.hub.cfg.OutboundQueueBytes-frameBytes {
			s.hub.closeSessionFromSend(s, errOutboundBurstTooLarge)
			return errOutboundBurstTooLarge
		}
		batchBytes += frameBytes
	}
	s.mu.Lock()
	if options.observedGID != 0 {
		if _, observed := s.observedObjects[options.observedGID]; !observed || s.sceneLoading {
			s.mu.Unlock()
			return nil
		}
	}
	if options.unobservedGID != 0 {
		if _, observed := s.observedObjects[options.unobservedGID]; observed || s.sceneLoading {
			s.mu.Unlock()
			return nil
		}
	}
	if options.revision != nil && ((!options.allowLoading && s.sceneLoading) || s.sceneRevision != *options.revision) {
		s.mu.Unlock()
		return nil
	}
	if s.closed {
		s.mu.Unlock()
		return ErrSessionClosed
	}
	if len(s.queue) > s.hub.cfg.OutboundQueue-len(frames) ||
		s.queueBytes+s.lossyBytes > s.hub.cfg.OutboundQueueBytes-batchBytes {
		s.mu.Unlock()
		s.hub.closeSessionFromSend(s, errSlowConsumer)
		return errSlowConsumer
	}
	for _, frame := range frames {
		if frame.Opcode > maxReservedOpcode && s.evicted.Load() {
			s.mu.Unlock()
			return ErrSessionEvicted
		}
	}
	if options.reset {
		s.sceneRevision++
		s.sceneLoading = true
		s.clearObservedLocked()
		clear(s.lossy)
		s.lossyKeys = nil
		s.lossyBytes = 0
	}
	for _, frame := range frames {
		s.queue = append(s.queue, Frame{Opcode: frame.Opcode, Payload: append([]byte(nil), frame.Payload...), Current: frame.Current})
	}
	for _, change := range changes {
		if change.GID == 0 {
			continue
		}
		if change.Visible {
			if s.observedObjects == nil {
				s.observedObjects = make(map[uint32]struct{})
			}
			s.observedObjects[change.GID] = struct{}{}
			s.publishedStale = true
		} else {
			delete(s.observedObjects, change.GID)
			s.publishedStale = true
			// Reliable removal takes priority over queued loss-tolerant motion.
			// Retire that motion here so it cannot follow the despawn on WS.
			kept := s.lossyKeys[:0]
			for _, slot := range s.lossyKeys {
				if slot.Key == uint64(change.GID) && (slot.Opcode == OpObjectSourceMove || slot.Opcode == OpObjectSourceCorrection) {
					s.lossyBytes -= s.lossy[slot].EncodedLen()
					delete(s.lossy, slot)
				} else {
					kept = append(kept, slot)
				}
			}
			s.lossyKeys = kept
		}
	}
	s.queueBytes += batchBytes
	s.hub.observeQueueDepth(len(s.queue)+len(s.lossyKeys), s.queueBytes+s.lossyBytes)
	s.cond.Broadcast()
	s.mu.Unlock()
	return nil
}

/*
================
sendSceneUnreliableKeyed
================
*/
func (s *Session) sendSceneUnreliableKeyed(opcode uint16, key uint64, payload []byte, revision *uint64) error {
	if opcode > maxReservedOpcode && s.evicted.Load() {
		return ErrSessionEvicted // lame duck: no ticks on any lane
	}
	if !IsLossTolerantOpcode(opcode) {
		return s.sendSceneBatch([]Frame{{Opcode: opcode, Payload: payload}}, revision, false)
	}
	s.mu.Lock()
	if revision != nil && (s.sceneLoading || s.sceneRevision != *revision) {
		s.mu.Unlock()
		return nil
	}
	if s.closed {
		s.mu.Unlock()
		return ErrSessionClosed
	}
	conn := s.conn
	s.mu.Unlock()
	if conn != nil && conn.SupportsUnreliable() {
		if err := conn.WriteUnreliable(Frame{Opcode: opcode, Payload: payload}); err == nil {
			s.hub.metrics.datagramsOut.Add(1)
			s.hub.metrics.datagramBytesOut.Add(uint64(2 + len(payload)))
			return nil
		}
	}

	owned := append([]byte(nil), payload...)
	slot := lossySlot{Opcode: opcode, Key: key}
	s.mu.Lock()
	if revision != nil && (s.sceneLoading || s.sceneRevision != *revision) {
		s.mu.Unlock()
		return nil
	}
	if s.closed {
		s.mu.Unlock()
		return ErrSessionClosed
	}
	previous, pending := s.lossy[slot]
	previousBytes := 0
	if pending {
		previousBytes = previous.EncodedLen()
	}
	frameBytes := 2 + len(owned)
	if !pending {
		if len(s.lossyKeys) >= s.hub.cfg.OutboundQueue {
			// Loss-tolerant by contract: shed the new frame instead of
			// killing the session.
			s.mu.Unlock()
			return nil
		}
	}
	if s.queueBytes+s.lossyBytes-previousBytes > s.hub.cfg.OutboundQueueBytes-frameBytes {
		// Preserve an existing coalesced value when its replacement is too
		// large; this lane may shed by contract but must stay byte-bounded.
		s.mu.Unlock()
		return nil
	}
	if !pending {
		s.lossyKeys = append(s.lossyKeys, slot)
	}
	s.lossy[slot] = Frame{Opcode: opcode, Payload: owned}
	s.lossyBytes += frameBytes - previousBytes
	s.hub.observeQueueDepth(len(s.queue)+len(s.lossyKeys), s.queueBytes+s.lossyBytes)
	s.cond.Broadcast()
	s.mu.Unlock()
	return nil
}
