/*
===========================================================================

character-cloth-palette-fixture.mjs - rendered cloth and palette equivalence

Capture semantic GPU inputs in draw order, independent of storage aliasing.
The shared presentation RNG trace makes cloth scheduling changes observable.
The baseline runs the shipped renderer, with no source rewriting or private
renderer implementation.

A skinned cloth draw is skinned on the GPU, with a cloth stream for the
vertices the solver owns (cloth-stream.ts). Its captured vertices are the
ones its vertex shader draws, composed the shader's way: the stream where
it says so, else the vertex skinned by the CPU's own arithmetic
(cloth-skin.ts), so they compare bit for bit with the baseline, which the
CPU drew. Its palette is consumed into those vertices, as before.

===========================================================================
*/
import "./native-source-loader.mjs";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { clothVertexCase } from "./cloth-vertex-cases.mjs";
import { createStrictGpu, GPU_BUFFER_USAGE, GPU_TEXTURE_USAGE } from "./strict-gpu.mjs";
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const { createCharacterPose } = await import( "../../src/engine/foundation/animation/animation-pose.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { identity } = await import( "../../src/engine/foundation/rendering/world-math.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );
const { createGeometryResources } = await import( "../../src/engine/runtime/renderer/device/geometry.ts" );
const { skinClothVertex } = await import( "../../src/engine/foundation/animation/cloth-skin.ts" );
const { CLOTH_STREAM_WORDS, STREAM_POSITION, STREAM_NORMAL, CLOTH_NO_SLOT } = await import(
	"../../src/engine/foundation/animation/cloth-stream.ts"
);
const { packGeometryVertices } = await import( "../../src/engine/foundation/rendering/geometry-vertices.ts" );
const JOINTS = 8;
const FRAME_COUNT = 120;
const RANDOM_SEED = 0x1188;
const INSTANCE_FLOATS = 40;
const INSTANCE_PALETTE_OFFSET = 17;
const INSTANCE_CLOTH_BASE = 19;
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
		writes.push( { buffer, offset, length: byteLength } );
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
		{
			texture: () => {
				throw Error( "Fixture has no image assets" );
			},
			acquire() {},
			drop() {}
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
function captureGeometry( tags, gpuAvailable, gpuTakes ) {
	let gpuCalls = 0;
	// What a sampling GPU holds, by the palette source's buffer (gpuTakes).
	const gpuMemory = new Map(), references = new Map();
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
				// A skinned cloth draw's mesh, its slot map and the stream its
				// owner wrote, one entry a slot.
				cloth: source.clothStream ?
					{
						geometry: source,
						map: slotMap( source.clothStream, source.positions.length / 3 ),
						stream: new Uint32Array( source.clothStream.length * CLOTH_STREAM_WORDS )
					} :
					undefined,
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
			// The slots the GPU sampled hold its palette, not the poisoned CPU copy.
			const memory = gpuMemory.get( bones.buffer ), base = bones.byteOffset / 4;
			if ( memory ) {
				// Its own bone buffer would receive the stale CPU copy instead.
				assert.ok( draw.offsets, `${draw.tag} reads sampled palettes through the shared palette` );
				for ( let k = 0; k < draw.bones.length; k++ ) {
					if ( Number.isNaN( draw.bones[k] ) ) draw.bones[k] = memory[base + k];
				}
			}
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
		writeClothStream
		================
		*/
		writeClothStream( draw, first, entries ) {
			device.resources.commands.writeClothStream( draw.deviceDraw, first, entries );
			draw.cloth.stream.set( entries, first * CLOTH_STREAM_WORDS );
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
	// A device that samples skeletons (gpuTakes): each slot it takes lands in
	// its own memory, computed by the CPU's evaluator for the same clip and
	// time, and the CPU copy of that slot is poisoned, as a real GPU leaves it
	// stale. A consumer that reads the CPU copy reads NaN.
	if ( gpuTakes ) {
		commands.prepareGpuBones = ( source, model, primitive, samples ) => {
			gpuCalls++;
			let memory = gpuMemory.get( source.buffer );
			if ( !memory ) {
				memory = new Float32Array( source.buffer.byteLength / 4 );
				gpuMemory.set( source.buffer, memory );
			}
			let reference = references.get( model );
			if ( !reference ) {
				reference = createCharacterPose( model );
				references.set( model, reference );
			}
			const stride = primitive.joints.length * 16, base = source.byteOffset / 4;
			for ( let i = 0; i < samples.length; i++ ) {
				const sample = samples[i];
				if ( !sample ) continue;
				reference.evaluate( sample.clip.name, sample.time, false );
				reference.palette( primitive, memory, base + i * stride );
				source.fill( NaN, i * stride, (i + 1) * stride );
			}
			return true;
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
			// Instance tables are slab ranges (buffer-slabs.ts): a write is a
			// cloth placement write when it lands inside a cloth draw's range.
			const ranges = draws.filter( draw => draw.tag.startsWith( "cloth" ) ).map( draw =>
				draw.deviceDraw.binding.entries.find( entry => entry.binding === 1 ).resource
			);
			return device.writes.filter( write =>
				ranges.some( range =>
					write.buffer === range.buffer && write.offset >= range.offset &&
					write.offset < range.offset + range.size
				)
			).length;
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
				const storage = entries.find( entry => entry.binding === 1 ).resource;
				const packed = new Float32Array(
					device.bytes.get( storage.buffer ).buffer,
					storage.offset,
					storage.size / 4
				);
				assert.equal( draw.deviceDraw.instanceCount, draw.instances.length / 16 );
				for ( let row = 0; row < draw.deviceDraw.instanceCount; row++ ) {
					assert.deepEqual(
						packed.slice( row * INSTANCE_FLOATS, row * INSTANCE_FLOATS + 16 ),
						draw.instances.slice( row * 16, row * 16 + 16 )
					);
					// The shaders find a cloth draw's stream after its skin entries.
					assert.equal(
						packed[row * INSTANCE_FLOATS + INSTANCE_CLOTH_BASE],
						draw.cloth ? draw.cloth.geometry.positions.length / 3 : 0
					);
				}
				if ( !draw.bones ) continue;
				const palette = entries.find( entry => entry.binding === 7 ).resource.buffer;
				const uploaded = new Float32Array( device.bytes.get( palette ).buffer );
				// The byte count measures body palette sharing; a cloth draw's
				// palette upload replaced its vertex stream upload instead.
				if ( !draw.cloth ) buffers.add( palette );
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
slotMap

Each vertex's stream slot, or CLOTH_NO_SLOT, from the slots of an upload's
clothStream, as the device's slot map holds them.
================
*/
function slotMap( slots, count ) {
	const map = new Uint32Array( count ).fill( CLOTH_NO_SLOT );
	slots.forEach( ( vertex, slot ) => map[vertex] = slot );
	return map;
}

/*
================
clothVertices

A skinned cloth draw's vertices as its vertex shader composes them: each
vertex skinned by its palette, except where its slot's cloth stream entry
says the solver owns the position or the normal.
================
*/
function clothVertices( draw ) {
	const { geometry, map, stream } = draw.cloth, floats = new Float32Array( stream.buffer );
	const vertices = packGeometryVertices( geometry ), skin = new Float64Array( 6 );
	for ( let i = 0; i < geometry.positions.length / 3; i++ ) {
		const entry = map[i] * CLOTH_STREAM_WORDS, flags = map[i] === CLOTH_NO_SLOT ? 0 : stream[entry + 3];
		skinClothVertex( geometry, draw.bones, i, skin );
		for ( let axis = 0; axis < 3; axis++ ) {
			vertices[i * 14 + axis] = flags & STREAM_POSITION ? floats[entry + axis] : skin[axis];
			vertices[i * 14 + 3 + axis] = flags & STREAM_NORMAL ? floats[entry + 4 + axis] : skin[3 + axis];
		}
	}
	return vertices;
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
		const vertices = draw.cloth ? clothVertices( draw ) : draw.capturedVertices;
		for ( const key of [ "instances", "vertices", "fade", "appearance", "lights", "transform" ] ) {
			const values = key === "vertices" ? vertices : draw[key];
			hash.update( `${key}:${values?.byteLength ?? -1}:` );
			if ( values ) hash.update( Buffer.from( values.buffer, values.byteOffset, values.byteLength ) );
		}
		hash.update( draw.bones && !draw.cloth ? "bones:" : "no-bones:" );
		if ( draw.bones && !draw.cloth ) {
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
offStretchActors

The far-cloth scenario's actors for frame: three wearers cross the cloth
distance (fraction .25, characters.ts drawCloth) in both directions at
different frames; far ones are crowded at fraction .8, so the level of
detail holds every other pose sample, except wearer 2's second stretch,
at fraction .5 in no crowd: sampled every frame. Wearer 1 stays off for 40 frames,
comes near for two, goes off for one, and later changes clip while off and
leaves view for eight frames; wearer 2 starts off; wearer 3 goes off for
one frame in every nine. The clock's frame times (clothVertexCase) restart
the cloth both on a solver step and between steps. With allFar, every
wearer stays beyond the distance and in view throughout, on one clip.
================
*/
function offStretchActors( frame, allFar ) {
	const far = allFar ? [ true, true, true ] : [
		frame >= 20 && frame < 60 || frame === 62 || frame >= 90,
		frame < 30 || frame >= 75 && frame < 95,
		frame % 9 === 4
	];
	const absent = allFar ? [ false, false, false ] : [ frame >= 105 && frame < 113, frame >= 50 && frame < 53, false ];
	/** @type {import("../../src/engine/contracts/character.ts").CharacterActor[]} */
	const actors = [];
	for ( let i = 0; i < 3; i++ ) {
		if ( absent[i] ) continue;
		const gid = i + 1;
		actors.push( {
			gid,
			model: "body-first",
			clip: gid === 1 && frame >= 100 && !allFar ? "stand" : "move",
			time: (frame * .031 + gid * .13) % 1,
			loop: true,
			scale: 1,
			pose: { regionId: 257, x: gid * 10, y: 0, z: 100, yaw: radians( gid / 10 ) },
			animationLod: !far[i] ?
				{ fraction: .1, crowded: false } :
				gid === 2 && frame >= 75 && !allFar ?
				{ fraction: .5, crowded: false } :
				{ fraction: .8, crowded: true }
		} );
	}
	return actors;
}

/*
================
streamDigest

The cloth streams the frame's cloth draws hold, every word.
================
*/
function streamDigest( draws ) {
	const hash = createHash( "sha256" );
	for ( const draw of draws ) {
		if ( draw.cloth ) hash.update( new Uint8Array( draw.cloth.stream.buffer ) );
	}
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
	// offStretch: "mixed" or "far", the far-cloth scenario (offStretchActors).
	{
		moving = false,
		variants = false,
		gpuTakes = false,
		offStretch = /** @type {false | "mixed" | "far"} */ (false)
	} = {}
) {
	const random = createPresentationRandom( RANDOM_SEED, 1, 100000 );
	const owner = createCharacters( random ), tags = new Map();
	owner.model( "body-first", model( false, tags ), [] );
	owner.model( "cloth-first", model( true, tags ), [] );
	const geometry = captureGeometry( tags, gpuAvailable, gpuTakes ), clock = clothVertexCase( true, true );
	const frames = [];
	let boneWriteBytes = 0, cpuEvaluations = 0;
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
			const actors = offStretch ? offStretchActors( frame, offStretch === "far" ) : gids.map( gid => ({
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
				true,
				true,
				!!offStretch || input.enabled
			));
			assert.equal( draws.length, actors.length * PRIMITIVE_NAMES.length );
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
			// The device's buffers hold the poisoned CPU copy where the GPU sampled.
			if ( !gpuTakes ) boneWriteBytes += geometry.verify( draws );
			frames.push( {
				streams: streamDigest( draws ),
				cpuEvaluations: owner.stats().cpuEvaluations,
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
		cpuEvaluations = owner.stats().cpuEvaluations;
	} finally {
		owner.dispose( geometry.commands, null );
		geometry.dispose();
	}
	return { frames, gpuCalls: geometry.gpuCalls(), boneWriteBytes, cpuEvaluations };
}
