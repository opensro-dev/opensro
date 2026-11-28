/*
===========================================================================

pool.go - the v1.696 catalog's public projection

Builds the reference the browser's Magic POP window reads
(enterworld.GachaPoolReference): one machine's visible set in table
order, with each row's displayed plus level and blue options, and the
select window's category tree.

===========================================================================
*/

package gacha

import "opensro.online/server/internal/game/enterworld"

/*
================
PublicPool

Nil unless this is a v1.696 catalog. The first NPC the map serves owns
the published pool; the v1.696 map has one machine row (NPC 9251).
optionID resolves an M token's magic option row id by its tag and degree,
the same resolution the exchange writes into the option word (alchemy's
OptionByTag); a nil resolver publishes the name and value alone.
================
*/
func (c *Catalog) PublicPool(levelCap int64, optionID func(token SpecToken) (uint16, bool)) *enterworld.GachaPoolReference {
	if c == nil || !c.V1696 {
		return nil
	}
	npcs := c.Npcs()
	if len(npcs) == 0 {
		return nil
	}
	pool := &enterworld.GachaPoolReference{
		Npc:        npcs[0],
		VisibleSet: c.VisibleSetForNpc(npcs[0]),
		LevelCap:   levelCap,
		Entries:    []enterworld.GachaPoolEntry{},
		Tree:       []enterworld.GachaPoolBranch{},
	}
	for _, prize := range c.VisiblePrizes(npcs[0]) {
		entry := enterworld.GachaPoolEntry{
			Entry:       prize.EntryID,
			RefObjID:    prize.RewardRefObjID,
			Quantity:    prize.Quantity,
			MinLevelCap: prize.MinLevelCap,
			Options:     []enterworld.GachaPoolOption{},
		}
		if plus, ok := prize.Spec.Plus(); ok {
			entry.Plus = plus
		}
		for _, option := range prize.Spec.MagicOptions() {
			published := enterworld.GachaPoolOption{Name: option.Name, Value: option.Value}
			if optionID != nil {
				if id, ok := optionID(option); ok {
					published.ParamID = id
				}
			}
			entry.Options = append(entry.Options, published)
		}
		pool.Entries = append(pool.Entries, entry)
	}
	for _, node := range c.tree {
		pool.Tree = append(pool.Tree, enterworld.GachaPoolBranch{
			TypeID:     node.TypeID,
			Group:      node.Group,
			GroupLabel: node.GroupLabel,
			Sub:        node.Sub,
			SubLabel:   node.SubLabel,
			Degrees:    node.Degrees,
		})
	}
	return pool
}
