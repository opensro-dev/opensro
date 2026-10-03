/*
===========================================================================

rebirth_retry_test.go - safe retries after a silent resurrection refusal

The client cannot wait for a rebirth acknowledgement that the protocol does
not send. Repeated explicit choices must preserve the server's single revival.

===========================================================================
*/

package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestRebirthRetryAfterSilentRefusal

Exercise the actual preparation failure and snapshot-conflict branches before
retrying with the same actor and runtime, without clearing state or logging in.
================
*/
func TestRebirthRetryAfterSilentRefusal(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		name := "preparation failure"
		if conflict {
			name = "concurrent character update"
		}
		t.Run(name, func(t *testing.T) {
			character := rebirthTestCharacter(1, 0)
			rt, _ := newTestRuntime(character, testItems())
			deps := rt.deps
			failure := failedRebirthPreparation{Dependencies: deps}
			if conflict {
				failure.mutate = func() { character.Gold = testInt64(12345) }
			}
			rt.deps = failure
			refused := rt.HandleLocalRebirth(testDivision, character, []byte{wire.RebirthAtSpecifiedPoint})
			if len(refused.Frames) != 0 || enterworld.CharacterAlive(character) {
				t.Fatalf("preparation refusal changed life or answered: %+v", refused)
			}
			rt.deps = deps
			retry := rt.HandleLocalRebirth(testDivision, character, []byte{wire.RebirthAtSpecifiedPoint})
			if len(retry.Frames) == 0 || !enterworld.CharacterAlive(character) {
				t.Fatalf("same-session retry failed: %+v", retry)
			}
			if conflict && *character.Gold != 12345 {
				t.Fatal("retry lost the concurrent character update")
			}
		})
	}
}

/*
================
TestRepeatedRebirthChoicesDoNotCommitTwice

Both native choices share the life gate. Delayed or repeated clicks cannot
refresh vitals, relocate a living character or advance its life revision.
================
*/
func TestRepeatedRebirthChoicesDoNotCommitTwice(t *testing.T) {
	for _, choice := range []byte{wire.RebirthAtSpecifiedPoint, wire.RebirthAtPresentPoint} {
		character := rebirthTestCharacter(1, 0)
		rt, _ := newTestRuntime(character, testItems())
		first := rt.HandleLocalRebirth(testDivision, character, []byte{choice})
		if len(first.Frames) == 0 || !enterworld.CharacterAlive(character) {
			t.Fatalf("first choice %d failed: %+v", choice, first)
		}
		before := character.Snapshot()
		key := simulation.WorldKey(testDivision, character.Name)
		seed := func() simulation.WorldState { return simulation.SeedWorldState(character) }
		world := rt.Worlds.Snapshot(key, seed)
		for _, retryChoice := range []byte{wire.RebirthAtSpecifiedPoint, wire.RebirthAtPresentPoint} {
			retry := rt.HandleLocalRebirth(testDivision, character, []byte{retryChoice})
			if len(retry.Frames) != 0 || len(retry.Broadcast) != 0 ||
				!reflect.DeepEqual(character.Snapshot(), before) ||
				!reflect.DeepEqual(rt.Worlds.Snapshot(key, seed), world) {
				t.Fatalf("duplicate choice %d after %d changed the living actor: %+v", retryChoice, choice, retry)
			}
		}
	}
}
