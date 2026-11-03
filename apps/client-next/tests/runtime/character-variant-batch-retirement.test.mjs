/*
===========================================================================

character-variant-batch-retirement.test.mjs - a variant batch follows its model

A fading, deferred, tinted or glowing actor batches under "<model>\0<variant>",
not under the model id. Model retirement must reclaim those variant batches
too: one that outlives its model keeps a GPU animation row bound to the
retired model's identity, so the re-admitted model trips the stream identity
guard the device owner enforces, and its draws strand GPU resources.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const { createGeometryResources } = await import( "../../src/engine/runtime/renderer/device/geometry.ts" );
const { createGpuAnimationResources } = await import( "../../src/engine/runtime/renderer/device/animation.ts" );
const { createStrictGpu, GPU_BUFFER_USAGE, GPU_TEXTURE_USAGE } = await import( "../helpers/strict-gpu.mjs" );
const { identity } = await import( "../../src/engine/foundation/rendering/world-math.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );
const { CHARACTER_MODELS } = await import( "../../src/engine/foundation/animation/character-budget.ts" );

/*
================
model

One skinned primitive on a single GPU-eligible clip. A rebuilt assembly
shares the body's primitive objects while its node table is a fresh array:
exactly the replacement a palette row bound to the retired assembly rejects.
================
*/
/** @returns {import("../../src/engine/contracts/character.ts").CharacterModel} */
function model( primitiveName ) {
	return {
		nodes: [ {
			name: "root",
			parent: -1,
			translation: [ 0, 0, 0 ],
			rotation: [ 0, 0, 0, 1 ],
			scale: [ 1, 1, 1 ]
		} ],
		images: [],
		clips: [ {
			name: "stand",
			duration: 1,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "STEP",
				times: Float32Array.of( 0, .5, 1 ),
				values: Float32Array.of( 0, 0, 0, 1, 0, 0, 2, 0, 0 )
			} ]
		} ],
		primitives: [ {
			name: primitiveName,
			node: 0,
			image: -1,
			joints: [ 0 ],
			inverseBind: identity(),
			geometry: {
				positions: new Float32Array( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				joints: new Uint32Array( 12 ),
				weights: new Float32Array( [ 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ] ),
				transform: identity()
			}
		} ]
	};
}

/*
================
deviceFixture

The real geometry and GPU animation owners on the strict fake device, so the
palette and stream identity contracts are the shipped ones.
================
*/
function deviceFixture() {
	globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;
	globalThis.GPUTextureUsage = GPU_TEXTURE_USAGE;
	const fake = createStrictGpu(), device = /** @type {GPUDevice} */ (/** @type {unknown} */ (fake.device));
	const pipeline = /** @type {GPURenderPipeline} */ (/** @type {unknown} */ ({
		getBindGroupLayout() {
			return {};
		}
	}));
	const animation = createGpuAnimationResources( device );
	const environment = device.createBuffer( { size: 16, usage: GPU_BUFFER_USAGE.UNIFORM } );
	const resources = createGeometryResources(
		device,
		() => device,
		error => {
			throw error;
		},
		() => pipeline,
		{
			texture() {
				throw Error( "Fixture has no image assets" );
			},
			acquire() {},
			drop() {}
		},
		device.createSampler(),
		device.createSampler(),
		environment,
		undefined,
		animation
	);
	return {
		commands: resources.commands,
		dispose: () => {
			resources.dispose();
			environment.destroy();
		},
		animation,
		live: fake.live
	};
}

test("a variant batch is reclaimed when residency drops its model", async () => {
	const { commands, dispose, animation, live } = deviceFixture();
	await animation.ready;
	const owner = createCharacters(),
		images = {
			upload() {
				throw Error( "Fixture has no image assets" );
			},
			release() {}
		};
	const attach = { model: "weapon", parts: [ "blade" ], covers: [] };
	const frame = ( gid, time ) => {
		owner.actors( [ {
			gid,
			model: "assembled",
			clip: "stand",
			time,
			loop: true,
			scale: 1,
			pose: { regionId: 257, x: 0, y: 0, z: 100, yaw: radians( 0 ) },
			opacity: .5
		} ] );
		return owner.prepare( commands, images, 257, undefined, false, time, false, true, true, true ).length;
	};
	try {
		owner.retain( [ "body", "weapon" ] );
		owner.model( "body", model( "body" ), [] );
		owner.model( "weapon", model( "blade" ), [] );
		owner.assembly( "assembled", "body", [ attach ] );
		// The first frame uploads the batch; the second admits its GPU row.
		assert.equal( frame( 1, 0 ), 2, "the assembled body and blade draw" );
		assert.equal( frame( 1, .6 ), 2 );
		assert.equal( animation.stats().streams, 1, "the fading batch holds one GPU animation row" );
		// The actor despawns; the next admission under the owned-model budget
		// reclaims residency between frames, where no prepare pass runs.
		owner.actors( [] );
		for ( let i = 0; i < CHARACTER_MODELS; i++ ) {
			owner.model( "pressure-" + i, model( "body" ), [] );
		}
		assert.equal( owner.hasModel( "assembled" ), false, "residency dropped the assembly" );
		// The same assembly returns with a rebuilt node table; its still-fading
		// actor must not meet the retired model's palette row.
		owner.assembly( "assembled", "body", [ attach ] );
		assert.equal( frame( 2, 0 ), 2, "the respawned assembly draws again" );
		assert.equal( frame( 2, .6 ), 2 );
		assert.equal( animation.stats().models, 1, "one admitted skeleton serves both generations" );
		assert.equal( animation.stats().streams, 1, "the retired generation left no animation row behind" );
	} finally {
		owner.dispose( commands, null );
		dispose();
	}
	assert.equal( live(), 0, "every GPU resource the fixture created is destroyed" );
});
