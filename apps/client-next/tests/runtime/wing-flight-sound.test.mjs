/*
===========================================================================

wing-flight-sound.test.mjs - tests for wing-flight.ts (#3547)

Port-only, not native (owner 2026-11-21: the default, no row): a wing set
motion advances without its fallback footstep cues; for every other clip
the definition the sound pass gets is the published one.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { flightSoundDefinition, wingFlightClip, WING_FLIGHT_SET } = await import(
	"../../src/engine/foundation/animation/wing-flight.ts"
);

// The published man_avatar_fly.ban row: the Default run's snd_run1 cues.
/** @type {import("../../src/engine/foundation/animation/animation-metadata.ts").AnimationMetadata} */
const FLY = {
	trackEvents: [],
	timeWarpCurve: { scale: 1, records: [] },
	durationMs: 2666,
	soundEvents: [ { cursorMs: 289, cue: "snd_run1" }, { cursorMs: 619, cue: "snd_run1" } ]
};
const WING_RUN = `native:${WING_FLIGHT_SET}:7`;

test("a wing flight advances without footsteps", () => {
	const quiet = flightSoundDefinition( WING_RUN, FLY );
	assert.deepEqual( quiet?.soundEvents, [] );
	assert.equal( quiet?.durationMs, FLY.durationMs );
	// The published definition itself is never changed.
	assert.equal( FLY.soundEvents.length, 2 );
});

test("other clips keep their cues", () => {
	for ( const clip of [ "run", "native:default:7", "native:twohand_sword:7", "native:avatar_wings:7" ] ) {
		assert.equal( wingFlightClip( clip ), false );
		assert.equal( flightSoundDefinition( clip, FLY ), FLY );
	}
	assert.equal( flightSoundDefinition( WING_RUN, undefined ), undefined );
});
