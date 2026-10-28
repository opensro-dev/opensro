/*
===========================================================================

qinshiTombOverlay.test.mjs - the Qin-Shi tomb overlay bake gates

The v1.150 textdata has no Qin-Shi Emperor Tomb rows; the server appends
its committed overlays behind SRO_QINSHI_TOMB (port-only, not native).
The client bake must see the Tomb references exactly when the shard can
spawn them: opted-in character row loads merge the overlay
characterdata, unopted loads (the server's own game-data projection)
never do, and a flag-off bake is the retail table alone. An unset flag
takes the shard's deployed posture (profile, else registry default), so
a default build bakes what the default shard spawns (#2024).

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { loadCharacterDataRows } from "../../build/char/resolveCharRoster.mjs";
import { computeResourceBuildFingerprint } from "../../build/shared/resourceBuildFingerprint.mjs";
import {
	loadQinShiTombCharacterRows,
	loadQinShiTombSkillRows,
	loadQinShiTombTeleportLinkRows,
	qinshiTombEnabled
} from "../../build/shared/qinshiTomb.mjs";

// The server source root this process started with (fixtureServerRoot
// points it elsewhere for one test).
const ORIGINAL_SERVER_ROOT = process.env.SRO_SERVER_SOURCE_ROOT;

/*
================
withTombFlag

Set SRO_QINSHI_TOMB for one test and restore the caller's value after.
================
*/
function withTombFlag( t, value ) {
	const saved = process.env.SRO_QINSHI_TOMB;
	t.after( () => {
		if ( saved === undefined ) delete process.env.SRO_QINSHI_TOMB;
		else process.env.SRO_QINSHI_TOMB = saved;
	} );
	if ( value === undefined ) delete process.env.SRO_QINSHI_TOMB;
	else process.env.SRO_QINSHI_TOMB = value;
}

/*
================
fixtureTextdata

A one-row characterdata shard directory: the fixture NPC plus whatever
the overlay contributes.
================
*/
function fixtureTextdata( t ) {
	const dir = fs.mkdtempSync( path.join( os.tmpdir(), "sro-tomb-textdata-" ) );
	t.after( () => fs.rmSync( dir, { recursive: true, force: true } ) );
	fs.writeFileSync( path.join( dir, "characterdata_10000.txt" ), "1\t999001\tNPC_FIXTURE\r\n", "utf16le" );
	return dir;
}

test("qinshiTombEnabled mirrors the server flag words", ( t ) => {
	withTombFlag( t, "off" );
	for ( const word of [ "1", "on", "true", "yes", " ON ", "True" ] ) {
		process.env.SRO_QINSHI_TOMB = word;
		assert.equal( qinshiTombEnabled(), true, word );
	}
	for ( const word of [ "0", "off", "false", "no", "2" ] ) {
		process.env.SRO_QINSHI_TOMB = word;
		assert.equal( qinshiTombEnabled(), false, word );
	}
});

/*
================
fixtureServerRoot

A server source tree holding only what the posture reads: the release
profile (profileValue undefined leaves the key out) and a registry row
with the given default. Pointed at through SRO_SERVER_SOURCE_ROOT for one
test.
================
*/
function fixtureServerRoot( t, profileValue, registryDefault ) {
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "sro-tomb-posture-" ) );
	// Restore the value the process started with, whatever order several
	// fixtures' hooks run in.
	t.after( () => {
		if ( ORIGINAL_SERVER_ROOT === undefined ) delete process.env.SRO_SERVER_SOURCE_ROOT;
		else process.env.SRO_SERVER_SOURCE_ROOT = ORIGINAL_SERVER_ROOT;
		fs.rmSync( root, { recursive: true, force: true } );
	} );
	fs.mkdirSync( path.join( root, "ops", "release" ), { recursive: true } );
	fs.mkdirSync( path.join( root, "internal", "config", "shardsettings" ), { recursive: true } );
	const profile = profileValue === undefined ? {} : { SRO_QINSHI_TOMB: profileValue };
	fs.writeFileSync( path.join( root, "ops", "release", "shard-settings.json" ), JSON.stringify( profile ) );
	fs.writeFileSync(
		path.join( root, "internal", "config", "shardsettings", "shardsettings.go" ),
		`var Settings = []Setting{\n\t{Env: "SRO_QINSHI_TOMB", NomadVar: "qinshi_tomb", Default: "${registryDefault}", Catalogue: true},\n}\n`
	);
	process.env.SRO_SERVER_SOURCE_ROOT = root;
}

test("an unset tomb flag takes the shard's deployed posture (#2024)", ( t ) => {
	// The checkout's own posture: the shard spawns the tomb by default, so
	// a default build must bake it - the split that shipped invisible
	// tomb monsters.
	for ( const unset of [ undefined, "", "  " ] ) {
		withTombFlag( t, unset );
		assert.equal( qinshiTombEnabled(), true, `unset ${JSON.stringify( unset )} reads the shard default` );
	}
	// The registry default decides when the profile is silent...
	withTombFlag( t, undefined );
	fixtureServerRoot( t, undefined, "off" );
	assert.equal( qinshiTombEnabled(), false );
	// ...the deployed profile overrides it...
	fixtureServerRoot( t, "on", "off" );
	assert.equal( qinshiTombEnabled(), true );
	fixtureServerRoot( t, "off", "on" );
	assert.equal( qinshiTombEnabled(), false );
	// ...and an explicit build env still wins over both.
	process.env.SRO_QINSHI_TOMB = "on";
	assert.equal( qinshiTombEnabled(), true );
});

test("a registry the bake cannot read stops the build", ( t ) => {
	withTombFlag( t, undefined );
	fixtureServerRoot( t, undefined, "on" );
	fs.writeFileSync(
		path.join( process.env.SRO_SERVER_SOURCE_ROOT, "internal", "config", "shardsettings", "shardsettings.go" ),
		"var Settings = []Setting{}\n"
	);
	assert.throws( () => qinshiTombEnabled(), /cannot see the shard's posture/ );
});

test("tomb character rows join only an opted-in load with the flag on", ( t ) => {
	const textdataDir = fixtureTextdata( t );
	const overlayCount = loadQinShiTombCharacterRows().rows.length;
	assert.ok( overlayCount > 0, "the committed characterdata overlay is empty" );

	withTombFlag( t, "off" );
	const flagOff = loadCharacterDataRows( textdataDir, { codenamePattern: /./, qinshiTombOverlay: true } );
	assert.equal( flagOff.size, 1 );
	assert.ok( flagOff.has( "NPC_FIXTURE" ) );
	assert.ok( !flagOff.has( "MOB_TQ_TOMBSOLDIER" ) );

	process.env.SRO_QINSHI_TOMB = "on";
	const flagOn = loadCharacterDataRows( textdataDir, { codenamePattern: /./, qinshiTombOverlay: true } );
	assert.equal( flagOn.size, 1 + overlayCount );
	assert.ok( flagOn.has( "MOB_TQ_TOMBSOLDIER" ) );
	assert.ok( flagOn.has( "MOB_QT_TQ_THIEF_01_COS" ) );

	// The server's own game-data projection stays out even with the flag
	// on: the server appends the overlay itself and would meet its rows
	// twice.
	const unopted = loadCharacterDataRows( textdataDir, { codenamePattern: /./ } );
	assert.equal( unopted.size, 1 );
	assert.ok( !unopted.has( "MOB_TQ_TOMBSOLDIER" ) );
});

test("flipping the tomb flag changes the resource build fingerprint", async ( t ) => {
	// Otherwise an up-to-date tree would skip the build and keep the
	// flag-off assets after the flag turns on (lane memos hash the same
	// knob list).
	withTombFlag( t, "off" );
	const off = await computeResourceBuildFingerprint();
	process.env.SRO_QINSHI_TOMB = "on";
	const on = await computeResourceBuildFingerprint();
	assert.notEqual( on.hash, off.hash );
});

test("tomb overlay rows are collision-free", () => {
	const characters = loadQinShiTombCharacterRows().rows;
	const codenames = characters.map( ( cols ) => cols[2] );
	assert.equal( new Set( codenames ).size, codenames.length, "duplicate overlay codename" );
	const characterIds = characters.map( ( cols ) => Number( cols[1] ) );
	assert.equal( new Set( characterIds ).size, characterIds.length, "duplicate overlay RefObjID" );
	assert.ok( characterIds.every( ( id ) => Number.isSafeInteger( id ) && id > 0 ) );
	for ( const cols of characters ) {
		assert.equal( cols.length, 120, `${cols[2]} column count` );
	}

	const skills = loadQinShiTombSkillRows().rows;
	assert.ok( skills.length > 0, "the committed skilldata overlay is empty" );
	const skillIds = skills.map( ( cols ) => Number( cols[1] ) );
	assert.equal( new Set( skillIds ).size, skillIds.length, "duplicate overlay skill id" );
	assert.ok( skillIds.every( ( id ) => Number.isSafeInteger( id ) && id > 0 ) );
	// Every overlay skill survives the skill bake's Service==1 / width
	// admission, or the bake would silently drop a Tomb skill.
	for ( const cols of skills ) {
		assert.ok( cols.length >= 118 && cols[0].trim() === "1", cols[3] );
	}

	const teleportLinks = loadQinShiTombTeleportLinkRows().rows;
	assert.equal( teleportLinks.length, 77, "expected 77 Tomb teleport link rows" );
	for ( const cols of teleportLinks ) {
		assert.equal( cols.length, 21 );
	}
});
