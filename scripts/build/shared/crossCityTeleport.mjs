/*
===========================================================================

crossCityTeleport.mjs - the direct town-to-town gate links

Port-only, not native: every Silk Road town gate (Jangan, Donwhang, Hotan,
Samarkand, Constantinople) also leads straight to the other four. The 12
links live only in the server's committed overlay
(apps/server/internal/game/action/data), appended by the server behind
SRO_CROSS_CITY_TELEPORT (action.CrossCityTeleportEnabled, default off).
The bake reads the same file through here so the client publishes the
destination rows exactly when the shard admits them.

===========================================================================
*/

import { loadServerOverlayRows, shardSwitchEnabled } from "./shardSwitch.mjs";

const CROSS_CITY_TELEPORT_ENV = "SRO_CROSS_CITY_TELEPORT";

// Mirrors action.CrossCityTeleportEnabled: 1, on, true or yes.
const CROSS_CITY_TELEPORT_ON_WORDS = [ "1", "on", "true", "yes" ];

/*
================
crossCityTeleportEnabled

The switch as the shard runs it (shardSwitch.mjs).
================
*/
export function crossCityTeleportEnabled() {
	return shardSwitchEnabled( CROSS_CITY_TELEPORT_ENV, CROSS_CITY_TELEPORT_ON_WORDS );
}

/*
================
loadCrossCityTeleportLinkRows

The 12 teleportlink rows joining the town gates the native chain does not
join directly, each for 5000 gold per native trip it replaces.
================
*/
export function loadCrossCityTeleportLinkRows() {
	return loadServerOverlayRows( "internal/game/action/data/cross_city_teleportlink.tsv" );
}
