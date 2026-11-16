/*
===========================================================================

shared-palette-gpu.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const { createCharacters, createGeometryResources, clothStreamSlots } = {
	...(await load( "src/engine/runtime/renderer/characters/characters.ts" )),
	...(await load( "src/engine/runtime/renderer/device/geometry.ts" )),
	...(await load( "src/engine/foundation/animation/cloth-stream.ts" ))
};
globalThis.GPUBufferUsage = { UNIFORM: 1, COPY_DST: 2, STORAGE: 4, VERTEX: 8, INDEX: 16 };
globalThis.GPUTextureUsage = { TEXTURE_BINDING: 1, COPY_DST: 2 };
const I = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
function fixture() {
	const buffers = [],
		writes = [],
		gpu = {
			createBuffer( { size, label } ) {
				const b = {
					label,
					size,
					bytes: new Uint8Array( size ),
					destroyed: 0,
					destroy() {
						this.destroyed++;
					}
				};
				buffers.push( b );
				return b;
			},
			createTexture() {
				return {
					createView() {
						return {};
					},
					destroy() {}
				};
			},
			// The sun cascade's comparison sampler (bindings 13/14).
			createSampler() {
				return {};
			},
			createBindGroup( { entries } ) {
				return entries;
			},
			pushErrorScope() {},
			popErrorScope() {
				return Promise.resolve( null );
			},
			queue: {
				writeTexture() {},
				writeBuffer( buffer, offset, data, start, length ) {
					assert.equal( buffer.destroyed, 0 );
					buffer.bytes.set( new Uint8Array( data, start, length ), offset );
					writes.push( { buffer, length } );
				}
			}
		};
	const resources = createGeometryResources(
		gpu,
		() => gpu,
		e => {
			throw e;
		},
		// One geometry pipeline per state; it says whether it is the row-state variant.
		( state, surface, rowStates ) => ({
			rowStates: !!rowStates,
			getBindGroupLayout() {
				return {};
			}
		}),
		{ texture() {}, acquire() {}, drop() {} },
		{},
		{},
		{},
		() => ({})
	);
	return { resources, buffers, writes };
}
const binding = ( draw, n ) => draw.binding.find( e => e.binding === n ).resource.buffer;
// The floats a binding exposes: slab ranges bind at an offset (buffer-slabs.ts).
const bound = ( draw, n ) => {
	const resource = draw.binding.find( e => e.binding === n ).resource;
	return new Float32Array(
		resource.buffer.bytes.buffer,
		resource.offset ?? 0,
		(resource.size ?? resource.buffer.size) / 4
	);
};
const geometry = () => ({
	positions: Float32Array.of( -1, -1, 0, 1, -1, 0, 0, 1, 0 ),
	indices: Uint32Array.of( 0, 1, 2 ),
	transform: I(),
	joints: new Uint32Array( 12 ),
	weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 )
});
// Cloth on that mesh: vertex 0 free, 1 a pin the constraint reads, 2 a pin
// no step reads. Its stream has two slots.
const cloth = () => ({
	mobility: [ 1, 0, 0 ],
	pins: [ 0, 1, 1 ],
	constraints: [ [ 0, 1, 1 ] ],
	order: [ 0 ],
	force: null,
	gravity: 1,
	gravityMobility: 0,
	windMobility: 0,
	damping: .9,
	windPeriod: 1
});
test("shared GPU palette has one upload per revision and one lifetime across draw handles", () => {
	const f = fixture(), bones = new Float32Array( 64 ), g = { ...geometry(), bones }, c = f.resources.commands;
	const a = c.upload( g, undefined, Uint32Array.of( 0 ) ),
		b = c.upload( g, undefined, Uint32Array.of( 0 ) ),
		buffer = binding( a, 7 );
	assert.equal( buffer, binding( b, 7 ) );
	f.writes.length = 0;
	assert.equal( c.updateBones( a, bones.subarray( 0, 16 ), 1 ), 64 );
	assert.equal( c.updateBones( b, bones.subarray( 0, 16 ), 1 ), 0 );
	assert.equal( f.writes.length, 1 );
	assert.throws( () => c.updateBones( a, bones, 0 ), /revision/ );
	assert.throws( () => c.updateBones( a, bones.slice(), 2 ), /revision/ );
	assert.throws( () => c.updateInstances( a, I(), undefined, undefined, undefined, Uint32Array.of( 4 ) ), /offset/ );
	const grown = c.updateInstances(
		a,
		new Float32Array( 32 ),
		undefined,
		undefined,
		undefined,
		Uint32Array.of( 0, 1 )
	);
	assert.equal( binding( grown, 7 ), buffer );
	const packed = bound( grown, 1 );
	assert.equal( packed[17], 0 );
	assert.equal( packed[18], 1 );
	assert.equal( packed[57], 1 );
	c.release( a );
	assert.equal( buffer.destroyed, 0 );
	c.release( grown );
	assert.equal( buffer.destroyed, 0 );
	c.release( b );
	assert.equal( buffer.destroyed, 1 );
	f.resources.dispose();
	assert.ok( f.buffers.every( b => b.destroyed === 1 ) );
});
test("shader-addressed shared palettes equal expanded poses through merge, split, membership and recovery", () => {
	let f = fixture();
	const retired = [],
		owner = createCharacters(),
		p = { name: "body", node: 0, image: -1, joints: [ 0 ], inverseBind: I(), geometry: geometry() },
		q = { ...p, name: "clothing", inverseBind: I(), geometry: geometry() };
	const model = {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		images: [],
		clips: [ {
			name: "move",
			duration: 1,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "LINEAR",
				times: Float32Array.of( 0, 1 ),
				values: Float32Array.of( 0, 0, 0, 1, 2, 3 )
			} ]
		} ],
		primitives: [ p, q ]
	};
	owner.model( "m", model, [] );
	for ( let frame = 0; frame < 40; frame++ ) {
		if ( frame === 20 ) {
			f.resources.dispose();
			retired.push( ...f.buffers );
			owner.invalidate();
			f = fixture();
		}
		const rows = Array.from(
			{ length: frame % 3 === 0 ? 3 : 4 },
			( _, i ) => ({
				gid: i + 1,
				model: "m",
				clip: "move",
				time: frame % 2 ? .25 : (frame + i) / 100,
				loop: true,
				scale: 1,
				pose: { regionId: 257, x: i, y: 0, z: 10, yaw: 0 }
			})
		);
		owner.actors( rows );
		f.writes.length = 0;
		const draws = owner.prepare( f.resources.commands, {}, 257 );
		assert.equal( binding( draws[0], 7 ), binding( draws[1], 7 ) );
		for ( const draw of draws ) {
			const packed = bound( draw, 1 ),
				bones = new Float32Array( binding( draw, 7 ).bytes.buffer );
			for ( let i = 0; i < rows.length; i++ ) {
				const offset = packed[i * 40 + 17] * 16, expected = I();
				expected[12] = rows[i].time;
				expected[13] = rows[i].time * 2;
				expected[14] = rows[i].time * 3;
				assert.deepEqual( bones.slice( offset, offset + 16 ), expected, `frame ${frame} actor ${i}` );
			}
		}
		assert.ok( f.writes.filter( w => w.buffer === binding( draws[0], 7 ) ).length <= 1 );
	}
	owner.dispose( f.resources.commands, null );
	f.resources.dispose();
	assert.ok( [ ...retired, ...f.buffers ].every( b => b.destroyed === 1 ) );
});

test("an immutable skinned mesh has one GPU copy across draws, counted by reference", () => {
	const f = fixture(), c = f.resources.commands, mesh = geometry();
	const streams = draw => [ draw.vertices, draw.indices, binding( draw, 6 ) ];
	// Two batches drawing the same body part, each with its own palette.
	const a = c.upload( { ...mesh, bones: new Float32Array( 16 ) } ),
		b = c.upload( { ...mesh, bones: new Float32Array( 16 ) } );
	assert.deepEqual( streams( b ), streams( a ) );
	assert.equal( binding( a, 6 ).label, "geometry-skin", "binding 6 is the skin table" );
	assert.notEqual( binding( a, 7 ), binding( b, 7 ), "each draw keeps its own palette" );
	// Another array, or a stream the draw rewrites, is not that mesh.
	const other = c.upload( { ...mesh, uvs: new Float32Array( 6 ), bones: new Float32Array( 16 ) } ),
		dynamic = c.upload( { ...mesh, dynamicVertices: true, bones: new Float32Array( 16 ) } );
	assert.notEqual( other.vertices, a.vertices );
	assert.notEqual( dynamic.vertices, a.vertices );
	// Two wearers of one dress draw its vertices and indices from the same
	// copy, but each its own skin table: the owner writes the stream into it.
	const slots = clothStreamSlots( cloth() ),
		wearers = [ 0, 1 ].map( () => c.upload( { ...mesh, clothStream: slots, bones: new Float32Array( 16 ) } ) );
	assert.deepEqual( streams( wearers[0] ).slice( 0, 2 ), streams( a ).slice( 0, 2 ) );
	assert.deepEqual( streams( wearers[1] ).slice( 0, 2 ), streams( a ).slice( 0, 2 ) );
	assert.notEqual( binding( wearers[0], 6 ), binding( a, 6 ) );
	assert.notEqual( binding( wearers[0], 6 ), binding( wearers[1], 6 ) );
	assert.equal(
		binding( wearers[0], 6 ).size,
		binding( a, 6 ).size * 2 + slots.length * 32,
		"skin entries, the slot map, then a stream entry a slot"
	);
	const ownTable = binding( wearers[0], 6 );
	c.release( wearers[0] );
	assert.equal( ownTable.destroyed, 1, "a wearer's table retires with its draw" );
	assert.equal( a.vertices.destroyed, 0, "the mesh stays for its other draws" );
	c.release( wearers[1] );
	assert.throws( () => c.updateIndices( a, Uint32Array.of( 0, 1, 2 ) ), /shared mesh/ );
	const shared = streams( a );
	c.release( a );
	assert.ok( shared.every( buffer => buffer.destroyed === 0 ), "the other draw still reads the mesh" );
	c.release( b );
	assert.ok( shared.every( buffer => buffer.destroyed === 1 ), "the last draw retires it" );
	// A new upload after that makes a new copy.
	const again = c.upload( { ...mesh, bones: new Float32Array( 16 ) } );
	assert.notEqual( again.vertices, shared[0] );
	f.resources.dispose();
	assert.ok( f.buffers.every( buffer => buffer.destroyed === 1 ), "every buffer is destroyed exactly once" );
});

test("a failed upload gives back its use of a shared mesh", () => {
	const f = fixture(), c = f.resources.commands, mesh = geometry();
	const a = c.upload( { ...mesh, bones: new Float32Array( 16 ) } );
	// Palette offsets past the palette fail the upload after the mesh is taken.
	assert.throws( () => c.upload( { ...mesh, bones: new Float32Array( 16 ) }, undefined, Uint32Array.of( 64 ) ) );
	const shared = a.vertices;
	c.release( a );
	assert.equal( shared.destroyed, 1, "no use is left behind by the failed upload" );
	f.resources.dispose();
});

test("two assemblies over one body share its GPU copy and its budget charge", async () => {
	const { characterMeshBytes } = await load( "src/engine/foundation/animation/character-budget.ts" );
	const f = fixture();
	const model = name => ({
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		images: [],
		clips: [ { name: "idle", duration: 1, channels: [] } ],
		primitives: [ { name, node: 0, image: -1, joints: [ 0 ], inverseBind: I(), geometry: geometry() } ]
	});
	const row = ( gid, name ) => ({
		gid,
		model: name,
		clip: "idle",
		time: 0,
		loop: true,
		scale: 1,
		pose: { regionId: 257, x: gid, y: 0, z: 10, yaw: 0 }
	});
	/*
	================
	frame

	One frame of the given assemblies of the body with a sword or a shield,
	one actor each: the frame's render bytes and draws.
	================
	*/
	const frame = names => {
		const owner = createCharacters();
		for ( const name of [ "body", "sword", "shield" ] ) owner.model( name, model( name ), [] );
		for ( const item of [ "sword", "shield" ] ) {
			owner.assembly( item + "-wearer", "body", [ { model: item, parts: [ item ], covers: [] } ] );
		}
		owner.actors( names.map( ( name, i ) => row( i + 1, name ) ) );
		const draws = owner.prepare( f.resources.commands, {}, 257 );
		const result = { bytes: owner.stats().renderBytes, draws: [ ...draws ] };
		owner.dispose( f.resources.commands, null );
		return result;
	};
	const sword = frame( [ "sword-wearer" ] ), shield = frame( [ "shield-wearer" ] );
	const both = frame( [ "sword-wearer", "shield-wearer" ] );
	// Two batches; the body mesh is charged once for both, each item once.
	assert.equal( both.bytes, sword.bytes + shield.bytes - characterMeshBytes( geometry() ) );
	// And drawn from one GPU copy: the four draws use three vertex streams.
	assert.equal( both.draws.length, 4 );
	assert.equal( new Set( both.draws.map( draw => draw.vertices ) ).size, 3 );
	f.resources.dispose();
});

test("only immutable skinned primitives are charged as shared meshes", async () => {
	const { characterSharedMesh, characterBatchBytes, characterMeshBytes } = await load(
		"src/engine/foundation/animation/character-budget.ts"
	);
	const skinned = { name: "body", node: 0, image: -1, joints: [ 0 ], inverseBind: I(), geometry: geometry() };
	assert.equal( characterSharedMesh( skinned ), true );
	// A skinned cloth piece's solver writes only its own skin table.
	assert.equal( characterSharedMesh( { ...skinned, cloth: cloth() } ), true );
	// Ribbons and unskinned cloth rewrite their streams, and an unskinned mesh
	// lives in the slabs: each batch keeps its own copy, charged with the batch.
	const unskinned = { ...geometry(), joints: undefined, weights: undefined };
	for (
		const own of [
			{ ...skinned, ribbon: {} },
			{ ...skinned, cloth: cloth(), geometry: unskinned },
			{ ...skinned, geometry: unskinned }
		]
	) {
		assert.equal( characterSharedMesh( own ), false );
	}
	const model = primitive => ({ nodes: [], images: [], clips: [], primitives: [ primitive ] });
	const own = { ...skinned, geometry: { ...geometry(), joints: undefined, weights: undefined } };
	// A shared mesh leaves the batch charge; one of a batch's own stays in it.
	assert.equal(
		characterBatchBytes( model( own ), 1 ) - characterBatchBytes( model( skinned ), 1 ),
		// a GPU copy and a CPU mirror of the 14-float stream, and the indices
		geometry().positions.length / 3 * 14 * 4 * 2 + geometry().indices.byteLength
	);
	assert.equal( characterMeshBytes( geometry() ), 3 * 56 + 3 * 4 + 3 * 32, "stream, indices and skin table once" );
	// A shared cloth row keeps its solver state (92 bytes a vertex) and its
	// own skin table: what the device allocates for its draw.
	const f = fixture(),
		draw = f.resources.commands.upload( {
			...geometry(),
			clothStream: clothStreamSlots( cloth() ),
			bones: new Float32Array( 16 )
		} );
	assert.equal( binding( draw, 6 ).size, (3 + 3 + 2) * 32, "skin entries, slot map, two stream slots" );
	assert.equal(
		characterBatchBytes( model( { ...skinned, cloth: cloth() } ), 1 ) - characterBatchBytes( model( skinned ), 1 ),
		3 * 92 + binding( draw, 6 ).size
	);
	f.resources.dispose();
});

test("glow wearers without cloth share one batch whose row states hold each actor's one-row glow", () => {
	const merged = fixture(), reference = fixture();
	const mergedOwner = createCharacters(), referenceOwner = createCharacters();
	const glow = {
		threshold: 1,
		image: 0,
		uv: [ .5, -.25 ],
		color1: [ 1, .5, 0 ],
		color2: [ 0, .25, 1 ],
		period: 700,
		gain: 2,
		alphaTest: true
	};
	const body = { name: "body", node: 0, image: -1, joints: [ 0 ], inverseBind: I(), geometry: geometry() };
	const model = {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		images: [],
		clips: [ {
			name: "move",
			duration: 1,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "LINEAR",
				times: Float32Array.of( 0, 1 ),
				values: Float32Array.of( 0, 0, 0, 1, 2, 3 )
			} ]
		} ],
		primitives: [ body, { ...body, name: "weapon", inverseBind: I(), geometry: geometry(), equipmentGlow: glow } ]
	};
	// Frame order is not gid order: a merged batch keeps frame order.
	const gids = [ 3, 1, 4, 2 ];
	mergedOwner.model( "m", model, [] );
	// The reference: today's one-row batches, one model an actor.
	for ( const gid of gids ) referenceOwner.model( "r" + gid, model, [] );
	const floats = ( draw, n ) => Array.from( bound( draw, n ) );
	// The material without the five glow floats.
	const rest = values => values.filter( ( _, i ) => ![ 48, 49, 50, 52, 53 ].includes( i ) );
	const rowStates = draw => draw.binding.find( entry => entry.binding === 16 );
	const merges = new Set(), lit = ( frame, i ) => frame >= 8 || (frame + i) % 3 !== 0;
	for ( let frame = 0; frame < 12; frame++ ) {
		const rows = name =>
			gids.map( ( gid, i ) => ({
				gid,
				model: name( gid ),
				clip: "move",
				time: (frame + i) / 10,
				loop: true,
				scale: 1,
				// The glow's level-of-detail bit flips per actor across frames,
				// then every actor draws its glow.
				animationLod: { fraction: lit( frame, i ) ? 0 : .75, crowded: false },
				pose: { regionId: 257, x: i, y: 0, z: 10, yaw: 0 }
			}) );
		mergedOwner.actors( rows( () => "m" ) );
		referenceOwner.actors( rows( gid => "r" + gid ) );
		const actual = mergedOwner.prepare( merged.resources.commands, {}, 257, undefined, false, frame * .05 ),
			expected = referenceOwner.prepare( reference.resources.commands, {}, 257, undefined, false, frame * .05 );
		merged.resources.flushMaterials();
		reference.resources.flushMaterials();
		assert.equal( expected.length, gids.length * 2 );
		// Each merged batch is the actors with one glow bit, in frame order.
		const batches = [];
		for ( let i = 0; i < gids.length; i++ ) {
			const batch = batches.find( b => b.lit === lit( frame, i ) );
			if ( batch ) batch.actors.push( i );
			else batches.push( { lit: lit( frame, i ), actors: [ i ] } );
		}
		assert.equal( actual.length, batches.length * 2, `frame ${frame}` );
		batches.forEach( ( batch, b ) => {
			batch.actors.forEach( ( actor, row ) => {
				for ( let p = 0; p < 2; p++ ) {
					const draw = actual[b * 2 + p], one = expected[actor * 2 + p];
					assert.equal( draw.instanceCount, batch.actors.length );
					const instance = floats( draw, 1 ).slice( row * 40, row * 40 + 40 ),
						oneInstance = floats( one, 1 ).slice( 0, 40 );
					// Every instance float but the palette offset, and the palette it names.
					assert.deepEqual(
						instance.filter( ( _, i ) => i !== 17 ),
						oneInstance.filter( ( _, i ) => i !== 17 )
					);
					const palette = new Float32Array( binding( draw, 7 ).bytes.buffer ),
						onePalette = new Float32Array( binding( one, 7 ).bytes.buffer );
					assert.deepEqual(
						palette.slice( instance[17] * 16, instance[17] * 16 + 16 ),
						onePalette.slice( oneInstance[17] * 16, oneInstance[17] * 16 + 16 )
					);
					const material = floats( draw, 2 ),
						oneMaterial = floats( one, 2 ),
						at = `frame ${frame} gid ${gids[actor]} part ${p}`;
					// The reference is the plain shader with no row-state binding.
					assert.equal( one.pipeline.rowStates, false );
					assert.equal( rowStates( one ), undefined );
					if ( p === 0 || batch.actors.length === 1 ) {
						// A part without glow, or a one-row batch: today's draw.
						assert.equal( draw.pipeline.rowStates, false );
						assert.equal( rowStates( draw ), undefined );
						assert.deepEqual( material, oneMaterial, at );
						continue;
					}
					merges.add( batch.actors.length );
					assert.equal( draw.pipeline.rowStates, true, "the glow is read from the row states" );
					assert.deepEqual( rest( material ), rest( oneMaterial ), at );
					const state = floats( draw, 16 ).slice( row * 8, row * 8 + 8 );
					assert.deepEqual(
						[ ...state.slice( 0, 3 ), ...state.slice( 4, 6 ) ],
						[ ...oneMaterial.slice( 48, 51 ), ...oneMaterial.slice( 52, 54 ) ],
						at
					);
				}
			} );
		} );
	}
	assert.deepEqual( [ ...merges ].sort(), [ 2, 3, 4 ], "batches of two, three and four rows were compared" );
	mergedOwner.dispose( merged.resources.commands, null );
	referenceOwner.dispose( reference.resources.commands, null );
	merged.resources.dispose();
	reference.resources.dispose();
});
