/*
===========================================================================

character-bounds-admission.test.mjs - clip catalog growth and frustum admission

Exercise the renderer against an independently posed triangle. Catalog growth
must cover new root motion on sources, existing assemblies and borrowed models.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const { createCharacterPose } = await import( "../../src/engine/foundation/animation/animation-pose.ts" );
const { bindNativeClip } = await import( "../../src/engine/foundation/animation/native-clip.ts" );
const { geometryVertex } = await import( "../../src/engine/foundation/rendering/picking.ts" );
const { identity, placement } = await import( "../../src/engine/foundation/rendering/world-math.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );

const ORIGIN_REGION = 257;
const ROOT_DISTANCE = 10;
const TRIANGLE_HALF_SIZE = .25;
const MATRIX_FLOATS = 16;
const TRIANGLE_VERTICES = 3;

/*
================
fixtureModel
================
*/
/** @returns {import("../../src/engine/contracts/character.ts").CharacterModel} */
function fixtureModel() {
	return {
		nodes: [ {
			name: "root",
			parent: -1,
			translation: [ 0, 0, 0 ],
			rotation: [ 0, 0, 0, 1 ],
			scale: [ 1, 1, 1 ]
		} ],
		clips: [ { name: "stand", duration: 1, channels: [] } ],
		images: [],
		primitives: [ {
			name: "body",
			node: 0,
			image: -1,
			joints: [ 0 ],
			inverseBind: identity(),
			geometry: {
				positions: Float32Array.of(
					-TRIANGLE_HALF_SIZE,
					-TRIANGLE_HALF_SIZE,
					0,
					TRIANGLE_HALF_SIZE,
					-TRIANGLE_HALF_SIZE,
					0,
					0,
					TRIANGLE_HALF_SIZE,
					0
				),
				joints: new Uint32Array( 12 ),
				weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ),
				indices: Uint32Array.of( 0, 1, 2 ),
				transform: identity()
			}
		} ]
	};
}

/*
================
translationClip
================
*/
/** @returns {import("../../src/engine/foundation/animation/native-clip.ts").NativeClip} */
function translationClip( distance ) {
	return {
		duration: 1,
		channels: [ {
			bone: "root",
			path: "translation",
			interpolation: "LINEAR",
			times: Float32Array.of( 0, 1 ),
			values: Float32Array.of( 0, 0, 0, distance, 0, 0 )
		} ]
	};
}

/*
================
captureGeometry

Only CPU palette and instance capabilities are needed by this fixture.
================
*/
/** @returns {any} */
function captureGeometry() {
	return {
		/*
		================
		upload
		================
		*/
		upload( data ) {
			return {
				bones: data.bones.slice(),
				instances: data.instances.slice(),
				instanceCount: data.instances.length / MATRIX_FLOATS,
				indexCount: data.indices.length
			};
		},
		/*
		================
		updateInstances
		================
		*/
		updateInstances( draw, instances ) {
			draw.instances = instances.slice();
			draw.instanceCount = instances.length / MATRIX_FLOATS;
			return draw;
		},
		/*
		================
		updateBones
		================
		*/
		updateBones( draw, bones ) {
			draw.bones = bones.slice();
		},
		/*
		================
		release
		================
		*/
		release() {}
	};
}

/*
================
assertInsideView

A strict clip-space oracle, independent of the renderer's sphere test.
All three vertices lie inside, so the nondegenerate triangle intersects.
================
*/
function assertInsideView( view, point ) {
	const [x, y, z] = point;
	const cx = view[0] * x + view[4] * y + view[8] * z + view[12];
	const cy = view[1] * x + view[5] * y + view[9] * z + view[13];
	const cz = view[2] * x + view[6] * y + view[10] * z + view[14];
	const cw = view[3] * x + view[7] * y + view[11] * z + view[15];
	assert.ok( cw > 0 && Math.abs( cx ) < cw && Math.abs( cy ) < cw && cz > 0 && cz < cw );
}

for ( const admission of [ "source", "assembly", "borrowed" ] ) {
	for ( const reflected of [ false, true ] ) {
		test(`catalog growth admits posed geometry: ${admission}, reflected=${reflected}`, () => {
			const renderer = createCharacters(), source = fixtureModel(), geometry = captureGeometry();
			/** @type {any} */
			const images = {};
			let model = source, modelId = "body";
			if ( admission === "borrowed" ) renderer.borrowModel( modelId, source, [] );
			else renderer.model( modelId, source, [] );
			if ( admission === "assembly" ) {
				modelId = "equipped";
				renderer.assembly( modelId, "body", [] );
			}
			const view = identity(), reflectedView = reflected ? identity() : undefined;
			if ( reflected ) view[12] = -ROOT_DISTANCE * 100;
			const oracleView = reflectedView ?? view;
			const actor = {
				gid: 1,
				model: modelId,
				pose: { regionId: ORIGIN_REGION, x: ROOT_DISTANCE, y: 0, z: .5, yaw: radians( 0 ) },
				clip: "stand",
				time: 1,
				loop: false,
				scale: 1
			};
			/*
			================
			extend
			================
			*/
			function extend( name, native ) {
				model = { ...model, clips: [ ...model.clips, bindNativeClip( native, name, model.nodes ) ] };
				if ( admission === "borrowed" ) renderer.extendBorrowedAnimations( modelId, model );
				else renderer.animation( "body", name, native );
			}
			try {
				renderer.actors( [ actor ] );
				assert.equal(
					renderer.prepare(
						geometry,
						images,
						ORIGIN_REGION,
						view,
						false,
						0,
						false,
						true,
						true,
						false,
						reflectedView
					)
						.length,
					0,
					"rest geometry is entirely outside both views"
				);
				extend( "reach", translationClip( -ROOT_DISTANCE * 2 ) );
				extend( "next", translationClip( 0 ) );
				// The actor names the next clip while an outgoing layer still reaches the view.
				/** @type {import("../../src/engine/contracts/character.ts").CharacterActor} */
				const displayed = {
					...actor,
					clip: "next",
					layers: [
						{ clip: "reach", time: 1, loop: false, weight: .5, lane: "event" },
						{ clip: "next", time: 1, loop: false, weight: 1, lane: "timed" }
					]
				};
				const primitive = model.primitives[0], pose = createCharacterPose( model );
				const palette = new Float32Array( primitive.joints.length * MATRIX_FLOATS );
				pose.evaluate( displayed.clip, displayed.time, displayed.loop, displayed.layers );
				pose.palette( primitive, palette );
				const instance = placement( ORIGIN_REGION, ORIGIN_REGION, ROOT_DISTANCE, 0, .5, radians( 0 ) );
				for ( let vertex = 0; vertex < TRIANGLE_VERTICES; vertex++ ) {
					assertInsideView( oracleView, geometryVertex( primitive.geometry, instance, vertex, palette ) );
				}
				renderer.actors( [ displayed ] );
				/** @type {any} */
				const draws = renderer.prepare(
					geometry,
					images,
					ORIGIN_REGION,
					view,
					false,
					1,
					false,
					true,
					true,
					false,
					reflectedView
				);
				assert.equal( draws.length, 1, "a posed triangle inside the view must survive admission" );
				for ( let vertex = 0; vertex < TRIANGLE_VERTICES; vertex++ ) {
					assertInsideView(
						oracleView,
						geometryVertex( primitive.geometry, draws[0].instances, vertex, draws[0].bones )
					);
				}
			} finally {
				renderer.dispose( geometry, images );
			}
		});
	}
}
