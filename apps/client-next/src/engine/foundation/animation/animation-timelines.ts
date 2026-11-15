/*
===========================================================================

animation-timelines.ts - key-time brackets for a clip's channels

The channels of a clip whose authored key times are byte-identical share
one bracket calculation: one timeline. A native clip keys every bone at
the same times, so its channels (73 on average for a player body) form a
single timeline. A pose keeps, for a clip it samples, which timeline
each channel reads and one mutable cursor per timeline, not a record per
channel. Sampled values and animation event state are never shared.

===========================================================================
*/
import type { CharacterClip } from "@/engine/contracts/character";

// What a pose retains for a clip it sampled: the timeline record and its
// arrays, a channel's timeline index, and one cursor per timeline.
// characterPoseBytes charges these.
export const TIMELINE_CLIP_BYTES = 128;
export const TIMELINE_INDEX_BYTES = 4;
export const TIMELINE_CURSOR_BYTES = 80;

type TimedClip = { readonly channels: readonly { readonly times: Float32Array; }[]; };

/*
================
ClipTimelines

A clip's timelines: the key times of each distinct timeline, and for each
channel the index of the timeline it reads.
================
*/
export interface ClipTimelines {
	readonly times: readonly Float32Array[];
	readonly index: Uint32Array;
}

/*
================
sameTimes

Word-for-word equality, so -0 and 0 or two NaN payloads stay distinct.
================
*/
function sameTimes( a: Float32Array, b: Float32Array ) {
	if ( a.length !== b.length ) return false;
	const left = new Uint32Array( a.buffer, a.byteOffset, a.length ),
		right = new Uint32Array( b.buffer, b.byteOffset, b.length );
	for ( let i = 0; i < left.length; i++ ) if ( left[i] !== right[i] ) return false;
	return true;
}

/*
================
clipTimelines

The clip's timelines. A hash collision never aliases two timelines:
equal hashes still compare every word.
================
*/
export function clipTimelines( clip: TimedClip ): ClipTimelines {
	const buckets = new Map<number, number[]>(), times: Float32Array[] = [];
	const index = new Uint32Array( clip.channels.length );
	for ( let channel = 0; channel < clip.channels.length; channel++ ) {
		const keys = clip.channels[channel]!.times,
			words = new Uint32Array( keys.buffer, keys.byteOffset, keys.length );
		let hash = 2166136261;
		for ( const word of words ) hash = Math.imul( hash ^ word, 16777619 );
		let bucket = buckets.get( hash );
		if ( !bucket ) {
			bucket = [];
			buckets.set( hash, bucket );
		}
		let timeline = bucket.find( candidate => sameTimes( times[candidate]!, keys ) );
		if ( timeline === undefined ) {
			timeline = times.length;
			times.push( keys );
			bucket.push( timeline );
		}
		index[channel] = timeline;
	}
	return { times, index };
}

/*
================
clipTimelineBytes

What one pose retains once it has sampled the clip.
================
*/
export function clipTimelineBytes( clip: TimedClip ): number {
	return TIMELINE_CLIP_BYTES + clip.channels.length * TIMELINE_INDEX_BYTES +
		clipTimelines( clip ).times.length * TIMELINE_CURSOR_BYTES;
}

/*
================
TimelineCursor

A timeline's bracket at the last sampled time: the key at or before it,
the next key, their span and the fraction between them.
================
*/
export interface TimelineCursor {
	readonly times: Float32Array;
	low: number;
	next: number;
	span: number;
	fraction: number;
}

/*
================
createAnimationTimelines

A pose's cursors over one clip. Channel c reads cursors[index[c]].
================
*/
export function createAnimationTimelines( clip: CharacterClip | TimedClip ) {
	const plan = clipTimelines( clip );
	const cursors: TimelineCursor[] = plan.times.map( times => ({ times, low: 0, next: 0, span: 0, fraction: 0 }) );
	return {
		index: plan.index,
		cursors,
		/*
		================
		sample

		Moves every cursor to time: the last key at or before it (a binary
		search unless the previous bracket still holds), clamped at the ends.
		================
		*/
		sample( time: number ) {
			for ( const cursor of cursors ) {
				const times = cursor.times;
				let low = cursor.low, high = times.length - 1;
				if ( !(times[low]! <= time && (low === high || time < times[low + 1]!)) ) {
					low = 0;
					while ( low < high ) {
						const mid = Math.ceil( (low + high) / 2 );
						if ( times[mid]! <= time ) low = mid;
						else high = mid - 1;
					}
				}
				cursor.low = low;
				cursor.next = Math.min( low + 1, times.length - 1 );
				cursor.span = times[cursor.next]! - times[low]!;
				cursor.fraction = cursor.span ? Math.max( 0, Math.min( 1, (time - times[low]!) / cursor.span ) ) : 0;
			}
		}
	};
}
