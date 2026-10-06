/*
===========================================================================

published-characters.test.mjs - published models pass the production decoder

Reads the generated client-public tree: NPC models, item drops and every
character attachment the item catalog publishes must be packed with their
recorded hashes and accepted by the production model decoder.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { readFileSync, existsSync } from "node:fs";
import { createHash } from "node:crypto";
import { zstdDecompressSync } from "node:zlib";
import { build } from "esbuild";
import { readPublishedAssetBytesSync, readPublishedAssetJsonSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { equipmentModelFiles, hwanModelFiles } from "../../../../scripts/build/assetPackOwnership.mjs";

const entry = "src/engine/runtime/assets/worker/model/model.ts";
const publicRoot = CLIENT_PUBLIC_ROOT;
const createModelDecoder = await loadModelDecoder( [] );

/*
================
loadModelDecoder

Bundles the production model worker with optional esbuild plugins and
returns its decoder factory.
================
*/
async function loadModelDecoder( plugins ) {
	const built = await build( {
		entryPoints: [ entry ],
		bundle: true,
		platform: "node",
		format: "esm",
		write: false,
		plugins,
		footer: { js: `//# sourceURL=${entry}` }
	} );
	const source = Buffer.from( built.outputFiles[0].contents ).toString( "base64" );
	return (await import( "data:text/javascript;base64," + source )).createModelDecoder;
}

/*
================
sha256
================
*/
function sha256( bytes ) {
	return createHash( "sha256" ).update( bytes ).digest( "hex" );
}

test("every published NPC model passes the production character decoder", () => {
	const manifest = readPublishedAssetJsonSync( "/assets/npc/manifest.json", publicRoot );
	const paths = new Set( Object.values( manifest.models ).map( model => model.glb ) );
	assert.ok( paths.size > 0, "NPC manifest must contain models" );
	const decoder = createModelDecoder();
	for ( const resource of paths ) {
		assert.equal( typeof resource, "string" );
		assert.doesNotThrow(
			() => decoder.character( decoder.decode( readPublishedAssetBytesSync( resource, publicRoot ) ) ),
			resource
		);
	}
});

test("packed NPC models match their published authority and pass the production decoder", () => {
	const index = JSON.parse( readFileSync( path.join( publicRoot, "assets/packs/manifest.json" ), "utf8" ) );
	const manifest = readPublishedAssetJsonSync( "/assets/npc/manifest.json", publicRoot );
	const paths = new Set( Object.values( manifest.models ).map( model => model.glb ) );
	const grouped = new Map();
	for ( const resource of paths ) {
		const member = index.assets.find( asset => asset.path === resource );
		assert.ok( member, `Unpacked NPC ${resource}` );
		if ( !grouped.has( member.packPath ) ) grouped.set( member.packPath, [] );
		grouped.get( member.packPath ).push( member );
	}
	const decoder = createModelDecoder();
	for ( const [packPath, members] of grouped ) {
		const pack = index.groups.flatMap( group => group.packs ).find( pack => pack.path === packPath );
		const identity = path.join( publicRoot, packPath );
		const bytes = existsSync( identity ) ?
			readFileSync( identity ) :
			zstdDecompressSync( readFileSync( path.join( publicRoot, pack.zstdPath ?? `${packPath}.zst` ) ) );
		assert.equal( sha256( bytes ), pack.sha256 );
		const start = 12 + bytes.readUInt32LE( 8 );
		for ( const member of members ) {
			const model = bytes.subarray( start + member.offset, start + member.offset + member.length );
			assert.equal( sha256( model ), member.sha256, member.path );
			assert.deepEqual(
				model,
				readPublishedAssetBytesSync( member.path, publicRoot ),
				`Stale packed NPC ${member.path}`
			);
			assert.doesNotThrow( () => decoder.character( decoder.decode( model ) ), member.path );
		}
	}
});

test("special COS references share authored state-50 models and matching VAT clips", () => {
	const manifest = readPublishedAssetJsonSync( "/assets/npc/manifest.json", publicRoot );
	const cos = Object.values( manifest.models ).filter( row => row.kind === "cos" );
	// Reviewed 2026-10-02: every COS band whose BSR ships. Riding mounts,
	// transports and quest companions author no state-50 emote; 582110's
	// action-1 route plays one for the growth pets, which all author it.
	assert.equal( cos.length, 1217 );
	assert.equal( new Set( cos.map( row => row.glb ) ).size, 43 );
	const rows = cos.filter( row => row.codename.startsWith( "COS_P_" ) );
	assert.equal( rows.length, 1130 );
	assert.equal( new Set( rows.map( row => row.glb ) ).size, 22 );
	for ( const row of rows ) {
		assert.equal( row.animationStates.emote0.stateId, 50 );
		assert.ok( row.clips.includes( "emote0" ) );
		assert.ok( row.vat.clips.includes( "emote0" ), row.codename );
	}
});

test("every authored item drop passes the production model decoder", () => {
	const manifest = readPublishedAssetJsonSync( "/assets/itemdrop/manifest.json", publicRoot );
	const decoder = createModelDecoder();
	const entries = Object.values( manifest.models );
	assert.equal( entries.length, 38 );
	for ( const row of entries ) {
		const model = decoder.character( decoder.decode( readPublishedAssetBytesSync( row.glb, publicRoot ) ) );
		assert.ok( model.primitives.length, row.glb );
		for ( const clip of row.clips ) assert.ok( model.clips.some( c => c.name === clip ), row.glb + ": " + clip );
	}
});

test("every catalog attachment is packed once and accepted by the production decoder", () => {
	const roster = readPublishedAssetJsonSync( "/assets/char/roster.json", publicRoot );
	const index = readPublishedAssetJsonSync( "/assets/packs/manifest.json", publicRoot );
	const decoder = createModelDecoder();
	const owners = new Map();
	for ( const asset of index.assets ) owners.set( asset.path, [ ...(owners.get( asset.path ) ?? []), asset ] );
	const avatarBodies = Object.values( roster.dress.equipment )
		.filter( row => row.avatarSlot !== undefined )
		.flatMap( row => Object.values( row.bodies ).filter( Boolean ) );
	assert.ok( avatarBodies.length > 0, "the item catalog must publish avatar bodies" );
	const files = [ ...equipmentModelFiles( roster.dress ), ...hwanModelFiles( roster.dress ) ];
	for ( const body of avatarBodies ) assert.ok( files.includes( body.glb ), `Unowned avatar body ${body.glb}` );
	for ( const glb of files ) {
		const bytes = readPublishedAssetBytesSync( glb, publicRoot );
		const members = owners.get( glb ) ?? [];
		assert.equal( members.length, 1, glb );
		assert.equal( sha256( bytes ), members[0].sha256, glb );
		assert.ok( decoder.character( decoder.decode( bytes ) ).primitives.length, glb );
	}
});

for ( const enabled of [ false, true ] ) {
	test(`ordinary clothing ambient uses ${enabled ? "native actor" : "compatibility BMT"} input`, async () => {
		const plugins = enabled ?
			[ {
				name: "native-lighting-test",
				setup( build ) {
					build.onLoad( { filter: /video-options\.ts$/ }, args => ({
						contents: readFileSync( args.path, "utf8" ).replace(
							"NATIVE_CHARACTER_LIGHTING = false",
							"NATIVE_CHARACTER_LIGHTING = true"
						),
						loader: "ts"
					}) );
				}
			} ] :
			[];
		const createVariantDecoder = await loadModelDecoder( plugins );
		const roster = readPublishedAssetJsonSync( "/assets/char/roster.json", publicRoot );
		const garment = Object.entries( roster.dress.defaultWear ).find( ( [key] ) => key.startsWith( "CH_M_" ) );
		assert.ok( garment, "the catalog must publish Chinese male default wear" );
		const decoder = createVariantDecoder();
		const doc = decoder.decode( readPublishedAssetBytesSync( garment[1].glb, publicRoot ) );
		assert.ok( decoder.character( doc ).primitives.length );
		const altered = structuredClone( doc );
		for ( const m of altered.json.materials ) {
			m.extras = { sroAmbientFactor: [ .13, .29, .47, 1 ] };
			m.pbrMetallicRoughness.baseColorFactor = [ .71, .83, .97, 1 ];
		}
		for ( const p of decoder.character( altered ).primitives ) {
			assert.equal( p.geometry.material.stageFactor, 2 );
			assert.equal( p.geometry.material.objectLight, 1 );
			assert.deepEqual( p.geometry.material.ambient, enabled ? [ .6, .6, .6 ] : [ .13, .29, .47 ] );
			assert.deepEqual( p.geometry.material.color, [ .71, .83, .97, 1 ] );
		}
		altered.json.materials[0].extras.sroAmbientFactor = [ 1, 2 ];
		assert.throws( () => decoder.character( altered ), /ambient/ );
	});
}
