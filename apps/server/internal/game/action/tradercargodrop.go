/*
===========================================================================

tradercargodrop.go - a trader's goods stay in the world when the trader
leaves it

Port-only, not native (owner decision 2026-10-14, the balance advisor's
option 3). Natively only the transport's own death drops its goods
(cosdeath.go). A rider's death releases the ridden vehicle with
its record and its cargo (coscancel.go), and a logout takes a
summoned transport and its goods offline with the trader. Either way a
trader cornered by thieves or hunters can make the goods vanish.

With SRO_TRADER_CARGO_DROP on:

Death while riding. A trader killed on its transport (by a player, a
thief or a monster alike) drops the goods around the transport before
the native release deletes the vehicle.

Death while the transport is parked. Native: the transport stays in the
world with its goods, and killing it drops them.

Disconnect or logout. A summoned transport (ridden or parked) carrying
goods outside a town dies where it stands. Its goods drop unowned, as on
its own death, and its record is deleted (kind-1 release, as on death).
Inside a town (a region that forbids player combat) the native dismiss
applies and the goods go offline with the trader.

A reload or reconnect is not a logout: the new session evicts the old
one and the character carries on, so a replaced close keeps the transport
and its goods, as a server shutdown does.

Each trader is judged alone: a party member's transport is untouched by
another member's death or logout. An empty transport is native in every
case. A server shutdown closes every session, but it is not a player
action, so it drops nothing (the caller tells the close apart).

Off, the default here, every case is native. The GameWorld's Nomad job
turns it on.

===========================================================================
*/
package action

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

// EnvTraderCargoDrop turns the rule on ("on", "1", "true").
const EnvTraderCargoDrop = "SRO_TRADER_CARGO_DROP"

/*
================
TraderCargoDropFromEnv
================
*/
func TraderCargoDropFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvTraderCargoDrop))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
loadedTransport

Whether pet is a summoned transport carrying goods: the only vehicle the
rule ever touches.
================
*/
func (rt *Runtime) loadedTransport(pet *enterworld.CharacterCOS) bool {
	if pet == nil || !pet.Summoned || pet.Container == nil || len(pet.Container.Rows) == 0 {
		return false
	}
	ref, found := rt.cosReference(pet)
	return found && ref.TidWord>>11 == cosBandTransport
}

/*
================
dropRiddenCargoInDoor

The death-while-riding case, called from releaseRiddenVehicleInDoor before
the vehicle is released. The caller holds c's door. Returns the drops'
public frames, nil when the rule is off or the ride carries nothing.
================
*/
func (rt *Runtime) dropRiddenCargoInDoor(division string, c *enterworld.Character, now int64) []wire.Frame {
	if !rt.TraderCargoDrop || !rt.loadedTransport(c.ActiveCOS) {
		return nil
	}
	return rt.scatterTransportCargo(division, c, c.ActiveCOS, now, "rider-death", "")
}

/*
================
DropDisconnectedCargo

The disconnect case, called by the session-close hook before
ForgetCharacterSession with the close's cause. A summoned, loaded
transport outside a town dies where it stands: its goods scatter unowned
and its record is deleted. A server shutdown and a replaced close (a
reload or reconnect: the new session evicts the old one before it takes
the character's admission, so the ownership test below cannot see it)
drop nothing. Like ForgetCharacterSession, it also ignores a close from
a session that no longer owns the character.
================
*/
func (rt *Runtime) DropDisconnectedCargo(division, name string, session uint64, cause error) {
	if !rt.TraderCargoDrop || transport.ShutdownClose(cause) || transport.ReplacedClose(cause) {
		return
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if owner, exists := rt.characterAdmissions.Load(simulation.WorldKey(division, name)); exists &&
		owner.(populationAdmission).session != session {
		return
	}
	c := rt.findCharacter(division, name)
	if c == nil {
		return
	}
	var public []wire.Frame
	rt.deps.Update(c, "trader-cargo-disconnect", func() bool {
		public = rt.dropDisconnectedCargoInDoor(division, c, rt.Now().UnixMilli())
		return public != nil
	})
	if len(public) > 0 && rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, c.Name, public)
	}
}

/*
================
dropDisconnectedCargoInDoor

The caller holds c's door. The town test reads the transport's own
position: a parked transport can stand outside the town its owner left
from.
================
*/
func (rt *Runtime) dropDisconnectedCargoInDoor(division string, c *enterworld.Character, now int64) []wire.Frame {
	pet := c.ActiveCOS
	if !rt.loadedTransport(pet) {
		return nil
	}
	at := rt.companionLiveSpawn(division, c, pet, now)
	if allowed, known := world.RegionPlayerCombat(at.RegionID); known && !allowed {
		return nil
	}
	public := rt.scatterTransportCargo(division, c, pet, now, "disconnect", "")
	gid := pet.GID
	if pet.Mounted {
		public = append(public, wire.Frame{Opcode: wire.OpCosRideState,
			Payload: wire.EncodeCosRideState(enterworld.ObjectIDForCharacter(c), false, gid)})
	}
	pet.Mounted = false
	pet.Summoned = false
	pet.StateFlags &^= cosStateSummoned
	public = append(public, rt.retireCosRuntime(division, c, gid)...)
	public = append(public, wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: gid}.Encode()})
	// The kind-1 release of a vehicle's death deletes
	// the record.
	c.ActiveCOS = nil
	return public
}
