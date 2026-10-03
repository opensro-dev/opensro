/*
===========================================================================

action-refresh.test.mjs - tests for pipelines.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
async function load( file ) {
	return import( sourceFileUrl( "src/engine/foundation/" + file + ".ts" ).href );
}
const { movementEntryRate, refreshActionStates, transitionActionStates } = await load( "animation/action-refresh" );
const { defaultWearFrozen, refreshDefaultWear } = await load( "animation/default-wear-policy" );
const { footprintGeometry } = await load( "rendering/footprints" );
const trace = row =>
	row.effects.map( e =>
		e.kind === "enter" || e.kind === "leave" ?
			e.kind + e.state :
			e.kind === "navigation" ?
			"nav" + Number( e.enabled ) :
			e.kind
	);
test("move entry consumes waypoint correction rate with native cap and directional catchup", () => {
	const p = ( x, z = 0 ) => ({ regionId: 1, x, y: 0, z, angle: 0 }), path = { from: p( 0 ), to: p( 10 ) };
	assert.equal( movementEntryRate( p( 0 ), p( 0 ), path, true ), 1 );
	assert.equal( movementEntryRate( p( -20 ), p( 0 ), path, true ), Math.fround( 1.3 ) );
	assert.equal( movementEntryRate( p( -5 ), p( 0 ), path, false ), Math.fround( 1.1 ) );
	assert.equal( movementEntryRate( p( 5 ), p( 0 ), path, false ), Math.fround( .9 ) );
	assert.equal( movementEntryRate( p( 0, 5 ), p( 0 ), path, false ), 1 );
	assert.equal( movementEntryRate( p( -4.99 ), p( 0 ), path, false ), 1 );
	assert.equal( movementEntryRate( p( -1 ), p( 0 ), { from: p( 0 ), to: p( 0 ) }, true ), Math.fround( 1.3 ) );
});
test("8EA1A0 moving refresh uses live bits and rejects the early movement reentry", () => {
	const row = refreshActionStates( 0x208, true, true );
	assert.deepEqual( trace( row ), [
		"leave9",
		"enter8",
		"feet",
		"nav0",
		"leave3",
		"leave8",
		"commit",
		"enter3",
		"enter8"
	] );
	assert.equal( row.mask, 0x108 );
	assert.equal( row.navigation, false );
	const next = transitionActionStates( row.mask, true, [ { kind: "enter", state: 9 } ] );
	assert.equal( next.mask, 0x208 );
	assert.equal( next.navigation, true );
	assert.deepEqual( trace( next ), [ "leave8", "enter9", "nav1", "enter9", "nav1" ] );
});
test("standing and sitting refreshes preserve native entry history without replaying posture transitions", () => {
	const standing = refreshActionStates( 0x108, false, true ), sitting = refreshActionStates( 0x40, false, true );
	assert.deepEqual( trace( standing ), [ "leave3", "leave8", "commit", "enter3", "enter8" ] );
	assert.deepEqual( trace( sitting ), [ "leave6", "commit", "enter6" ] );
	assert.equal( sitting.effects.at( -1 ).previous, 0 );
	assert.equal( standing.effects.find( e => e.kind === "enter" && e.state === 3 ).previous, 0 );
	assert.deepEqual( trace( transitionActionStates( 0x108, false, [ { kind: "enter", state: 6 } ] ) ), [
		"leave3",
		"leave8",
		"enter6"
	] );
	assert.equal(
		transitionActionStates( 0x108, false, [ { kind: "enter", state: 6 } ] ).effects.at( -1 ).previous,
		0x108
	);
});
test("finite exhaustive callback sequences keep legal base states and unchanged refresh has no effects", () => {
	const operations = [ { kind: "enter", state: 3 }, { kind: "enter", state: 6 }, { kind: "enter", state: 9 }, {
		kind: "leave",
		state: 9
	}, { kind: "refresh" } ];
	for ( let code = 0; code < 3125; code++ ) {
		for ( const available of [ false, true ] ) {
			let mask = 0x108, n = code;
			for ( let i = 0; i < 5; i++ ) {
				const command = operations[n % 5];
				n = Math.floor( n / 5 );
				const row = transitionActionStates( mask, available, [ command ] );
				mask = row.mask;
				assert.notEqual( mask & 0x300, 0x300, "unreachable simultaneous idle and movement" );
				assert.ok( !(mask & 0x40) || !(mask & 8) );
				const unchanged = refreshActionStates( mask, available, false );
				assert.equal( unchanged.mask, mask );
				assert.deepEqual( unchanged.effects, [] );
			}
		}
	}
});
test("native language gate uses original case-sensitive shard marker and preserves committed handles", () => {
	for ( let language = 1; language <= 5; language++ ) {
		for ( const name of [ undefined, "Normal", "Server#$T", "#$t" ] ) {
			assert.equal( defaultWearFrozen( language, name ), false );
		}
	}
	for ( const name of [ "", "Normal", "#$t", "#$ T" ] ) assert.equal( defaultWearFrozen( 0, name ), true );
	for ( const name of [ "#$T", "Server#$Tprivate", "A#$T#$T" ] ) assert.equal( defaultWearFrozen( 0, name ), false );
	assert.throws( () => defaultWearFrozen( 0, undefined ) );
	assert.throws( () => defaultWearFrozen( 6, "Server" ) );
	const previous = [ "clothes_BA", "clothes_LA" ], desired = [ "light_BA", "light_LA" ];
	assert.equal( refreshDefaultWear( previous, desired, true ), previous );
	assert.equal( refreshDefaultWear( previous, desired, false ), desired );
	assert.deepEqual( refreshDefaultWear( [], desired, true ), [] );
});
test("footprints clip real terrain, rotate, mirror only U and reject absent/high terrain", () => {
	const cells = new Map( [ [ "0:0", { cell: [ 0, 0 ], heights: new Float32Array( 289 ) } ] ] ),
		point = [ 150, 0, 150 ];
	for ( const yaw of [ 0, Math.PI / 2, .07853981852531433 ] ) {
		const left = footprintGeometry( cells, point, yaw, false ),
			right = footprintGeometry( cells, point, yaw, true );
		assert.ok( left.indices.length );
		assert.deepEqual( left.positions, right.positions );
		for ( let i = 0; i < left.uvs.length; i += 2 ) {
			assert.ok( Math.abs( left.uvs[i] + right.uvs[i] - 1 ) < 1e-6 );
			assert.equal( left.uvs[i + 1], right.uvs[i + 1] );
		}
		for ( const v of left.uvs ) assert.ok( v >= -1e-5 && v <= 1.00001 );
	}
	assert.equal( footprintGeometry( new Map(), point, 0, false ), null );
	assert.equal( footprintGeometry( cells, [ 150, 60, 150 ], 0, false ), null );
});

test("ground decals disable depth writes without changing selection or ordinary alpha pipelines", async () => {
	const { createPipelines, DEFAULT_BLEND } = await import(
		sourceFileUrl( "src/engine/runtime/renderer/device/pipelines.ts" ).href
	);
	const { geometryPipelineState } = await import(
		sourceFileUrl( "src/engine/runtime/renderer/device/geometry.ts" ).href
	);
	const base = { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: true, doubleSided: false };
	// A ground decal blends normally, without the depth test or depth writes.
	assert.deepEqual( geometryPipelineState( { ...base, groundDecal: true } ), {
		blend: DEFAULT_BLEND,
		cull: true,
		depthWrite: false,
		depthCompare: "always"
	} );
	// An ordinary blended material keeps the depth test and leaves depth alone.
	assert.deepEqual( geometryPipelineState( base ), {
		blend: DEFAULT_BLEND,
		cull: true,
		depthWrite: false,
		depthCompare: "less-equal"
	} );
	// Opaque geometry writes depth.
	assert.equal( geometryPipelineState( { ...base, blend: false } ).depthWrite, true );
	const device = {
		createShaderModule: x => x,
		createSampler: x => x,
		createRenderPipelineAsync: async x => x,
		createRenderPipeline: x => x
	};
	const pipelines = createPipelines( device, "bgra8unorm" );
	await pipelines.ready;
	const decal = pipelines.geometry( geometryPipelineState( { ...base, groundDecal: true } ) );
	assert.equal( decal.depthStencil.depthCompare, "always" );
	assert.equal( decal.depthStencil.depthWriteEnabled, false );
	assert.equal( decal.fragment.targets[0].blend.color.srcFactor, "src-alpha" );
	// The same state is one pipeline.
	assert.equal( pipelines.geometry( geometryPipelineState( { ...base, groundDecal: true } ) ), decal );
});
