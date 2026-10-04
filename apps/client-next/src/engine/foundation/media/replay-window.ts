/*
===========================================================================

replay-window.ts - the arithmetic of the bug reporter's rolling replay

The recorder keeps encoded frames for the last minute. A decodable piece
of H.264 must start on a key frame, so every cut here moves its start back
to the nearest key frame instead of dropping frames the clip depends on.
The bitrate is derived from the server's upload cap so a full window
always fits one report.

===========================================================================
*/
import type { Mp4Sample, Mp4Track } from "./mp4";

// Share of the upload cap the video may use; the rest is headroom for the
// encoder overshooting its target and for the request's other parts.
const BITRATE_BUDGET = 0.9;
const MIN_BITRATE = 250000;
const MAX_BITRATE = 5000000;
// Selections arrive as float seconds; a key frame's own time converted back
// to microseconds can land just below it and must still select that frame.
const TIME_EPSILON_US = 1;
// The recorder's frame bound: Main profile level 3.1 holds 1280x720.
const REPLAY_MAX_WIDTH = 1280;
const REPLAY_MAX_HEIGHT = 720;

/*
================
replayKeepFrom

Index of the first sample to keep so the ring still covers windowUs before
its newest sample: the last key frame at or before that cutoff.
================
*/
export function replayKeepFrom( samples: readonly Mp4Sample[], windowUs: number ): number {
	const newest = samples[samples.length - 1];
	if ( !newest ) return 0;
	const cutoff = newest.timestampUs - windowUs;
	let keep = 0;
	for ( let index = 0; index < samples.length && samples[index]!.timestampUs <= cutoff; index++ ) {
		if ( samples[index]!.key ) keep = index;
	}
	return keep;
}

/*
================
replayClip

The samples a player's selection [startUs, endUs] needs, starting at the
key frame at or before startUs. Empty when no key frame precedes the end.
================
*/
export function replayClip( samples: readonly Mp4Sample[], startUs: number, endUs: number ): Mp4Sample[] {
	let first = -1;
	for (
		let index = 0;
		index < samples.length && samples[index]!.timestampUs <= Math.max( startUs, endUs );
		index++
	) {
		if ( samples[index]!.key && (first < 0 || samples[index]!.timestampUs <= startUs + TIME_EPSILON_US) ) {
			first = index;
		}
	}
	if ( first < 0 ) return [];
	return samples.slice( first ).filter( sample => sample.timestampUs <= endUs );
}

/*
================
replayBytes
================
*/
export function replayBytes( samples: readonly Mp4Sample[] ): number {
	return samples.reduce( ( sum, sample ) => sum + sample.data.length, 0 );
}

/*
================
replayBitrate

Video bits per second that keep `seconds` of video, plus `otherBytes`
(the audio, copied unchanged), under maxBytes.
================
*/
export function replayBitrate( maxBytes: number, seconds: number, otherBytes = 0 ): number {
	const target = Math.floor( (maxBytes * BITRATE_BUDGET - otherBytes) * 8 / Math.max( 1, seconds ) );
	return Math.min( MAX_BITRATE, Math.max( MIN_BITRATE, target ) );
}

/*
================
replayAudioFrom

Index of the first audio sample at or after `fromUs`: audio older than the
oldest kept video frame has nothing to accompany.
================
*/
export function replayAudioFrom( samples: readonly Mp4Sample[], fromUs: number ): number {
	const index = samples.findIndex( sample => sample.timestampUs >= fromUs );
	return index < 0 ? samples.length : index;
}

/*
================
replayClipTrack

A whole track cut to [startUs, endUs]: the video from the key frame at or
before startUs, and the audio playing between that frame and endUs.
================
*/
export function replayClipTrack( track: Mp4Track, startUs: number, endUs: number ): Mp4Track {
	const samples = replayClip( track.samples, startUs, endUs );
	const from = samples[0]?.timestampUs ?? Infinity;
	const audio = track.audio ?
		{
			...track.audio,
			samples: track.audio.samples.filter( sample => sample.timestampUs >= from && sample.timestampUs <= endUs )
		} :
		undefined;
	return { ...track, samples, ...(audio ? { audio } : {}) };
}

/*
================
replayTrackBytes
================
*/
export function replayTrackBytes( track: Mp4Track ): number {
	return replayBytes( track.samples ) + replayBytes( track.audio?.samples ?? [] );
}

/*
================
replaySize

Encoded size for a canvas: scaled to fit 1280x720 (H.264 level 3.1's
largest frame) with its aspect ratio kept, both sides even (4:2:0). An
ultrawide canvas gets fewer lines rather than being squeezed to 16:9.
================
*/
export function replaySize( width: number, height: number ): readonly [number, number] {
	if ( width <= 0 || height <= 0 ) return [ REPLAY_MAX_WIDTH, REPLAY_MAX_HEIGHT ];
	const scale = Math.min( 1, REPLAY_MAX_WIDTH / width, REPLAY_MAX_HEIGHT / height );
	const h = Math.max( 2, Math.floor( height * scale / 2 ) * 2 );
	const w = Math.min( REPLAY_MAX_WIDTH, Math.round( width / height * h / 2 ) * 2 );
	return [ Math.max( 2, w ), h ];
}

/*
================
replayKeyTimes

Seconds from the first sample to every key frame: the only places a clip
can start without re-encoding.
================
*/
export function replayKeyTimes( samples: readonly Mp4Sample[] ): number[] {
	const base = samples[0]?.timestampUs ?? 0;
	return samples.flatMap( sample => sample.key ? [ (sample.timestampUs - base) / 1e6 ] : [] );
}

/*
================
replaySnapStart

The key frame time nearest to `seconds` that still leaves `minSeconds`
before `endSeconds`; the first key frame when none does.
================
*/
export function replaySnapStart(
	keys: readonly number[],
	seconds: number,
	endSeconds: number,
	minSeconds: number
): number {
	let best = keys[0] ?? 0;
	for ( const key of keys ) {
		if ( key > endSeconds - minSeconds ) break;
		if ( Math.abs( key - seconds ) < Math.abs( best - seconds ) ) best = key;
	}
	return best;
}

/*
================
replayReportState

The report's Replay line when no clip went with it. A player who sees only
a screenshot is told nothing, so the line carries the recorder's own reason
for staff to read.
================
*/
export function replayReportState( enabled: boolean, buffered: boolean, lastError: string | null ): string {
	if ( !enabled ) return "off";
	if ( buffered ) return "recording, not attached";
	return lastError ? "not recording: " + lastError : "recording, nothing buffered yet";
}
