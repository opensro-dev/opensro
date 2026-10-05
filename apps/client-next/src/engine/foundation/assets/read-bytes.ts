/*
===========================================================================

read-bytes.ts - bounded stream reads

Stateless stream operations shared by the asset and simulation workers. The
caller owns cancellation and the returned bytes.

===========================================================================
*/

/*
================
readBytes

Reads a stream into one buffer, failing once it exceeds limit bytes. A
single-chunk stream returns that chunk's own buffer instead of a copy.
================
*/
export async function readBytes(
	stream: ReadableStream<Uint8Array>,
	limit: number,
	received?: ( bytes: number ) => void
): Promise<Uint8Array<ArrayBuffer>> {
	if ( !Number.isSafeInteger( limit ) || limit < 1 ) throw new Error( "Invalid byte limit" );
	const reader = stream.getReader(), chunks: Uint8Array[] = [];
	let size = 0;
	try {
		while ( true ) {
			const part = await reader.read();
			if ( part.done ) break;
			size += part.value.byteLength;
			if ( size > limit ) throw new Error( "Response exceeds byte limit" );
			chunks.push( part.value );
			received?.( part.value.byteLength );
		}
	} catch ( error ) {
		await reader.cancel().catch( () => {} );
		throw error;
	} finally {
		reader.releaseLock();
	}
	const only = chunks.length === 1 ? chunks[0]! : null;
	if (
		only && only.buffer instanceof ArrayBuffer && only.byteOffset === 0 &&
		only.byteLength === only.buffer.byteLength
	) {
		return only as Uint8Array<ArrayBuffer>;
	}
	const bytes = new Uint8Array( size );
	let offset = 0;
	for ( const chunk of chunks ) {
		bytes.set( chunk, offset );
		offset += chunk.length;
	}
	return bytes;
}

/*
================
gunzipBytes

Inflates gzip bytes already in memory, bounded by limit. The bytes enter the
DecompressionStream as one chunk: wrapping them in a Blob first registers
and copies them through the browser's blob registry (1 s of worker time in
a 17 s streaming trace). The input is read, never detached.
================
*/
export function gunzipBytes( bytes: Uint8Array<ArrayBuffer>, limit: number ): Promise<Uint8Array<ArrayBuffer>> {
	const source = new ReadableStream<BufferSource>( {
		/*
		================
		start
		================
		*/
		start( controller ) {
			controller.enqueue( bytes );
			controller.close();
		}
	} );
	return readBytes( source.pipeThrough( new DecompressionStream( "gzip" ) ), limit );
}
