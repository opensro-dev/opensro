/*
===========================================================================

ammunitionpreference.go - the smallest-stack reload choice

Port-only, not native (owner decision 2026-11-26, #4527). Native reloads
the magazine from the lowest bag slot of the same packed TID and never
compares stack counts. The client's Experimental row "Reload bolts and
arrows from the smallest stack" asks the server to take the smallest
matching stack instead, ties going to the lower slot; off is native.

The choice rides the transport control lane (commerce-controls.ts):
one byte, 1 for smallest, 0 for native. There is no answer - nothing on
the client waits for one - and the value is per character for the
session: declared at world entry while the row is on, again whenever
the row is toggled under a live world, and never written to the store.
A declaration a closed tab leaves behind is the next declaration's to
replace; the row's owner rewrites it every entry.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

// opAmmoReloadPreference is the client's declaration of its
// smallest-stack reload row (client commerce-controls.ts
// AMMO_RELOAD_CONTROL; port-only, not native).
const opAmmoReloadPreference = 22

/*
================
registerAmmoReload
================
*/
func (rt *Runtime) registerAmmoReload(hub *transport.Hub) {
	hub.Handle(opAmmoReloadPreference, rt.hubHandler(hub, rt.HandleAmmoReloadPreference))
}

/*
================
HandleAmmoReloadPreference

One byte: 1 reloads from the smallest stack, 0 is the native lowest
slot. Anything else is not a declaration and changes nothing; there is
no answer either way.
================
*/
func (rt *Runtime) HandleAmmoReloadPreference(_ string, character *enterworld.Character, payload []byte) OpResult {
	if character == nil || len(payload) != 1 || payload[0] > 1 {
		return OpResult{}
	}
	if rt.ammoReloadSmallest == nil {
		rt.ammoReloadSmallest = make(map[string]bool)
	}
	rt.ammoReloadSmallest[character.Name] = payload[0] == 1
	return OpResult{}
}

/*
================
ammoReloadsSmallest

Whether this character's client asked for the smallest-stack reload.
No declaration is the native lowest slot.
================
*/
func (rt *Runtime) ammoReloadsSmallest(character *enterworld.Character) bool {
	if character == nil {
		return false
	}
	return rt.ammoReloadSmallest[character.Name]
}
