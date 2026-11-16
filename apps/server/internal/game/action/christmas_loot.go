/*
===========================================================================

christmas_loot.go - the winter monsters' Red Santa Socks

Port-only, not native (SRO_CHRISTMAS_EVENT, default off). While the event
is on, a Snow Slave's kill leaves its Red Santa Socks and nothing else; a
winter unique's leaves its socks beside its ordinary loot
(progression/christmas_loot.go). The socks are appended after admission,
as the v1.188 server's winter event drops its token from its own kill
hook outside the loot tables, so a hunter of any level loots them; the
ordinary loot keeps the native level gap. Off, no branch runs:
byte-native loot.

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

/*
================
Runtime.christmasSockDrops

The Red Santa Socks a winter monster's kill leaves, one ground item each.
Appended after admission and the drop cap, as the Halloween box is, so
neither can take them.
================
*/
func (rt *Runtime) christmasSockDrops(defeated monster.Instance, at simulation.Spawn, owner string, now time.Time) []grounditem.Item {
	if !rt.Christmas.Enabled() {
		return nil
	}
	count := progression.ChristmasSocks(defeated.Ref.Codename)
	socks := make([]grounditem.Item, 0, count)
	for range count {
		if sock, ok := rt.prepareSelectedDrop(loot.DropItem{Codename: progression.ChristmasSockCodename, Count: 1, Assigned: true}, at, owner, now); ok {
			socks = append(socks, sock)
		}
	}
	return socks
}

/*
================
Runtime.christmasSocksOnly

A Snow Slave's whole loot: its socks, owned by the killer and scattered
as any drop is. False for every other monster, and with the event off.
================
*/
func (rt *Runtime) christmasSocksOnly(killer *enterworld.Character, defeated monster.Instance, at simulation.Spawn, now time.Time) ([]grounditem.Item, bool) {
	if !rt.Christmas.Enabled() || !progression.ChristmasSocksOnly(defeated.Ref.Codename) {
		return nil, false
	}
	ownerJID := enterworld.ObjectIDForCharacter(killer)
	socks := rt.christmasSockDrops(defeated, at, killer.Name, now)
	for i := range socks {
		socks[i].OwnerJID = ownerJID
		socks[i] = rt.scatterMonsterDrop(socks[i], at, defeated.Rarity())
	}
	return socks, true
}
