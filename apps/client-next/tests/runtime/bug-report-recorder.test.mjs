/*
===========================================================================

bug-report-recorder.test.mjs - the replay loop on machines that differ

The recorder runs against fake WebCodecs and DOM globals: a PC whose only
H.264 encoder is software, and an encoder the browser fails mid-session
(Chrome reclaims idle codecs in background tabs). Both used to leave the
player with a screenshot-only report.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createReplayRecorder } = await import( sourceFileUrl( "src/engine/runtime/bug-report/recorder.ts" ).href );
const { replayReportState } = await import( sourceFileUrl( "src/engine/foundation/media/replay-window.ts" ).href );

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
		setAttribute() {},
		remove() {},
		play: async () => {},
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
	const canvas = { width: 1280, height: 720, captureStream: () => ({ getTracks: () => [] }) };
	return {
		canvas,
		encoders,
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
