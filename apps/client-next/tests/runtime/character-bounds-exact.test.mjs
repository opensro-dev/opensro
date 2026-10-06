/*
===========================================================================

character-bounds-exact.test.mjs - tests for character-bounds.ts,
animation-pose.ts, picking.ts, model.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
const { characterRadius } = await import( "../../src/engine/foundation/animation/character-bounds.ts" );
const { createCharacterPose } = await import( "../../src/engine/foundation/animation/animation-pose.ts" );
const { geometryVertex } = await import( "../../src/engine/foundation/rendering/picking.ts" );
const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
test("identity skeleton levels do not compound fictitious scale", () => {
	const nodes = Array.from(
		{ length: 8 },
		( _, i ) => ({
			name: String( i ),
			parent: i - 1,
			matrix: identity(),
			translation: [ 0, 0, 0 ],
			rotation: [ 0, 0, 0, 1 ],
			scale: [ 1, 1, 1 ]
		})
	);
	const model = {
		nodes,
		clips: [],
		primitives: [ { joints: [ 7 ], inverseBind: identity(), geometry: { positions: Float32Array.of( 1, 0, 0 ) } } ]
	};
	const radius = characterRadius( model );
	assert.ok( radius >= 1 && radius < 1.0001 );
});
test("tight character envelopes contain skinned vertices through affine hierarchy and animation", () => {
	let seed = 341;
	const random = () => ((seed = (Math.imul( seed, 1664525 ) + 1013904223) >>> 0) / 2 ** 32);
	const affine = () => {
		const m = identity();
		for ( const i of [ 0, 1, 2, 4, 5, 6, 8, 9, 10 ] ) m[i] = random() * 3 - 1.5;
		for ( const i of [ 12, 13, 14 ] ) m[i] = random() * 4 - 2;
		return m;
	};
	for ( let trial = 0; trial < 50; trial++ ) {
		const nodes = Array.from(
			{ length: 4 },
			( _, i ) => ({
				name: String( i ),
				parent: i - 1,
				matrix: i === 1 ? undefined : affine(),
				translation: [ .1, .3, -.2 ],
				rotation: [ 0, 0, 0, 1 ],
				scale: [ 1, 1, 1 ]
			})
		);
		const geometry = {
			positions: Float32Array.from( { length: 60 }, () => random() * 4 - 2 ),
			joints: Uint32Array.from( { length: 80 }, ( _, i ) => i % 4 ),
			weights: Float32Array.from( { length: 80 }, () => .25 )
		};
		const primitive = {
			geometry,
			joints: [ 0, 1, 2, 3 ],
			inverseBind: Float32Array.from( nodes.flatMap( () => [ ...affine() ] ) )
		};
		const model = {
			nodes,
			primitives: [ primitive ],
			clips: [ {
				name: "turn",
				duration: 1,
				channels: [ {
					node: 1,
					path: "rotation",
					interpolation: "LINEAR",
					times: Float32Array.of( 0, 1 ),
					values: Float32Array.of( 0, 0, 0, 1, 0, 1, 0, 0 )
				} ]
			} ]
		};
		const radius = characterRadius( model ), pose = createCharacterPose( model ), bones = new Float32Array( 64 );
		for ( let frame = 0; frame <= 20; frame++ ) {
			pose.evaluate( "turn", frame / 20, false );
			pose.palette( primitive, bones );
			for ( let v = 0; v < 20; v++ ) {
				assert.ok(
					Math.hypot( ...geometryVertex( geometry, identity(), v, bones ) ) <= radius + 1e-4,
					`trial ${trial}, frame ${frame}, vertex ${v}`
				);
			}
		}
	}
});

test("joint-local bound does not count the bind offset twice", () => {
	const inverse = identity();
	inverse[13] = -100;
	const geometry = {
		positions: Float32Array.of( 0, 101, 0 ),
		joints: Uint32Array.of( 0, 0, 0, 0 ),
		weights: Float32Array.of( 1, 0, 0, 0 )
	};
	const model = {
		nodes: [ {
			name: "root",
			parent: -1,
			translation: [ 0, 100, 0 ],
			rotation: [ 0, 0, 0, 1 ],
			scale: [ 1, 1, 1 ]
		} ],
		clips: [],
		primitives: [ { joints: [ 0 ], inverseBind: inverse, geometry } ]
	};
	const radius = characterRadius( model );
	assert.ok( radius >= 101 && radius < 101.01, `radius ${radius}` );
});

test("published Mangnyang envelope contains every skinned vertex across every clip", async () => {
	const { createModelDecoder } = await import( "../../src/engine/runtime/assets/worker/model/model.ts" ),
		decoder = createModelDecoder();
	const model = decoder.character(
		decoder.decode(
			readPublishedAssetBytesSync(
				"/assets/npc/mob/china/mangnyang.glb",
				CLIENT_PUBLIC_ROOT
			)
		)
	);
	const radius = characterRadius( model ), pose = createCharacterPose( model );
	let checked = 0;
	for ( const clip of model.clips ) {
		for ( let sample = 0; sample <= 16; sample++ ) {
			pose.evaluate( clip.name, clip.duration * sample / 16, false );
			for ( const primitive of model.primitives ) {
				const bones = new Float32Array( primitive.joints.length * 16 );
				pose.palette( primitive, bones );
				for ( let v = 0; v < primitive.geometry.positions.length / 3; v++ ) {
					assert.ok(
						Math.hypot( ...geometryVertex( primitive.geometry, identity(), v, bones ) ) <= radius,
						`${clip.name}, ${sample}, ${v}`
					);
					checked++;
				}
			}
		}
	}
	assert.ok( checked > 10000 );
	assert.ok( radius < 93, "fixture must tighten the former 93.92-unit bound" );
});
