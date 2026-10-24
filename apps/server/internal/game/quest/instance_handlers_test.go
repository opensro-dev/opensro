/*
===========================================================================

instance_handlers_test.go - the instance handler registry's dispatch

===========================================================================
*/

package quest

import (
	"testing"

	"opensro.online/server/internal/game/world/instance"
)

/*
================
TestInstanceHandlerDispatchReplaysEveryKind
================
*/
func TestInstanceHandlerDispatchReplaysEveryKind(t *testing.T) {
	rt := &Runtime{}
	var got []InstanceEvent
	record := func(event InstanceEvent) { got = append(got, event) }
	rt.RegisterInstanceHandler(instance.DefinitionID(10), InstanceHandler{
		Setup: record, Enter: record, Leave: record, Dead: record,
		PartyChanged: record, Update: record, InvitationAnswered: record,
	})
	kinds := []InstanceEventKind{
		InstanceEventSetup, InstanceEventEnter, InstanceEventLeave, InstanceEventDead,
		InstanceEventPartyChanged, InstanceEventUpdate, InstanceEventInvitationAnswered,
	}
	for _, kind := range kinds {
		rt.DispatchInstanceEvent(InstanceEvent{
			Kind: kind, Division: "global-official", Definition: instance.DefinitionID(10),
			World: instance.Pack(instance.DefinitionID(10), 1), Name: "Member", Gid: 100002, Accepted: kind == InstanceEventInvitationAnswered,
		})
	}
	if len(got) != len(kinds) {
		t.Fatalf("dispatched %d events, want %d", len(got), len(kinds))
	}
	for i, kind := range kinds {
		if got[i].Kind != kind || got[i].Name != "Member" || got[i].Gid != 100002 {
			t.Fatalf("event %d = %+v, want kind %d for Member", i, got[i], kind)
		}
	}
	if !got[len(got)-1].Accepted {
		t.Fatal("the answered event lost its answer")
	}
}

/*
================
TestInstanceHandlerDispatchSkipsWhatIsNotRegistered
================
*/
func TestInstanceHandlerDispatchSkipsWhatIsNotRegistered(t *testing.T) {
	rt := &Runtime{}
	entered := 0
	rt.RegisterInstanceHandler(instance.DefinitionID(10), InstanceHandler{
		Enter: func(InstanceEvent) { entered++ },
	})
	// Other kinds on a partially registered handler, and every kind on
	// an unregistered definition, are no-ops (and must not panic).
	for _, kind := range []InstanceEventKind{InstanceEventSetup, InstanceEventLeave, InstanceEventUpdate} {
		rt.DispatchInstanceEvent(InstanceEvent{Kind: kind, Definition: instance.DefinitionID(10)})
	}
	rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventEnter, Definition: instance.DefinitionID(19)})
	if entered != 0 {
		t.Fatalf("enter fired %d times for unregistered kinds/definitions", entered)
	}
	rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventEnter, Definition: instance.DefinitionID(10)})
	if entered != 1 {
		t.Fatalf("enter fired %d times, want 1", entered)
	}
}
