/*
===========================================================================

assetPackOwnership.test.mjs - one owning group per published path

Regression for 2026-09-23: a full build packed equipment GLBs into
game-models, then a focused publisher appended equipment-models over the
same paths. The published index had 572 duplicate owners and the client
rejected it wholesale ("Missing pack or duplicate asset"), so no asset
could load.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { collectAssetPackGroups } from "../../build/assetPackGroups.mjs";
import { validateAssetPackIndex } from "../../build/assetPackIndexValidation.mjs";
import { mergeAssetPackGroupUpdates, publishAssetPackManifest } from "../../build/assetPackPublication.mjs";
import { cosModelFiles, equipmentModelFiles } from "../../build/assetPackOwnership.mjs";

/*
================
tempRoot
================
*/
async function tempRoot( t ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-pack-ownership-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	return root;
}

/*
================
put
================
*/
async function put( root, publicPath, contents = "x" ) {
	const file = path.join( root, ...publicPath.slice( 1 ).split( "/" ) );
	await mkdir( path.dirname( file ), { recursive: true } );
	await writeFile( file, contents );
}

/*
================
index

A minimal pack index naming each group's paths.
================
*/
function index( groups ) {
	const rows = [];
	const out = { format: "sro-asset-pack-index", version: 1, groups: [], assets: rows };
	for ( const [name, paths] of Object.entries( groups ) ) {
		const packPath = `/assets/packs/${name}.bin`;
		out.groups.push( {
			name,
			assetCount: paths.length,
			totalBytes: paths.length,
			packs: [ { path: packPath, bytes: 12 + paths.length, assetCount: paths.length, sha256: "0".repeat( 64 ) } ]
		} );
		paths.forEach( ( publicPath, i ) =>
			rows.push( { path: publicPath, packPath, group: name, offset: i, length: 1, sha256: "0".repeat( 64 ) } )
		);
	}
	return out;
}

test("full build gives roster/NPC-owned models to their dedicated groups, never game-models", async ( t ) => {
	const root = await tempRoot( t );
	const equipment = "/assets/char/equipment/ch_m_clothes_ba.glb";
	const hwan = "/assets/char/hwan/ch_m.glb";
	const cos = "/assets/npc/cos/p_wolf_01.glb";
	const cosVat = "/assets/npc/vat/cos/p_wolf_01.vat.bin";
	const cosVatJson = "/assets/npc/vat/cos/p_wolf_01.vat.json";
	const generic = "/assets/npc/mob/wolf.glb";
	const stale = "/assets/char/equipment/unreferenced.glb";
	for ( const file of [ equipment, hwan, cos, cosVat, cosVatJson, generic, stale ] ) await put( root, file );
	await put( root, "/assets/npc/vat/mob/wolf.vat.bin" );
	// Roots the full-build listing always sweeps; empty here.
	for ( const dir of [ "char/vat", "anim", "audio", "textdata" ] ) {
		await mkdir( path.join( root, "assets", dir ), { recursive: true } );
	}
	await put(
		root,
		"/assets/char/roster.json",
		JSON.stringify( {
			dress: {
				equipment: { 1: { bodies: { m: { glb: equipment } } } },
				defaultWear: {},
				avatarAuxiliary: {},
				hwan: { 1: { glb: hwan } }
			}
		} )
	);
	await put(
		root,
		"/assets/npc/manifest.json",
		JSON.stringify( {
			models: {
				wolf: { kind: "mob", glb: generic },
				pet: { kind: "cos", glb: cos, vat: { manifest: cosVatJson, bin: cosVat } }
			}
		} )
	);

	const { groups } = await collectAssetPackGroups( {
		uiImagePreloadPaths: [],
		missionMinimapTilePaths: [],
		includeOutdoorWorld: false,
		publicRoot: root
	} );
	const byName = Object.fromEntries( groups.map( ( group ) => [ group.name, group.files ] ) );
	assert.deepEqual( byName["equipment-models"], [ equipment ] );
	assert.deepEqual( byName["hwan-models"], [ hwan ] );
	assert.deepEqual( new Set( byName["mission-cos-models"] ), new Set( [ cos, cosVatJson, cosVat ] ) );
	assert.deepEqual( new Set( byName["game-models"] ), new Set( [ generic, stale ] ) );
	assert.deepEqual( byName["mission-npc-vat"], [ "/assets/npc/vat/mob/wolf.vat.bin" ] );

	const owners = new Map();
	for ( const group of groups ) {
		for ( const file of group.files ) {
			assert.equal(
				owners.has( file.toLowerCase() ),
				false,
				`${file} owned by ${owners.get( file.toLowerCase() )} and ${group.name}`
			);
			owners.set( file.toLowerCase(), group.name );
		}
	}
});

test("publishers and the full build derive membership from the same functions", () => {
	const dress = {
		equipment: { 1: { bodies: { m: { glb: "/assets/char/equipment/a.glb" }, f: null } } },
		defaultWear: { m: { glb: "/assets/char/equipment/a.glb" } },
		avatarAuxiliary: { 1: { glb: "/assets/char/equipment/aux.glb" } }
	};
	assert.deepEqual( equipmentModelFiles( dress ), [
		"/assets/char/equipment/a.glb",
		"/assets/char/equipment/aux.glb"
	] );
	assert.deepEqual( cosModelFiles( { models: { a: { kind: "cos", glb: "/assets/npc/cos/a.glb" } } } ), [
		"/assets/npc/cos/a.glb"
	] );
});

test("the index validator rejects a path owned by two groups (case-folded like the client)", () => {
	validateAssetPackIndex( index( { "game-models": [ "/assets/a.glb" ], "equipment-models": [ "/assets/b.glb" ] } ) );
	assert.throws(
		() =>
			validateAssetPackIndex(
				index( {
					"game-models": [ "/assets/char/equipment/A.glb" ],
					"equipment-models": [ "/assets/char/equipment/a.glb" ]
				} )
			),
		/exactly one owning group/
	);
	assert.throws( () => validateAssetPackIndex( index( { g: [ "/assets/a%20b.png" ] } ) ), /not client-admissible/ );
});

test("the shared merge replaces only declared groups and refuses undeclared updates", () => {
	const previous = index( { "game-models": [ "/assets/a.glb" ], "equipment-models": [ "/assets/old.glb" ] } );
	const update = index( { "equipment-models": [ "/assets/new.glb" ] } );
	const merged = mergeAssetPackGroupUpdates( previous, [ update ], [ "equipment-models" ] );
	assert.deepEqual( merged.assets.map( ( asset ) => asset.path ), [ "/assets/a.glb", "/assets/new.glb" ] );
	validateAssetPackIndex( merged );
	assert.throws(
		() => mergeAssetPackGroupUpdates( previous, [ update ], [ "game-data" ] ),
		/not declared as replaced/
	);
});

test("publication fails closed on a duplicate-owner index and keeps the live manifest", async ( t ) => {
	const root = await tempRoot( t );
	const filename = path.join( root, "assets", "packs", "manifest.json" );
	await put( root, "/assets/packs/manifest.json", "previous valid publication" );
	const broken = index( {
		"game-models": [ "/assets/char/equipment/a.glb" ],
		"equipment-models": [ "/assets/char/equipment/a.glb" ]
	} );
	await assert.rejects(
		publishAssetPackManifest( root, filename, Buffer.from( JSON.stringify( broken ) ) ),
		/exactly one owning group/
	);
	assert.equal( await readFile( filename, "utf8" ), "previous valid publication" );
});
