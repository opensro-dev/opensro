/*
===========================================================================

idle-variation-clock.test.mjs - an idle fidget plays over the running stand loop

Native 8E5BB0 plays the state-7 fidget through CCObjCharacter_PlayAnimation
(8EADF0), which installs it as a one-shot (CRTAniMixer_InstallOneShot
ADECF0): the one-shot list grows, the timed list that holds the stand loop
is left alone. The exit (8E5CB0 -> 8EA820) fades only that one-shot. So the
stand loop's clock never restarts for a fidget. Player report #4147: an
idle character's legs jumped when a fidget began and again when it ended,
because the port restarted the stand loop's clock both times.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { fixture, entity } from "../helpers/presentation-fixture.mjs";

// Longer than the whole run, so the stand clock never wraps in the test.
const STAND_SECONDS = 30;
const FRAME_SECONDS = .1;

/*
================
standClock

The stand loop's cursor in the actor the renderer draws: its own time when
it is the only layer, or the timed stand layer's while a fidget plays.
================
*/
function standClock( actor ) {
	if ( !actor.layers ) {
		assert.equal( actor.clip, "stand" );
		return actor.time;
	}
	const stand = actor.layers.find( layer => layer.clip === "stand" && layer.lane === "timed" );
	assert.ok( stand, "the stand loop stays installed under the fidget" );
	return stand.time;
}

test("an idle fidget neither restarts nor rewinds the stand loop under it", () => {
	const f = fixture(
		{},
		2,
		false,
		false,
		false,
		true,
		false,
		false,
		false,
		false,
		undefined,
		undefined,
		undefined,
		{},
		{},
		{
			clipDurations: { stand: STAND_SECONDS }
		}
	);
	try {
		f.warm();
		let fidgeted = false, ended = false, previous;
		for ( let t = 14; t < 19; t += FRAME_SECONDS ) {
			f.step( [ entity( 1 ) ], t );
			const actor = f.actors[0], clock = standClock( actor );
			const fidget = !!actor.layers?.some( layer => layer.lane === "event" && layer.clip.startsWith( "idle" ) );
			if ( fidget ) fidgeted = true;
			else if ( fidgeted ) ended = true;
			if ( previous !== undefined ) {
				assert.ok(
					Math.abs( clock - previous - FRAME_SECONDS ) < 1e-6,
					`stand clock at ${t.toFixed( 1 )} s moved ${
						(clock - previous).toFixed( 3 )
					} s in one ${FRAME_SECONDS} s frame`
				);
			}
			previous = clock;
		}
		assert.ok( fidgeted && ended, "a fidget began and ended inside the run" );
		assert.equal( f.presentation.error(), null );
	} finally {
		f.dispose();
	}
});
