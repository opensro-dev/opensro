/*
===========================================================================

tick_deliver_test.go - captured sends keep their slot in a hook's batches

===========================================================================
*/
package simulation

import (
	"slices"
	"testing"
	"time"
)

/*
================
TestHookDeliverRunsInItsBatchOrder

A batch with Deliver is the producer's captured send: the ticker runs it
in place, between the batches around it, and routes nothing for it.
================
*/
func TestHookDeliverRunsInItsBatchOrder(t *testing.T) {
	var order []string
	hook := func(int64) []DivisionFrames {
		return []DivisionFrames{
			{DivisionID: "d", Deliver: func() { order = append(order, "first") }},
			{DivisionID: "d", Deliver: func() { order = append(order, "second") }},
		}
	}
	ticker, _ := timedTicker(&fakeTickClock{now: time.UnixMilli(1000)}, NamedTickHook{Hook: hook})
	ticker.RunTick(0)
	if !slices.Equal(order, []string{"first", "second"}) {
		t.Fatalf("delivery order = %v", order)
	}
}
