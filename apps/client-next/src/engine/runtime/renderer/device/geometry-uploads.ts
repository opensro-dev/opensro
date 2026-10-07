/*
===========================================================================

geometry-uploads.ts - CPU stream snapshots and their submission upload arena

Destinations and bindings stay owned by geometry. One queue write fills a
copy source; ordered copies precede the frame's compute and render commands.
Only CPU-owned streams enter here. Each submit consumes its snapshots before
the next queue write can reuse the source, including deferred frame submits.

===========================================================================
*/
import { destroyNow, type Retire } from "./retirement";

const INITIAL_UPLOAD_BYTES = 16384;
const DEFAULT_MAX_BUFFER_BYTES = 256 * 1024 * 1024;

/*
================
UploadCopy

Byte offsets preserve partial updates, overlap order and untouched GPU bytes.
================
*/
type UploadCopy = {
	buffer: GPUBuffer;
	offset: number;
	start: number;
	size: number;
};

/*
================
createGeometryUploads
================
*/
export function createGeometryUploads( device: GPUDevice, retire: Retire = destroyNow ) {
	const limit = device.limits?.maxBufferSize ?? DEFAULT_MAX_BUFFER_BYTES;
	const copies: UploadCopy[] = [];
	let active = false, used = 0, data = new Uint8Array( 0 );
	let staging: GPUBuffer | undefined, stagingBytes = 0;

	/*
	================
	submit

	The upload and frame command buffers share one submit. Reusing staging
	after this call is ordered after all readers by the GPU queue.
	================
	*/
	function submit( commands?: GPUCommandBuffer ) {
		if ( !used ) {
			if ( commands ) device.queue.submit( [ commands ] );
			return;
		}
		if ( stagingBytes < used ) {
			const next = device.createBuffer( {
				label: "geometry-upload-arena",
				size: data.byteLength,
				usage: GPUBufferUsage.COPY_SRC | GPUBufferUsage.COPY_DST
			} );
			if ( staging ) retire( staging );
			staging = next;
			stagingBytes = data.byteLength;
		}
		device.queue.writeBuffer( staging!, 0, data.buffer as ArrayBuffer, 0, used );
		const encoder = device.createCommandEncoder( { label: "geometry-uploads" } );
		for ( const copy of copies ) {
			encoder.copyBufferToBuffer( staging!, copy.start, copy.buffer, copy.offset, copy.size );
		}
		const uploads = encoder.finish();
		device.queue.submit( commands ? [ uploads, commands ] : [ uploads ] );
		used = 0;
		copies.length = 0;
	}

	return {
		/*
		================
		beginFrame
		================
		*/
		beginFrame() {
			active = true;
		},
		/*
		================
		write

		Snapshot bytes immediately: packing scratch and caller views may change
		before submission. Byte copies also preserve floating-point bit patterns.
		================
		*/
		write( buffer: GPUBuffer, offset: number, source: Float32Array ) {
			const size = source.byteLength;
			if ( active && used + size > limit ) submit();
			if ( !active || !size || size > limit ) {
				device.queue.writeBuffer( buffer, offset, source.buffer as ArrayBuffer, source.byteOffset, size );
				return;
			}
			if ( data.byteLength < used + size ) {
				let capacity = Math.min( INITIAL_UPLOAD_BYTES, limit );
				while ( capacity < used + size ) capacity = Math.min( capacity * 2, limit );
				const next = new Uint8Array( capacity );
				next.set( data.subarray( 0, used ) );
				data = next;
			}
			data.set( new Uint8Array( source.buffer, source.byteOffset, size ), used );
			copies.push( { buffer, offset, start: used, size } );
			used += size;
		},
		submit,
		/*
		================
		endFrame

		An abandoned frame still commits CPU updates, as queue.writeBuffer did.
		Flush while device retirement is open, including released destinations.
		================
		*/
		endFrame() {
			try {
				submit();
			} finally {
				active = false;
			}
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			copies.length = 0;
			used = 0;
			active = false;
			data = new Uint8Array( 0 );
			if ( staging ) retire( staging );
			staging = undefined;
			stagingBytes = 0;
		}
	};
}
