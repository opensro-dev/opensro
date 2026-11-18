/*
===========================================================================

runtime_characters.go - server-owned actors in the character authority

Port-only AI actors use the same locked combat doors as players. They are
indexed for targeting, but never written to character storage or selected
by a player account. Registration is boot-only, before gameplay starts.

===========================================================================
*/
package store

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
)

// maxRuntimeActors bounds one division's AI actors (the caravan roster is 40).
const maxRuntimeActors = 64

/*
================
RegisterRuntimeCharacters
================
*/
func (src storeCharacterSource) RegisterRuntimeCharacters(division string, actors []*domain.Character) error {
	s := src.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(division) == "" || len(actors) > maxRuntimeActors || len(s.runtimeCharacters[division]) != 0 {
		return fmt.Errorf("invalid or duplicate runtime actor roster for %q", division)
	}
	ids, names := make(map[int64]bool), make(map[string]bool)
	for _, c := range actors {
		if c == nil || !c.RuntimeOnly || c.ID <= 0 || c.ID > 0x7fffffff || c.AccountID != "" || c.Name == "" {
			return fmt.Errorf("invalid runtime actor")
		}
		name := characterLookupName(c.Name)
		index := s.characterLookup(division)
		if ids[c.ID] || names[name] || index.ids[c.ID] != nil || index.names[name] != nil {
			return fmt.Errorf("runtime actor identity collision: %s", c.Name)
		}
		ids[c.ID], names[name] = true, true
	}
	if s.runtimeCharacters == nil {
		s.runtimeCharacters = make(map[string][]*domain.Character)
	}
	s.runtimeCharacters[division] = append([]*domain.Character(nil), actors...)
	for _, c := range actors {
		s.charDivision[c] = division
	}
	s.rebuildCharacterLookupLocked(division)
	return nil
}

/*
================
LoadCaravanAgentMemories / SaveCaravanAgentMemory

Forward the memory port through the character source used by GameWorld.
================
*/
func (src storeCharacterSource) LoadCaravanAgentMemories(division string) ([]domain.CaravanAgentMemoryRow, error) {
	return src.s.LoadCaravanAgentMemories(division)
}

func (src storeCharacterSource) SaveCaravanAgentMemory(row domain.CaravanAgentMemoryRow) error {
	return src.s.SaveCaravanAgentMemory(row)
}

/*
================
LoadCaravanRouteHeat / SaveCaravanRouteHeat
================
*/
func (src storeCharacterSource) LoadCaravanRouteHeat(division string) ([]domain.CaravanRouteHeatRow, error) {
	return src.s.LoadCaravanRouteHeat(division)
}

func (src storeCharacterSource) SaveCaravanRouteHeat(row domain.CaravanRouteHeatRow) error {
	return src.s.SaveCaravanRouteHeat(row)
}
