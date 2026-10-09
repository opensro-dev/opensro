/*
===========================================================================

boothModelResources.test.mjs - stall roster, bake and publication contracts

Fixture data exercises the real item resolver and shared secondary publisher.
Pack collection runs against an isolated public tree, never shared assets.

===========================================================================
*/

import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {
	collectBoothModelRoster,
	DEFAULT_BOOTH_MODELS,
	loadBoothModelRoster
} from "../../build/char/boothModelRoster.mjs";
import { bakeNpcSecondaryResources } from "../../build/char/npcSecondaryResources.mjs";
import { npcManifestModels, splitNpcManifestModels } from "../../build/shared/npcManifest.mjs";
import { readPreviousResourceGlbPaths, resourceGlbOutput } from "../../build/char/resourceGlbOutput.mjs";
import { collectAssetPackGroups } from "../../build/assetPackGroups.mjs";

/*
================
fixtureRoot
================
*/
async function fixtureRoot( t ) {
	const root = await fs.mkdtemp( path.join( os.tmpdir(), "sro-booth-models-" ) );
	t.after( () => fs.rm( root, { recursive: true, force: true } ) );
	return root;
}

/*
================
itemRow
================
*/
function itemRow( { id, code, model = "xxx", linkedCode = "xxx", enabled = true } ) {
	const columns = Array( 131 ).fill( "0" );
	columns[0] = enabled ? "1" : "0";
	columns[1] = String( id );
	columns[2] = code;
	columns[4] = linkedCode;
	columns[52] = model;
	columns[130] = "-1";
	return columns.join( "\t" );
}

/*
================
bakeContext
================
*/
function bakeContext( root, bake ) {
	return {
		publicAssetsRoot: path.join( root, "assets" ),
		models: [],
		bakedByBsr: new Map(),
		outputOwners: new Map(),
		retailAnimationModels: new Map(),
		retailAnimationResources: new Map(),
		bake
	};
}

test("booths use enabled item models and one-hop links, never drop models", async t => {
	const root = await fixtureRoot( t );
	const base = itemRow( { id: 1, code: "ITEM_BASE", model: "ITEM/AVATAR/BOOTH.BSR" } );
	const linked = itemRow( { id: 2, code: "ITEM_MALL_BOOTH_LINKED", linkedCode: "ITEM_BASE" } );
	const direct = itemRow( { id: 3, code: "ITEM_MALL_BOOTH_DIRECT", model: "item/avatar/booth.bsr" } ).split( "\t" );
	direct[55] = "item/etc/drop_ch_bag.bsr";
	const disabled = itemRow( { id: 4, code: "ITEM_MALL_BOOTH_DISABLED", model: "item/disabled.bsr", enabled: false } );
	await fs.writeFile(
		path.join( root, "itemdata_1.txt" ),
		[ base, linked, direct.join( "\t" ), disabled ].join( "\n" ),
		"utf16le"
	);
	const roster = loadBoothModelRoster( root );
	assert.equal( roster.length, DEFAULT_BOOTH_MODELS.length + 1 );
	for ( const bsr of DEFAULT_BOOTH_MODELS ) assert.ok( roster.some( row => row.bsrPath === bsr ) );
	const custom = roster.find( row => row.bsrPath === "res/item/avatar/booth.bsr" );
	assert.deepEqual( custom.fields.requiredBy, [ "ITEM_MALL_BOOTH_LINKED", "ITEM_MALL_BOOTH_DIRECT" ] );
	assert.equal( custom.isMob, false );
	assert.deepEqual( custom.requiredStates, [] );
	assert.throws(
		() => collectBoothModelRoster( new Map( [ [ 1, { code: "ITEM_MALL_BOOTH_BROKEN", resolvedModel: null } ] ] ) ),
		/no item model/
	);
	assert.throws(
		() =>
			collectBoothModelRoster(
				new Map( [ [ 1, { code: "ITEM_MALL_BOOTH_ESCAPE", resolvedModel: "res/../escape.bsr" } ] ] )
			),
		/Unsafe BSR/
	);
});

test("secondary booths retain resource metadata, share one bake and have one pack owner", async t => {
	const root = await fixtureRoot( t );
	const calls = [];
	const context = bakeContext( root, async ( bsr, output, isMob, requiredStates ) => {
		calls.push( { bsr, isMob, requiredStates } );
		await fs.mkdir( path.dirname( output.diskPath ), { recursive: true } );
		await fs.writeFile( output.diskPath, "fixture glb" );
		return {
			glb: output.publicPath,
			bytes: 11,
			clips: [ "stand" ],
			allowedClips: [ "stand" ],
			animationStates: { stand: { stateId: 0, durationMs: 1000 } },
			particleModifiers: [ { entries: [ { effectPath: "system/booth.efp" } ] } ],
			retailAnimationCatalog: { default: [] }
		};
	} );
	const requests = collectBoothModelRoster( new Map() );
	const counts = await bakeNpcSecondaryResources( context, requests );
	assert.deepEqual( counts, { built: 2, covered: 2, reused: 0 } );
	assert.ok( calls.every( call => !call.isMob && call.requiredStates.length === 0 ) );
	const reused = await bakeNpcSecondaryResources( context, [ requests[0] ] );
	assert.deepEqual( reused, { built: 0, covered: 1, reused: 1 } );
	assert.equal( calls.length, 2 );
	const manifest = splitNpcManifestModels( context.models );
	assert.equal( Object.keys( manifest.resources ).length, 0 );
	assert.equal( Object.keys( manifest.boothModels ).length, 2 );
	const joined = npcManifestModels( JSON.parse( JSON.stringify( manifest ) ) );
	for ( const request of requests ) {
		const resource = manifest.boothModels[request.bsrPath];
		assert.equal(
			resource.glb,
			resourceGlbOutput( request.bsrPath, { namespace: "npc", publicAssetsRoot: context.publicAssetsRoot } )
				.publicPath
		);
		assert.deepEqual( joined[request.bsrPath], context.models.find( row => row.bsr === request.bsrPath ) );
		assert.equal( resource.animationStates.stand.stateId, 0 );
		assert.equal( resource.particleModifiers[0].entries[0].effectPath, "system/booth.efp" );
		assert.equal( resource.retailAnimationCatalog, undefined );
	}
	const manifestPath = path.join( context.publicAssetsRoot, "npc", "manifest.json" );
	await fs.writeFile( manifestPath, JSON.stringify( manifest ) );
	assert.deepEqual(
		readPreviousResourceGlbPaths( manifestPath ).sort(),
		Object.values( manifest.boothModels ).map( row => row.glb ).sort()
	);
	for ( const directory of [ "char/vat", "npc/vat", "anim", "audio", "textdata" ] ) {
		await fs.mkdir( path.join( context.publicAssetsRoot, directory ), { recursive: true } );
	}
	const { groups } = await collectAssetPackGroups( {
		publicRoot: root,
		uiImagePreloadPaths: [],
		missionMinimapTilePaths: [],
		includeOutdoorWorld: false
	} );
	for ( const resource of Object.values( manifest.boothModels ) ) {
		assert.deepEqual( groups.filter( group => group.files.includes( resource.glb ) ).map( group => group.name ), [
			"game-models"
		] );
	}
});

test("failed booth bakes remain failures and cannot become empty successful resources", async t => {
	const root = await fixtureRoot( t );
	const context = bakeContext( root, async () => {
		throw new Error( "missing booth mesh" );
	} );
	const counts = await bakeNpcSecondaryResources( context, collectBoothModelRoster( new Map() ) );
	assert.deepEqual( counts, { built: 0, covered: 0, reused: 0 } );
	assert.ok( context.models.every( row => row.error === "missing booth mesh" ) );
	assert.deepEqual( splitNpcManifestModels( context.models ).boothModels, {} );
});

test("secondary reuse rejects an incompatible primary animation policy", async t => {
	const root = await fixtureRoot( t );
	const context = bakeContext( root, async () => {
		assert.fail( "must reject before baking" );
	} );
	const request = collectBoothModelRoster( new Map() )[0];
	context.bakedByBsr.set( request.bsrPath, { isMob: true, baked: { glb: "/assets/npc/body.glb" } } );
	const counts = await bakeNpcSecondaryResources( context, [ request ] );
	assert.deepEqual( counts, { built: 0, covered: 0, reused: 0 } );
	assert.match( context.models[0].error, /incompatible clip policy/ );
});
