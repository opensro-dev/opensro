/*
===========================================================================

moveinteraction_test.go - which interactions pin a ground move (U8-16)

The original's move handler ignores a player's command while its
interaction state is a stall (keeping one or visiting one) or an NPC's
function window, but it takes the walk while an exchange is open and
leaves the visit standing. The movement lane is wired to
PlayerMovementInteractionLocked; the direct skill admission keeps the
wider gate (interactionlock_test.go).

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/movement"
)

/*
================
TestMovementInteractionPinsStallAndWindowNotExchange
================
*/
func TestMovementInteractionPinsStallAndWindowNotExchange(t *testing.T) {
	c := rebirthTestCharacter(20, 100)
	rt, clock := newTestRuntime(c, testItems())
	moves := movement.NewRuntime(rt.deps.(*enterworld.Deps), rt.Worlds)
	moves.Now = func() time.Time { return time.UnixMilli(clock.NowMs()) }
	moves.InteractionLocked = rt.PlayerMovementInteractionLocked
	// Angular movement needs no terrain fixture and still consumes the same gate.
	request := []byte{0, 0, 1, 0}
	admitted := func(step string) {
		t.Helper()
		if out := moves.HandleMove(testDivision, c, request); out.Refusal != nil {
			t.Fatalf("%s: move refused: %+v", step, out.Refusal)
		}
	}
	refused := func(step string) {
		t.Helper()
		out := moves.HandleMove(testDivision, c, request)
		if out.Refusal == nil || out.Refusal.Reason != "interactionLocked" || len(out.Frames) != 0 {
			t.Fatalf("%s: move = %+v, want a silent interactionLocked refusal", step, out)
		}
	}

	admitted("a free player")

	// An open exchange walks.
	rt.Exchanges.Open(testDivision, c.Name, "Partner")
	admitted("in an exchange")
	rt.Exchanges.Close(testDivision, c.Name)

	// Keeping a stall pins.
	if err := rt.Stalls.Open(testDivision, c.Name, "stall", 0); err != nil {
		t.Fatal(err)
	}
	refused("keeping a stall")
	rt.Stalls.Close(testDivision, c.Name)
	admitted("after the stall closed")

	// Visiting a stall pins, and the refused walk does not end the visit.
	if err := rt.Stalls.Open(testDivision, "Host", "stall", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Stalls.Enter(testDivision, c.Name, "Host"); err != nil {
		t.Fatal(err)
	}
	refused("visiting a stall")
	if _, visiting := rt.Stalls.Visiting(testDivision, c.Name); !visiting {
		t.Fatal("the refused walk ended the visit")
	}
	if _, err := rt.Stalls.Leave(testDivision, c.Name); err != nil {
		t.Fatal(err)
	}
	rt.Stalls.Close(testDivision, "Host")
	admitted("after leaving the stall")

	// An NPC's open function window pins until it closes.
	rt.Selected.Set(testDivision, c.Name, 17)
	rt.Selected.OpenFunction(testDivision, c.Name, 17)
	refused("with an NPC window open")
	rt.Selected.Clear(testDivision, c.Name)
	admitted("after the window closed")
}
