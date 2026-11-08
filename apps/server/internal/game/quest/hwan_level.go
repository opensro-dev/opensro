/*
===========================================================================

hwan_level.go - a character's Blue Berserk level

Port-only, not native. The shard stores the level in the character row
(_Char.HwanLevel), and only the Blue Berserk quests' reward rows raise it
(_RefQuestReward.Hwan: _1_6 gives 1, _2_6 gives 2, QNO_SD_HW_018/022 give 3,
_020/024 give 4). Here the level is derived: the highest level any quest the
character has completed grants. That is the same number without a second copy
of it to keep in step, and a character record that needs no schema step.

===========================================================================
*/
package quest

import "opensro.online/server/internal/game/enterworld"

/*
================
HwanLevel

The character's Blue Berserk level, 0 to domain.HwanLevelMax.
================
*/
func (rt *Runtime) HwanLevel(c *enterworld.Character) uint8 {
	if rt == nil || rt.Defs == nil || c == nil {
		return 0
	}
	var level uint8
	for _, refID := range c.CompletedQuestIds {
		if def, ok := rt.Defs.ByRefID(refID); ok {
			level = max(level, def.RewardHwanLevel)
		}
	}
	return level
}
