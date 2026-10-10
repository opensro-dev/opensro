/*
===========================================================================

read-bytes.ts - bounded stream reads

Stateless stream operations shared by the asset and simulation workers. The
caller owns cancellation and the returned bytes.

===========================================================================
*/

/*
================
isResponseByteLimitError

Observed overflow is corruption evidence; backend errors and cancellation are
not. A discriminant preserves that distinction without matching error prose.
================
*/
export function isResponseByteLimitError( error: unknown ): boolean {
	return error instanceof Error && "code" in error && error.code === "ASSET_BYTE_LIMIT";
}

/*
================
readBytes

Reads a stream into one buffer, failing once it exceeds limit bytes. A
single-chunk stream returns that chunk's own buffer instead of a copy.
An aborted signal cancels the reader and throws its reason: a cancelled
read must never return the bytes received so far as a whole response.
================
*/
export async function readBytes(
	stream: ReadableStream<Uint8Array>,
	limit: number,
	received?: ( bytes: number ) => void,
	signal?: AbortSignal
): Promise<Uint8Array<ArrayBuffer>> {
	if ( !Number.isSafeInteger( limit ) || limit < 1 ) throw new Error( "Invalid byte limit" );
	signal?.throwIfAborted();
	const reader = stream.getReader(), chunks: Uint8Array[] = [];
	const abort = () => void reader.cancel( signal?.reason ).catch( () => {} );
	signal?.addEventListener( "abort", abort, { once: true } );
	let size = 0;
	try {
		while ( true ) {
			const part = await reader.read();
			signal?.throwIfAborted();
			if ( part.done ) break;
			size += part.value.byteLength;
			if ( size > limit ) {
				throw Object.assign( new Error( "Response exceeds byte limit" ), { code: "ASSET_BYTE_LIMIT" } );
			}
			chunks.push( part.value );
			received?.( part.value.byteLength );
		}
	} catch ( error ) {
		// Not awaited: a source whose cancel never settles must not hold the read.
		void reader.cancel().catch( () => {} );
		throw error;
	} finally {
		signal?.removeEventListener( "abort", abort );
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
