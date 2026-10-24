/*
===========================================================================

itemuse_loading_test.go - a refused item use is answered while loading

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestItemUseAnsweredWhileSceneLoading

The twin of the item-move case: a 0x75BD sent after the client retired
its loading screen but before the server processed its game-ready used
to be discarded unanswered, so the client's pending item use ran out its
full wait and forced a reconnect. The refusal must carry the native
item-use failure answer instead.
================
*/
func TestItemUseAnsweredWhileSceneLoading(t *testing.T) {
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

	// A potion-shaped request: slot 20, the fixture row's type word. The
	// loading gate refuses before the handler ever decodes it.
	client.send(t, wire.OpItemUseRequest, []byte{20, 0x6c, 0x0a})
	client.expectFrame(t, wire.OpItemUseResponse, []byte{2, wire.ErrCodeInvalidRequest})
}
