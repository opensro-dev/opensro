/*
===========================================================================

onboarding-steps.test.mjs - which tour step comes next, and what is remembered

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const { nextStep, parseSeen, tourSteps, WELCOME_ID, welcomeCopy } = await import(
	"../../src/engine/foundation/ui/onboarding-steps.ts"
);

test("steps are offered in order, each once, and only while their element is on screen", () => {
	const steps = tourSteps(), onScreen = new Set( [ "#fps-toggle" ] );
	const visible = selector => [ ...onScreen ].some( shown => selector.split( "," ).includes( shown ) );
	assert.equal( nextStep( steps, new Set(), visible )?.id, "fps-chip" );
	assert.equal( nextStep( steps, new Set( [ "fps-chip" ] ), visible ), null );
	// The bug launcher waits until bug reports are enabled.
	onScreen.add( ".sro-bug-launcher" );
	assert.equal( nextStep( steps, new Set( [ "fps-chip" ] ), visible )?.id, "bug-report" );
});

test("the queued-skill step lights only the chip's place above key 1, with an example skill", async () => {
	const { skillQueueChipBacking } = await import( "../../src/engine/foundation/ui/skill-press-feedback.ts" );
	const step = tourSteps().find( row => row.id === "skill-queue" );
	assert.ok( step && step.area );
	const ids = step.target.split( "," );
	assert.equal( ids.length, 4 );
	assert.ok( ids.includes( '[data-ui-id="hotbar:1"]' ), "page 1, key 1" );
	assert.ok( !ids.includes( '[data-ui-id="hotbar:0"]' ), "not the M slot" );
	// The same box the chip's backing takes over a 32 px slot, entirely above it.
	const [x, y, width, height] = step.area;
	assert.deepEqual( [ x, y, width, height ].map( v => v * 32 ), skillQueueChipBacking( [ 0, 0, 32, 32 ] ) );
	assert.ok( y + height < 0 && width < 1 );
	assert.match( step.sample ?? "", /^\/assets\/images\/Media_extracted\/icon\/skill\/.+\.png$/ );
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

test("the welcome notice says what the server is, asks for bug reports and links the source", () => {
	const copy = welcomeCopy(), text = copy.paragraphs.join( " " );
	assert.ok( copy.title );
	assert.match( text, /continuous development/ );
	assert.match( text, /not a playable server yet/ );
	assert.match( text, /\/bug/ );
	assert.equal( copy.link.href, "https://github.com/opensro-dev/opensro" );
	assert.ok( !tourSteps().some( step => step.id === WELCOME_ID ), "its id is apart from the steps'" );
});
