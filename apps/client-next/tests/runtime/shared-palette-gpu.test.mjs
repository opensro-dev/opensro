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
const { createCharacters, createGeometryResources } = {
	...(await load( "src/engine/runtime/renderer/characters/characters.ts" )),
	...(await load( "src/engine/runtime/renderer/device/geometry.ts" ))
};
globalThis.GPUBufferUsage = { UNIFORM: 1, COPY_DST: 2, STORAGE: 4, VERTEX: 8, INDEX: 16 };
globalThis.GPUTextureUsage = { TEXTURE_BINDING: 1, COPY_DST: 2 };
const I = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
function fixture() {
	const buffers = [],
		writes = [],
		gpu = {
			createBuffer( { size } ) {
				const b = {
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
		// One geometry pipeline per state.
		() => ({
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
const geometry = () => ({
	positions: Float32Array.of( -1, -1, 0, 1, -1, 0, 0, 1, 0 ),
	indices: Uint32Array.of( 0, 1, 2 ),
	transform: I(),
	joints: new Uint32Array( 12 ),
	weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 )
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
	const packed = new Float32Array( binding( grown, 1 ).bytes.buffer );
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
			const packed = new Float32Array( binding( draw, 1 ).bytes.buffer ),
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
