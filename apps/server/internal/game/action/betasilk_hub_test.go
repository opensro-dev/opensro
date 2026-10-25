/*
===========================================================================

betasilk_hub_test.go - the action hub notes beta silk activity

SRO_BETA_SILK_ACTIVE_ONLY counts an account as playing when the hub admits
one of its commands (register.go hubHandler). These tests drive the real
hub, so removing the NoteActivity call fails them.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

/*
================
TestBetaSilkHubNotesOnlyAdmittedCommands

A command refused while the scene loads is not play; the same command
after game-ready is, and notes its account once.
================
*/
func TestBetaSilkHubNotesOnlyAdmittedCommands(t *testing.T) {
	h := newPublicationHarness(t, false, 50)
	beta := NewBetaSilk(&fakeSilkWallet{exists: true}, 50)
	beta.ActiveOnly = true
	h.rt.BetaSilkActivity = beta
	// The harness admits its sessions as this account.
	h.character.AccountID = "publication-test"
	h.server.Hub.Handle(wire.OpItemUseRequest, h.rt.hubHandler(h.server.Hub, func(string, *enterworld.Character, []byte) OpResult {
		return OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: []byte{2, 0}}}}
	}))
	conn, _ := h.connect(t, nil)
	h.enter(t, conn)
	send := func() {
		conn.in <- transport.Frame{Opcode: wire.OpItemUseRequest, Payload: h.request}
		conn.in <- transport.Frame{Opcode: publicationFence}
		publicationThrough(t, conn, publicationFence)
	}
	noted := func() int {
		beta.mu.Lock()
		defer beta.mu.Unlock()
		return len(beta.noted)
	}
	send()
	if n := noted(); n != 0 {
		t.Fatalf("a command refused while the scene loads noted %d accounts", n)
	}
	h.ready(t, conn)
	send()
	if n := noted(); n != 1 {
		t.Fatalf("an admitted command noted %d accounts, want 1", n)
	}
}

/*
================
TestBetaSilkHubIgnoresActivityWhenOff

With the flag off the hub's note is inert: nothing is recorded.
================
*/
func TestBetaSilkHubIgnoresActivityWhenOff(t *testing.T) {
	h := newPublicationHarness(t, false, 50)
	beta := NewBetaSilk(&fakeSilkWallet{exists: true}, 50)
	h.rt.BetaSilkActivity = beta
	h.server.Hub.Handle(wire.OpItemUseRequest, h.rt.hubHandler(h.server.Hub, func(string, *enterworld.Character, []byte) OpResult {
		return OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: []byte{2, 0}}}}
	}))
	conn, _ := h.connect(t, nil)
	h.enter(t, conn)
	h.ready(t, conn)
	conn.in <- transport.Frame{Opcode: wire.OpItemUseRequest, Payload: h.request}
	conn.in <- transport.Frame{Opcode: publicationFence}
	publicationThrough(t, conn, publicationFence)
	if beta.noted != nil {
		t.Fatal("the hub recorded activity with SRO_BETA_SILK_ACTIVE_ONLY off")
	}
}
