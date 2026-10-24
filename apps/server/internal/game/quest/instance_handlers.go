/*
===========================================================================

instance_handlers.go - quest handlers for instanced worlds

The reference server registers quest programmes on instance lifecycle
events (CEventHandlers_Register(scope, eventId, handler, receiver)): a
definition's setup runs on the world event, and enter/leave/update/
party/invitation handlers hang off the instance it creates. This file
is the port's table of those registrations, keyed by instance
definition: one handler per definition, each event kind optional (the
native registration is per event, so an unregistered kind is never
called). The action lane reports the events (instance_events.go
there); quest programmes register here - Rahid 6 is step 5.

Kind values are the native event ids step 0 pinned (setup 0x11,
enter 3, leave 4, update 0x13, party-changed 0x16, answered 0x18).
The death event's id is registered dynamically natively and unpinned,
so Dead carries a port-local value with no wire meaning.

Handlers run on the reporting lane's goroutine, most of them under
that lane's division lock: a handler records or queues, it never calls
back into the lane that reported the event.

===========================================================================
*/

package quest

import (
	"sync"

	"opensro.online/server/internal/game/world/instance"
)

/*
================
InstanceEventKind

One instanced-world lifecycle event. Values are the native event ids
where step 0 pinned them; see the file banner for the death value.
================
*/
type InstanceEventKind uint8

const (
	InstanceEventSetup              InstanceEventKind = 0x11
	InstanceEventEnter              InstanceEventKind = 3
	InstanceEventLeave              InstanceEventKind = 4
	InstanceEventUpdate             InstanceEventKind = 0x13
	InstanceEventPartyChanged       InstanceEventKind = 0x16
	InstanceEventInvitationAnswered InstanceEventKind = 0x18
	// InstanceEventDead is port-local: the native death handler is
	// registered dynamically, so its event id is unpinned (step 0).
	InstanceEventDead InstanceEventKind = 0xFF
)

/*
================
InstanceEvent

One event occurrence. Name/Gid name the character the event is about
(empty for setup and update, which belong to the instance itself);
Accepted carries the answer for InvitationAnswered only.
================
*/
type InstanceEvent struct {
	Kind       InstanceEventKind
	Division   string
	Definition instance.DefinitionID
	World      instance.ID
	Name       string
	Gid        uint32
	Accepted   bool
}

/*
================
InstanceHandler

The handlers one definition's programme registered. A nil field is an
unregistered event and is never called.
================
*/
type InstanceHandler struct {
	Setup              func(InstanceEvent)
	Enter              func(InstanceEvent)
	Leave              func(InstanceEvent)
	Dead               func(InstanceEvent)
	PartyChanged       func(InstanceEvent)
	Update             func(InstanceEvent)
	InvitationAnswered func(InstanceEvent)
}

/*
================
instanceHandlerTable

The registrations, by definition. Registration happens at wiring,
before any session enters the world; the mutex only keeps a late or
concurrent registration from racing a dispatch.
================
*/
type instanceHandlerTable struct {
	mu       sync.Mutex
	handlers map[instance.DefinitionID]InstanceHandler
}

/*
================
RegisterInstanceHandler

Registers (or replaces) the handler for one instance definition.
================
*/
func (r *Runtime) RegisterInstanceHandler(definition instance.DefinitionID, handler InstanceHandler) {
	r.instanceHandlers.mu.Lock()
	defer r.instanceHandlers.mu.Unlock()
	if r.instanceHandlers.handlers == nil {
		r.instanceHandlers.handlers = make(map[instance.DefinitionID]InstanceHandler)
	}
	r.instanceHandlers.handlers[definition] = handler
}

/*
================
DispatchInstanceEvent

Delivers one event to the handler registered for its definition. An
unregistered definition, or a kind the handler left nil, is a no-op:
the native registry likewise calls only what a programme registered.
================
*/
func (r *Runtime) DispatchInstanceEvent(event InstanceEvent) {
	r.instanceHandlers.mu.Lock()
	handler, ok := r.instanceHandlers.handlers[event.Definition]
	r.instanceHandlers.mu.Unlock()
	if !ok {
		return
	}
	var fn func(InstanceEvent)
	switch event.Kind {
	case InstanceEventSetup:
		fn = handler.Setup
	case InstanceEventEnter:
		fn = handler.Enter
	case InstanceEventLeave:
		fn = handler.Leave
	case InstanceEventDead:
		fn = handler.Dead
	case InstanceEventPartyChanged:
		fn = handler.PartyChanged
	case InstanceEventUpdate:
		fn = handler.Update
	case InstanceEventInvitationAnswered:
		fn = handler.InvitationAnswered
	}
	if fn != nil {
		fn(event)
	}
}
