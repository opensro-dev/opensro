/*
===========================================================================

instance_events.go - reporting instanced-world lifecycle events

The quest lane registers per-definition handlers for instance events
(quest/instance_handlers.go); this lane owns the occurrences: a layer
opens, a character transfers in or returns, a tracked layer ticks, a
resident dies, a resident's party changes, a member answers the
invitation. instanceEvent is the one reporting door: it stamps the
character identity onto the event and hands it to the installed sink
(wiring points it at the quest registry; a nil sink - detached tests -
reports nowhere). Feeds live with their owners: setup/enter/leave/
update in instance_world.go, the answered event in instance_invite.go,
death and party-change below.

Kind values mirror the quest registry's one-for-one and are
port-local in both; the two enums convert at the wiring seam.

===========================================================================
*/

package action

import (
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
	// InstanceEventActivated follows Enter for the same character:
	// the entered character finished activating in the instance, the
	// post-entry checkpoint a programme validates entry conditions
	// at (transferCharacterToInstance raises it).
	InstanceEventActivated InstanceEventKind = 8
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
instanceEvent

Reports one occurrence to the installed sink. character may be nil for
the instance-scoped kinds (setup, update).
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
	rt.InstanceEventSink(event)
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
