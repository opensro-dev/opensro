/*
===========================================================================

fortress_revival.go - where a PC revives inside a fortress during its war

CGObjPC_ResolveTownRecallPosition (4E08A0) asks the PC's world while a
fortress war runs and the world is a siege world (slot 31,
CGameWorld_Siege_IsSiege 600C50). CGameWorld_Siege_ResolveRevivalPosition
(601700, siege vtable +0x90) then answers by the PC's side:

  - no guild: one of the fortress's gates in the field (601C90)
  - the holder's guild: a revival gate of the fortress world (TID4 2)
  - an ally of the holder: a fortress gate inside the world (TID4 1)
  - an attacker: its guild's camp (CSiegeFortress_ResolveAttackerStonePos
    61FAF0, a headquarters it placed), else a field gate

Each kind picks one matching teleport at random (51D260, rand() % count).

INFERENCE: 51D260 joins a field gate to its fortress through the
building's fortress codename, which the v1.150 teleportbuilding rows leave
empty for STORE_CH_FORT_GATE1..3; this server joins them through the
teleport link that leads from the gate into the fortress world. Without
guild unions no guild is an ally, and no camps are placed yet, so an
attacker revives at a field gate.

===========================================================================
*/
package action

import (
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/instance"
)

const (
	// The TID4 of a 4/1/1 teleport building that 51D260 selects by.
	gateKindFortressGate uint8 = 1
	gateKindRevival      uint8 = 2
)

/*
================
fortressRevival

The 601700 answer for c, or false when c revives at its appointed town.
================
*/
func (rt *Runtime) fortressRevival(division string, c *enterworld.Character) (travelPoint, bool) {
	if rt.Fortresses == nil || rt.portals == nil || !rt.Fortresses.WarActive(division) {
		return travelPoint{}, false
	}
	definition, ok := instance.Lookup(instance.ID(domain.CharacterWorldInstance(c)).Definition())
	if !ok || !definition.Siege() {
		return travelPoint{}, false
	}
	fortressID, ok := rt.Fortresses.ForWorld(definition)
	if !ok {
		return travelPoint{}, false
	}
	record, _ := rt.Fortresses.Get(division, fortressID)
	if c.GuildID != nil && *c.GuildID != 0 && *c.GuildID == record.Holder() {
		if point, ok := rt.randomGate(rt.worldGates(definition.ID, gateKindRevival)); ok {
			return point, true
		}
	}
	point, ok := rt.randomGate(rt.fieldGates(definition.ID))
	return point, ok
}

/*
================
worldGates

The teleports of one world whose building is a 4/1/1 gate of kind.
================
*/
func (rt *Runtime) worldGates(world instance.DefinitionID, kind uint8) []portalDestination {
	var out []portalDestination
	for _, d := range rt.portals.destinations {
		if d.world == world && d.gateKind == kind {
			out = append(out, d)
		}
	}
	return out
}

/*
================
fieldGates

The fortress gates outside the world whose links lead into it.
================
*/
func (rt *Runtime) fieldGates(world instance.DefinitionID) []portalDestination {
	var out []portalDestination
	for key := range rt.portals.links {
		source, target := rt.portals.destinations[key[0]], rt.portals.destinations[key[1]]
		if source.gateKind == gateKindFortressGate && source.world != world && target.world == world {
			out = append(out, source)
		}
	}
	return out
}

/*
================
randomGate

51D260's rand() % count over the gates, in teleport order.
================
*/
func (rt *Runtime) randomGate(gates []portalDestination) (travelPoint, bool) {
	if len(gates) == 0 {
		return travelPoint{}, false
	}
	sort.Slice(gates, func(i, j int) bool { return gates[i].id < gates[j].id })
	unique := gates[:1]
	for _, gate := range gates[1:] {
		if gate.id != unique[len(unique)-1].id {
			unique = append(unique, gate)
		}
	}
	roll := rt.RevivalRoll
	if roll == nil {
		roll = combat.SecureRoll32767
	}
	value, err := roll()
	if err != nil {
		return travelPoint{}, false
	}
	gate := unique[int(value)%len(unique)]
	return travelPoint{spawn: gate.spawn, world: portalWorld(gate)}, true
}
