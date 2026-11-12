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

import fs from "node:fs";
import path from "node:path";
import { splitTextDataRow } from "./textDataIo.mjs";
import { serverSourceRoot } from "../world/paths.mjs";

// The switch, its deployed profile, and the shard settings registry
// that holds its default (shardsettings.go, the server's one owner).
const MUHAN_ENV = "SRO_MUHAN";
const SHARD_PROFILE = path.join( "config", "shard-settings.json" );
const SHARD_REGISTRY = path.join( "internal", "config", "shardsettings", "shardsettings.go" );

/*
================
flagWordOn

Mirrors monster.MuhanEnabled: the value trimmed and folded to lower
case must be 1, on or true.
================
*/
function flagWordOn( value ) {
	const word = String( value ).trim().toLowerCase();
	return word === "1" || word === "on" || word === "true";
}

/*
================
muhanShardPosture

The switch's value as the deployment sets it when the build env does
not: the release profile's entry, else the registry Default. A registry
row this cannot read throws - a posture the bake cannot see must stop
the build, never read as off.
================
*/
export function muhanShardPosture() {
	const serverRoot = serverSourceRoot();
	const profile = JSON.parse( fs.readFileSync( path.join( serverRoot, SHARD_PROFILE ), "utf8" ) );
	if ( typeof profile[MUHAN_ENV] === "string" ) {
		return profile[MUHAN_ENV];
	}
	const registry = fs.readFileSync( path.join( serverRoot, SHARD_REGISTRY ), "utf8" );
	const row = new RegExp( `\\{\\s*Env:\\s*"${MUHAN_ENV}"[^}]*?Default:\\s*"([^"]*)"` ).exec( registry );
	if ( !row ) {
		throw new Error(
			`${MUHAN_ENV}: no Default in ${SHARD_REGISTRY}; the bake cannot see the shard's posture`
		);
	}
	return row[1];
}

/*
================
muhanEnabled

An explicit SRO_MUHAN wins (any non-blank value, "off" included);
unset or blank takes the shard's deployed posture.
================
*/
export function muhanEnabled() {
	const value = process.env[MUHAN_ENV] ?? "";
	if ( value.trim() !== "" ) {
		return flagWordOn( value );
	}
	return flagWordOn( muhanShardPosture() );
}

/*
================
loadMuhanRows

One overlay file as column rows. The files are UTF-8, tab-separated,
in the exact v1.150 teleportdata/teleportlink column layout, with `#`
header comment lines and blank lines skipped (the server's
SplitTabbedText framing).
================
*/
function loadMuhanRows( relativePath ) {
	const sourcePath = path.join( serverSourceRoot(), relativePath );
	const text = fs.readFileSync( sourcePath, "utf8" ).replace( /^\ufeff/, "" );
	const rows = [];
	for ( const line of text.split( /\r?\n/ ) ) {
		if ( !line.trim() ) continue;
		const cols = splitTextDataRow( line );
		const first = String( cols[0] ?? "" ).trim();
		if ( first.startsWith( "#" ) || first.startsWith( "//" ) ) continue;
		rows.push( cols );
	}
	return { sourcePath, rows };
}

/*
================
loadMuhanTeleportDataRows

The 9 Muhan teleportdata rows: the arena manager hub (227) and the
eight solo/party gates (228..235) from the v1.696 client.
================
*/
export function loadMuhanTeleportDataRows() {
	return loadMuhanRows( "internal/game/action/data/muhan_teleportdata.tsv" );
}

/*
================
loadMuhanTeleportLinkRows

The 16 Muhan teleportlink rows: hub to the eight gates, and each gate
back to the town gate (teleport 5). Fee 0, condition-free.
================
*/
export function loadMuhanTeleportLinkRows() {
	return loadMuhanRows( "internal/game/action/data/muhan_teleportlink.tsv" );
}
