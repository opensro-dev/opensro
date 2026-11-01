/*
===========================================================================

observed_route_test.go - tests for OpResult.Around through the real hub

A heal's effect add is published around its recipient, not around
the caster. These drive hubHandler, the path Runtime.Register installs,
over a real transport session whose observed set is set by hand.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

const (
	// aroundRecipientGID and aroundStrangerGID are two objects other than
	// the actor; only their observers may receive what is routed around them.
	aroundRecipientGID = 0x00c0ffee
	aroundStrangerGID  = 0x00c0ffef
	// aroundFenceOpcode closes each read: everything the hub queued before
	// it has arrived.
	aroundFenceOpcode = wire.OpSkillCastResult
	// aroundSpawnOpcode and aroundDespawnOpcode carry the scope changes: a
	// batch without bytes applies nothing (sendSceneObjectBatch).
	aroundSpawnOpcode   = 0x3015
	aroundDespawnOpcode = 0x3016
)

/*
================
aroundPayloads

The first payload byte of every OpAttachedEffect frame the session receives
from one hubHandler call routing an add around each of two objects.
================
*/
func aroundPayloads(t *testing.T, h *publicationHarness, conn *publicationConn, session *transport.Session) []byte {
	t.Helper()
	handler := h.rt.hubHandler(h.server.Hub, func(string, *enterworld.Character, []byte) OpResult {
		return OpResult{Around: []ObservedFrames{
			{SourceGID: aroundRecipientGID, Frames: []wire.Frame{{Opcode: wire.OpAttachedEffect, Payload: []byte{1}}}},
			{SourceGID: aroundStrangerGID, Frames: []wire.Frame{{Opcode: wire.OpAttachedEffect, Payload: []byte{2}}}},
		}}
	})
	handler(session, wire.OpTargetInteract, nil)
	SendFrames(session, []wire.Frame{{Opcode: aroundFenceOpcode, Payload: []byte{1}}})
	var got []byte
	for _, f := range publicationThrough(t, conn, aroundFenceOpcode) {
		if f.Opcode == wire.OpAttachedEffect && len(f.Payload) == 1 {
			got = append(got, f.Payload[0])
		}
	}
	return got
}

/*
==================
TestAroundReachesOnlyTheRecipientsObservers

A session that observes the recipient receives the frame routed around it,
the actor's own session included; one that observes neither the recipient
nor the stranger (here: only its own caster) receives nothing routed around
them, which the caster's Broadcast route would have delivered.
==================
*/
func TestAroundReachesOnlyTheRecipientsObservers(t *testing.T) {
	h := newPublicationHarness(t, false, 50)
	conn, welcome := h.connect(t, nil)
	h.enter(t, conn)
	session, ok := h.server.Hub.Session(welcome.SessionID)
	if !ok {
		t.Fatal("session not found")
	}
	h.ready(t, conn)
	revision, _ := session.SceneRevision()

	if err := session.PublishSceneObjects(revision, []transport.ObjectScopeChange{{GID: aroundRecipientGID, Visible: true}}, []transport.Frame{{Opcode: aroundSpawnOpcode, Payload: []byte{0}}}); err != nil {
		t.Fatal(err)
	}
	if got := aroundPayloads(t, h, conn, session); len(got) != 1 || got[0] != 1 {
		t.Fatalf("an observer of the recipient received %v, want only the recipient's frame", got)
	}

	if err := session.PublishSceneObjects(revision, []transport.ObjectScopeChange{{GID: aroundRecipientGID, Visible: false}}, []transport.Frame{{Opcode: aroundDespawnOpcode, Payload: []byte{0}}}); err != nil {
		t.Fatal(err)
	}
	if got := aroundPayloads(t, h, conn, session); len(got) != 0 {
		t.Fatalf("a viewer of neither object received %v", got)
	}
}
