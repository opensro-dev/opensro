/*
===========================================================================

instance_retirement_retry_test.go - retry an evacuation already requested

The retirement read changes cleanup retries, not the programme's deadline or
the moment evacuation begins. A non-flag-war instance exercises that shared
contract with an occupied layer whose first return cannot prepare its entry.

===========================================================================
*/

package action

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
TestInstanceWorldRetriesRequestedEvacuationBeforeItsDeadline
================
*/
func TestInstanceWorldRetriesRequestedEvacuationBeforeItsDeadline(t *testing.T) {
	for _, duration := range []time.Duration{0, time.Hour} {
		t.Run(fmt.Sprint(duration), func(t *testing.T) {
			c := instanceWorldTestCharacter(11, "Returning")
			mate := instanceWorldTestCharacter(12, "Waiting")
			rt, clock := newInstanceWorldRuntime(t, c, mate)
			admitInstanceWorldCharacter(t, rt, c, 1, instanceWorldTestStart)
			admitInstanceWorldCharacter(t, rt, mate, 2, instanceWorldTestStart)
			deadline := int64(0)
			if duration != 0 {
				deadline = clock.NowMs() + duration.Milliseconds()
			}
			lease := openInstanceWorldOrFail(t, rt, instanceWorldTestPartyDefinition, deadline)
			if status := rt.TransferCharacterToInstance(testDivision, c, lease, instanceWorldTestEntry); status != instance.Success {
				t.Fatal("transfer", status)
			}
			if status := rt.TransferCharacterToInstance(testDivision, mate, lease, instanceWorldTestEntry); status != instance.Success {
				t.Fatal("transfer second resident", status)
			}
			before := c.Snapshot()
			rt.tickInstanceWorlds(clock.NowMs())
			if !reflect.DeepEqual(c.Snapshot(), before) || rt.instanceWorldRetiring(testDivision, lease) {
				t.Fatal("the retirement read started an evacuation before its deadline")
			}
			deps := rt.deps
			rt.deps = portalRejectEntry{Dependencies: deps}
			rt.CloseInstanceWorld(testDivision, lease)
			if !reflect.DeepEqual(c.Snapshot(), before) || !rt.instanceWorldRetiring(testDivision, lease) ||
				len(rt.Monsters.PendingPopulationRetirements(testDivision)) != 0 {
				t.Fatalf("failed occupied evacuation: character retained=%v retiring=%v pending release requests=%d",
					reflect.DeepEqual(c.Snapshot(), before), rt.instanceWorldRetiring(testDivision, lease),
					len(rt.Monsters.PendingPopulationRetirements(testDivision)))
			}
			if admitted, ok := rt.EntryPopulationLease(testDivision, c.Name); !ok || admitted != lease {
				t.Fatal("failed evacuation lost its population admission")
			}
			rt.deps = deps
			clock.Advance(time.Millisecond)
			rt.tickInstanceWorlds(clock.NowMs())
			if domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance || c.World.InstanceReturn != nil {
				t.Fatal("an already-requested evacuation did not retry before the future deadline")
			}
			if *c.CurrentHP != *before.CurrentHP || *c.CurrentMP != *before.CurrentMP || c.NativeBodyStatus != before.NativeBodyStatus {
				t.Fatal("ordinary evacuation changed the living participant's vitals or body status")
			}
			if _, open := rt.Monsters.PopulationLease(testDivision, lease.ID); open {
				t.Fatal("successful retry left the evacuated lifetime allocated")
			}
		})
	}
}
