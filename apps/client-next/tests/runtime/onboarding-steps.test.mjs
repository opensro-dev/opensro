/*
===========================================================================

onboarding-steps.test.mjs - which tour step comes next, and what is remembered

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const { nextStep, parseSeen, tourSteps } = await import( "../../src/engine/foundation/ui/onboarding-steps.ts" );

test("steps are offered in order, each once, and only while their element is on screen", () => {
	const steps = tourSteps(), onScreen = new Set( [ "#fps-toggle", '[data-ui-id="hotbar:0"]' ] );
	const visible = selector => onScreen.has( selector );
	assert.equal( nextStep( steps, new Set(), visible )?.id, "fps-chip" );
	assert.equal( nextStep( steps, new Set( [ "fps-chip" ] ), visible )?.id, "skill-queue" );
	assert.equal( nextStep( steps, new Set( [ "fps-chip", "skill-queue" ] ), visible ), null );
	// A contextual step waits for its element: the party board appears later.
	onScreen.add( '[data-ui-id^="party-target:"]' );
	assert.equal( nextStep( steps, new Set( [ "fps-chip", "skill-queue" ] ), visible )?.id, "party-masteries" );
});

test("step ids are unique so progress survives reordering and new steps", () => {
	const ids = tourSteps().map( step => step.id );
	assert.equal( new Set( ids ).size, ids.length );
	for ( const step of tourSteps() ) assert.ok( step.title && step.text && step.target, step.id );
});

test("stored progress keeps only step ids and tolerates anything unreadable", () => {
	assert.deepEqual( [ ...parseSeen( '["fps-chip",3,null,"chat-time"]' ) ], [ "fps-chip", "chat-time" ] );
	for ( const raw of [ null, "", "{", '{"fps-chip":true}', "42" ] ) assert.equal( parseSeen( raw ).size, 0 );
});
