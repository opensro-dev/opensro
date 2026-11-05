/*
===========================================================================

particle-query-dispose.test.mjs - a disposed query ends quietly (#2560)

The fake device holds the read buffer's map open the way a real one
does: disposing the owner destroys the buffer under the pending map
and the map rejects. On a live owner the same rejection is an error
and must keep travelling; only the disposal ends quietly.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createParticleQuery } = await import(
	"../../src/engine/runtime/renderer/device/particle-query.ts"
);

/*
================
fixture

One owner over a fake device. pendingMaps holds each mapAsync call
until the test settles it; resolving one hands back the samples the
test armed.
================
*/
function fixture( t ) {
	const previous = /** @type {[string, PropertyDescriptor | undefined][]} */ (
		[ "GPUBufferUsage", "GPUMapMode" ].map(
			k => [ k, Object.getOwnPropertyDescriptor( globalThis, k ) ]
		)
	);
	t.after( () => {
		for ( const [k, v] of previous ) {
			if ( v ) Object.defineProperty( globalThis, k, v );
			else delete globalThis[k];
		}
	} );
	globalThis.GPUBufferUsage = /** @type {any} */ ({
		STORAGE: 1,
		COPY_DST: 2,
		UNIFORM: 4,
		QUERY_RESOLVE: 8,
		COPY_SRC: 16,
		MAP_READ: 32
	});
	globalThis.GPUMapMode = /** @type {any} */ ({ READ: 1 });
	const pendingMaps = [];
	let samples = new BigUint64Array( [ 0n ] );
	const device = /** @type {any} */ ({
		/*
		================
		createShaderModule
		================
		*/
		createShaderModule() {
			return { getCompilationInfo: async () => ({ messages: [] }) };
		},
		/*
		================
		createRenderPipelineAsync
		================
		*/
		async createRenderPipelineAsync() {
			return { getBindGroupLayout: () => ({}) };
		},
		/*
		================
		createBuffer

		The read buffer's destroy rejects its pending map, the way the
		browser answers a map whose buffer was destroyed under it.
		================
		*/
		createBuffer( descriptor ) {
			const buffer = {
				label: descriptor.label,
				mapState: "unmapped",
				/*
				================
				mapAsync
				================
				*/
				mapAsync() {
					return new Promise( ( resolve, reject ) => {
						pendingMaps.push( { resolve, reject, buffer } );
					} );
				},
				/*
				================
				getMappedRange
				================
				*/
				getMappedRange() {
					return samples.buffer;
				},
				/*
				================
				unmap
				================
				*/
				unmap() {
					buffer.mapState = "unmapped";
				},
				/*
				================
				destroy
				================
				*/
				destroy() {
					for ( const pending of pendingMaps.splice( 0 ) ) {
						pending.reject( Object.assign( Error( "Buffer unmapped" ), { name: "AbortError" } ) );
					}
				}
			};
			return buffer;
		},
		/*
		================
		createQuerySet
		================
		*/
		createQuerySet() {
			return { destroy() {} };
		},
		/*
		================
		createBindGroup
		================
		*/
		createBindGroup() {
			return {};
		},
		/*
		================
		createCommandEncoder
		================
		*/
		createCommandEncoder() {
			return {
				beginRenderPass: () => ({
					setPipeline() {},
					setBindGroup() {},
					beginOcclusionQuery() {},
					draw() {},
					endOcclusionQuery() {},
					end() {}
				}),
				resolveQuerySet() {},
				copyBufferToBuffer() {},
				finish: () => ({})
			};
		},
		queue: { writeBuffer() {}, submit() {} }
	});
	const owner = createParticleQuery( device, "bgra8unorm" );
	return {
		owner,
		/*
		================
		armSamples
		================
		*/
		armSamples( values ) {
			samples = new BigUint64Array( values );
		},
		/*
		================
		pendingMap

		The one map the running query is waiting on, once it gets there.
		================
		*/
		async pendingMap() {
			for ( let i = 0; i < 100 && !pendingMaps.length; i++ ) await Promise.resolve();
			assert.equal( pendingMaps.length, 1, "the query never reached its map" );
			return pendingMaps[0];
		},
		/*
		================
		ask

		One point's query, not awaited.
		================
		*/
		ask() {
			return owner.query(
				new Float32Array( 4 ),
				new Float32Array( 16 ),
				/** @type {any} */ ({}),
				/** @type {any} */ ({})
			);
		}
	};
}

test("a query disposed during its map ends quietly with no point visible", async t => {
	const f = fixture( t );
	const answer = f.ask();
	await f.pendingMap();
	f.owner.dispose();
	assert.deepEqual( await answer, [ false ] );
	// A fresh call on the disposed owner is still a caller error.
	await assert.rejects( f.ask(), /Invalid particle query/ );
});

test("a rejection on a live owner is an error and keeps travelling", async t => {
	const f = fixture( t );
	const answer = f.ask();
	const pending = await f.pendingMap();
	pending.reject( Error( "device exploded" ) );
	await assert.rejects( answer, /device exploded/ );
	f.owner.dispose();
});

test("a completed map answers from its samples", async t => {
	const f = fixture( t );
	f.armSamples( [ 3n ] );
	const answer = f.ask();
	const pending = await f.pendingMap();
	pending.buffer.mapState = "mapped";
	pending.resolve();
	assert.deepEqual( await answer, [ true ] );
	f.owner.dispose();
});

test("disposal after the map lands still ends quietly", async t => {
	const f = fixture( t );
	f.armSamples( [ 3n ] );
	const answer = f.ask();
	const pending = await f.pendingMap();
	pending.resolve();
	f.owner.dispose();
	assert.deepEqual( await answer, [ false ] );
});
