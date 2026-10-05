/*
===========================================================================

character-shadows.test.mjs - tests for character-shadow.ts,
character-shadows.ts, characters.ts, video-options.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { shadowProjection, characterShadowReceiver } = await import(
	"../../src/engine/foundation/rendering/character-shadow.ts"
);
const { createCharacterShadows } = await import( "../../src/engine/runtime/renderer/device/character-shadows.ts" );
const { terrainCellKey } = await import( "../../src/engine/foundation/rendering/terrain-interaction.ts" );
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const cells = new Map();
for ( let z = -1; z <= 1; z++ ) {
	for ( let x = -1; x <= 1; x++ ) cells.set( terrainCellKey( x, z ), { heights: new Float32Array( 289 ) } );
}
test("shadow projection is translation invariant, casts away from +X light, and follows terrain heights", () => {
	const a = shadowProjection( [ 0, 0, 0 ], 20 ), b = shadowProjection( [ 320, 10, -320 ], 20 );
	const project = ( m, p ) => [ 0, 1, 2 ].map( i => m[i] * p[0] + m[i + 4] * p[1] + m[i + 8] * p[2] + m[i + 12] );
	const first = project( a.matrix, [ 0, 10, 0 ] ), translated = project( b.matrix, [ 320, 20, -320 ] );
	first.forEach( ( v, i ) => assert.ok( Math.abs( v - translated[i] ) < 1e-5 ) );
	const mesh = characterShadowReceiver( cells, a );
	assert.ok( defined( mesh ).indices.length );
	assert.equal( defined( defined( mesh ).material ).depthWrite, false );
	for ( let i = 0; i < defined( mesh ).positions.length / 3; i++ ) {
		const x = defined( mesh ).positions[i * 3];
		assert.equal(
			defined( defined( mesh ).colors )[i * 4 + 3],
			Math.fround( Math.trunc( 150 - Math.min( 150, Math.max( 0, (-x - 12) * 9 ) ) ) / 255 )
		);
	}
	assert.ok( [ ...defined( mesh ).positions ].every( Number.isFinite ) );
	assert.ok( [ ...defined( mesh ).uvs ].every( Number.isFinite ) );
	assert.equal( characterShadowReceiver( new Map(), a ), null );
	const blob = characterShadowReceiver( cells, a, 20 );
	assert.ok(
		defined( defined( blob ).colors ).every( ( v, i ) => v === (i % 4 === 3 ? Math.fround( 168 / 255 ) : 0) )
	);
	assert.equal( defined( blob ).indices.length, 25 * 6, "circle receiver includes neighboring tiles at the edge" );
});
test("visible character selection caps at ten, excludes effects, preserves attachment ownership and retires culled batches", () => {
	const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ),
		owner = createCharacters();
	const model = {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		images: [],
		clips: [ { name: "stand", duration: 1, channels: [] } ],
		primitives: [ {
			name: "body",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			geometry: {
				positions: Float32Array.of( 0, 0, 0, 0, 20, 0, 1, 0, 0 ),
				indices: Uint32Array.of( 0, 1, 2 ),
				transform: identity()
			}
		} ]
	};
	owner.model( "body", model, [] );
	const actors = Array.from(
		{ length: 12 },
		( _, i ) => ({
			gid: i + 1,
			model: "body",
			shadowSize: 15,
			pose: { regionId: 257, x: i * 5, y: 0, z: 0, yaw: 0 },
			clip: "stand",
			time: 0,
			loop: true,
			scale: 1
		})
	);
	actors.push( {
		...actors[0],
		gid: 20,
		shadowSize: undefined,
		attachment: { gid: 1, root: true, offset: [ 0, 0, 0 ] },
		shadowAttachment: true
	} );
	actors.push( {
		...actors[0],
		gid: 21,
		shadowSize: undefined,
		attachment: { gid: 1, root: true, offset: [ 0, 0, 0 ] }
	} );
	const gpu = {
		/*
		================
		upload
		================
		*/
		upload( data ) {
			return { instanceCount: data.instances.length / 16, indexCount: 3 };
		},
		/*
		================
		updateInstances
		================
		*/
		updateInstances( d, data ) {
			d.instanceCount = data.length / 16;
			return d;
		},
		/*
		================
		updateBones
		================
		*/
		updateBones() {
			return 0;
		},
		release() {}
	};
	owner.actors( actors );
	let draws = owner.prepare( gpu, {}, 257, undefined, false );
	assert.equal( owner.shadowCandidates( draws, [ 0, 0, 0 ], 2 ).length, 10 );
	assert.equal(
		owner.shadowCandidates( draws, [ 0, 0, 0 ], 2 )[0].parts.length,
		2,
		"body plus wing; ordinary attached effect excluded"
	);
	assert.equal( owner.shadowCandidates( draws, [ 4000, 0, 0 ], 2 ).length, 0 );
	assert.equal( owner.shadowCandidates( draws, [ 4000, 0, 0 ], 1 ).length, 10 );
	assert.equal( owner.shadowCandidates( draws, [ 0, 0, 0 ], 0 ).length, 0 );
	owner.actors( [] );
	draws = owner.prepare( gpu, {}, 257, undefined, false );
	assert.deepEqual( owner.shadowCandidates( draws, [ 0, 0, 0 ], 2 ), [] );
	owner.dispose( gpu, null );
});
test("receivers follow submitted LOD/seam heights and do not double-darken terrain material overlays", () => {
	const projection = shadowProjection( [ 10, 0, 10 ], 20 ),
		surface = {
			positions: Float32Array.of( 0, 4, 0, 0, 6, 40, 40, 8, 40, 40, 5, 0 ),
			indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
			start: 0,
			count: 6
		};
	const actual = new Map( [ [ terrainCellKey( 0, 0 ), [ surface, surface ] ] ] );
	for ( const size of [ 20 ] ) {
		const mesh = characterShadowReceiver( cells, projection, size, actual );
		assert.equal( defined( mesh ).indices.length, 6 );
		assert.deepEqual( [ ...defined( mesh ).positions ], [
			0,
			4,
			0,
			0,
			6,
			40,
			40,
			8,
			40,
			0,
			4,
			0,
			40,
			8,
			40,
			40,
			5,
			0
		] );
	}
	assert.equal(
		characterShadowReceiver( cells, projection, 20, new Map() ),
		null,
		"culled/unsubmitted terrain must not receive a floating shadow"
	);
});
/*
================
shadowHarness

The shadow owner on a recording fake device: draw is a borrowed caster,
calls records pass labels, viewports and indexed draws, owned every
resource the owner created. restore puts navigator back.
================
*/
function shadowHarness() {
	const previous = Object.getOwnPropertyDescriptor( globalThis, "navigator" );
	Object.defineProperty( globalThis, "navigator", {
		configurable: true,
		value: { gpu: { getPreferredCanvasFormat: () => "rgba8unorm" } }
	} );
	globalThis.GPUBufferUsage = { UNIFORM: 1, COPY_DST: 2, VERTEX: 4, INDEX: 8 };
	globalThis.GPUTextureUsage = { RENDER_ATTACHMENT: 1, TEXTURE_BINDING: 2 };
	const owned = [], calls = [];
	const resource = data => {
		const r = {
			...data,
			destroyed: false,
			createView: () => ({}),
			/*
			================
			destroy
			================
			*/
			destroy() {
				assert.equal( this.destroyed, false );
				this.destroyed = true;
			}
		};
		owned.push( r );
		return r;
	};
	const gpu = {
		createShaderModule: () => ({}),
		createRenderPipeline: () => ({ getBindGroupLayout: () => ({}) }),
		createSampler: () => ({}),
		createBindGroup: () => ({}),
		createBuffer: resource,
		createTexture: resource,
		queue: { writeBuffer() {} }
	};
	const mesh = characterShadowReceiver( cells, shadowProjection( [ 0, 0, 0 ], 20 ) ),
		draw = { instanceCount: 3, indexCount: 12, vertices: {}, indices: {} },
		borrowed = { instances: {}, material: {}, skin: {}, bones: {} };
	const owner = createCharacterShadows(
			gpu,
			{},
			() => ({ createView: () => ({}) }),
			d => d === draw ? borrowed : undefined,
			"rgba8unorm"
		),
		request = {
			matrix: shadowProjection( [ 0, 0, 0 ], 20 ).matrix,
			receiver: mesh,
			blob: false,
			parts: [ { draw, instance: 2 } ]
		};
	const encoder = {
		/*
		================
		beginRenderPass
		================
		*/
		beginRenderPass( d ) {
			calls.push( d.label );
			return {
				/*
				================
				setViewport
				================
				*/
				setViewport( ...a ) {
					calls.push( a );
				},
				setPipeline() {},
				setBindGroup() {},
				setVertexBuffer() {},
				setIndexBuffer() {},
				/*
				================
				drawIndexed
				================
				*/
				drawIndexed( ...a ) {
					calls.push( a );
				},
				draw() {},
				end() {}
			};
		}
	};
	/*
	================
	restore
	================
	*/
	const restore = () => {
		if ( previous ) Object.defineProperty( globalThis, "navigator", previous );
		else delete globalThis.navigator;
	};
	return { owner, request, encoder, calls, owned, draw, restore };
}

test("shadow GPU lifecycle draws the selected batch instance, filters before receiving, and retires disabled resources", () => {
	const { owner, request, encoder, calls, owned, restore } = shadowHarness();
	try {
		assert.equal( owner.prepare( [ request ] ).length, 1 );
		owner.encode( encoder );
		assert.deepEqual( calls.filter( v => typeof v === "string" ), [
			"character-shadow-generate",
			"character-shadow-filter"
		] );
		assert.ok( calls.some( a => Array.isArray( a ) && a.join() === "12,1,0,0,2" ) );
		calls.length = 0;
		owner.prepare( [ { ...request, blob: true } ], {} );
		owner.encode( encoder );
		assert.equal( calls.length, 0 );
		owner.prepare( [] );
		assert.ok( owned.every( r => r.destroyed ) );
		owner.dispose();
	} finally {
		restore();
	}
});

test("borrowed caster draws are rendered only in the frame that prepared them", () => {
	const { owner, request, encoder, calls, draw, restore } = shadowHarness();
	const drawn = () => calls.filter( a => Array.isArray( a ) && a.length === 5 ).length;
	try {
		// A slot renders once per prepare: the deferred tail encodes again.
		owner.prepare( [ request ] );
		owner.encode( encoder );
		owner.encode( encoder );
		assert.equal( drawn(), 1 );
		// No prepare this frame: last frame's casters are not rendered again.
		calls.length = 0;
		owner.encode( encoder );
		assert.equal( calls.length, 0 );
		// A blob request whose image is not resident yet is skipped; the slot
		// must not keep the dynamic caster it held last frame.
		owner.prepare( [ request ] );
		owner.prepare( [ { ...request, blob: true } ] );
		owner.encode( encoder );
		assert.equal( calls.length, 0 );
		// A caster released between prepare and encode is dropped.
		owner.prepare( [ request ] );
		owner.forget( draw );
		owner.encode( encoder );
		assert.equal( drawn(), 0 );
		owner.dispose();
	} finally {
		restore();
	}
});

test("new players default to shadows: nothing (UIIT_STT_NONE)", async () => {
	const { defaultVideoOptions, videoRows } = await import( "../../src/engine/foundation/rendering/video-options.ts" );
	const options = defaultVideoOptions();
	const shadowRow = videoRows().find( r => r.key === "UIIT_STT_SHADOW_DETAIL" );
	assert.equal( shadowRow?.slot, 1 );
	assert.equal( shadowRow?.entries[0], "UIIT_STT_NONE" );
	assert.equal( options.records[0][1], 0 );
	assert.equal( options.records[1][1], 0 );
});

test("detailed shadow keeps the native fade band on coarse terrain while following its plane", () => {
	const surface = {
		positions: Float32Array.of( -80, 0, -80, -80, 0, 80, 80, 16, 80, 80, 16, -80 ),
		indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
		start: 0,
		count: 6
	};
	const mesh = characterShadowReceiver(
		cells,
		shadowProjection( [ 0, 8, 0 ], 60 ),
		undefined,
		new Map( [ [ terrainCellKey( 0, 0 ), [ surface ] ] ] )
	);
	let found = false;
	for ( let i = 0; i < defined( mesh ).positions.length / 3; i++ ) {
		const x = defined( mesh ).positions[i * 3], y = defined( mesh ).positions[i * 3 + 1];
		assert.ok( Math.abs( y - (x + 80) / 10 ) < 1e-5 );
		if ( x === -20 ) {
			found = true;
			assert.equal( defined( defined( mesh ).colors )[i * 4 + 3], Math.fround( 78 / 255 ) );
		}
	}
	assert.ok( found, "coarse endpoints cannot represent the 12..29-unit fade band" );
});
