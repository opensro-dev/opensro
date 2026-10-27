package action

import (
	"math"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/statuseffect"
)

// Derive at the transaction boundary from the effect owner. Merely learning
// or carrying a premium skill must not activate its Alchemy bonus, and stopping
// its active effect must not leave a cached bonus behind.
func (rt *Runtime) alchemyBonuses(division string, character *enterworld.Character) (int, int) {
	var reinforce, stone uint64
	if skills := rt.deps.SkillData(); skills != nil {
		for _, effect := range rt.effects.Snapshot(division, character.Name) {
			if effect.State != statuseffect.StateActive || effect.StopRequested {
				continue
			}
			if row, ok := skills.SkillByID(effect.SkillID); ok {
				reinforce += uint64(row.AlchemyReinforceBonus)
				stone += uint64(row.AlchemyStoneBonus)
			}
		}
	}
	// Natively equipped luca magic values add to AD. LUCK (charge protection)
	// is a different tag and is consumed by the reinforcement rule itself.
	if character.AvatarInventory != nil {
		for _, row := range character.AvatarInventory.Rows {
			if row.Slot < 0 || row.Slot > 3 {
				continue
			}
			for _, value := range row.MagicOptions {
				if m, ok := rt.Alchemy.Magic[uint16(value)]; ok && m.Tag == alchemy.MagicTagAlchemyBonus {
					reinforce += uint64(uint32(value >> 32))
				}
			}
		}
	}
	return int(min(reinforce, uint64(math.MaxInt32))), int(min(stone, uint64(math.MaxInt32)))
}
