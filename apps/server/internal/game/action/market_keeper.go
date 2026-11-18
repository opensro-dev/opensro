/*
===========================================================================

market_keeper.go - the storage keeper an in-game market write needs
(opensro#3529)

Port-only, not native (SRO_CONSIGNMENT). The window opens from a
storage keeper's talk menu, but that menu is the client's; the server
must not take a market write from anywhere a modified client sends one
(a dungeon, a fight), or the market becomes a remote bag delivery and a
remote listing desk. So every write needs a live storage keeper within
the NPC hit range, as a warehouse request does (storage.go).

The keeper is remembered from the window's opening reads, made while
it is selected: the player may click something else with the window open
and still use it, as the warehouse room stays usable after the selection
moves (openStorageNpc). Each write checks the remembered keeper is still
live and in reach, so walking away closes the market to writes.

INFERENCE: a storage ticket (remoteStorageOpen) does not open the
market; the market is a keeper's service, and the website already serves
everything a remote player may do without a bag.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

// marketRefusedKeeper is a write with no storage keeper in reach.
const marketRefusedKeeper = "keeper"

/*
================
marketKeeperInReach

Whether gid is a live storage keeper this character sees, within the
NPC hit range.
================
*/
func (rt *Runtime) marketKeeperInReach(division string, c *enterworld.Character, gid uint32) bool {
	npc, live := rt.npcForCurrentViewer(division, c, gid)
	return live && npc.TalkFlags&simulation.NpcTalkFlagStorage != 0 && rt.npcWithinHitRange(division, c, npc)
}

/*
================
marketAtKeeper

Whether c may write to the market now: the keeper it has selected, or
else the one it opened the window at, is in reach. A selected keeper in
reach becomes the remembered one.
================
*/
func (rt *Runtime) marketAtKeeper(division string, c *enterworld.Character) bool {
	key := simulation.WorldKey(division, c.Name)
	if gid, ok := rt.Selected.Get(division, c.Name); ok && rt.marketKeeperInReach(division, c, gid) {
		rt.marketKeepers.Store(key, gid)
		return true
	}
	remembered, ok := rt.marketKeepers.Load(key)
	return ok && rt.marketKeeperInReach(division, c, remembered.(uint32))
}

/*
================
marketWrites

Whether op changes the market (every op but the two reads).
================
*/
func marketWrites(op string) bool {
	return op != marketOpSearch && op != marketOpMine
}
