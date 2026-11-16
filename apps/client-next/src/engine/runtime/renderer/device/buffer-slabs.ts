/*
===========================================================================

buffer-slabs.ts - small GPU buffers carved from shared slabs

Every GPU buffer costs the browser's GPU process a placed allocation of its
own: on D3D12 at least 64 KB, measured at about 114 KB a buffer in Chrome,
however few bytes it holds. A world scene uploads several small buffers a
draw (material, transform, instances), thousands at the title stage, so
that overhead outweighed the data by four to one. A slab set hands out
aligned ranges of a few large buffers instead; a range binds as a
GPUBufferBinding (buffer, offset, size).

A range is freed through the device's retirement queue like a buffer: its
destroy() returns the bytes to the slab only once no recorded frame can
name them. Large requests keep a buffer of their own.

A staged set (createStagedSlabs) also keeps a CPU mirror of each buffer.
Its owner writes ranges into the mirror, in any order during a frame, and
flush sends each written buffer's dirty span in one queue.writeBuffer: a
few small ranges a frame cost one write, not one write a range. Every
byte of a staged buffer goes through write (the mirror is the buffer's
whole truth, so a flushed span may cover neighbours unchanged), and the
GPU never writes one.

===========================================================================
*/
import type { Retire } from "./retirement";

// One slab's size. A world scene's small buffers fit a few slabs.
const SLAB_BYTES = 4 << 20;
// A request above this keeps its own buffer: it already outweighs the
// allocation overhead, and it would fragment a slab.
const DEDICATED_BYTES = 1 << 20;
// WebGPU's default minUniformBufferOffsetAlignment and
// minStorageBufferOffsetAlignment; a device may only lower them.
const MIN_ALIGNMENT = 256;

/*
================
BufferSlice

One range of a slab (or a dedicated buffer). binding is the stable
GPUBufferBinding a bind group entry names; destroy hands it back.
================
*/
export interface BufferSlice {
	readonly binding: GPUBufferBinding & {
		readonly buffer: GPUBuffer;
		readonly offset: number;
		readonly size: number;
	};
	destroy(): void;
}

/*
================
Slab
================
*/
type Slab = {
	buffer: GPUBuffer;
	// Free ranges as [offset, size], sorted by offset and never adjacent.
	free: [number, number][];
	used: number;
};

/*
================
Stage

A staged buffer's mirror, as bytes and as the floats over the same
memory, and the byte span written since the last flush (empty while
low >= high).
================
*/
type Stage = {
	buffer: GPUBuffer;
	bytes: Uint8Array<ArrayBuffer>;
	floats: Float32Array<ArrayBuffer>;
	low: number;
	high: number;
};

/*
================
createBufferSlabs

Ranges of usage-typed slabs for one device generation. dispose destroys
every slab and dedicated buffer at once (the device is going away).
Bound ranges (uniforms, storage) align to the device's binding offset
alignment; a stride instead aligns vertex or index ranges to their
element, which draws address by baseVertex and firstIndex.
================
*/
export function createBufferSlabs( device: GPUDevice, label: string, usage: number, retire: Retire, stride = 0 ) {
	const { write: _write, flush: _flush, ...slabs } = slabSet( device, label, usage, retire, {
		stride,
		staged: false
	} );
	return slabs;
}

/*
================
createStagedSlabs

A set of bound ranges written through write and uploaded by flush, which
the owner calls before the submit whose commands read the ranges.
================
*/
export function createStagedSlabs( device: GPUDevice, label: string, usage: number, retire: Retire ) {
	return slabSet( device, label, usage, retire, { stride: 0, staged: true } );
}

/*
================
SlabLayout
================
*/
type SlabLayout = { readonly stride: number; readonly staged: boolean; };

/*
================
slabSet
================
*/
function slabSet( device: GPUDevice, label: string, usage: number, retire: Retire, layout: SlabLayout ) {
	const { stride, staged } = layout;
	const alignment = stride || Math.max(
		MIN_ALIGNMENT,
		device.limits?.minUniformBufferOffsetAlignment ?? 0,
		device.limits?.minStorageBufferOffsetAlignment ?? 0
	);
	const slabs: Slab[] = [], dedicated = new Set<GPUBuffer>();
	// A staged set's mirrors by buffer, and those written since the last flush.
	const stages = new Map<GPUBuffer, Stage>(), dirty = new Set<Stage>();
	/*
	================
	mirror

	A staged set mirrors a new buffer; an unstaged one allocates nothing.
	================
	*/
	function mirror( buffer: GPUBuffer, size: number ) {
		if ( !staged ) return;
		const bytes = new Uint8Array( size );
		stages.set( buffer, {
			buffer,
			bytes,
			floats: new Float32Array( bytes.buffer, 0, size >> 2 ),
			low: Infinity,
			high: 0
		} );
	}
	/*
	================
	unmirror

	A destroyed buffer's mirror goes with it: a flush must not write it.
	================
	*/
	function unmirror( buffer: GPUBuffer ) {
		const stage = stages.get( buffer );
		if ( !stage ) return;
		stages.delete( buffer );
		dirty.delete( stage );
	}
	/*
	================
	release

	Returns a range to its slab, merging it with free neighbours. One empty
	slab is kept for the next scene; a second empty one is destroyed, or a
	set's high-water mark would stay allocated after every scene that set
	it. Ranges reach here only through the retirement queue, so no recorded
	frame still names the slab.
	================
	*/
	function release( slab: Slab, offset: number, size: number ) {
		const free = slab.free;
		let i = 0;
		while ( i < free.length && free[i]![0] < offset ) i++;
		free.splice( i, 0, [ offset, size ] );
		const next = free[i + 1];
		if ( next && offset + size === next[0] ) {
			free[i]![1] += next[1];
			free.splice( i + 1, 1 );
		}
		const previous = free[i - 1];
		if ( previous && previous[0] + previous[1] === offset ) {
			previous[1] += free[i]![1];
			free.splice( i, 1 );
		}
		slab.used -= size;
		if ( !slab.used && slabs.some( other => other !== slab && !other.used ) ) {
			slabs.splice( slabs.indexOf( slab ), 1 );
			unmirror( slab.buffer );
			slab.buffer.destroy();
		}
	}
	/*
	================
	carve

	First fit in the slabs, a new slab when none has room.
	================
	*/
	function carve( size: number ): [Slab, number] {
		for ( const slab of slabs ) {
			const free = slab.free;
			for ( let i = 0; i < free.length; i++ ) {
				const range = free[i]!;
				if ( range[1] < size ) continue;
				const offset = range[0];
				range[0] += size;
				range[1] -= size;
				if ( !range[1] ) free.splice( i, 1 );
				slab.used += size;
				return [ slab, offset ];
			}
		}
		const slab: Slab = {
			buffer: device.createBuffer( { label, size: SLAB_BYTES, usage } ),
			free: [ [ size, SLAB_BYTES - size ] ],
			used: size
		};
		slabs.push( slab );
		mirror( slab.buffer, SLAB_BYTES );
		return [ slab, 0 ];
	}
	return {
		/*
		================
		allocate

		size bytes, bound as exactly size bytes at an aligned offset.
		================
		*/
		allocate( size: number ): BufferSlice {
			if ( !Number.isSafeInteger( size ) || size <= 0 ) throw Error( "Invalid slab allocation" );
			const span = Math.ceil( size / alignment ) * alignment;
			if ( span > DEDICATED_BYTES ) {
				const buffer = device.createBuffer( { label, size, usage } );
				dedicated.add( buffer );
				mirror( buffer, size );
				return {
					binding: Object.freeze( { buffer, offset: 0, size } ),
					destroy() {
						dedicated.delete( buffer );
						unmirror( buffer );
						buffer.destroy();
					}
				};
			}
			const [slab, offset] = carve( span );
			let live = true;
			return {
				binding: Object.freeze( { buffer: slab.buffer, offset, size } ),
				destroy() {
					if ( !live ) return;
					live = false;
					release( slab, offset, span );
				}
			};
		},
		/*
		================
		free

		Retires a range: its bytes return once the open frame closes.
		================
		*/
		free( slice: BufferSlice ) {
			retire( slice );
		},
		/*
		================
		write

		Copies the first bytes of source into slice's mirror range, at byte
		at within the range (staged sets only). The GPU sees them after the
		next flush. A whole Float32Array copies without a temporary view: the
		per-draw updates of a frame write hundreds of them.
		================
		*/
		write( slice: BufferSlice, source: ArrayBufferView, bytes: number, at = 0 ) {
			const { buffer, offset, size } = slice.binding, stage = stages.get( buffer );
			if (
				!stage || bytes % 4 || at % 4 || at < 0 || at + bytes > size || bytes > source.byteLength
			) throw Error( "Invalid staged slab write" );
			if ( !bytes ) return;
			const start = offset + at;
			// Same-type typed array copies keep every bit pattern (NaN payloads).
			if ( source instanceof Float32Array && bytes === source.byteLength ) stage.floats.set( source, start / 4 );
			else stage.bytes.set( new Uint8Array( source.buffer, source.byteOffset, bytes ), start );
			stage.low = Math.min( stage.low, start );
			stage.high = Math.max( stage.high, start + bytes );
			dirty.add( stage );
		},
		/*
		================
		flush

		One queue.writeBuffer a written buffer, covering its dirty span. The
		writes precede every later submit on the queue. Returns the count.
		================
		*/
		flush(): number {
			const writes = dirty.size;
			for ( const stage of dirty ) {
				const { low, high } = stage;
				device.queue.writeBuffer( stage.buffer, low, stage.bytes.buffer, low, high - low );
				stage.low = Infinity;
				stage.high = 0;
			}
			dirty.clear();
			return writes;
		},
		/*
		================
		stats
		================
		*/
		stats() {
			return {
				slabs: slabs.length,
				dedicated: dedicated.size,
				usedBytes: slabs.reduce( ( sum, slab ) => sum + slab.used, 0 )
			};
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			for ( const slab of slabs ) slab.buffer.destroy();
			for ( const buffer of dedicated ) buffer.destroy();
			slabs.length = 0;
			dedicated.clear();
			stages.clear();
			dirty.clear();
		}
	};
}
