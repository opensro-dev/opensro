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

import (
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// hwanOverrides holds the levels a GM set with /SETBERSERK, by character id. Process
// memory only, like /SETSPEED: the shard's restart forgets them and the quests'
// level is back.
type hwanOverrides struct {
	mu     sync.Mutex
	levels map[int64]uint8
}

func (o *hwanOverrides) get(id int64) (uint8, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	level, ok := o.levels[id]
	return level, ok
}

func (o *hwanOverrides) set(id int64, level uint8) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.levels == nil {
		o.levels = map[int64]uint8{}
	}
	o.levels[id] = level
}

/*
================
SetGMHwanLevel

The GM's /SETBERSERK: the character's Blue Berserk level is now level
(0 to domain.HwanLevelMax) whatever its quests say, until the shard restarts.
Port-only test tooling, not native. Reports whether the level was valid.
================
*/
func (rt *Runtime) SetGMHwanLevel(c *enterworld.Character, level uint8) bool {
	if rt == nil || c == nil || level > domain.HwanLevelMax {
		return false
	}
	rt.hwanGM.set(c.ID, level)
	return true
}

/*
================
HwanLevel

The character's Blue Berserk level, 0 to domain.HwanLevelMax: a GM's
/SETBERSERK level when one was set, else what the completed quests give.
================
*/
func (rt *Runtime) HwanLevel(c *enterworld.Character) uint8 {
	if rt == nil || rt.Defs == nil || c == nil {
		return 0
	}
	if level, set := rt.hwanGM.get(c.ID); set {
		return level
	}
	var level uint8
	for _, refID := range c.CompletedQuestIds {
		if def, ok := rt.Defs.ByRefID(refID); ok {
			level = max(level, def.RewardHwanLevel)
		}
	}
	return level
}
