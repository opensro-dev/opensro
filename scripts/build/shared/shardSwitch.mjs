/*
===========================================================================

shardSwitch.mjs - a port-only shard switch's posture, and its overlays

Port-only server switches (SRO_QINSHI_TOMB, SRO_MUHAN) append committed
overlay rows behind a flag, and the client bake must publish the same
rows exactly when the shard admits them. The bake follows the shard's
deployed posture, not a bare build env: an unset switch means what the
deployment means by it - the release profile's value, else the shard
registry's default. The bake once read unset as off while the shard
defaulted on, so the live shard spawned 79 species no shipped pack could
draw (#2024). One owner reads that posture and the overlay files, so a
third switch cannot copy the rule and drift.

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";
import { splitTextDataRow } from "./textDataIo.mjs";
import { serverSourceRoot } from "../world/paths.mjs";

// The deployed profile and the shard settings registry that holds every
// switch's default (shardsettings.go, the server's one owner).
const SHARD_PROFILE = path.join( "config", "shard-settings.json" );
const SHARD_REGISTRY = path.join( "internal", "config", "shardsettings", "shardsettings.go" );

// file -> { size, mtimeMs, text }: the profile and the registry as last read.
const sourceTexts = new Map();

/*
================
readSourceText

A server source file's text, read again only when its size or mtime
moved. The bake asks a switch's posture once per skill row and once per
model (qinshiTombCombatEnabled); reading and scanning shardsettings.go
each time cost the npc lane about 1.8 s of a warm build.
================
*/
function readSourceText( file ) {
	const stat = fs.statSync( file );
	const known = sourceTexts.get( file );
	if ( known && known.size === stat.size && known.mtimeMs === stat.mtimeMs ) return known.text;
	const text = fs.readFileSync( file, "utf8" );
	sourceTexts.set( file, { size: stat.size, mtimeMs: stat.mtimeMs, text } );
	return text;
}

/*
================
shardSwitchPosture

The switch's value as the deployment sets it when the build env does
not: the release profile's entry, else the registry Default. A registry
row this cannot read throws - a posture the bake cannot see must stop
the build, never read as off.
================
*/
export function shardSwitchPosture( env ) {
	const serverRoot = serverSourceRoot();
	const profile = JSON.parse( readSourceText( path.join( serverRoot, SHARD_PROFILE ) ) );
	if ( typeof profile[env] === "string" ) {
		return profile[env];
	}
	const registry = readSourceText( path.join( serverRoot, SHARD_REGISTRY ) );
	const row = new RegExp( `\\{\\s*Env:\\s*"${env}"[^}]*?Default:\\s*"([^"]*)"` ).exec( registry );
	if ( !row ) {
		throw new Error( `${env}: no Default in ${SHARD_REGISTRY}; the bake cannot see the shard's posture` );
	}
	return row[1];
}

/*
================
shardSwitchEnabled

An explicit env value wins (any non-blank value, "off" included); unset
or blank takes the shard's deployed posture. onWords are the words the
server's own reader accepts once trimmed and folded to lower case: each
switch mirrors its Go reader, and the readers differ ("yes").
================
*/
export function shardSwitchEnabled( env, onWords ) {
	const value = process.env[env] ?? "";
	const word = (value.trim() !== "" ? value : shardSwitchPosture( env )).trim().toLowerCase();
	return onWords.includes( word );
}

/*
================
loadServerOverlayRows

One committed server overlay as column rows. The files are UTF-8,
tab-separated, in the exact v1.150 table column layout, with `#` or
`//` header comment lines and blank lines skipped (the server's
SplitTabbedText framing).
================
*/
export function loadServerOverlayRows( relativePath ) {
	const sourcePath = path.join( serverSourceRoot(), relativePath );
	const text = fs.readFileSync( sourcePath, "utf8" ).replace( /^﻿/, "" );
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
