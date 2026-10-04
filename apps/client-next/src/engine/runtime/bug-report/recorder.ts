/*
===========================================================================

recorder.ts - the bug reporter's rolling replay of the game, with sound

Keeps the last minute of the canvas as H.264, and of the game's sound as
AAC, in memory, so a player who just saw a bug can attach what happened.
Nothing leaves the page unless the player sends a report.

Video path (measured on a windowed Chrome, issue #90):

	canvas.captureStream(30) -> hidden <video> -> requestVideoFrameCallback
	-> 720p OffscreenCanvas -> VideoEncoder (realtime H.264, 5 Mbps)

Reading the WebGPU canvas directly (new VideoFrame(canvas)) in the frame
callback returns black frames on a real display: the canvas is discarded
once presented. The stream copies each presented frame, and the <video>
route works where MediaStreamTrackProcessor does not exist (Safari).

Sound path: the audio owner's capture stream (effects and music, as the
player hears them) -> MediaStreamTrackProcessor -> AudioEncoder (AAC-LC).
Browsers without the processor record video only. Audio timestamps come
from the audio clock; the first frame maps them onto the video's clock.

The video ring always starts on a key frame (one every two seconds),
which is what lets a clip be cut out of it without re-encoding; audio
older than the oldest kept video frame is dropped with it.

===========================================================================
*/
import type { Mp4Audio, Mp4Sample, Mp4Track } from "@/engine/foundation/media/mp4";
import { replayAudioFrom, replayKeepFrom, replaySize } from "@/engine/foundation/media/replay-window";

const CAPTURE_FPS = 30;
// Recorded quality; reports larger than the upload cap are re-encoded to
// fit (transcode.ts) while this original stays on the player's device.
export const RECORD_BITRATE = 5000000;
// Key frames by wall time, not frame count: the game's frame rate varies,
// and clip cuts snap back to the previous key frame.
const KEY_FRAME_MS = 2000;
// Chrome reads "prefer-hardware" as hardware only: on a PC without an H.264
// encoder (older or blocklisted GPUs, remote desktops, most Linux) the
// config is unsupported and the replay never starts. Let it fall back.
export const REPLAY_HARDWARE: HardwareAcceleration = "no-preference";
// After an encoder failure (Chrome reclaims idle codecs in background tabs)
// the loop reopens one this long after the error instead of stopping for
// the rest of the session.
const ENCODER_RETRY_MS = 5000;
// Frames waiting in the encoder before new ones are skipped instead of
// queueing memory and latency behind a slow encoder.
const MAX_ENCODE_QUEUE = 4;
const MAX_AUDIO_QUEUE = 32;
// rVFC can fire at display rate; keep the encoded rate at CAPTURE_FPS.
const MIN_FRAME_MS = 1000 / CAPTURE_FPS - 2;
// Main profile, level 3.1: 1280x720 at 30 fps.
export const VIDEO_CODEC = "avc1.4d401f";
const AUDIO_CODEC = "mp4a.40.2";
const AUDIO_BITRATE = 128000;
const STILL_QUALITY = 0.85;

// Chrome's MediaStreamTrackProcessor; absent from TypeScript's DOM library
// and from Safari, which therefore records video only.
declare const MediaStreamTrackProcessor:
	| { new( init: { track: MediaStreamTrack; } ): { readonly readable: ReadableStream<AudioData>; }; }
	| undefined;

/*
================
RecorderSettings
================
*/
export interface RecorderSettings {
	readonly windowSeconds: number;
}

/*
================
ReplayRecorder
================
*/
export interface ReplayRecorder {
	/** Starts the loop; resolves false when the browser cannot encode. */
	start( settings: RecorderSettings ): Promise<boolean>;
	/** Stops recording and forgets every buffered frame. */
	stop(): void;
	running(): boolean;
	/** A frozen copy of the buffered replay, or null when there is none. */
	snapshot(): Mp4Track | null;
	/** A JPEG of the current canvas, for reports without a replay. */
	still(): Promise<Blob | null>;
	/** The last capture or encoder failure, for the report's diagnostics. */
	lastError(): string | null;
	/** Frames skipped because the encoder fell behind, since the loop started. */
	dropped(): number;
}

/*
================
createReplayRecorder

`sound` returns the audio owner's capture stream, or null until the page
has one (an audio context needs a user gesture); it is polled per frame.
================
*/
export function createReplayRecorder( canvas: HTMLCanvasElement, sound: () => MediaStream | null ): ReplayRecorder {
	let settings: RecorderSettings | null = null;
	let stream: MediaStream | null = null;
	let video: HTMLVideoElement | null = null;
	let encoder: VideoEncoder | null = null;
	let scaled: OffscreenCanvas | null = null;
	let context: OffscreenCanvasRenderingContext2D | null = null;
	let size: readonly [number, number] = [ 0, 0 ];
	let samples: Mp4Sample[] = [];
	let avcC: Uint8Array | null = null;
	let lastKeyMs = -Infinity;
	let lastFrameMs = -Infinity;
	let dropped = 0;
	let error: string | null = null;
	let failedAtMs = -Infinity;
	let audioTrack: MediaStreamTrack | null = null;
	let audioEncoder: AudioEncoder | null = null;
	let audioSamples: Mp4Sample[] = [];
	let audioFormat: Omit<Mp4Audio, "samples"> | null = null;
	let audioOffsetUs: number | null = null;
	// Bumped by every start/stop so callbacks of a torn-down loop do nothing.
	let generation = 0;

	/*
	================
	encoderConfig
	================
	*/
	function encoderConfig( width: number, height: number ): VideoEncoderConfig {
		return {
			codec: VIDEO_CODEC,
			width,
			height,
			bitrate: RECORD_BITRATE,
			framerate: CAPTURE_FPS,
			latencyMode: "realtime",
			hardwareAcceleration: REPLAY_HARDWARE,
			avc: { format: "avc" }
		};
	}

	/*
	================
	openEncoder

	A new size needs a new encoder and a new decoder record, and old frames
	cannot share an MP4 with new ones: the ring restarts.
	================
	*/
	function openEncoder( width: number, height: number ) {
		encoder?.close();
		samples = [];
		audioSamples = [];
		avcC = null;
		lastKeyMs = -Infinity;
		size = [ width, height ];
		scaled = new OffscreenCanvas( width, height );
		context = scaled.getContext( "2d", { alpha: false } );
		const owner = generation;
		encoder = new VideoEncoder( {
			output: ( chunk, metadata ) => {
				if ( owner !== generation ) return;
				const description = metadata?.decoderConfig?.description;
				if ( description ) avcC = copyBytes( description );
				const data = new Uint8Array( chunk.byteLength );
				chunk.copyTo( data );
				samples.push( { data, timestampUs: chunk.timestamp, key: chunk.type === "key" } );
				const keep = replayKeepFrom( samples, (settings?.windowSeconds ?? 60) * 1e6 );
				if ( keep > 0 ) samples.splice( 0, keep );
				const stale = replayAudioFrom( audioSamples, samples[0]!.timestampUs );
				if ( stale > 0 ) audioSamples.splice( 0, stale );
			},
			error: failure => {
				if ( owner !== generation ) return;
				error = "Replay encoder: " + String( failure );
				failedAtMs = performance.now();
				// The ring restarts with the next encoder (openEncoder).
				encoder = null;
			}
		} );
		encoder.configure( encoderConfig( width, height ) );
	}

	/*
	================
	frame

	One presented canvas frame, delivered by the hidden <video>.
	================
	*/
	function frame( owner: number, now: number ) {
		if ( owner !== generation || !video ) return;
		video.requestVideoFrameCallback( next => frame( owner, next ) );
		if ( now - lastFrameMs < MIN_FRAME_MS ) return;
		lastFrameMs = now;
		if ( !audioTrack ) listen( owner );
		const [width, height] = replaySize( video.videoWidth, video.videoHeight );
		if ( width !== size[0] || height !== size[1] || !encoder || encoder.state === "closed" ) {
			if ( now - failedAtMs < ENCODER_RETRY_MS ) return;
			openEncoder( width, height );
		}
		if ( !encoder || !context || !scaled ) return;
		if ( encoder.encodeQueueSize > MAX_ENCODE_QUEUE ) {
			dropped++;
			return;
		}
		context.drawImage( video, 0, 0, width, height );
		const image = new VideoFrame( scaled, { timestamp: Math.round( performance.now() * 1000 ) } );
		const keyFrame = now - lastKeyMs >= KEY_FRAME_MS;
		if ( keyFrame ) lastKeyMs = now;
		encoder.encode( image, { keyFrame } );
		image.close();
	}

	/*
	================
	listen

	Starts the sound once the audio owner has a capture stream. A clone of
	its track is read, so stopping the replay never silences the game.
	================
	*/
	function listen( owner: number ) {
		if ( typeof MediaStreamTrackProcessor !== "function" || typeof AudioEncoder !== "function" ) return;
		const track = sound()?.getAudioTracks()[0];
		if ( !track ) return;
		audioTrack = track.clone();
		const reader = new MediaStreamTrackProcessor( { track: audioTrack } ).readable.getReader();
		Promise.resolve().then( () => pumpAudio( owner, reader ) ).catch( failure => {
			if ( owner === generation ) error = "Replay audio: " + String( failure );
		} );
	}

	/*
	================
	pumpAudio
	================
	*/
	async function pumpAudio( owner: number, reader: ReadableStreamDefaultReader<AudioData> ) {
		for ( ;; ) {
			const { value, done } = await reader.read();
			if ( done || owner !== generation ) {
				value?.close();
				await reader.cancel().catch( () => {} );
				return;
			}
			if ( !audioEncoder && !(await openAudioEncoder( owner, value )) ) {
				value.close();
				await reader.cancel().catch( () => {} );
				return;
			}
			audioOffsetUs ??= Math.round( performance.now() * 1000 ) - value.timestamp;
			if ( audioEncoder!.encodeQueueSize <= MAX_AUDIO_QUEUE ) audioEncoder!.encode( value );
			value.close();
		}
	}

	/*
	================
	openAudioEncoder

	Configured from the first frame: the stream's rate and channel count are
	the audio context's, known only once it delivers.
	================
	*/
	async function openAudioEncoder( owner: number, first: AudioData ) {
		const sampleRate = first.sampleRate, channels = first.numberOfChannels;
		const config: AudioEncoderConfig = {
			codec: AUDIO_CODEC,
			sampleRate,
			numberOfChannels: channels,
			bitrate: AUDIO_BITRATE
		};
		const support = await AudioEncoder.isConfigSupported( config );
		if ( owner !== generation ) return false;
		if ( !support.supported ) {
			error = "Replay: AAC audio encoding is not supported; recording video only";
			return false;
		}
		audioEncoder = new AudioEncoder( {
			output: ( chunk, metadata ) => {
				if ( owner !== generation ) return;
				const description = metadata?.decoderConfig?.description;
				if ( description ) audioFormat = { sampleRate, channels, config: copyBytes( description ) };
				const data = new Uint8Array( chunk.byteLength );
				chunk.copyTo( data );
				audioSamples.push( { data, timestampUs: chunk.timestamp + (audioOffsetUs ?? 0), key: true } );
			},
			error: failure => {
				if ( owner !== generation ) return;
				error = "Replay audio encoder: " + String( failure );
				closeAudio();
			}
		} );
		audioEncoder.configure( config );
		return true;
	}

	/*
	================
	closeAudio
	================
	*/
	function closeAudio() {
		if ( audioEncoder && audioEncoder.state !== "closed" ) audioEncoder.close();
		audioEncoder = null;
		audioTrack?.stop();
		audioTrack = null;
		audioSamples = [];
		audioFormat = null;
		audioOffsetUs = null;
	}

	/*
	================
	start

	Synchronous on purpose: the Option window starts it from inside the
	frame, which may not run async functions. The work continues in the
	promise callbacks.
	================
	*/
	function start( next: RecorderSettings ): Promise<boolean> {
		if ( typeof VideoEncoder !== "function" || typeof canvas.captureStream !== "function" ) {
			error = "Replay: this browser has no WebCodecs video encoder";
			return Promise.resolve( false );
		}
		if ( video && settings?.windowSeconds === next.windowSeconds ) return Promise.resolve( true );
		stop();
		const owner = generation;
		const [width, height] = replaySize( canvas.width, canvas.height );
		return VideoEncoder.isConfigSupported( encoderConfig( width, height ) ).then( support => {
			if ( owner !== generation ) return false;
			if ( !support.supported ) {
				error = "Replay: H.264 encoding is not supported";
				return false;
			}
			return capture( next );
		} );
	}

	/*
	================
	capture

	Opens the stream and its hidden <video>, then waits for playback.
	================
	*/
	function capture( next: RecorderSettings ): Promise<boolean> {
		settings = next;
		const owner = ++generation;
		stream = canvas.captureStream( CAPTURE_FPS );
		const source = document.createElement( "video" );
		video = source;
		source.muted = true;
		source.playsInline = true;
		source.setAttribute( "aria-hidden", "true" );
		source.className = "sro-replay-source";
		source.srcObject = stream;
		document.body.append( source );
		return source.play().then( () => {
			if ( owner !== generation ) return false;
			source.requestVideoFrameCallback( now => frame( owner, now ) );
			error = null;
			return true;
		}, failure => {
			if ( owner === generation ) {
				error = "Replay: capture did not start: " + String( failure );
				stop();
			}
			return false;
		} );
	}

	/*
	================
	stop
	================
	*/
	function stop() {
		generation++;
		if ( encoder && encoder.state !== "closed" ) encoder.close();
		encoder = null;
		closeAudio();
		for ( const track of stream?.getTracks() ?? [] ) track.stop();
		stream = null;
		video?.remove();
		video = null;
		scaled = null;
		context = null;
		samples = [];
		avcC = null;
		size = [ 0, 0 ];
		settings = null;
		lastFrameMs = -Infinity;
		failedAtMs = -Infinity;
		dropped = 0;
	}

	/*
	================
	snapshot
	================
	*/
	function snapshot(): Mp4Track | null {
		if ( !avcC || !samples.length ) return null;
		const track = { width: size[0], height: size[1], avcC, samples: samples.slice() };
		if ( !audioFormat || !audioSamples.length ) return track;
		return { ...track, audio: { ...audioFormat, samples: audioSamples.slice() } };
	}

	/*
	================
	still

	Draws the last presented frame. Without a running loop a one-frame
	stream is opened just for this, for the same black-frame reason.
	================
	*/
	async function still(): Promise<Blob | null> {
		let source = video, temporary: MediaStream | null = null;
		try {
			if ( !source ) {
				if ( typeof canvas.captureStream !== "function" ) return null;
				temporary = canvas.captureStream( CAPTURE_FPS );
				source = document.createElement( "video" );
				source.muted = true;
				source.srcObject = temporary;
				await source.play();
				await new Promise<void>( resolve => source!.requestVideoFrameCallback( () => resolve() ) );
			}
			const [width, height] = replaySize( source.videoWidth, source.videoHeight );
			const image = new OffscreenCanvas( width, height );
			image.getContext( "2d", { alpha: false } )?.drawImage( source, 0, 0, width, height );
			return await image.convertToBlob( { type: "image/jpeg", quality: STILL_QUALITY } );
		} catch ( failure ) {
			error = "Screenshot: " + String( failure );
			return null;
		} finally {
			for ( const track of temporary?.getTracks() ?? [] ) track.stop();
		}
	}

	return {
		start,
		stop,
		running: () => video !== null,
		snapshot,
		still,
		lastError: () => error,
		dropped: () => dropped
	};
}

/*
================
copyBytes
================
*/
export function copyBytes( source: AllowSharedBufferSource ): Uint8Array {
	if ( ArrayBuffer.isView( source ) ) {
		return new Uint8Array( source.buffer.slice( source.byteOffset, source.byteOffset + source.byteLength ) );
	}
	return new Uint8Array( source.slice( 0 ) );
}
