/*
===========================================================================

hunting_guide.go - immutable hunting atlas access through the population owner

Port-only, not native. This reads the template, never live monster state.

===========================================================================
*/
package simulation

import "opensro.online/server/internal/game/world/monster"

/*
================
HuntingGuide
================
*/
func (s *MonsterState) HuntingGuide() []monster.HuntingGuideEntry {
	if s == nil {
		return nil
	}
	return s.template.HuntingGuide()
}
