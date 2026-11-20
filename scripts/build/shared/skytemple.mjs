/*
===========================================================================

skytemple.mjs - whether the Sky Temple fields are open on this shard

Port-only, not native: the Sky Temple import (scripts/skytemple_gates.py)
lists the four fields (GATE_OTHER_SKYTEMPLE_A..D) on Jangan's own gate.
The server keeps them closed, with every link to them, while
SRO_SKYTEMPLE is off (monster.SkytempleHidesGate); the teleport bake
leaves them out under the same switch, so the menu offers exactly what
the shard admits.

===========================================================================
*/

import { shardSwitchEnabled } from "./shardSwitch.mjs";

const SKYTEMPLE_ENV = "SRO_SKYTEMPLE";

// Mirrors monster.SkytempleEnabled: 1, on or true.
const SKYTEMPLE_ON_WORDS = [ "1", "on", "true" ];

// Mirrors the server's skytempleGatePrefix.
const SKYTEMPLE_GATE_PREFIX = "GATE_OTHER_SKYTEMPLE_";

/*
================
skytempleEnabled

The switch as the shard runs it (shardSwitch.mjs).
================
*/
export function skytempleEnabled() {
	return shardSwitchEnabled( SKYTEMPLE_ENV, SKYTEMPLE_ON_WORDS );
}

/*
================
closedSkytempleIds

The teleportdata ids the bake leaves out, with every link naming one:
the Sky Temple field gates while the fields are closed, none while open.
rows are teleportdata column rows (id in column 1, codename in 2).
================
*/
export function closedSkytempleIds( rows, open ) {
	if ( open ) return new Set();
	return new Set(
		rows
			.filter( ( columns ) => (columns[2] ?? "").startsWith( SKYTEMPLE_GATE_PREFIX ) )
			.map( ( columns ) => Number( columns[1] ) >>> 0 )
	);
}
