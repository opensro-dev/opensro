/*
===========================================================================

christmas_loot.go - a Snow Slave's snow flake

The v1.188 server's winter event drop hook (SRO_CHRISTMAS_EVENT in
mode winter): a
Snow Slave's death drops one Snow flake
(progression.ChristmasSnowflakeDropPercent: native half the time, here
always) and nothing else. The flake
comes from the quest's own hook outside the loot tables, so a hunter of
any level loots it. The White Knights and the Princesses keep their own
loot. Off, no branch runs: byte-native loot.

===========================================================================
*/
package action

import (
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// christmasDrawScale is DropRoll's span (0..32767).
const christmasDrawScale = 32768

/*
================
Runtime.christmasSnowflakeOnly

A Snow Slave's whole loot: its flake when the roll falls under the drop
chance, owned by the killer and scattered as any drop is. False for every
other monster, and outside the event.
================
*/
func (rt *Runtime) christmasSnowflakeOnly(killer *enterworld.Character, defeated monster.Instance, at simulation.Spawn, now time.Time) ([]grounditem.Item, bool) {
	if !rt.Christmas.ActiveAt(now) || defeated.Ref.Codename != progression.ChristmasSnowSlaveCodename {
		return nil, false
	}
	if rt.DropRoll == nil {
		return nil, true
	}
	draw, err := rt.DropRoll()
	if err != nil || int64(draw&(christmasDrawScale-1))*100 >= progression.ChristmasSnowflakeDropPercent*christmasDrawScale {
		return nil, true
	}
	flake, ok := rt.prepareSelectedDrop(loot.DropItem{Codename: progression.ChristmasSnowflakeCodename, Count: 1, Assigned: true}, at, killer.Name, now)
	if !ok {
		return nil, true
	}
	flake.OwnerJID = enterworld.ObjectIDForCharacter(killer)
	return []grounditem.Item{rt.scatterMonsterDrop(flake, at, defeated.Rarity())}, true
}
