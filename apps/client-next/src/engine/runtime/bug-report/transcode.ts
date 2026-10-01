/*
===========================================================================

transcode.ts - fitting a replay clip under the upload cap

The replay records at 5 Mbps; Discord takes 10 MiB per file on unboosted
servers. A clip over the cap is decoded and encoded again at the bitrate
that fits (audio is copied unchanged), and the full-quality original stays
on the player's device (archive.ts). Variable bitrate encoders overshoot,
so an attempt that still does not fit is repeated lower.

Decoding and encoding are bounded by their queues: the decoder is fed only
while both have room, waiting on their "dequeue" events.

===========================================================================
*/
import type { Mp4Sample, Mp4Track } from "@/engine/foundation/media/mp4";
import { replayBitrate, replayTrackBytes } from "@/engine/foundation/media/replay-window";
import { copyBytes, VIDEO_CODEC } from "./recorder";

const MAX_QUEUE = 8;
const KEY_FRAME_US = 2000000;
// Successive bitrate scales when an attempt overshoots the cap.
const ATTEMPTS = [ 1, 0.8, 0.6 ] as const;

/*
================
fitTrack

The clip itself when it already fits; otherwise a re-encoded copy that does.
`progress` receives 0..1 across all attempts.
================
*/
export async function fitTrack(
	track: Mp4Track,
	maxBytes: number,
	progress: ( fraction: number ) => void
): Promise<Mp4Track> {
	if ( replayTrackBytes( track ) <= maxBytes ) return track;
	const first = track.samples[0]!.timestampUs, last = track.samples[track.samples.length - 1]!.timestampUs;
	const seconds = Math.max( 1, (last - first) / 1e6 );
	const audioBytes = replayTrackBytes( { ...track, samples: [] } );
	for ( const [attempt, scale] of ATTEMPTS.entries() ) {
		const bitrate = Math.floor( replayBitrate( maxBytes, seconds, audioBytes ) * scale );
		const result = await reencode( track, bitrate, fraction => progress( (attempt + fraction) / ATTEMPTS.length ) );
		if ( replayTrackBytes( result ) <= maxBytes ) {
			progress( 1 );
			return result;
		}
	}
	throw Error( "The clip could not be compressed under the upload limit; choose a shorter part" );
}

/*
================
reencode
================
*/
async function reencode( track: Mp4Track, bitrate: number, progress: ( fraction: number ) => void ): Promise<Mp4Track> {
	const samples: Mp4Sample[] = [];
	let avcC: Uint8Array | null = null, failure: unknown = null, done = 0, lastKeyUs = -Infinity;
	const encoder = new VideoEncoder( {
		output: ( chunk, metadata ) => {
			const description = metadata?.decoderConfig?.description;
			if ( description ) avcC = copyBytes( description );
			const data = new Uint8Array( chunk.byteLength );
			chunk.copyTo( data );
			samples.push( { data, timestampUs: chunk.timestamp, key: chunk.type === "key" } );
		},
		error: error => {
			failure = error;
		}
	} );
	encoder.configure( {
		codec: VIDEO_CODEC,
		width: track.width,
		height: track.height,
		bitrate,
		framerate: 30,
		latencyMode: "quality",
		hardwareAcceleration: "prefer-hardware",
		avc: { format: "avc" }
	} );
	const decoder = new VideoDecoder( {
		output: frame => {
			const keyFrame = frame.timestamp - lastKeyUs >= KEY_FRAME_US;
			if ( keyFrame ) lastKeyUs = frame.timestamp;
			encoder.encode( frame, { keyFrame } );
			frame.close();
			progress( ++done / track.samples.length );
		},
		error: error => {
			failure = error;
		}
	} );
	decoder.configure( {
		codec: VIDEO_CODEC,
		codedWidth: track.width,
		codedHeight: track.height,
		description: track.avcC,
		hardwareAcceleration: "prefer-hardware"
	} );
	try {
		for ( const sample of track.samples ) {
			while ( !failure && (decoder.decodeQueueSize > MAX_QUEUE || encoder.encodeQueueSize > MAX_QUEUE) ) {
				await dequeued( decoder.decodeQueueSize > MAX_QUEUE ? decoder : encoder );
			}
			if ( failure ) throw failure;
			decoder.decode(
				new EncodedVideoChunk( {
					type: sample.key ? "key" : "delta",
					timestamp: sample.timestampUs,
					data: sample.data
				} )
			);
		}
		await decoder.flush();
		await encoder.flush();
		if ( failure ) throw failure;
	} finally {
		if ( decoder.state !== "closed" ) decoder.close();
		if ( encoder.state !== "closed" ) encoder.close();
	}
	if ( !avcC || !samples.length ) throw Error( "Re-encoding produced no video" );
	return { ...track, avcC, samples };
}

/*
================
dequeued

Resolves when the codec takes the next item off its queue.
================
*/
function dequeued( codec: VideoDecoder | VideoEncoder ): Promise<void> {
	return new Promise( resolve => codec.addEventListener( "dequeue", () => resolve(), { once: true } ) );
}
