/*
===========================================================================

worldnpc.go - the NPCs that stand in one world instance, not the field

Two kinds of NPC stand outside the static roster, in one world only:
a quest instance's exit gate, raised at runtime (questgate.go), and the
static world NPCs the boot loads per world definition (the port-only
Muhan arena NPCs, simulation/muhan_npcs.go), which stand in every layer
of their definition. This module composes both for the ticker's NPC
scope pass (WorldNpcs) and resolves a static world NPC for the
interaction paths (worldNpcAt), so select and the NPC function request
answer it as any roster NPC.

A quest gate is resolved by its own owner (questGateSelect,
handleQuestGate): its travel rows are not a roster NPC's functions.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
WorldNpcs

Every NPC standing in this division's packed world instance beside the
roster: its gates, then the static world NPCs of its definition. The
ticker's scope pass publishes them to that world's sessions only.
================
*/
func (rt *Runtime) WorldNpcs(division string, world uint32) []simulation.NpcDef {
	npcs := rt.QuestGateNpcs(division, world)
	definition := instance.ID(world).Definition()
	for index := range rt.worldNpcs {
		if rt.worldNpcs[index].World == definition {
			npcs = append(npcs, rt.worldNpcs[index].Npc)
		}
	}
	return npcs
}

/*
================
worldNpcAt

The static world NPC with this object id when it stands in the
character's own world definition; nil otherwise. Visibility is the
caller's test, as for a roster NPC.
================
*/
func (rt *Runtime) worldNpcAt(character *enterworld.Character, gid uint32) *simulation.NpcDef {
	definition := instance.ID(domain.CharacterWorldInstance(character)).Definition()
	for index := range rt.worldNpcs {
		if rt.worldNpcs[index].Npc.ObjectID == gid && rt.worldNpcs[index].World == definition {
			return &rt.worldNpcs[index].Npc
		}
	}
	return nil
}
