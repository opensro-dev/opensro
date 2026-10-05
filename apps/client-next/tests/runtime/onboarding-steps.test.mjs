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
	const steps = tourSteps(), onScreen = new Set( [ "#fps-toggle" ] );
	const visible = selector => [ ...onScreen ].some( shown => selector.split( "," ).includes( shown ) );
	assert.equal( nextStep( steps, new Set(), visible )?.id, "fps-chip" );
	assert.equal( nextStep( steps, new Set( [ "fps-chip" ] ), visible ), null );
	// The bug launcher waits until bug reports are enabled.
	onScreen.add( ".sro-bug-launcher" );
	assert.equal( nextStep( steps, new Set( [ "fps-chip" ] ), visible )?.id, "bug-report" );
});

test("the queued-skill step lights main bar key 1 on every page and the chip above it", () => {
	const step = tourSteps().find( row => row.id === "skill-queue" );
	const ids = step.target.split( "," );
	assert.equal( ids.length, 4 );
	assert.ok( ids.includes( '[data-ui-id="hotbar:1"]' ), "page 1, key 1" );
	assert.ok( !ids.includes( '[data-ui-id="hotbar:0"]' ), "not the M slot" );
	assert.ok( step.reachAbove > 0.5 && step.reachAbove < 1.5 );
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
