/*
===========================================================================

bug-report-media.test.mjs - the replay window and the MP4 writer

The browser pieces (capture, WebCodecs) need a real page; what is pure
arithmetic and byte layout is checked here against the client's sources.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { muxMp4 } = await import( "../../src/engine/foundation/media/mp4.ts" );
const {
	replayKeepFrom,
	replayClip,
	replayBitrate,
	replaySize,
	replayBytes,
	replayRecoveryLink,
	replayLinkedReport
} = await import(
	"../../src/engine/foundation/media/replay-window.ts"
);

/*
================
samples

One sample every 33 ms, a key frame every `gop` samples.
================
*/
function samples( count, gop = 60 ) {
	return Array.from( { length: count }, ( _, index ) => ({
		data: Uint8Array.of( 0, 0, 0, 1, index & 255 ),
		timestampUs: index * 33333,
		key: index % gop === 0
	}) );
}

/*
================
boxes

Top-level ISO BMFF boxes as [type, offset, size].
================
*/
/** @returns {[string, number, number][]} */
function boxes( /** @type {Uint8Array} */ bytes, from = 0, to = bytes.length ) {
	const view = new DataView( bytes.buffer, bytes.byteOffset );
	/** @type {[string, number, number][]} */
	const out = [];
	for ( let at = from; at < to; ) {
		const size = view.getUint32( at ), type = String.fromCharCode( ...bytes.subarray( at + 4, at + 8 ) );
		assert.ok( size >= 8 && at + size <= to, `box ${type} overruns its parent` );
		out.push( [ type, at, size ] );
		at += size;
	}
	return out;
}

/*
================
find

Walks a path of container boxes ("moov/trak/mdia") and returns the last.
================
*/
/** @returns {[string, number, number]} */
function find( /** @type {Uint8Array} */ bytes, /** @type {string} */ path ) {
	let from = 0, to = bytes.length;
	/** @type {[string, number, number] | undefined} */
	let found;
	for ( const type of path.split( "/" ) ) {
		found = boxes( bytes, from, to ).find( ( [t] ) => t === type );
		assert.ok( found, `missing ${path}` );
		from = found[1] + 8;
		to = found[1] + found[2];
	}
	assert.ok( found, `empty path ${path}` );
	return found;
}

test("replayKeepFrom keeps the window and starts on a key frame", () => {
	const list = samples( 300 );
	const keep = replayKeepFrom( list, 4000000 );
	assert.equal( list[keep].key, true );
	const newest = list[list.length - 1], later = list[keep + 60];
	assert.ok( newest && later );
	assert.ok( newest.timestampUs - list[keep].timestampUs >= 4000000 );
	assert.ok( newest.timestampUs - later.timestampUs < 4000000 );
	assert.equal( replayKeepFrom( [], 1 ), 0 );
	assert.equal( replayKeepFrom( samples( 10 ), 60000000 ), 0 );
});

test("replayClip backs up to the key frame before the selection", () => {
	const list = samples( 300 );
	const clip = replayClip( list, list[130].timestampUs, list[200].timestampUs );
	assert.equal( clip[0], list[120] );
	assert.equal( clip.at( -1 ), list[200] );
	assert.deepEqual( replayClip( list, list[0].timestampUs, list[10].timestampUs ), list.slice( 0, 11 ) );
	assert.deepEqual( replayClip( list.slice( 1, 50 ), 0, list[40].timestampUs ), [] );
	assert.equal( replayBytes( clip ), clip.length * 5 );
});

test("replayBitrate keeps a full minute under the upload cap", () => {
	const bitrate = replayBitrate( 10 * 1024 * 1024, 60 );
	assert.ok( bitrate * 60 / 8 < 10 * 1024 * 1024 );
	assert.ok( bitrate > 1000000 );
	assert.equal( replayBitrate( 1, 60 ), 250000 );
	assert.equal( replayBitrate( 500 * 1024 * 1024, 60 ), 5000000 );
});

test("replaySize fits 1280x720, keeps the aspect ratio and stays even", () => {
	assert.deepEqual( replaySize( 1920, 1080 ), [ 1280, 720 ] );
	assert.deepEqual( replaySize( 1120, 720 ), [ 1120, 720 ] );
	assert.deepEqual( replaySize( 1024, 768 ), [ 960, 720 ] );
	assert.deepEqual( replaySize( 3440, 1440 ), [ 1276, 534 ] );
	assert.deepEqual( replaySize( 4000, 100 ), [ 1280, 32 ] );
	assert.deepEqual( replaySize( 640, 360 ), [ 640, 360 ] );
	assert.deepEqual( replaySize( 801, 451 ), [ 800, 450 ] );
});

test("muxMp4 writes ftyp, a fast-start moov and the samples in mdat", () => {
	const list = samples( 90, 30 ), avcC = Uint8Array.of( 1, 0x4d, 0, 0x1f, 0xff, 0xe0, 0 );
	const bytes = muxMp4( { width: 1280, height: 720, avcC, samples: list } );
	assert.deepEqual( boxes( bytes ).map( ( [type] ) => type ), [ "ftyp", "moov", "mdat" ] );

	const view = new DataView( bytes.buffer );
	const [, mdatAt] = find( bytes, "mdat" );
	const [, stcoAt] = find( bytes, "moov/trak/mdia/minf/stbl/stco" );
	assert.equal( view.getUint32( stcoAt + 16 ), mdatAt + 8, "chunk offset points at the samples" );
	assert.deepEqual( bytes.subarray( mdatAt + 8, mdatAt + 13 ), list[0].data );

	const [, stszAt] = find( bytes, "moov/trak/mdia/minf/stbl/stsz" );
	assert.equal( view.getUint32( stszAt + 16 ), 90 );
	const [, stssAt] = find( bytes, "moov/trak/mdia/minf/stbl/stss" );
	assert.deepEqual( [ 0, 1, 2 ].map( i => view.getUint32( stssAt + 16 + i * 4 ) ), [ 1, 31, 61 ] );
	assert.equal( view.getUint32( stssAt + 12 ), 3 );

	const [, mdhdAt] = find( bytes, "moov/trak/mdia/mdhd" );
	assert.equal( view.getUint32( mdhdAt + 20 ), 90000 );
	assert.equal( view.getUint32( mdhdAt + 24 ), 90 * 3000, "90 frames of 1/30 s at 90 kHz" );
	const [, avcCAt] = find( bytes, "moov/trak/mdia/minf/stbl/stsd" );
	assert.ok( bytes.subarray( avcCAt, avcCAt + 200 ).includes( 0x4d ) );
});

test("muxMp4 refuses clips that cannot decode", () => {
	assert.throws( () => muxMp4( { width: 2, height: 2, avcC: new Uint8Array( 7 ), samples: [] } ) );
	assert.throws( () =>
		muxMp4( { width: 2, height: 2, avcC: new Uint8Array( 7 ), samples: samples( 5 ).slice( 1 ) } )
	);
});

test("replayKeyTimes lists clip starts and replaySnapStart keeps a minimum clip", async () => {
	const { replayKeyTimes, replaySnapStart } = await import( "../../src/engine/foundation/media/replay-window.ts" );
	const list = samples( 300 );
	const keys = replayKeyTimes( list );
	assert.deepEqual( keys.map( k => +k.toFixed( 2 ) ), [ 0, 2, 4, 6, 8 ] );
	assert.equal( replaySnapStart( keys, 4.9, 9.9, 1 ), keys[2] );
	assert.equal( replaySnapStart( keys, 5.1, 9.9, 1 ), keys[3] );
	assert.equal( replaySnapStart( keys, 9.5, 9.9, 1 ), keys[4] );
	assert.equal( replaySnapStart( keys, 9.5, 5.5, 1 ), keys[2] );
	assert.equal( replaySnapStart( keys, 3, 0.5, 1 ), keys[0] );
	assert.equal( replaySnapStart( [], 3, 10, 1 ), 0 );
});

/*
================
audioFrames

AAC frames of 1024 samples at 48 kHz (21.333 ms), starting at `fromUs`.
================
*/
function audioFrames( count, fromUs = 0 ) {
	return Array.from( { length: count }, ( _, index ) => ({
		data: Uint8Array.of( 0x21, index & 255, 0x80 ),
		timestampUs: fromUs + Math.round( index * 1024 / 48000 * 1e6 ),
		key: true
	}) );
}

test("muxMp4 adds an AAC track after the video, delayed by an edit list", async () => {
	const { replayClipTrack, replayTrackBytes, replayAudioFrom } = await import(
		"../../src/engine/foundation/media/replay-window.ts"
	);
	const video = samples( 90, 30 ), config = Uint8Array.of( 0x11, 0x90 );
	const audio = { sampleRate: 48000, channels: 2, config, samples: audioFrames( 140, 50000 ) };
	const bytes = muxMp4( {
		width: 1280,
		height: 720,
		avcC: Uint8Array.of( 1, 0x4d, 0, 0x1f, 0xff, 0xe0, 0 ),
		samples: video,
		audio
	} );
	const view = new DataView( bytes.buffer );
	const [, moovAt, moovSize] = find( bytes, "moov" );
	const traks = boxes( bytes, moovAt + 8, moovAt + moovSize ).filter( ( [type] ) => type === "trak" );
	assert.equal( traks.length, 2 );
	const [, audioTrakAt, audioTrakSize] = traks[1];
	const audioBoxes = boxes( bytes, audioTrakAt + 8, audioTrakAt + audioTrakSize ).map( ( [type] ) => type );
	assert.deepEqual( audioBoxes, [ "tkhd", "edts", "mdia" ] );

	const sub = bytes.subarray( audioTrakAt, audioTrakAt + audioTrakSize );
	const [, elstAt] = find( sub, "trak/edts/elst" );
	const subView = new DataView( sub.buffer, sub.byteOffset );
	assert.equal( subView.getUint32( elstAt + 16 ), 50, "50 ms of silence before the first frame" );
	assert.equal( subView.getInt32( elstAt + 20 ), -1 );
	const [, stcoAt] = find( sub, "trak/mdia/minf/stbl/stco" );
	const [, mdatAt] = find( bytes, "mdat" );
	assert.equal(
		subView.getUint32( stcoAt + 16 ),
		mdatAt + 8 + replayBytes( video ),
		"audio chunk follows the video"
	);
	const [, mdhdAt] = find( sub, "trak/mdia/mdhd" );
	assert.equal( subView.getUint32( mdhdAt + 20 ), 48000 );
	assert.equal( subView.getUint32( mdhdAt + 24 ), 140 * 1024 );
	const [, esdsAt, esdsSize] = find( sub, "trak/mdia/minf/stbl/stsd" );
	assert.ok(
		Buffer.from( sub.subarray( esdsAt, esdsAt + esdsSize ) ).includes(
			Buffer.from( [ 0x05, 0x80, 0x80, 0x80, 2, 0x11, 0x90 ] )
		),
		"AudioSpecificConfig inside esds"
	);
	assert.equal( view.getUint32( mdatAt ), 8 + replayBytes( video ) + 140 * 3 );

	const track = { width: 2, height: 2, avcC: new Uint8Array( 7 ), samples: video, audio };
	const clip = replayClipTrack( track, video[35].timestampUs, video[70].timestampUs );
	assert.equal( clip.samples[0], video[30] );
	const clipAudio = clip.audio?.samples ?? [];
	const firstAudio = clipAudio[0], lastAudio = clipAudio[clipAudio.length - 1];
	assert.ok( firstAudio && lastAudio, "the clip keeps its audio" );
	assert.ok( firstAudio.timestampUs >= video[30].timestampUs );
	assert.ok( lastAudio.timestampUs <= video[70].timestampUs );
	assert.equal( replayTrackBytes( clip ), replayBytes( clip.samples ) + clipAudio.length * 3 );
	assert.equal( replayAudioFrom( audio.samples, 1e12 ), audio.samples.length );
	assert.equal( replayAudioFrom( audio.samples, 0 ), 0 );
});

test("zipStore writes an archive every unzip reads back byte for byte", async () => {
	const { zipStore, crc32 } = await import( "../../src/engine/foundation/archive/zip.ts" );
	const { execFileSync } = await import( "node:child_process" );
	const { mkdtempSync, writeFileSync, readFileSync } = await import( "node:fs" );
	const { tmpdir } = await import( "node:os" );
	const { join } = await import( "node:path" );
	assert.equal( crc32( new TextEncoder().encode( "123456789" ) ), 0xcbf43926 );
	const video = Uint8Array.from( { length: 70000 }, ( _, i ) => i * 7 & 255 );
	const json = new TextEncoder().encode( '{"id":"BR-ñ"}' );
	const zip = zipStore(
		[ { name: "replay.mp4", data: video }, { name: "report.json", data: json } ],
		new Date( 2026, 9, 1, 17, 40, 12 )
	);
	const dir = mkdtempSync( join( tmpdir(), "zip-" ) );
	writeFileSync( join( dir, "r.zip" ), zip );
	execFileSync( "unzip", [ "-q", "-t", "r.zip" ], { cwd: dir } );
	execFileSync( "unzip", [ "-q", "r.zip", "-d", "out" ], { cwd: dir } );
	assert.deepEqual( new Uint8Array( readFileSync( join( dir, "out", "replay.mp4" ) ) ), video );
	assert.equal( readFileSync( join( dir, "out", "report.json" ), "utf8" ), '{"id":"BR-ñ"}' );
});

test("replayClip keeps the selected key frame when its time comes back from float seconds", async () => {
	const { replayKeyTimes } = await import( "../../src/engine/foundation/media/replay-window.ts" );
	// Measured in a real replay: 8039200 / 1e6 * 1e6 === 8039199.999999999.
	const offsets = [ 0, 2001100, 4019400, 6021000, 8039200, 10057700 ];
	const list = offsets.flatMap( offset =>
		[ 0, 33333, 66666 ].map( ( step, index ) => ({
			data: Uint8Array.of( 0 ),
			timestampUs: 40_000_000 + offset + step,
			key: index === 0
		}) )
	);
	const base = list[0].timestampUs;
	for ( const key of replayKeyTimes( list ) ) {
		const clip = replayClip( list, base + key * 1e6, base + (key + 1) * 1e6 );
		assert.equal(
			clip[0].timestampUs,
			base + Math.round( key * 1e6 ),
			`clip at ${key} starts on its own key frame`
		);
	}
});

test("muxMp4 keeps audio after a skipped frame on its own time", async () => {
	const video = samples( 90, 30 ), config = Uint8Array.of( 0x11, 0x90 );
	// Frames 0-9, then 5 skipped by the recorder, then 10 more.
	const frames = audioFrames( 25 ).filter( ( _, index ) => index < 10 || index >= 15 );
	const bytes = muxMp4( {
		width: 1280,
		height: 720,
		avcC: Uint8Array.of( 1, 0x4d, 0, 0x1f, 0xff, 0xe0, 0 ),
		samples: video,
		audio: { sampleRate: 48000, channels: 2, config, samples: frames }
	} );
	const [, moovAt, moovSize] = find( bytes, "moov" );
	const [, audioTrakAt, audioTrakSize] = boxes( bytes, moovAt + 8, moovAt + moovSize )
		.filter( ( [type] ) => type === "trak" )[1];
	const sub = bytes.subarray( audioTrakAt, audioTrakAt + audioTrakSize );
	const view = new DataView( sub.buffer, sub.byteOffset );
	const [, mdhdAt] = find( sub, "trak/mdia/mdhd" );
	assert.equal( view.getUint32( mdhdAt + 24 ), 25 * 1024, "the track spans the hole" );
	const [, sttsAt] = find( sub, "trak/mdia/minf/stbl/stts" );
	/** @type {[number, number][]} */
	const runs = [];
	for ( let index = 0; index < view.getUint32( sttsAt + 12 ); index++ ) {
		runs.push( [ view.getUint32( sttsAt + 16 + index * 8 ), view.getUint32( sttsAt + 20 + index * 8 ) ] );
	}
	assert.deepEqual( runs, [ [ 9, 1024 ], [ 1, 6 * 1024 ], [ 10, 1024 ] ] );
});

test("the replay recovery link keeps the game page's path", () => {
	assert.equal(
		replayRecoveryLink( "https://example.org", "/play", "AB12CD" ),
		"https://example.org/play#bug=AB12CD"
	);
	assert.equal(
		replayRecoveryLink( "https://example.org", "/play/", "AB12CD" ),
		"https://example.org/play/#bug=AB12CD"
	);
	assert.equal( replayRecoveryLink( "http://127.0.0.1:5180", "/", "AB12CD" ), "http://127.0.0.1:5180/#bug=AB12CD" );
});

test("a recovery link names its report, whatever else the hash carries", () => {
	const id = "BR-261005-2009-70E4";
	const link = new URL( replayRecoveryLink( "https://example.org", "/play", id ) );
	assert.equal( replayLinkedReport( link.hash ), id );
	assert.equal( replayLinkedReport( "#overview&bug=" + id ), id );
	assert.equal( replayLinkedReport( "" ), null );
	assert.equal( replayLinkedReport( "#bug=not-a-report" ), null );
});
