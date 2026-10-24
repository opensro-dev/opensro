/*
===========================================================================

instance_events.go - reporting instanced-world lifecycle events

The quest lane registers per-definition handlers for instance events
(quest/instance_handlers.go); this lane owns the occurrences: a layer
opens, a character transfers in or returns, a tracked layer ticks, a
resident dies, a resident's party changes, a member answers the
invitation. instanceEvent is the one reporting door: it stamps the
character identity onto the event and QUEUES it; handlers are never
called from reporting code, because most occurrences are raised while
the division lock is held and the lock is not reentrant - a handler
that called back into this lane (InvitePartyToInstance above all)
deadlocked the division. Queued events are delivered, in order, when
the division lock is released (operation_locks.go) - so EVERY kind
reaches its handler with no division lock held, InvitationAnswered
included, and a handler may freely call back into the runtime. An
occurrence raised with no lock in hand (a bare OpenInstanceWorld, a
bare recordInstanceDeath) waits for the next lock release on any
division, which the tick guarantees - a tracked layer's own Update
release is one. A nil
sink - detached tests - reports nowhere. Feeds live with their
owners: setup/enter/leave/update in instance_world.go, the answered
event in instance_invite.go, death and party-change below.

Kind values mirror the quest registry's one-for-one and are
port-local in both; the two enums convert at the wiring seam.

===========================================================================
*/

package action

import (
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
InstanceEventKind

One instanced-world lifecycle event, mirroring quest.InstanceEventKind.
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
	// InstanceEventDead, port-local as in the quest registry.
	InstanceEventDead InstanceEventKind = 7
)

/*
================
InstanceEvent

One event occurrence, mirroring quest.InstanceEvent. Name/Gid name the
character the event is about (zero for setup and update); Accepted
carries the answer for InvitationAnswered only.
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
instanceEventQueue

Occurrences waiting for delivery. The mutex is the queue's own (never
the division lock's): reporting code appends under it while holding
any lock context, delivery runs holding none. flushing marks a
delivery pass in flight, so a re-entrant or concurrent flush (a
handler calling back into the runtime releases lanes too) leaves
its events to the pass already running, and order stays FIFO.
================
*/
type instanceEventQueue struct {
	mu       sync.Mutex
	pending  []InstanceEvent
	flushing bool
}

/*
================
instanceEvent

Reports one occurrence: stamps the character identity onto it and
queues it for delivery at the next division-lock release (the file
banner has the contract). character may be nil for the
instance-scoped kinds (setup, update).
================
*/
func (rt *Runtime) instanceEvent(kind InstanceEventKind, division string, definition instance.DefinitionID, world instance.ID, character *enterworld.Character, accepted bool) {
	if rt.InstanceEventSink == nil {
		return
	}
	event := InstanceEvent{Kind: kind, Division: division, Definition: definition, World: world, Accepted: accepted}
	if character != nil {
		event.Name = character.Name
		event.Gid = enterworld.ObjectIDForCharacter(character)
	}
	rt.instanceEvents.mu.Lock()
	rt.instanceEvents.pending = append(rt.instanceEvents.pending, event)
	rt.instanceEvents.mu.Unlock()
}

/*
================
flushInstanceEvents

Delivers every queued occurrence to the installed sink, in order.
Called from the division lane's release (operation_locks.go), so
handlers always run with no division lock held; a handler may queue
further events (by calling back into the runtime), and this same pass
delivers them after the current batch. A flush attempted while a pass
is already running is a no-op - the running pass sees the whole queue.
================
*/
func (rt *Runtime) flushInstanceEvents() {
	for {
		rt.instanceEvents.mu.Lock()
		if rt.instanceEvents.flushing || len(rt.instanceEvents.pending) == 0 {
			rt.instanceEvents.mu.Unlock()
			return
		}
		rt.instanceEvents.flushing = true
		batch := rt.instanceEvents.pending
		rt.instanceEvents.pending = nil
		sink := rt.InstanceEventSink
		rt.instanceEvents.mu.Unlock()

		rt.deliverInstanceEvents(sink, batch)
	}
}

/*
================
deliverInstanceEvents

Runs one batch to the sink. The deferred reset clears the in-flight
mark on every exit - a panicking handler included: the panic still
propagates to the lane release's caller, but the queue is never left
wedged behind it, so the next release resumes delivery (whatever
recovered the panic keeps receiving events). The undelivered tail of
a panicking batch is dropped; those occurrences' handler already
failed loudly.
================
*/
func (rt *Runtime) deliverInstanceEvents(sink func(InstanceEvent), batch []InstanceEvent) {
	defer func() {
		rt.instanceEvents.mu.Lock()
		rt.instanceEvents.flushing = false
		rt.instanceEvents.mu.Unlock()
	}()
	if sink == nil {
		return
	}
	for _, event := range batch {
		sink(event)
	}
}

/*
================
instanceDefinitionOf

The instance definition a character is currently inside, when their
transfer anchor says the instance lane put them there. A character in
a permanent world, or inside without an anchor (a GM warp), reports
false: the instance events belong to the lane's own transfers.
================
*/
func instanceDefinitionOf(c *enterworld.Character) (instance.DefinitionID, instance.ID, bool) {
	if c == nil || c.World == nil || c.World.InstanceReturn == nil {
		return 0, 0, false
	}
	world := instance.ID(domain.CharacterWorldInstance(c))
	return world.Definition(), world, true
}

/*
================
recordInstanceDeath

A resident died. The death event goes to the programme that owns the
instance the victim was transferred into; deaths anywhere else are not
instance events. Called from the fatal-hit publication (playervictim.go)
under the division lock, like the flag-war death hook beside it.
================
*/
func (rt *Runtime) recordInstanceDeath(division string, victim *enterworld.Character) {
	definition, world, inside := instanceDefinitionOf(victim)
	if !inside {
		return
	}
	rt.instanceEvent(InstanceEventDead, division, definition, world, victim, false)
}

/*
================
NotePartyChanged

A character's party membership changed (the party lane reports joins
and departures through the hook wiring installs). Only a resident's
change is an instance event; the programme decides what it means for
its party.
================
*/
func (rt *Runtime) NotePartyChanged(division, name string) {
	unlock := rt.lockDivision(division)
	defer unlock()
	character := rt.findCharacter(division, name)
	definition, world, inside := instanceDefinitionOf(character)
	if !inside {
		return
	}
	rt.instanceEvent(InstanceEventPartyChanged, division, definition, world, character, false)
}
