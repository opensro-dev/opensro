/*
===========================================================================

music-error.test.mjs - tests for music-error.ts

Each MediaError code and the sink-failure message get their own incident
text, and the browser's message travels with it (#4581).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { musicElementError } = await import( sourceFileUrl( "src/engine/foundation/audio/music-error.ts" ).href );
const TRACK = "/assets/audio/music/battlearena_1.mp3";

test("each media error code names its own failure", () => {
	assert.equal( musicElementError( { code: 1, message: "" }, TRACK ), `Music load was aborted: ${TRACK}` );
	assert.equal(
		musicElementError( { code: 2, message: "net" }, TRACK ),
		`Music stream failed to load: ${TRACK} (net)`
	);
	assert.equal( musicElementError( { code: 3, message: "" }, TRACK ), `Music could not be decoded: ${TRACK}` );
	assert.equal(
		musicElementError( { code: 4, message: "MEDIA_ELEMENT_ERROR: Format error" }, TRACK ),
		`Music format is not supported: ${TRACK} (MEDIA_ELEMENT_ERROR: Format error)`
	);
	assert.equal( musicElementError( { code: 9 }, TRACK ), `Music element failed (code 9): ${TRACK}` );
});

test("a decode code whose message names the output sink is an unavailable output, not a broken file", () => {
	assert.equal(
		musicElementError( { code: 3, message: "NS_ERROR_DOM_MEDIA_MEDIASINK_ERR (0x806e000b)" }, TRACK ),
		`Music could not play: the audio output is unavailable: ${TRACK} (NS_ERROR_DOM_MEDIA_MEDIASINK_ERR (0x806e000b))`
	);
});

test("an error event with no MediaError still names the track", () => {
	assert.equal( musicElementError( null, TRACK ), `Music element failed: ${TRACK}` );
	assert.equal( musicElementError( undefined, undefined ), "Music element failed: unknown track" );
});
