/*
===========================================================================

janganTombTeleport.mjs - the Jangan gate's Qin-Shi Tomb floor links

Port-only, not native: the Jangan town gate (teleport 1) also offers the
Qin-Shi Emperor Tomb floors 1-4. The seven links live only in the server's
committed overlay (apps/server/internal/game/action/data), appended by
the server behind SRO_JANGAN_TOMB_TELEPORT and SRO_QINSHI_TOMB
(action.JanganTombTeleportEnabled, default off). The bake reads the same file
through here so the client publishes the destination rows exactly when
the shard admits them.

===========================================================================
*/

import { qinshiTombEnabled } from "./qinshiTomb.mjs";
import { loadServerOverlayRows, shardSwitchEnabled } from "./shardSwitch.mjs";

const JANGAN_TOMB_TELEPORT_ENV = "SRO_JANGAN_TOMB_TELEPORT";

// Mirrors action.JanganTombTeleportEnabled: 1, on, true or yes.
const JANGAN_TOMB_TELEPORT_ON_WORDS = [ "1", "on", "true", "yes" ];

/*
================
janganTombTeleportEnabled

The switch as the shard runs it (shardSwitch.mjs), and only while the
tomb itself is open, as the server reads it.
================
*/
export function janganTombTeleportEnabled() {
	return qinshiTombEnabled() && shardSwitchEnabled( JANGAN_TOMB_TELEPORT_ENV, JANGAN_TOMB_TELEPORT_ON_WORDS );
}

/*
================
loadJanganTombTeleportLinkRows

The 7 teleportlink rows from the Jangan gate to the tomb floor 1-3
arrival rows (50, 53, 120) and the four floor 4 corners (129 NW, 126 NE,
128 SW, 127 SE).
================
*/
export function loadJanganTombTeleportLinkRows() {
	return loadServerOverlayRows( "internal/game/action/data/jangan_tomb_teleportlink.tsv" );
}
