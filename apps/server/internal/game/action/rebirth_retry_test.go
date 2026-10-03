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

	"opensro.online/server/internal/domain"

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

/*
================
TestRebirthRetryAfterConcurrentParamJobExpiry

Unlike timed-skill checkpoints, param-job expiry does not take the action
maintenance/division lock. Exercise that production writer at the preparation
boundary rather than inventing an arbitrary character mutation.
================
*/
func TestRebirthRetryAfterConcurrentParamJobExpiry(t *testing.T) {
	character := rebirthTestCharacter(1, 0)
	rt, clock := newTestRuntime(character, testItems())
	character.ParamJobs = []domain.ParamJob{{
		ItemRefObjID: 7, Codename: "ITEM_ETC_INTERNAL_150EXP_SCROLL",
		Param: paramExpRate, Value: 150, EndUnixMs: clock.NowMs(),
	}}
	rt.paramJobOwners.track(testDivision, character.Name)
	deps := rt.deps
	rt.deps = failedRebirthPreparation{Dependencies: deps, mutate: func() {
		rt.advanceParamJobs(clock.NowMs())
	}}
	refused := rt.HandleLocalRebirth(testDivision, character, []byte{wire.RebirthAtSpecifiedPoint})
	if len(character.ParamJobs) != 0 {
		t.Fatal("the production expiry writer did not retire the scroll")
	}
	if refused.DiagnosticRefusal != "" || len(refused.Frames) != 0 || enterworld.CharacterAlive(character) {
		t.Fatalf("changed preparation was not silently refused: %+v", refused)
	}
	rt.deps = deps
	retry := rt.HandleLocalRebirth(testDivision, character, []byte{wire.RebirthAtSpecifiedPoint})
	if len(retry.Frames) == 0 || !enterworld.CharacterAlive(character) || len(character.ParamJobs) != 0 {
		t.Fatalf("same-session retry failed or resurrected the expired scroll: %+v", retry)
	}
}
