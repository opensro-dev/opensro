/*
===========================================================================

muhan.mjs - the Muhan (Battlefield of Infinity) overlays for the bake

Port-only, not native: the v1.150 textdata has no Muhan rows. The arena
manager hub and the eight Muhan gate rows (teleport ids 227..235) and
their 16 links live only in the server's committed overlays
(apps/server/internal/game/action/data, #3314), appended by the server
behind SRO_MUHAN (monster.MuhanEnabled, default off). The bake reads
the same two files through here so the client publishes the
destination menu exactly when the shard admits the gates.

The bake follows the shard's deployed posture, not a bare build env:
an unset SRO_MUHAN means what the deployment means by it - the release
profile's value, else the shard registry's default (off). Mirrors
qinshiTomb.mjs, the overlay that set the pattern.

===========================================================================
*/

import { loadServerOverlayRows, shardSwitchEnabled, shardSwitchPosture } from "./shardSwitch.mjs";

const MUHAN_ENV = "SRO_MUHAN";

// Mirrors monster.MuhanEnabled: 1, on or true.
const MUHAN_ON_WORDS = [ "1", "on", "true" ];

/*
================
muhanShardPosture

SRO_MUHAN as the deployment sets it (shardSwitch.mjs).
================
*/
export function muhanShardPosture() {
	return shardSwitchPosture( MUHAN_ENV );
}

/*
================
muhanEnabled

An explicit SRO_MUHAN wins (any non-blank value, "off" included);
unset or blank takes the shard's deployed posture.
================
*/
export function muhanEnabled() {
	return shardSwitchEnabled( MUHAN_ENV, MUHAN_ON_WORDS );
}

/*
================
loadMuhanTeleportDataRows

The 9 Muhan teleportdata rows: the arena manager hub (227) and the
eight solo/party gates (228..235) from the v1.696 client.
================
*/
export function loadMuhanTeleportDataRows() {
	return loadServerOverlayRows( "internal/game/action/data/muhan_teleportdata.tsv" );
}

/*
================
loadMuhanTeleportLinkRows

The 16 Muhan teleportlink rows: hub to the eight gates, and each gate
back to the town gate (teleport 5). Fee 0, condition-free.
================
*/
export function loadMuhanTeleportLinkRows() {
	return loadServerOverlayRows( "internal/game/action/data/muhan_teleportlink.tsv" );
}
