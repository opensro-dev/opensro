/*
===========================================================================

instance-prompt-layout.test.mjs - the quest instance prompt's box

7644E0 opens the quest instance prompt through
CGInterface_ShowSimpleMessageBox (6888C0), so it takes the simple message
box's geometry (CIFSimpleMessageBox_LayoutControls 52E720: at least
360x151, sized by its text), not the 300x176 party proposal frame #2334
had inferred (#4403).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { instanceBoxLayout } = await import( "../../src/engine/foundation/ui/instance-proposal.ts" );
const { textMessageBoxLayout } = await import( "../../src/engine/foundation/ui/text-message-box.ts" );

const measure = value => value.length * 6;

test("the quest instance prompt is the simple message box around its question (#4403)", () => {
	const question = "Do you want to enter the instance dungeon?";
	const box = instanceBoxLayout( { width: 1280, height: 720, question, measure }, null );
	const simple = textMessageBoxLayout( 1280, 720, [ measure( question ) ] );
	// 52E720's floor: 0x168 x 0x97, centred on the screen.
	assert.deepEqual( box.frame, [ 460, 284, 360, 151 ] );
	assert.deepEqual( box.frame, simple.frame );
	assert.deepEqual( box.accept, simple.accept );
	assert.deepEqual( box.refuse, simple.refuse );
	assert.deepEqual( box.lines.map( line => line.text ), [ question ] );
	assert.deepEqual( box.lines[0].rect, simple.lines[0] );
});

test("a wider question widens the box, and a dragged box keeps its place (#4403)", () => {
	const question = "x".repeat( 80 );
	const box = instanceBoxLayout( { width: 1280, height: 720, question, measure }, [ 40, 30 ] );
	assert.equal( box.frame[2], 80 * 6 + 60 );
	assert.deepEqual( box.frame.slice( 0, 2 ), [ 40, 30 ] );
});
