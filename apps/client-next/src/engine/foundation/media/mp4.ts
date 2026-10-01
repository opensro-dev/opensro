/*
===========================================================================

mp4.ts - a minimal MP4 (ISO BMFF) writer: H.264 video plus optional AAC

The bug reporter's replay is encoded by WebCodecs: AVC samples in "avc"
format (length-prefixed NAL units) with an avcC decoder record, and raw
AAC frames with their AudioSpecificConfig. This file wraps them in the
smallest file every player accepts: ftyp, a moov placed before the media
(fast start, so Discord can play it while it downloads) and one mdat with
each track's samples as a single chunk (video, then audio).

Time zero is the first video sample. Audio that starts later is delayed
with an edit list (an empty edit), so lips and footsteps stay in sync.

Box layouts follow ISO/IEC 14496-12 (version 0 boxes throughout),
14496-15 for the avc1 sample entry and 14496-14 for mp4a/esds. The client
may not import npm packages, which is why this exists.

===========================================================================
*/

// Video media clock: 90 kHz, the usual video timescale; movie header in ms.
const VIDEO_TIMESCALE = 90000;
const MOVIE_TIMESCALE = 1000;
const MICROSECONDS = 1000000;
// A trailing video sample has no successor to measure; assume this frame time.
const FALLBACK_SAMPLE_US = 33333;
// An AAC-LC frame always carries 1024 samples per channel.
const AAC_FRAME_SAMPLES = 1024;
const UNDETERMINED_LANGUAGE = 0x55c4;
const VIDEO_TRACK_ID = 1;
const AUDIO_TRACK_ID = 2;
// MPEG-4 Audio (14496-3) and the AudioStream type in an ES descriptor.
const OBJECT_TYPE_AAC = 0x40;
const STREAM_TYPE_AUDIO = 0x15;

/*
================
Mp4Sample

One encoded access unit. timestampUs is the presentation time; samples
arrive in decode order, which equals presentation order without B-frames
(the realtime encoder configuration the recorder uses never emits them).
================
*/
export interface Mp4Sample {
	readonly data: Uint8Array;
	readonly timestampUs: number;
	readonly key: boolean;
}

/*
================
Mp4Audio

AAC-LC frames and the AudioSpecificConfig the encoder described them with.
Timestamps share the video samples' clock.
================
*/
export interface Mp4Audio {
	readonly sampleRate: number;
	readonly channels: number;
	readonly config: Uint8Array;
	readonly samples: readonly Mp4Sample[];
}

/*
================
Mp4Track
================
*/
export interface Mp4Track {
	readonly width: number;
	readonly height: number;
	readonly avcC: Uint8Array;
	readonly samples: readonly Mp4Sample[];
	readonly audio?: Mp4Audio;
}

/*
================
TrackLayout

One trak's timing, computed once and used for both moov passes.
================
*/
interface TrackLayout {
	readonly durations: number[];
	readonly mediaDuration: number;
	readonly movieDuration: number;
	// Movie-time delay before the track's first sample (an empty edit).
	readonly delay: number;
}

/*
================
muxMp4
================
*/
export function muxMp4( track: Mp4Track ): Uint8Array {
	if ( !track.samples.length ) throw Error( "MP4 needs at least one sample" );
	if ( !track.samples[0]!.key ) throw Error( "MP4 must start on a key frame" );
	const audio = track.audio?.samples.length ? track.audio : undefined;
	const video = videoLayout( track.samples );
	const sound = audio ? audioLayout( audio, track.samples[0]!.timestampUs ) : undefined;
	const videoBytes = sampleBytes( track.samples ), audioBytes = audio ? sampleBytes( audio.samples ) : 0;
	const ftyp = box( "ftyp", ascii( "isom" ), u32( 512 ), ascii( "isomiso2avc1mp41" ) );
	// Chunk offsets depend on the moov size, which does not depend on the
	// offsets' values: build once to measure, then again with real offsets.
	const measured = moovBox( track, video, sound, 0, 0 );
	const media = ftyp.length + measured.length + 8;
	const moov = moovBox( track, video, sound, media, media + videoBytes );
	const out = new Uint8Array( media + videoBytes + audioBytes );
	let at = 0;
	for ( const part of [ ftyp, moov, u32( 8 + videoBytes + audioBytes ), ascii( "mdat" ) ] ) {
		out.set( part, at );
		at += part.length;
	}
	for ( const sample of [ ...track.samples, ...(audio?.samples ?? []) ] ) {
		out.set( sample.data, at );
		at += sample.data.length;
	}
	return out;
}

/*
================
videoLayout

Each sample lasts until its successor, in the 90 kHz media clock.
================
*/
function videoLayout( samples: readonly Mp4Sample[] ): TrackLayout {
	const durations = samples.map( ( sample, index ) => {
		const next = samples[index + 1];
		const us = next ? Math.max( 1, next.timestampUs - sample.timestampUs ) : FALLBACK_SAMPLE_US;
		return Math.max( 1, Math.round( us * VIDEO_TIMESCALE / MICROSECONDS ) );
	} );
	const mediaDuration = durations.reduce( ( sum, value ) => sum + value, 0 );
	return {
		durations,
		mediaDuration,
		movieDuration: Math.round( mediaDuration * MOVIE_TIMESCALE / VIDEO_TIMESCALE ),
		delay: 0
	};
}

/*
================
audioLayout

AAC frames have a fixed length; their timestamps only place the first one.
================
*/
function audioLayout( audio: Mp4Audio, zeroUs: number ): TrackLayout {
	const durations = audio.samples.map( () => AAC_FRAME_SAMPLES );
	const mediaDuration = durations.length * AAC_FRAME_SAMPLES;
	const delayUs = Math.max( 0, audio.samples[0]!.timestampUs - zeroUs );
	return {
		durations,
		mediaDuration,
		movieDuration: Math.round( mediaDuration * MOVIE_TIMESCALE / audio.sampleRate ),
		delay: Math.round( delayUs * MOVIE_TIMESCALE / MICROSECONDS )
	};
}

/*
================
moovBox
================
*/
function moovBox(
	track: Mp4Track,
	video: TrackLayout,
	sound: TrackLayout | undefined,
	videoOffset: number,
	audioOffset: number
) {
	const duration = Math.max( video.movieDuration, sound ? sound.delay + sound.movieDuration : 0 );
	const mvhd = fullBox(
		"mvhd",
		0,
		0,
		u32( 0 ),
		u32( 0 ),
		u32( MOVIE_TIMESCALE ),
		u32( duration ),
		u32( 0x00010000 ),
		u16( 0x0100 ),
		zeros( 10 ),
		matrix(),
		zeros( 24 ),
		u32( sound ? AUDIO_TRACK_ID + 1 : VIDEO_TRACK_ID + 1 )
	);
	const traks = [
		trakBox( {
			id: VIDEO_TRACK_ID,
			layout: video,
			timescale: VIDEO_TIMESCALE,
			handler: "vide",
			handlerName: "VideoHandler",
			header: fullBox( "vmhd", 0, 1, zeros( 8 ) ),
			entry: avc1Box( track ),
			samples: track.samples,
			offset: videoOffset,
			width: track.width,
			height: track.height,
			sync: true
		} )
	];
	if ( sound && track.audio ) {
		traks.push( trakBox( {
			id: AUDIO_TRACK_ID,
			layout: sound,
			timescale: track.audio.sampleRate,
			handler: "soun",
			handlerName: "SoundHandler",
			header: fullBox( "smhd", 0, 0, zeros( 4 ) ),
			entry: mp4aBox( track.audio ),
			samples: track.audio.samples,
			offset: audioOffset,
			width: 0,
			height: 0,
			sync: false
		} ) );
	}
	return box( "moov", mvhd, ...traks );
}

/*
================
TrakSpec
================
*/
interface TrakSpec {
	readonly id: number;
	readonly layout: TrackLayout;
	readonly timescale: number;
	readonly handler: "vide" | "soun";
	readonly handlerName: string;
	readonly header: Uint8Array;
	readonly entry: Uint8Array;
	readonly samples: readonly Mp4Sample[];
	readonly offset: number;
	readonly width: number;
	readonly height: number;
	// Video lists its key frames; in audio every sample is a sync sample.
	readonly sync: boolean;
}

/*
================
trakBox
================
*/
function trakBox( spec: TrakSpec ) {
	const { layout } = spec;
	const tkhd = fullBox(
		"tkhd",
		0,
		3,
		u32( 0 ),
		u32( 0 ),
		u32( spec.id ),
		zeros( 4 ),
		u32( layout.delay + layout.movieDuration ),
		zeros( 8 ),
		u16( 0 ),
		u16( 0 ),
		u16( spec.handler === "soun" ? 0x0100 : 0 ),
		zeros( 2 ),
		matrix(),
		u32( spec.width * 65536 ),
		u32( spec.height * 65536 )
	);
	const mdhd = fullBox(
		"mdhd",
		0,
		0,
		u32( 0 ),
		u32( 0 ),
		u32( spec.timescale ),
		u32( layout.mediaDuration ),
		u16( UNDETERMINED_LANGUAGE ),
		u16( 0 )
	);
	const hdlr = fullBox(
		"hdlr",
		0,
		0,
		u32( 0 ),
		ascii( spec.handler ),
		zeros( 12 ),
		ascii( spec.handlerName + "\0" )
	);
	const dinf = box( "dinf", fullBox( "dref", 0, 0, u32( 1 ), fullBox( "url ", 0, 1 ) ) );
	const tables = [
		fullBox( "stsd", 0, 0, u32( 1 ), spec.entry ),
		sttsBox( layout.durations ),
		...(spec.sync ? [ stssBox( spec.samples ) ] : []),
		fullBox( "stsc", 0, 0, u32( 1 ), u32( 1 ), u32( spec.samples.length ), u32( 1 ) ),
		fullBox(
			"stsz",
			0,
			0,
			u32( 0 ),
			u32( spec.samples.length ),
			...spec.samples.map( sample => u32( sample.data.length ) )
		),
		fullBox( "stco", 0, 0, u32( 1 ), u32( spec.offset ) )
	];
	const minf = box( "minf", spec.header, dinf, box( "stbl", ...tables ) );
	const parts = [ tkhd ];
	if ( layout.delay > 0 ) parts.push( editBox( layout ) );
	parts.push( box( "mdia", mdhd, hdlr, minf ) );
	return box( "trak", ...parts );
}

/*
================
editBox

An empty edit of `delay`, then the whole media at normal rate.
================
*/
function editBox( layout: TrackLayout ) {
	const entry = ( duration: number, mediaTime: number ) => [ u32( duration ), u32( mediaTime ), u32( 0x00010000 ) ];
	return box(
		"edts",
		fullBox( "elst", 0, 0, u32( 2 ), ...entry( layout.delay, -1 ), ...entry( layout.movieDuration, 0 ) )
	);
}

/*
================
avc1Box

One avc1 visual sample entry carrying the encoder's avcC record.
================
*/
function avc1Box( track: Mp4Track ) {
	return box(
		"avc1",
		zeros( 6 ),
		u16( 1 ),
		zeros( 16 ),
		u16( track.width ),
		u16( track.height ),
		u32( 0x00480000 ),
		u32( 0x00480000 ),
		zeros( 4 ),
		u16( 1 ),
		zeros( 32 ),
		u16( 0x0018 ),
		u16( 0xffff ),
		box( "avcC", track.avcC )
	);
}

/*
================
mp4aBox

The mp4a audio sample entry with its esds: an ES descriptor holding the
decoder configuration and the encoder's AudioSpecificConfig.
================
*/
function mp4aBox( audio: Mp4Audio ) {
	const decoderSpecific = descriptor( 0x05, audio.config );
	const decoderConfig = descriptor(
		0x04,
		Uint8Array.of( OBJECT_TYPE_AAC, STREAM_TYPE_AUDIO, 0, 0, 0 ),
		u32( 0 ),
		u32( 0 ),
		decoderSpecific
	);
	const es = descriptor(
		0x03,
		u16( AUDIO_TRACK_ID ),
		Uint8Array.of( 0 ),
		decoderConfig,
		descriptor( 0x06, Uint8Array.of( 2 ) )
	);
	return box(
		"mp4a",
		zeros( 6 ),
		u16( 1 ),
		zeros( 8 ),
		u16( audio.channels ),
		u16( 16 ),
		zeros( 4 ),
		u32( audio.sampleRate * 65536 ),
		fullBox( "esds", 0, 0, es )
	);
}

/*
================
descriptor

An MPEG-4 descriptor: tag, then its size in the four-byte expandable form.
================
*/
function descriptor( tag: number, ...parts: Uint8Array[] ) {
	const size = parts.reduce( ( sum, part ) => sum + part.length, 0 );
	return concat( [
		Uint8Array.of( tag, 0x80 | size >> 21 & 0x7f, 0x80 | size >> 14 & 0x7f, 0x80 | size >> 7 & 0x7f, size & 0x7f ),
		...parts
	] );
}

/*
================
sttsBox

Run-length table of sample durations.
================
*/
function sttsBox( durations: readonly number[] ) {
	const runs: [number, number][] = [];
	for ( const duration of durations ) {
		const last = runs[runs.length - 1];
		if ( last && last[1] === duration ) last[0]++;
		else runs.push( [ 1, duration ] );
	}
	return fullBox(
		"stts",
		0,
		0,
		u32( runs.length ),
		...runs.flatMap( ( [count, duration] ) => [ u32( count ), u32( duration ) ] )
	);
}

/*
================
stssBox

1-based numbers of the key (sync) samples.
================
*/
function stssBox( samples: readonly Mp4Sample[] ) {
	const keys = samples.flatMap( ( sample, index ) => sample.key ? [ index + 1 ] : [] );
	return fullBox( "stss", 0, 0, u32( keys.length ), ...keys.map( u32 ) );
}

/*
================
sampleBytes
================
*/
function sampleBytes( samples: readonly Mp4Sample[] ) {
	return samples.reduce( ( sum, sample ) => sum + sample.data.length, 0 );
}

/*
================
box
================
*/
function box( type: string, ...parts: Uint8Array[] ): Uint8Array {
	return concat( [ u32( 8 + parts.reduce( ( sum, part ) => sum + part.length, 0 ) ), ascii( type ), ...parts ] );
}

/*
================
fullBox

A box whose payload opens with a version byte and 24 bits of flags.
================
*/
function fullBox( type: string, version: number, flags: number, ...parts: Uint8Array[] ): Uint8Array {
	return box( type, u32( (version << 24 | flags) >>> 0 ), ...parts );
}

/*
================
concat
================
*/
function concat( parts: readonly Uint8Array[] ): Uint8Array {
	const out = new Uint8Array( parts.reduce( ( sum, part ) => sum + part.length, 0 ) );
	let at = 0;
	for ( const part of parts ) {
		out.set( part, at );
		at += part.length;
	}
	return out;
}

/*
================
matrix

The identity transform in 16.16 / 2.30 fixed point.
================
*/
function matrix() {
	const out = new Uint8Array( 36 ), view = new DataView( out.buffer );
	view.setUint32( 0, 0x00010000 );
	view.setUint32( 16, 0x00010000 );
	view.setUint32( 32, 0x40000000 );
	return out;
}

/*
================
u32
================
*/
function u32( value: number ) {
	const out = new Uint8Array( 4 );
	new DataView( out.buffer ).setUint32( 0, value >>> 0 );
	return out;
}

/*
================
u16
================
*/
function u16( value: number ) {
	const out = new Uint8Array( 2 );
	new DataView( out.buffer ).setUint16( 0, value & 0xffff );
	return out;
}

/*
================
zeros
================
*/
function zeros( count: number ) {
	return new Uint8Array( count );
}

/*
================
ascii
================
*/
function ascii( text: string ) {
	return Uint8Array.from( text, character => character.charCodeAt( 0 ) );
}
