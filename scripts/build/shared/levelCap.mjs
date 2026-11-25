/*
===========================================================================

levelCap.mjs - the shard's level cap and the cap-110 expansion, as the bake sees them

The bake publishes what the shard admits (shardSwitch.mjs): an explicit
build env wins, else the deployed profile, else the registry default.
SRO_LEVEL_CAP is read with the server's own vocabulary
(progression.LevelCapFromEnv), so build and server never disagree. The
cap-110 expansion's game data follows the item switch and the level cap
together (cap110_blue_berserk.py): SRO_CAP110_ITEMS on with a cap of 110.

===========================================================================
*/

import { shardSwitchEnabled, shardSwitchPosture } from "./shardSwitch.mjs";

export const LEVEL_CAP_ENV = "SRO_LEVEL_CAP";
export const NATIVE_LEVEL_CAP = 90;
export const EXPANSION_LEVEL_CAP = 110;
const NATIVE_CAP_WORDS = [ "", "0", "off", "false", "90" ];
const EXPANSION_CAP_WORDS = [ "110" ];
const CAP110_ITEMS_ENV = "SRO_CAP110_ITEMS";
// cap110_item_import.py's reader.
const CAP110_ITEMS_ON = [ "1", "on", "true" ];

/*
================
configuredLevelCap

90 or 110; any other word stops the build, as it stops the server's boot.
================
*/
export function configuredLevelCap( env = process.env ) {
	const explicit = (env[LEVEL_CAP_ENV] ?? "").trim();
	const word = (explicit !== "" ? explicit : shardSwitchPosture( LEVEL_CAP_ENV )).trim().toLowerCase();
	if ( NATIVE_CAP_WORDS.includes( word ) ) return NATIVE_LEVEL_CAP;
	if ( EXPANSION_CAP_WORDS.includes( word ) ) return EXPANSION_LEVEL_CAP;
	throw new Error( `${LEVEL_CAP_ENV} must be off, 90 or 110, not ${JSON.stringify( word )}` );
}

/*
================
cap110ExpansionEnabled

Port-only, not native: whether the shard runs the cap-110 expansion's
game data (the Blue Berserk system and its records).
================
*/
export function cap110ExpansionEnabled() {
	return configuredLevelCap() === EXPANSION_LEVEL_CAP && shardSwitchEnabled( CAP110_ITEMS_ENV, CAP110_ITEMS_ON );
}
