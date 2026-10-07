/*
===========================================================================

character-cloth-palette-fixture.mjs - rendered cloth and palette equivalence

Capture semantic GPU inputs in draw order, independent of storage aliasing.
The shared presentation RNG trace makes cloth scheduling changes observable.
The baseline runs the shipped renderer, with no source rewriting or private
renderer implementation.

===========================================================================
*/
import "./native-source-loader.mjs";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { clothVertexCase } from "./cloth-vertex-cases.mjs";
import { createStrictGpu, GPU_BUFFER_USAGE, GPU_TEXTURE_USAGE } from "./strict-gpu.mjs";
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { identity } = await import( "../../src/engine/foundation/rendering/world-math.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );
const { createGeometryResources } = await import( "../../src/engine/runtime/renderer/device/geometry.ts" );
const JOINTS = 8;
const FRAME_COUNT = 120;
const RANDOM_SEED = 0x1188;
const INSTANCE_FLOATS = 40;
const INSTANCE_PALETTE_OFFSET = 17;
const PRIMITIVE_NAMES = [ "body", "cloth-a", "body-alias", "distinct", "cloth-b" ];

/*
================
model

Two cloth pieces alias the body's palette. A fourth primitive deliberately
has a different inverse bind, so sharing cannot silently collapse all bones.
The second model reverses the canonical primitive's cloth/body ownership.
================
*/
/** @returns {import("../../src/engine/contracts/character.ts").CharacterModel} */
function model( clothFirst, tags ) {
	const inverseBind = new Float32Array( JOINTS * 16 );
	for ( let joint = 0; joint < JOINTS; joint++ ) inverseBind.set( identity(), joint * 16 );
	const fixture = clothVertexCase( true, true );
	const primitives = PRIMITIVE_NAMES.map( ( name, index ) => {
		const geometry = { ...fixture.primitive.geometry, positions: fixture.primitive.geometry.positions.slice() };
		// Model admission owns a copy of each array. Give the fixture pieces
		// distinct geometry so semantic identity survives that copy.
		geometry.positions[0] += index / 8;
		tags.set( geometry.positions[0], name );
		const binding = inverseBind.slice();
		if ( name === "distinct" ) binding[12] = .25;
		return {
			name,
			node: 0,
			image: -1,
			joints: Array.from( { length: JOINTS }, ( _, i ) => i ),
			inverseBind: binding,
			geometry,
			cloth: name.startsWith( "cloth" ) ? fixture.primitive.cloth : undefined
		};
	} );
	if ( clothFirst ) [primitives[0], primitives[1]] = [ primitives[1], primitives[0] ];
	return {
		nodes: Array.from( { length: JOINTS }, ( _, i ) => ({
			name: `bone-${i}`,
			parent: i ? 0 : -1,
			translation: [ i / 10, i / 20, 0 ],
			rotation: [ 0, 0, 0, 1 ],
			scale: [ 1, 1, 1 ]
		}) ),
		images: [],
		clips: [
			{ name: "stand", duration: 1, channels: [] },
			{
				name: "move",
				duration: 1,
				channels: [ {
					node: 0,
					path: "translation",
					interpolation: "LINEAR",
					times: Float32Array.of( 0, 1 ),
					values: Float32Array.of( 0, 0, 0, 1, 2, 3 )
				}, {
					node: 2,
					path: "translation",
					interpolation: "LINEAR",
					times: Float32Array.of( 0, 1 ),
					values: Float32Array.of( .2, .1, 0, -.3, .7, 1 )
				} ]
			}
		],
		primitives
	};
}

/*
================
deviceFixture

Use the real geometry resource owner. The strict device validates lifetimes;
the byte recorder observes actual queue writes and shader-addressed palettes.
================
*/
function deviceFixture() {
	globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;
	globalThis.GPUTextureUsage = GPU_TEXTURE_USAGE;
	const fake = createStrictGpu(), device = fake.device, bytes = new Map(), writes = [];
	const bind = device.createBindGroup.bind( device ), write = device.queue.writeBuffer.bind( device.queue );
	device.createBindGroup = options => ({ ...bind( options ), entries: options.entries });
	device.queue.writeBuffer = ( buffer, offset, data, start = 0, length = undefined ) => {
		write( buffer );
		// WebGPU uses element offsets for typed arrays and byte offsets for
		// ArrayBuffer/DataView. Preserve both forms used by the geometry owner.
		const unit = data.BYTES_PER_ELEMENT ?? 1;
		const source = ArrayBuffer.isView( data ) ?
			new Uint8Array( data.buffer, data.byteOffset, data.byteLength ) :
			new Uint8Array( data );
		const byteStart = start * unit;
		const byteLength = length === undefined ? source.byteLength - byteStart : length * unit;
		let target = bytes.get( buffer );
		if ( !target ) {
			target = new Uint8Array( buffer.size );
			bytes.set( buffer, target );
		}
		target.set( source.subarray( byteStart, byteStart + byteLength ), offset );
		writes.push( { buffer, length: byteLength } );
	};
	// The strict fake implements the operations this resource owner uses;
	// browser-only GPUDevice brand and adapter metadata are irrelevant here.
	const gpu = /** @type {GPUDevice} */ (/** @type {unknown} */ (device));
	const pipeline = /** @type {GPURenderPipeline} */ (/** @type {unknown} */ ({
		getBindGroupLayout() {
			return {};
		}
	}));
	const environment = gpu.createBuffer( { size: 16, usage: GPU_BUFFER_USAGE.UNIFORM } );
	const resources = createGeometryResources(
		gpu,
		() => gpu,
		error => {
			throw error;
		},
		() => pipeline,
		() => {
			throw Error( "Fixture has no image assets" );
		},
		gpu.createSampler(),
		gpu.createSampler(),
		environment,
		() => gpu.createSampler()
	);
	return { resources, bytes, writes, environment, live: fake.live };
}

/*
================
captureGeometry

Record the data a draw consumes. Palette offsets are expanded during hashing
so deduplicated buffers compare against ordinary per-instance palettes.
================
*/
function captureGeometry( tags, gpuAvailable ) {
	let gpuCalls = 0;
	let device = deviceFixture();
	const commands = {
		...device.resources.commands,
		/*
		================
		upload
		================
		*/
		upload( source, texture, offsets ) {
			assert.ok( tags.has( source.positions[0] ), "every upload belongs to a fixture primitive" );
			const deviceDraw = device.resources.commands.upload( source, texture, offsets );
			return {
				...deviceDraw,
				deviceDraw,
				tag: tags.get( source.positions[0] ),
				material: source.material,
				instances: source.instances.slice(),
				bones: source.bones?.slice(),
				offsets: offsets?.slice(),
				capturedVertices: undefined,
				fade: undefined,
				appearance: undefined,
				lights: undefined,
				transform: source.transform.slice()
			};
		},
		/*
		================
		updateInstances
		================
		*/
		updateInstances( draw, matrices, fade, appearance, lights, offsets ) {
			draw.deviceDraw = device.resources.commands.updateInstances(
				draw.deviceDraw,
				matrices,
				fade,
				appearance,
				lights,
				offsets
			);
			draw.instances = matrices.slice();
			draw.fade = fade?.slice();
			draw.appearance = appearance?.slice();
			draw.lights = lights?.slice();
			draw.offsets = offsets?.slice();
			return draw;
		},
		/*
		================
		updateBones
		================
		*/
		updateBones( draw, bones, revision ) {
			const uploaded = device.resources.commands.updateBones( draw.deviceDraw, bones, revision );
			draw.bones = bones.slice();
			return uploaded;
		},
		/*
		================
		writeVertices
		================
		*/
		writeVertices( draw, offset, vertices ) {
			device.resources.commands.writeVertices( draw.deviceDraw, offset, vertices );
			assert.equal( offset, 0 );
			draw.capturedVertices = vertices.slice();
		},
		/*
		================
		updateTransform
		================
		*/
		updateTransform( draw, matrix ) {
			device.resources.commands.updateTransform( draw.deviceDraw, matrix );
			draw.transform = matrix.slice();
		},
		/*
		================
		release
		================
		*/
		release( draw ) {
			device.resources.commands.release( draw.deviceDraw );
		},
		prepareGpuBones: /** @type {(() => boolean) | undefined} */ (undefined)
	};
	if ( gpuAvailable ) {
		commands.prepareGpuBones = () => {
			gpuCalls++;
			return false;
		};
	}
	return {
		commands,
		gpuCalls: () => gpuCalls,
		/*
		================
		begin
		================
		*/
		begin() {
			device.writes.length = 0;
		},
		/*
		================
		clothPlacementWrites
		================
		*/
		clothPlacementWrites( draws ) {
			const buffers = new Set(
				draws.filter( draw => draw.tag.startsWith( "cloth" ) ).map( draw =>
					draw.deviceDraw.binding.entries.find( entry => entry.binding === 1 ).resource.buffer
				)
			);
			return device.writes.filter( write => buffers.has( write.buffer ) ).length;
		},
		/*
		================
		verify

		Expand the shader's actual palette offsets and compare GPU buffer bytes
		with the semantic input captured above, then count real palette traffic.
		================
		*/
		verify( draws ) {
			const buffers = new Set();
			for ( const draw of draws ) {
				const entries = draw.deviceDraw.binding.entries;
				const storage = entries.find( entry => entry.binding === 1 ).resource.buffer;
				const packed = new Float32Array( device.bytes.get( storage ).buffer );
				assert.equal( draw.deviceDraw.instanceCount, draw.instances.length / 16 );
				for ( let row = 0; row < draw.deviceDraw.instanceCount; row++ ) {
					assert.deepEqual(
						packed.slice( row * INSTANCE_FLOATS, row * INSTANCE_FLOATS + 16 ),
						draw.instances.slice( row * 16, row * 16 + 16 )
					);
				}
				if ( !draw.bones ) continue;
				const palette = entries.find( entry => entry.binding === 7 ).resource.buffer;
				const uploaded = new Float32Array( device.bytes.get( palette ).buffer );
				buffers.add( palette );
				for ( let row = 0; row < draw.deviceDraw.instanceCount; row++ ) {
					const offset = packed[row * INSTANCE_FLOATS + INSTANCE_PALETTE_OFFSET] * 16;
					const expected = (draw.offsets?.[row] ?? row * JOINTS) * 16;
					assert.deepEqual(
						uploaded.slice( offset, offset + JOINTS * 16 ),
						draw.bones.slice( expected, expected + JOINTS * 16 )
					);
				}
			}
			return device.writes.filter( write => buffers.has( write.buffer ) ).reduce(
				( sum, write ) => sum + write.length,
				0
			);
		},
		/*
		================
		invalidate
		================
		*/
		invalidate() {
			device.resources.dispose();
			device.environment.destroy();
			assert.equal( device.live(), 0 );
			device = deviceFixture();
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			device.resources.dispose();
			device.environment.destroy();
			assert.equal( device.live(), 0 );
		}
	};
}

/*
================
drawDigest

Hash float storage bytes, preserving signed zero and exact float32 results.
Draw order and authored material state are part of the semantic comparison.
================
*/
function drawDigest( draws, randomTrace ) {
	const hash = createHash( "sha256" );
	for ( const draw of draws ) {
		hash.update( JSON.stringify( { tag: draw.tag, material: draw.material } ) );
		for ( const key of [ "instances", "vertices", "fade", "appearance", "lights", "transform" ] ) {
			const values = key === "vertices" ? draw.capturedVertices : draw[key];
			hash.update( `${key}:${values?.byteLength ?? -1}:` );
			if ( values ) hash.update( Buffer.from( values.buffer, values.byteOffset, values.byteLength ) );
		}
		hash.update( draw.bones ? "bones:" : "no-bones:" );
		if ( draw.bones ) {
			for ( let instance = 0; instance < draw.instances.length / 16; instance++ ) {
				const offset = (draw.offsets?.[instance] ?? instance * JOINTS) * 16;
				const palette = draw.bones.subarray( offset, offset + JOINTS * 16 );
				assert.equal( palette.length, JOINTS * 16 );
				hash.update( Buffer.from( palette.buffer, palette.byteOffset, palette.byteLength ) );
			}
		}
	}
	hash.update( JSON.stringify( randomTrace ) );
	return hash.digest( "hex" );
}

/*
================
captureClothPalettes
================
*/
export function captureClothPalettes(
	gpuAvailable = false,
	admitUnusedClip = false,
	{ moving = false, variants = false } = {}
) {
	const random = createPresentationRandom( RANDOM_SEED, 1, 100000 );
	const owner = createCharacters( random ), tags = new Map();
	owner.model( "body-first", model( false, tags ), [] );
	owner.model( "cloth-first", model( true, tags ), [] );
	const geometry = captureGeometry( tags, gpuAvailable ), clock = clothVertexCase( true, true );
	const frames = [];
	let boneWriteBytes = 0;
	try {
		for ( let frame = 0; frame < FRAME_COUNT; frame++ ) {
			if ( admitUnusedClip && frame === 31 ) {
				owner.animation( "body-first", "new-action", {
					duration: 1,
					channels: [ {
						bone: "bone-1",
						path: "translation",
						interpolation: "LINEAR",
						times: Float32Array.of( 0, 1 ),
						values: Float32Array.of( 0, 0, 0, 1, 2, 3 )
					} ]
				} );
			}
			if ( frame === 60 ) {
				geometry.invalidate();
				owner.invalidate();
			}
			geometry.begin();
			const input = clock.input( frame );
			const preview = variants && frame % 17 < 5;
			const view = variants ? identity() : undefined;
			if ( view ) view[0] = view[5] = view[10] = .001;
			const gids = frame % 19 === 0 ? [] : frame % 3 === 0 ? [ 2, 3 ] : [ 1, 2, 3 ];
			/** @type {import("../../src/engine/contracts/character.ts").CharacterActor[]} */
			const actors = gids.map( gid => ({
				gid,
				model: frame % 13 === 0 && gid === 2 ? "cloth-first" : "body-first",
				clip: frame % 5 === 0 ? "stand" : "move",
				time: frame % 5 === 0 ? 0 : (frame * .031 + gid * .13) % 1,
				loop: true,
				scale: 1,
				pose: {
					regionId: 257,
					x: gid * 10 + (moving ? frame / 8 : 0),
					y: 0,
					z: 100,
					yaw: radians( gid / 10 + (moving ? frame / 200 : 0) )
				},
				opacity: frame % 11 < 3 && gid === 1 ? .5 : undefined,
				materialTint: frame % 7 === 0 ? [ .25, .5, .75 ] : undefined,
				pointLight: variants && frame % 9 < 4 ?
					{
						pose: { regionId: 257, x: frame, y: 5, z: 100 },
						ambient: [ .1, .2, .3 ],
						diffuse: [ .4, .5, .6 ],
						attenuation: .5,
						range: 100
					} :
					undefined,
				layers: frame % 7 === 0 ?
					[
						{ clip: "move", time: .2, loop: false, weight: .4, lane: "event" },
						{ clip: "move", time: frame * .031, loop: true, weight: 1, lane: "timed" }
					] :
					undefined
			}) );
			owner.actors( actors );
			const draws = /** @type {ReturnType<typeof geometry.commands.upload>[]} */ (owner.prepare(
				geometry.commands,
				{
					upload() {
						throw Error( "Fixture has no image assets" );
					},
					release() {}
				},
				257,
				view,
				preview,
				input.seconds,
				false,
				true,
				true,
				input.enabled
			));
			assert.equal( draws.length, gids.length * PRIMITIVE_NAMES.length );
			if ( variants ) {
				for ( const draw of draws ) {
					assert.equal(
						!!draw.lights,
						!preview && frame % 9 < 4,
						"light presence follows current render mode"
					);
					assert.deepEqual(
						draw.transform,
						preview ? view : identity(),
						"preview transforms cannot leak into world draws"
					);
				}
			}
			if ( moving ) {
				const expected = actors.map( actor => Math.fround( actor.pose.x ) ).sort( ( a, b ) => a - b );
				for ( const name of PRIMITIVE_NAMES ) {
					const actual = draws.filter( draw => draw.tag === name ).flatMap( draw =>
						Array.from(
							{ length: draw.instances.length / 16 },
							( _, row ) => draw.instances[row * 16 + 12]
						)
					).sort( ( a, b ) => a - b );
					assert.deepEqual( actual, expected, `frame ${frame}: ${name} uses current actor placements` );
				}
			}
			const trace = random.takeTrace();
			boneWriteBytes += geometry.verify( draws );
			frames.push( {
				clothPlacementWrites: geometry.clothPlacementWrites( draws ),
				poseEligibility: owner.stats( true ).poseEligibility,
				digest: drawDigest( draws, trace ),
				draws: draws.length,
				randomCalls: trace.length,
				primitives: Object.fromEntries(
					PRIMITIVE_NAMES.map( name => [ name, drawDigest( draws.filter( draw => draw.tag === name ), [] ) ] )
				)
			} );
		}
	} finally {
		owner.dispose( geometry.commands, null );
		geometry.dispose();
	}
	return { frames, gpuCalls: geometry.gpuCalls(), boneWriteBytes };
}
