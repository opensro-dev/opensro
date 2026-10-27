/*
===========================================================================

transportfollow.go - a dismounted trade transport follows its trader
(port-only, not native)

Natively a trade transport the trader steps off stays where it stands and
holds the trader within 1000 units of it (the reference server's owner
movement tethers, the vehicle COS test; petai.go's
tether). The owner approved the balance advisor's ask as a server setting
(2026-10-15): with SRO_TRANSPORT_FOLLOW on, the dismounted transport walks
after its owner, a trader or a thief alike (the rule keys on the transport,
never on the owner's job) on the same PetFollower path as a captured quest monster
(advancePet's plain Advance), and no tether holds the trader.

With SRO_TRANSPORT_FOLLOW_TETHER also on (owner, 2026-10-24), the
following transport keeps the native 100 m tether: the trader may not walk
more than 1000 units from where the transport walks, refused with the
same notice as the original parked tether, so a trader cannot outrun a
transport held back by a fight or a wall. It acts only while the transport
follows.

Everything else stays native: mounting still needs the trader within
cosMountRange of the transport, thieves and bandits still target it at its
own pose (companionTargets reads the follower's presentation), and its
cargo still drops where it dies. Unset or "off" is native.

===========================================================================
*/
package action

import (
	"os"
	"strings"
)

// EnvTransportFollow turns the following trade transport on ("on", "1",
// "true").
const EnvTransportFollow = "SRO_TRANSPORT_FOLLOW"

// EnvTransportFollowTether keeps the 100 m tether on a following transport
// ("on", "1", "true"). It acts only with EnvTransportFollow on.
const EnvTransportFollowTether = "SRO_TRANSPORT_FOLLOW_TETHER"

/*
================
TransportFollowFromEnv
================
*/
func TransportFollowFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvTransportFollow))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
TransportFollowTetherFromEnv
================
*/
func TransportFollowTetherFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvTransportFollowTether))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
Runtime.companionFollows

Whether a summoned, unmounted companion of this TID walks after its owner:
the native following bands (followingCOSBand), plus the trade transport
while the port-only SRO_TRANSPORT_FOLLOW is on.
================
*/
func (rt *Runtime) companionFollows(tid uint16) bool {
	return followingCOSBand(tid>>11) || rt.TransportFollow && isVehicleCOS(tid)
}
