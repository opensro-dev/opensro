/*
===========================================================================

instance_handlers.go - quest handlers for instanced worlds

In the reference design a quest programme subscribes to the lifecycle
of an instanced world: a setup event as the world is created, then
enter, leave, update, party-change, invitation-answer and death
events for the instance it creates. This file is the port's table of
those subscriptions, keyed by instance definition: one handler per
definition, each event kind optional (subscription is per event, so
an unsubscribed kind is never called). The action lane reports the
events (instance_events.go there); quest programmes register here -
Rahid 6 is step 5.

Kind values are port-local: they name the events inside this process
only - nothing serialises them and no store keeps them. How each kind
lines up with the reference server's own event registry, including
the one subscription that is taken out dynamically and could not be
pinned statically, is recorded in the step-0 research checkpoint
(InstanceWorldFoundation.md).

Handlers run with NO division lock held: the action lane queues each
occurrence and delivers it when the producing operation's lane is
released, in occurrence order (instance_events.go there). A handler
may call back into the runtime - InvitePartyToInstance included;
before this contract, six of the seven kinds were delivered under
the non-reentrant division lock and only InvitationAnswered without
it, so the same call deadlocked from every other kind.

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

One instanced-world lifecycle event. Values are port-local; see the
file banner.
================
*/
type InstanceEventKind uint8

const (
	InstanceEventSetup              InstanceEventKind = 1
	InstanceEventEnter              InstanceEventKind = 2
	InstanceEventLeave              InstanceEventKind = 3
	InstanceEventUpdate             InstanceEventKind = 4
	InstanceEventPartyChanged       InstanceEventKind = 5
	InstanceEventInvitationAnswered InstanceEventKind = 6
	// The death kind rounds out the set; how its reference-server
	// counterpart subscribes is in the file banner.
	InstanceEventDead InstanceEventKind = 7
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
