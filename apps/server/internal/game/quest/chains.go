/*
===========================================================================

chains.go - shared quest prerequisite admission and graph validation

Completed predecessors and currently active companions are distinct native
conditions. NPC offers, map markers and acceptance use the same predicate.

===========================================================================
*/
package quest

import (
	"fmt"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
prerequisitesMet

An active requirement cannot be satisfied by an already completed quest.
================
*/
func prerequisitesMet(c *enterworld.Character, def *Definition) bool {
	if def.AcceptanceUnavailable != "" {
		return false
	}
	for _, id := range def.RequiredQuestIDs {
		if !questCompleted(c, id) {
			return false
		}
	}
	for _, id := range def.RequiredActiveQuestIDs {
		if activeQuestIndex(c, id) < 0 {
			return false
		}
	}
	return true
}

/*
================
validateQuestChains

Check both dependency families at load time, never recursively on NPC clicks.
================
*/
func validateQuestChains(defs *Definitions) error {
	visited := map[uint32]uint8{}
	var visit func(*Definition) error
	visit = func(def *Definition) error {
		if visited[def.RefID] == 1 {
			return fmt.Errorf("cyclic quest dependency at %s", def.Codename)
		}
		if visited[def.RefID] == 2 {
			return nil
		}
		visited[def.RefID] = 1
		parents := append([]uint32(nil), def.RequiredQuestIDs...)
		parents = append(parents, def.RequiredActiveQuestIDs...)
		for _, id := range parents {
			parent, ok := defs.ByRefID(id)
			if !ok {
				if defs.externalPrerequisites[id] {
					continue
				}
				return fmt.Errorf("missing predecessor %d", id)
			}
			if err := visit(parent); err != nil {
				return err
			}
		}
		visited[def.RefID] = 2
		return nil
	}
	for _, def := range defs.All() {
		if err := visit(def); err != nil {
			return err
		}
	}
	return nil
}
