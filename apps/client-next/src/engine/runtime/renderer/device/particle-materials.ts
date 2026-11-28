/*
===========================================================================

particle-materials.ts - resident material frame tables of the particle pass

A particle stream's material frames (a colour and a texture window a
frame) never change while the stream lives, and every stream of one
primitive names the same frames. The pass used to copy each stream's table
into its input page and upload the page every frame: 434 KB a frame for
120 effects, most of what the main thread and the GPU process uploaded
(#4692). This owner uploads a table once, when its first stream appears,
and hands every stream of those frames the same table.

A store is one GPU buffer no larger than the device's storage binding. A
table keeps its range until its last stream releases it; a released range
is never written again before beginFrame, so a pass already encoded this
frame still reads what it was encoded against. beginFrame, which runs
after the previous frame has submitted (particles.ts), destroys replaced
buffers and packs a store whose released ranges reach half of it.

===========================================================================
*/
import type { GpuWork } from "@/engine/foundation/rendering/gpu-work";
import type { ParticlePresentation } from "../internal/gpu-contract";
import type { Retire } from "./retirement";

// The least a storage binding of vec4f may be: the buffer a pass without
// material frames binds.
const BLANK_BYTES = 16;
// The smallest buffer the pass makes; larger ones double from it.
const INITIAL_BYTES = 16384;

/*
================
particleCapacity

The size of a particle pass buffer that holds bytes: doubled from
INITIAL_BYTES so growth is rare, and never past limit, the device's
largest storage binding.
================
*/
export function particleCapacity( bytes: number, limit: number ): number {
	if ( bytes > limit ) throw Error( "Particle buffer exceeds device storage limit" );
	let size = INITIAL_BYTES;
	while ( size < bytes ) size *= 2;
	return Math.min( size, limit );
}

type MaterialFrames = NonNullable<ParticlePresentation["frames"]>;

/*
================
MaterialStore

One resident buffer and its CPU mirror: top floats are in use, dead of
them by released tables. tables are in ascending start order.
================
*/
export type MaterialStore = {
	buffer?: GPUBuffer;
	mirror: Float32Array;
	top: number;
	dead: number;
	readonly tables: Set<MaterialTable>;
};

/*
================
MaterialTable

The colours then the windows of frames, at start floats in its store (a
multiple of four: the pass indexes vec4f). start moves only in beginFrame.
================
*/
export type MaterialTable = {
	readonly frames: MaterialFrames;
	readonly floats: number;
	readonly store: MaterialStore;
	start: number;
	users: number;
};

/*
================
createParticleMaterials

storageLimit is the device's largest storage binding: no store is larger.
================
*/
export function createParticleMaterials( device: GPUDevice, storageLimit: number, retire: Retire, work?: GpuWork ) {
	const stores: MaterialStore[] = [], replaced: GPUBuffer[] = [];
	const byFrames = new Map<MaterialFrames, MaterialTable>();
	let blank: GPUBuffer | undefined, uploads = 0;

	/*
	================
	upload

	Send floats of the store's mirror from start. A store that outgrew its
	buffer gets a larger one, whole; the old one stays readable until
	beginFrame, for the passes this frame already encoded.
	================
	*/
	function upload( store: MaterialStore, start: number, floats: number ) {
		if ( !store.buffer || store.buffer.size < store.top * 4 ) {
			if ( store.buffer ) replaced.push( store.buffer );
			store.buffer = device.createBuffer( {
				label: "particle-materials",
				size: particleCapacity( store.top * 4, storageLimit ),
				usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
			} );
			start = 0;
			floats = store.top;
		}
		device.queue.writeBuffer( store.buffer, start * 4, store.mirror.buffer as ArrayBuffer, start * 4, floats * 4 );
		work?.noteUpload( floats * 4 );
		uploads++;
	}

	/*
	================
	pack

	Close the gaps released tables left, keeping the tables' order, and
	upload the store again.
	================
	*/
	function pack( store: MaterialStore ) {
		let top = 0;
		for ( const table of store.tables ) {
			store.mirror.copyWithin( top, table.start, table.start + table.floats );
			table.start = top;
			top += table.floats;
		}
		store.top = top;
		store.dead = 0;
		upload( store, 0, top );
	}

	return {
		/*
		================
		acquire

		The table of frames for one more stream, uploaded now when it is the
		first.
		================
		*/
		acquire( frames: MaterialFrames ): MaterialTable {
			const known = byFrames.get( frames );
			if ( known ) {
				known.users++;
				return known;
			}
			const floats = frames.colors.length + frames.windows.length;
			if ( floats % 4 ) throw Error( "Particle material frames are not whole vec4f" );
			if ( floats * 4 > storageLimit ) throw Error( "Particle stream exceeds device storage limit" );
			let store = stores.find( candidate => (candidate.top + floats) * 4 <= storageLimit );
			if ( !store ) {
				store = { mirror: new Float32Array( 0 ), top: 0, dead: 0, tables: new Set() };
				stores.push( store );
			}
			const start = store.top, end = start + floats;
			if ( store.mirror.length < end ) {
				const grown = new Float32Array( particleCapacity( end * 4, storageLimit ) / 4 );
				grown.set( store.mirror.subarray( 0, start ) );
				store.mirror = grown;
			}
			store.mirror.set( frames.colors, start );
			store.mirror.set( frames.windows, start + frames.colors.length );
			store.top = end;
			const table: MaterialTable = { frames, floats, store, start, users: 1 };
			store.tables.add( table );
			byFrames.set( frames, table );
			upload( store, start, floats );
			return table;
		},
		/*
		================
		release

		One stream of the table is gone. The last one frees its range for the
		next pack.
		================
		*/
		release( table: MaterialTable ) {
			if ( --table.users ) return;
			byFrames.delete( table.frames );
			table.store.tables.delete( table );
			table.store.dead += table.floats;
		},
		/*
		================
		buffer

		What a pass binds for its material frames: the store's buffer, or a
		blank one for a pass none of whose streams has frames.
		================
		*/
		buffer( store: MaterialStore | undefined ): GPUBuffer {
			if ( store ) return store.buffer!;
			blank ??= device.createBuffer( {
				label: "particle-materials-none",
				size: BLANK_BYTES,
				usage: GPUBufferUsage.STORAGE
			} );
			return blank;
		},
		/*
		================
		beginFrame

		The previous frame has submitted: destroy the buffers growth replaced,
		drop empty stores and pack the ones half released.
		================
		*/
		beginFrame() {
			for ( const buffer of replaced ) retire( buffer );
			replaced.length = 0;
			for ( let i = stores.length - 1; i >= 0; i-- ) {
				const store = stores[i]!;
				if ( !store.dead || store.dead * 2 < store.top ) continue;
				if ( store.tables.size ) {
					pack( store );
					continue;
				}
				if ( store.buffer ) retire( store.buffer );
				stores.splice( i, 1 );
			}
		},
		stats: () => ({
			tables: byFrames.size,
			stores: stores.length,
			bytes: stores.reduce( ( sum, store ) => sum + store.top * 4, 0 ),
			uploads
		}),
		/*
		================
		dispose
		================
		*/
		dispose() {
			for ( const store of stores ) store.buffer?.destroy();
			for ( const buffer of replaced ) buffer.destroy();
			blank?.destroy();
			stores.length = replaced.length = 0;
			byFrames.clear();
			blank = undefined;
		}
	};
}
