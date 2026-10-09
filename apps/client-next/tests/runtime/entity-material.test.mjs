/*
===========================================================================

entity-material.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetJsonSync, readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { publicRoot } from "../../../../scripts/build/world/paths.mjs";
import { loadDataAsset } from "../../../../scripts/build/shared/jmxAssetIO.mjs";
import { parseCharacterBsr } from "../../../../scripts/build/char/formats.mjs";
import { entityMaterialMetadata } from "../../../../scripts/build/char/entityMaterialMetadata.mjs";
import { npcManifestModels } from "../../../../scripts/build/shared/npcManifest.mjs";
async function load( file ) {
	return import( sourceFileUrl( file ).href );
}
const { createCharacterDecoder } = await load( "src/engine/runtime/assets/worker/model/character/character.ts" );
const { createCharacters } = await load( "src/engine/runtime/renderer/characters/characters.ts" );
const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );

test("every original entity material modifier binds through GLB publication and real decoder without altering binary payload", async () => {
	const seen = new Set();
	let changed = 0, textures = 0, timelines = 0;
	for ( const domain of [ "itemdrop", "npc" ] ) {
		for (
			const [key, row] of Object.entries(
				// The NPC manifest keeps each GLB on its BSR's resource (v9).
				npcManifestModels( readPublishedAssetJsonSync( "/assets/" + domain + "/manifest.json", publicRoot ) )
			)
		) {
			if ( seen.has( row.glb ) ) continue;
			seen.add( row.glb );
			const source = row.bsr ?? "res/" + key,
				bytes = await loadDataAsset( source ),
				bsr = parseCharacterBsr( bytes, source );
			if ( !bsr.materialModifiers.length && !bsr.textureModifiers.length ) continue;
			for (
				const [slot, path] of [
					[ domain === "npc" ? 0 : undefined, row.glb ],
					...Object.entries( row.materialVariants ?? {} ).map( ( [id, path] ) => [ Number( id ), path ] )
				]
			) {
				const original = readPublishedAssetBytesSync( path, publicRoot ),
					next = await entityMaterialMetadata( original, bytes, source, slot ),
					end = 20 + next.readUInt32LE( 12 );
				assert.deepEqual(
					next.subarray( end ),
					original.subarray( 20 + original.readUInt32LE( 12 ) ),
					path + " binary changed"
				);
				const json = JSON.parse( next.subarray( 20, end ) ), binary = next.subarray( end + 8 );
				const published = JSON.parse( original.subarray( 20, 20 + original.readUInt32LE( 12 ) ) );
				assert.deepEqual(
					published.materials.map( m => m.extras ),
					json.materials.map( m => m.extras ),
					path + " published modifier ownership differs from source"
				);
				const model = createCharacterDecoder().decode( {
					json,
					binary: binary.buffer.slice( binary.byteOffset, binary.byteOffset + binary.byteLength )
				} );
				for ( const p of model.primitives ) {
					textures += Number( !!p.geometry.material.uvVelocity );
					timelines += Number( !!p.geometry.material.colorTimeline );
				}
				changed++;
			}
		}
	}
	assert.ok( changed > 30 );
	assert.ok( textures >= 1 );
	assert.ok( timelines >= 2 );
});

test("production renderer owns independent modifier clocks across clip changes, disappearance and late admission", () => {
	const material = {
		color: [ 1, 1, 1, 1 ],
		ambient: [ 1, 1, 1 ],
		alphaCutoff: 0,
		blend: false,
		doubleSided: true,
		colorTimeline: {
			duration: 1000,
			mode: 0,
			flags: 2,
			colors: [ { time: 0, value: [ 1, 0, 0, 1 ] }, { time: 1000, value: [ 0, 1, 0, 1 ] } ]
		},
		uvVelocity: [ 0, 0, 0, 0, -.08, .02 ]
	};
	const model = {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		images: [],
		clips: [ "stand", "run" ].map( name => ({ name, duration: 1, channels: [] }) ),
		primitives: [ {
			name: "mesh",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			geometry: {
				positions: new Float32Array( 9 ),
				indices: Uint32Array.of( 0, 1, 2 ),
				transform: identity(),
				material
			}
		} ]
	};
	const owner = createCharacters();
	owner.model( "model", model, [] );
	const actor = ( gid, extra = {} ) => ({
		gid,
		modifierId: gid,
		model: "model",
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
		scale: 1,
		clip: "stand",
		time: 0,
		loop: true,
		...extra
	});
	const gpu = {
		upload() {
			return {};
		},
		release() {},
		updateInstances: d => d,
		updateBones() {},
		updateMaterialColors( draw, rgb ) {
			draw.rgb = [ ...rgb ];
		},
		updateTextureTransform( draw, matrix ) {
			draw.uv = [ ...matrix ];
		}
	};
	owner.actors( [ actor( 1 ) ] );
	owner.prepare( gpu, {}, 257, undefined, false, 10 );
	owner.actors( [ actor( 1, { clip: "run", time: 0 } ), actor( 2 ) ] );
	let draws = owner.prepare( gpu, {}, 257, undefined, false, 10.5 );
	assert.equal( draws.length, 2 );
	assert.deepEqual( draws.map( d => d.rgb ), [ [ .5, .5, 0 ], [ 1, 0, 0 ] ] );
	assert.ok( Math.abs( draws[0].uv[2] + .04 ) < 1e-6 );
	assert.equal( draws[1].uv[2], 0 );
	owner.actors( [ actor( 9, { modifierId: 1, opacity: .5 } ) ] );
	draws = owner.prepare( gpu, {}, 257, undefined, false, 10.75 );
	assert.deepEqual( draws[0].rgb, [ .25, .75, 0 ] );
	assert.ok( Math.abs( draws[0].uv[2] + .06 ) < 1e-6 );
	owner.actors( [] );
	owner.prepare( gpu, {}, 257, undefined, false, 11 );
	owner.actors( [ actor( 1 ) ] );
	draws = owner.prepare( gpu, {}, 257, undefined, false, 11.5 );
	assert.deepEqual( draws[0].rgb, [ 1, 0, 0 ] );
	owner.dispose( gpu, null );
});

test("production modifier clocks consume capped native frame milliseconds without losing the fractional frame", async () => {
	const { createModelMaterialClocks } = await load( "src/engine/foundation/animation/model-material-clock.ts" );
	const material = {
		color: [ 1, 1, 1, 1 ],
		colorTimeline: {
			duration: 10000,
			mode: 2,
			flags: 2,
			colors: [ { time: 0, value: [ 1, 0, 0, 1 ] }, { time: 10000, value: [ 0, 1, 0, 1 ] } ]
		},
		uvVelocity: [ 0, 0, 0, 0, 1, 0 ]
	};
	const model = { primitives: [ { geometry: { material } } ] },
		actor = { gid: 1, model: "body" },
		owner = createModelMaterialClocks();
	owner.step( [ actor ], 0, () => model );
	for ( let frame = 1; frame <= 240; frame++ ) owner.step( [ actor ], frame / 240, () => model );
	// Native publishes 4 ms per frame at 240 Hz: 960 ms after 240 updates.
	let clock = owner.get( actor )[0];
	assert.ok( Math.abs( clock.color.rgb[1] - .1 ) < 1e-7 );
	assert.ok( Math.abs( clock.texture.matrix[2] - 1 ) < 1e-5 );
	owner.step( [ actor ], 11, () => model );
	clock = owner.get( actor )[0];
	assert.ok( Math.abs( clock.color.rgb[1] - .4 ) < 1e-7 );
	assert.ok( Math.abs( clock.texture.matrix[2] - 4 ) < 1e-5 );
	owner.step( [ actor ], 10, () => model );
	assert.ok( Math.abs( clock.color.rgb[1] - .4 ) < 1e-7 );
	owner.step( [ actor ], 10.5, () => model );
	assert.ok( Math.abs( clock.color.rgb[1] - .45 ) < 1e-7 );
	owner.reset();
	assert.equal( owner.bytes(), 0 );
});

test("production animation material queue retains clocks through blends, applies LOD, and restores the base pipeline", () => {
	const base = { color: [ 1, 1, 1, 1 ], ambient: [ 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true };
	const make = ( state, colors ) => ({
		kind: 1,
		stateId: state,
		animationSetName: "default",
		baseWords: [ 1056964608, 1, 272, 4294967295, 0, 0 ],
		baseBytes: [ 0, 0, 0, 0 ],
		field24: 1000,
		flags: 2,
		mode: 0,
		colors,
		scalars: [],
		words50: [ 0, 0, 0, 0 ],
		bytes60: Array( 16 ).fill( 0 ),
		field70: 0
	});
	const modifiers = {
		materialModifiers: [
			make( 0, [ { time: 0, value: [ 1, 0, 0, 1 ] }, { time: 1000, value: [ 0, 1, 0, 1 ] } ] ),
			make( 2, [ { time: 0, value: [ 0, 0, 1, 1 ] } ] )
		],
		textureModifiers: []
	};
	const m = {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		images: [],
		clips: [ { name: "stand", duration: 1, channels: [] } ],
		primitives: [ {
			name: "mesh",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			modifierSource: { material: base, index: 0, modifiers },
			geometry: {
				positions: new Float32Array( 9 ),
				indices: Uint32Array.of( 0, 1, 2 ),
				transform: identity(),
				material: base
			}
		} ]
	};
	const owner = createCharacters();
	owner.model( "m", m, [] );
	let revision = 0;
	const actor = ( state, lod = 0 ) => ({
		gid: 1,
		model: "m",
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
		scale: 1,
		clip: "stand",
		time: 0,
		loop: true,
		animationLod: { fraction: lod, crowded: false },
		modelAnimation: {
			selected: state === null ? null : { set: "default", state },
			revision: revision++,
			restarted: [],
			dispatch: []
		}
	});
	const gpu = {
		upload( data ) {
			return { rgb: data.material.color.slice( 0, 3 ) };
		},
		release() {},
		updateInstances: d => d,
		updateBones() {},
		updateMaterialColors( draw, rgb ) {
			draw.rgb = [ ...rgb ];
		},
		updateTextureTransform() {}
	};
	const step = ( time, state, lod ) => {
		owner.actors( [ actor( state, lod ) ] );
		return owner.prepare( gpu, {}, 257, undefined, false, time )[0].rgb;
	};
	assert.deepEqual( step( 0, 0 ), [ 1, 0, 0 ] );
	assert.deepEqual( step( .1, 0 ), [ Math.fround( .9 ), Math.fround( .1 ), 0 ] );
	assert.deepEqual( step( .2, 2 ), [ 0, 0, 1 ] );
	assert.deepEqual( step( .4, 0 ), [ Math.fround( .7 ), Math.fround( .3 ), 0 ] );
	assert.deepEqual( step( .5, 0, .6 ), [ 1, 1, 1 ] );
	assert.deepEqual( step( .55, 0, 0 ), [ Math.fround( .65 ), Math.fround( .35 ), 0 ] );
	assert.deepEqual( step( .6, null ), [ 1, 1, 1 ] );
	owner.dispose( gpu, null );
});

test("animation material color lanes advance independently and an override set suspends ambient clocks", async () => {
	const { createAnimatedMaterial } = await load( "src/engine/foundation/animation/animated-material.ts" );
	const make = ( kind, flags, colors ) => ({
		kind,
		stateId: kind === 2 ? -1 : 0,
		animationSetName: kind === 2 ? "ambient" : "default",
		baseWords: [ 1056964608, 1, 272, 4294967295, 0, 0 ],
		baseBytes: [ 0, 0, 0, 0 ],
		field24: 1000,
		flags,
		mode: 0,
		colors,
		words50: [ 0, 0, 0, 0 ],
		bytes60: Array( 16 ).fill( 0 ),
		field70: 0
	});
	const primitive = {
		modifierSource: {
			index: 0,
			material: { color: [ 1, 1, 1, 1 ] },
			modifiers: {
				materialModifiers: [
					make( 2, 1, [ { time: 0, value: [ 1, 0, 0, 1 ] }, { time: 1000, value: [ 0, 1, 0, 1 ] } ] ),
					make( 1, 2, [ { time: 0, value: [ 0, 0, 1, 1 ] }, { time: 1000, value: [ 0, 1, 0, 1 ] } ] )
				],
				textureModifiers: []
			}
		}
	};
	const owner = createAnimatedMaterial(), actor = { modelAnimation: { selected: { set: "default", state: 0 } } };
	owner.begin( 0 );
	owner.sample( primitive, actor );
	owner.begin( .1 );
	let result = owner.sample( primitive, actor );
	assert.deepEqual( result.colors.map( c => c.flags ), [ 1, 2 ] );
	assert.deepEqual( result.colors.map( c => [ ...c.rgb ] ), [ [ Math.fround( .9 ), Math.fround( .1 ), 0 ], [
		0,
		Math.fround( .1 ),
		Math.fround( .9 )
	] ] );
	actor.modelAnimation.selected.override = true;
	owner.begin( .2 );
	assert.deepEqual( owner.sample( primitive, actor ).colors.map( c => c.flags ), [ 2 ] );
	actor.modelAnimation.selected.override = false;
	owner.begin( .3 );
	result = owner.sample( primitive, actor );
	assert.equal( result.colors[0].rgb[1], Math.fround( .2 ) );
	assert.equal( result.colors[1].rgb[1], Math.fround( .3 ) );
});

test("modifier clock carries the fractional millisecond at every refresh rate", async () => {
	const { createModifierDelta } = await load( "src/engine/foundation/rendering/modifier-delta.ts" );
	for ( const [hz, lo, hi] of [ [ 60, 16, 17 ], [ 144, 6, 7 ], [ 240, 4, 5 ] ] ) {
		const tick = createModifierDelta();
		assert.equal( tick( 0 ), 0 );
		let total = 0;
		for ( let frame = 1; frame <= hz; frame++ ) {
			const ms = tick( frame / hz );
			assert.ok( ms === lo || ms === hi, `${hz}Hz published ${ms}` );
			total += ms;
		}
		// One real second of frames publishes one real second of milliseconds, at any
		// refresh rate. Truncating each frame independently loses 4% here and 14% at
		// 144 Hz, which is slower than the original rather than equal to it.
		assert.equal( total, 1000 );
		assert.equal( tick( 11 ), 3000 );
		assert.equal( tick( 10 ), 0 );
		assert.equal( tick( 10.5 ), 500 );
		assert.throws( () => tick( NaN ), /Invalid modifier clock/ );
		assert.equal( tick( 11 ), 500, "invalid input must not poison the clock" );
	}
});
