/*
===========================================================================

peer_visibility.go - which players each viewer is shown, kept by block

A viewer is shown every other same-instance player whose interest block is
its own block or one of the eight around it (worldgeom InterestBlock
Adjacent). The shown sets change only when a member joins, leaves, changes
instance or appearance (the full pass: every viewer against the block
index), or when a member crosses into another block. A crossing changes
only the pairs that member is part of, so only those are recomputed: its
own view, and every viewer that showed it or now may (the native
CMsgBlock_OnObjectEnter / Leave, which links and unlinks one object
against the blocks it entered and left). A plaza that straddles a block
corner sees crossings every tick; recomputing every viewer against every
candidate for each of them was the largest cost of the live tick.

The shown sets also keep their inversion, each player's viewers
(peerWatchers), updated at the same two points that change them, so the
movement legs reach a mover's viewers without rebuilding it every tick.

===========================================================================
*/
package simulation

import (
	"opensro.online/server/internal/game/item/wire"
)

/*
================
divisionTickState.peerVisibilityChanges

Compares each session's visibility key with the previous pass's. full is
true when anything but an interest block changed (or the pass has never
completed): membership, order, identity, instance, appearance or liveness.
Otherwise crossers are the sessions whose block changed, by index. The
keys are updated either way.
================
*/
func (state *divisionTickState) peerVisibilityChanges(sessions []SessionSnapshot, now int64, live map[string]bool) (full bool, crossers []int) {
	previous := len(state.peerVisibilityKeys)
	full = !state.peerVisibilityValid || previous != len(sessions)
	crossers = state.crossers[:0]
	state.faceless = state.faceless[:0]
	for i := range sessions {
		s := &sessions[i]
		key := peerVisibilityKey{s.SessionID, s.CharacterID, peerInterestKey{s.DivisionID, s.WorldInstance, peerInterestBlock(s.World.LiveSpawnAt(now))}, s.Appearance != nil, live[s.SessionID]}
		if s.Appearance == nil {
			// A viewer the block index does not hold (it holds peers).
			state.faceless = append(state.faceless, i)
		}
		if i >= previous {
			state.peerVisibilityKeys = append(state.peerVisibilityKeys, key)
			continue
		}
		old := state.peerVisibilityKeys[i]
		if old == key {
			continue
		}
		moved := old
		moved.interest.block = key.interest.block
		if moved == key {
			crossers = append(crossers, i)
		} else {
			full = true
		}
		state.peerVisibilityKeys[i] = key
	}
	if len(sessions) < previous {
		clear(state.peerVisibilityKeys[len(sessions):])
		state.peerVisibilityKeys = state.peerVisibilityKeys[:len(sessions)]
	}
	state.crossers = crossers
	return full, crossers
}

/*
================
peerVisibilityKey.sees

Whether the viewer keyed viewer is shown the player keyed peer: another
session of the same division and instance, with an appearance, in an
adjacent block. The relation the block index's candidates walk.
================
*/
func (viewer peerVisibilityKey) sees(peer peerVisibilityKey) bool {
	return viewer.session != peer.session && peer.appearance &&
		viewer.interest.division == peer.interest.division && viewer.interest.world == peer.interest.world &&
		viewer.interest.block.Adjacent(peer.interest.block)
}

/*
================
Ticker.runPeerVisibility

The per-tick peer spawn/despawn leg: a viewer is sent one 0x30D7 when a
peer enters its interest (at the peer's live pose) and one 0x36AB when it
leaves. The local player is never pushed to itself - the client renders
self on the local plane and the remote view must never carry it.
================
*/
func (t *Ticker) runPeerVisibility(state *divisionTickState, nowMs int64, sessions []SessionSnapshot, live map[string]bool) {
	full, crossers := state.peerVisibilityChanges(sessions, nowMs, live)
	if !full && len(crossers) == 0 {
		return
	}
	// A recovered delivery panic must not certify a partial visibility pass.
	state.peerVisibilityValid = false
	state.sessionIndexValid = false
	index := buildPeerInterestIndex(sessions, nowMs, false)
	if full {
		for i := range sessions {
			t.refreshPeerView(state, index, sessions, i, nowMs)
		}
		state.dropDepartedViewers(live)
	} else {
		for _, i := range crossers {
			t.refreshPeerView(state, index, sessions, i, nowMs)
			t.refreshPeerViewers(state, index, sessions, i, nowMs)
		}
	}
	state.peerVisibilityValid = true
}

/*
================
Ticker.refreshPeerView

One viewer's shown set made exact: every candidate of its 3x3 shown,
every shown player outside it despawned.
================
*/
func (t *Ticker) refreshPeerView(state *divisionTickState, index peerInterestIndex, sessions []SessionSnapshot, viewerIndex int, nowMs int64) {
	viewer := &sessions[viewerIndex]
	shown := state.shownFor(viewer.SessionID)
	state.candidates = index.candidates(viewer, viewer.World.LiveSpawnAt(nowMs), state.candidates)
	if state.liveGids == nil {
		state.liveGids = make(map[uint32]bool)
	}
	clear(state.liveGids)
	for _, peerIndex := range state.candidates {
		peer := &sessions[peerIndex]
		if peer.SessionID == viewer.SessionID {
			continue
		}
		gid := PlayerObjectID(peer.CharacterID)
		state.liveGids[gid] = true
		if !shown[gid] {
			t.showPeerTo(state, viewer, peer, index.poses[peerIndex], nowMs)
		}
	}
	for gid := range shown {
		if !state.liveGids[gid] {
			t.hidePeerFrom(state, viewer.SessionID, gid)
		}
	}
}

/*
================
Ticker.refreshPeerViewers

The other side of a crossing: every viewer that showed the crosser, or
whose 3x3 now holds it, shows it exactly when the relation says so.
================
*/
func (t *Ticker) refreshPeerViewers(state *divisionTickState, index peerInterestIndex, sessions []SessionSnapshot, peerIndex int, nowMs int64) {
	peer := &sessions[peerIndex]
	gid := PlayerObjectID(peer.CharacterID)
	peerKey := state.peerVisibilityKeys[peerIndex]
	pose := peer.World.LiveSpawnAt(nowMs)
	// The viewers the crosser may enter: the index's members of its 3x3,
	// and the members the index does not hold (no appearance of their own).
	state.candidates = append(index.candidates(peer, pose, state.candidates), state.faceless...)
	for _, viewerIndex := range state.candidates {
		viewer := &sessions[viewerIndex]
		if state.peerVisibilityKeys[viewerIndex].sees(peerKey) && !state.shownPeers[viewer.SessionID][gid] {
			t.showPeerTo(state, viewer, peer, pose, nowMs)
		}
	}
	// A viewer the crosser left behind is no candidate of its new block.
	for viewerID := range state.peerWatchers[gid] {
		viewerIndex, ok := state.sessionIndex(sessions, viewerID)
		if !ok || !state.peerVisibilityKeys[viewerIndex].sees(peerKey) {
			t.hidePeerFrom(state, viewerID, gid)
		}
	}
}

/*
================
divisionTickState.sessionIndex

The index of a session in this pass's snapshot, by id: built once per
incremental pass, from the keys the pass just wrote.
================
*/
func (state *divisionTickState) sessionIndex(sessions []SessionSnapshot, sessionID string) (int, bool) {
	if !state.sessionIndexValid {
		if state.sessionIndexes == nil {
			state.sessionIndexes = make(map[string]int, len(sessions))
		}
		clear(state.sessionIndexes)
		for i := range sessions {
			state.sessionIndexes[sessions[i].SessionID] = i
		}
		state.sessionIndexValid = true
	}
	i, ok := state.sessionIndexes[sessionID]
	return i, ok
}

/*
================
Ticker.showPeerTo

Sends viewer the peer's spawn: the item references it has not been sent
ride ahead of the row (freshReferenceIDs), the body state and, for a
moving peer, its move state and path follow it.
================
*/
func (t *Ticker) showPeerTo(state *divisionTickState, viewer, peer *SessionSnapshot, pose Spawn, nowMs int64) {
	gid := PlayerObjectID(peer.CharacterID)
	appearance := *peer.Appearance
	appearance.Walk, appearance.Run = peer.World.MovementSpeeds()
	var frames []Frame
	if t.ItemReferences != nil {
		frames = t.ItemReferences(freshReferenceIDs(state.referencesFor(viewer.SessionID), appearance.ItemIDs()))
	}
	frames = append(frames, Frame{
		ScopeGID: gid, ScopeVisible: true,
		Opcode:  wire.OpSingleObjectSpawn,
		Payload: BuildPeerSpawnRow(appearance, gid, pose),
	})
	if peer.NativeBodyStatus != 0 {
		frames = append(frames, Frame{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{Gid: gid, StateType: wire.StateChannelBody, Value: peer.NativeBodyStatus}).Encode()})
	}
	if peer.World.MovingAt(nowMs) {
		source := MovementSourceFromSpawn(pose)
		goal := peer.World.Spawn
		frames = append(frames,
			Frame{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{Gid: gid, StateType: wire.StateChannelMove, Value: peer.World.MovementMode}).Encode()},
			Frame{Opcode: OpMovementAck, Payload: BuildMovementAckPayload(gid, MovementRequest{Mode: MovementAckDestinationMode, RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z}, &source)},
		)
	}
	t.Push.PushToSession(viewer.SessionID, frames)
	state.rememberPeerPath(viewer.SessionID, gid, peer.World)
	state.shownFor(viewer.SessionID)[gid] = true
	state.addPeerWatcher(gid, viewer.SessionID)
}

/*
================
Ticker.hidePeerFrom

Sends the viewer the peer's despawn (the 4-byte gid wire the pickup path
sends) and forgets the pair.
================
*/
func (t *Ticker) hidePeerFrom(state *divisionTickState, viewerID string, gid uint32) {
	t.Push.PushToSession(viewerID, []Frame{{
		ScopeGID: gid,
		Opcode:   wire.OpObjectDespawn,
		Payload:  (wire.ObjectDespawn{Gid: gid}).Encode(),
	}})
	delete(state.shownPeers[viewerID], gid)
	delete(state.peerPaths[viewerID], gid)
	state.removePeerWatcher(gid, viewerID)
}

/*
================
divisionTickState.dropDepartedViewers

Departed viewers must not leak show bookkeeping (the settled-map rule),
nor stay listed as anyone's viewer.
================
*/
func (state *divisionTickState) dropDepartedViewers(live map[string]bool) {
	for sessionID, shown := range state.shownPeers {
		if live[sessionID] {
			continue
		}
		for gid := range shown {
			state.removePeerWatcher(gid, sessionID)
		}
		delete(state.shownPeers, sessionID)
		delete(state.peerPaths, sessionID)
		delete(state.peerReferences, sessionID)
	}
}

/*
================
divisionTickState.shownFor
================
*/
func (state *divisionTickState) shownFor(viewerID string) map[uint32]bool {
	shown := state.shownPeers[viewerID]
	if shown == nil {
		shown = make(map[uint32]bool)
		state.shownPeers[viewerID] = shown
	}
	return shown
}

/*
================
divisionTickState.referencesFor
================
*/
func (state *divisionTickState) referencesFor(viewerID string) map[uint32]bool {
	if state.peerReferences == nil {
		state.peerReferences = make(map[string]map[uint32]bool)
	}
	sent := state.peerReferences[viewerID]
	if sent == nil {
		sent = make(map[uint32]bool)
		state.peerReferences[viewerID] = sent
	}
	return sent
}

/*
================
divisionTickState.addPeerWatcher
================
*/
func (state *divisionTickState) addPeerWatcher(gid uint32, viewerID string) {
	if state.peerWatchers == nil {
		state.peerWatchers = make(map[uint32]map[string]struct{})
	}
	viewers := state.peerWatchers[gid]
	if viewers == nil {
		viewers = make(map[string]struct{})
		state.peerWatchers[gid] = viewers
	}
	viewers[viewerID] = struct{}{}
}

/*
================
divisionTickState.removePeerWatcher
================
*/
func (state *divisionTickState) removePeerWatcher(gid uint32, viewerID string) {
	viewers := state.peerWatchers[gid]
	delete(viewers, viewerID)
	if len(viewers) == 0 {
		delete(state.peerWatchers, gid)
	}
}
