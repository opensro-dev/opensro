/*
===========================================================================

itemmove_loading_test.go - a refused item move is answered while loading

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestItemMoveAnsweredWhileSceneLoading

The client retires its loading screen on its own game-ready send; until
the server processes it the scene is still loading there, but the client
already accepts commands. A 0x706D sent in that window used to be
discarded unanswered, so the client's pending move ran out its full wait
and forced a reconnect. The refusal must carry the native item-move
failure answer instead.
================
*/
func TestItemMoveAnsweredWhileSceneLoading(t *testing.T) {
	t.Parallel()
	character := wireCharacter()
	rt, _ := newTestRuntime(character, testItems())
	srv := wireStartServer(t, rt)

	client := wireConnect(t, srv, testDivision, character.Name)
	session, ok := srv.Hub.Session(client.sessionID)
	if !ok {
		t.Fatalf("session %d not registered in hub", client.sessionID)
	}
	session.BeginSceneAdmission()

	client.send(t, wire.OpItemMoveRequest, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeCosPickup,
		CosGID:       4101,
		GroundGID:    4102,
	}))
	client.expectFrame(t, wire.OpItemMoveResponse, []byte{2, wire.ErrCodeInvalidRequest})
}
