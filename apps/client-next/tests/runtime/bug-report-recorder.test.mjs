/*
===========================================================================

bug-report-recorder.test.mjs - the replay loop on machines that differ

The recorder runs against fake WebCodecs and DOM globals: a PC whose only
H.264 encoder is software, and an encoder the browser fails mid-session
(Chrome reclaims idle codecs in background tabs), a source video the
browser paused and a capture track that ended. Each used to leave the
player with a screenshot-only report for the rest of the session.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createReplayRecorder } = await import( sourceFileUrl( "src/engine/runtime/bug-report/recorder.ts" ).href );
const { replayReportState } = await import( sourceFileUrl( "src/engine/foundation/media/replay-window.ts" ).href );
const { replayNote } = await import( sourceFileUrl( "src/engine/runtime/bug-report/dialog.ts" ).href );

/*
================
installFakes

A machine with or without a hardware H.264 encoder: without one, Chrome
reports "prefer-hardware" unsupported (measured on headless Chromium 153).
================
*/
function installFakes( hardware ) {
	const encoders = [];
	let pending = null;
	class FakeEncoder {
		static async isConfigSupported( config ) {
			return { supported: hardware || config.hardwareAcceleration !== "prefer-hardware" };
		}
		constructor( init ) {
			this.init = init;
			this.state = "unconfigured";
			this.encodeQueueSize = 0;
			encoders.push( this );
		}
		configure() {
			this.state = "configured";
		}
		encode( frame, options ) {
			this.init.output(
				{ byteLength: 4, copyTo() {}, timestamp: frame.timestamp, type: options.keyFrame ? "key" : "delta" },
				{ decoderConfig: { description: Uint8Array.of( 1 ) } }
			);
		}
		close() {
			this.state = "closed";
		}
		fail() {
			this.state = "closed";
			this.init.error( new Error( "Codec reclaimed due to inactivity." ) );
		}
	}
	const video = {
		videoWidth: 1280,
		videoHeight: 720,
		paused: false,
		plays: 0,
		setAttribute() {},
		remove() {},
		async play() {
			video.plays++;
			video.paused = false;
		},
		requestVideoFrameCallback( callback ) {
			pending = callback;
		}
	};
	// Partial fakes of the browser globals the recorder reads.
	const page = /** @type {any} */ (globalThis);
	page.VideoEncoder = FakeEncoder;
	page.VideoFrame = class {
		constructor( source, init ) {
			this.timestamp = init.timestamp;
		}
		close() {}
	};
	page.OffscreenCanvas = class {
		getContext() {
			return { drawImage() {} };
		}
	};
	page.document = { createElement: () => video, body: { append() {} } };
	const track = { readyState: "live", stop() {} };
	let captures = 0;
	const canvas = {
		width: 1280,
		height: 720,
		captureStream() {
			captures++;
			return { getTracks: () => [ track ], getVideoTracks: () => [ track ] };
		}
	};
	return {
		canvas,
		encoders,
		video,
		track,
		captures: () => captures,
		// One presented frame, offset milliseconds after the test's start.
		present( offsetMs ) {
			const callback = pending;
			pending = null;
			callback?.( performance.now() + offsetMs );
		}
	};
}

test("a PC with only a software H.264 encoder records a replay", async () => {
	const fake = installFakes( false );
	const recorder = createReplayRecorder( fake.canvas, () => null );
	assert.equal( await recorder.start( { windowSeconds: 60 } ), true, recorder.lastError() );
	for ( let i = 0; i < 10; i++ ) fake.present( i * 40 );
	assert.ok( recorder.snapshot()?.samples.length > 0 );
	recorder.stop();
});

test("a failed encoder is reopened after a pause instead of ending the replay", async () => {
	const fake = installFakes( true );
	const recorder = createReplayRecorder( fake.canvas, () => null );
	await recorder.start( { windowSeconds: 60 } );
	fake.present( 0 );
	assert.equal( fake.encoders.length, 1 );
	fake.encoders[0].fail();
	assert.equal( recorder.running(), true, "the loop survives the failure" );
	assert.match( recorder.lastError(), /reclaimed/ );
	fake.present( 1000 );
	assert.equal( fake.encoders.length, 1, "no reopen inside the retry pause" );
	fake.present( 6000 );
	assert.equal( fake.encoders.length, 2, "a new encoder after the pause" );
	fake.present( 6040 );
	assert.ok( recorder.snapshot()?.samples.length > 0, "the ring fills again" );
	recorder.stop();
});

test("a report without a clip says why", () => {
	assert.equal( replayReportState( false, false, null ), "off" );
	assert.equal( replayReportState( true, true, null ), "recording, not attached" );
	assert.equal(
		replayReportState( true, false, "Replay: H.264 encoding is not supported" ),
		"not recording: Replay: H.264 encoding is not supported"
	);
	assert.equal( replayReportState( true, false, null ), "recording, nothing buffered yet" );
});

test("a source video the browser paused is resumed, once per retry pause", async () => {
	const fake = installFakes( true );
	const recorder = createReplayRecorder( fake.canvas, () => null );
	await recorder.start( { windowSeconds: 60 } );
	const before = fake.video.plays;
	fake.video.paused = true;
	recorder.watch();
	assert.equal( fake.video.plays, before + 1, "the paused source is asked to play" );
	fake.video.paused = true;
	recorder.watch();
	assert.equal( fake.video.plays, before + 1, "no second request inside the retry pause" );
	assert.equal( recorder.running(), true );
	recorder.stop();
});

test("an ended capture track restarts the capture", async () => {
	const fake = installFakes( true );
	const recorder = createReplayRecorder( fake.canvas, () => null );
	await recorder.start( { windowSeconds: 60 } );
	assert.equal( fake.captures(), 1 );
	fake.track.readyState = "ended";
	recorder.watch();
	fake.track.readyState = "live";
	await Promise.resolve();
	assert.equal( fake.captures(), 2, "a new capture stream" );
	assert.equal( recorder.running(), true );
	fake.present( 0 );
	fake.present( 40 );
	assert.ok( recorder.snapshot()?.samples.length > 0, "the new capture records" );
	recorder.stop();
});

test("a browser without WebCodecs is unsupported, not retried", async () => {
	installFakes( true );
	delete /** @type {any} */ (globalThis).VideoEncoder;
	const recorder = createReplayRecorder( { width: 1, height: 1, captureStream() {} }, () => null );
	assert.equal( await recorder.start( { windowSeconds: 60 } ), false );
	assert.equal( recorder.unsupported(), true );
	assert.equal( recorder.running(), false );
});

test("the report window says why there is no clip and what to do", () => {
	assert.match( replayNote( "off", null ), /turn on "Record bug replay" in the Option window/ );
	assert.match( replayNote( "starting", null ), /still starting/ );
	assert.match(
		replayNote( "restarting", "Replay: capture did not start" ),
		/stopped \(Replay: capture did not start\) and is restarting/
	);
	assert.match( replayNote( "unsupported", "Replay: H.264 encoding is not supported" ), /cannot record/ );
	for ( const state of [ "off", "starting", "restarting", "unsupported" ] ) {
		assert.match( replayNote( state, null ), /screenshot will be attached/ );
	}
});
