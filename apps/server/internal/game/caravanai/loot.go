/*
===========================================================================

loot.go - agents pick up what lies near them (port-only)

The world shows an agent the nearest ground item it may take (a Trader its
own dropped goods, a Thief other caravans' goods, gold and items); the
agent walks over on a quiet step and picks it up through the native grant
(WorldHooks.PickUp). Combat and a parley truce come first: a Trader leaves
the goods it dropped for a Thief until the truce is over and no Thief is
in sight.

===========================================================================
*/
package caravanai

const (
	// LootReach is how far an agent goes for loot it sees.
	LootReach float64 = 30 * UnitsPerMeter
	// lootPickupReach is how near it must stand to pick the item up.
	lootPickupReach float64 = 1.5 * UnitsPerMeter
	// lootGiveUpMs ends a walk to loot that does not get there.
	lootGiveUpMs int64 = 15_000
)

/*
================
LootSought

Whether the agent looks for loot now: a Trader with goods to recover when
no truce runs and it is not under attack, a Thief that is not fighting.
Idle agents strolling in town, waiting for hire, never do.
================
*/
func (r *Runtime) LootSought(division, name string, nowMs int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	u := r.findAgentLocked(division, name)
	if u == nil || u.RespawnPending || u.HP <= 0 || u.State == StateIdleInTown || u.State == StateRecruiting ||
		u.State == StateInCombat || u.State == StateParleying || u.UnderAttack {
		return false
	}
	switch u.Role {
	case RoleTrader:
		return !r.parleys.TruceActive(division, u.Name, nowMs)
	case RoleThief:
		return true
	}
	return false
}

/*
================
NoteLoot

The item the agent should pick up, or none (gid 0). A walk under way keeps
its item while that item is still the one in reach.
================
*/
func (r *Runtime) NoteLoot(division, name string, gid uint32, at Waypoint, nowMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u := r.findAgentLocked(division, name)
	if u == nil {
		return
	}
	if gid == 0 {
		u.lootGID = 0
		return
	}
	if u.lootGID == gid {
		return
	}
	u.lootGID, u.lootAt, u.lootUntilMs = gid, at, nowMs+lootGiveUpMs
}
