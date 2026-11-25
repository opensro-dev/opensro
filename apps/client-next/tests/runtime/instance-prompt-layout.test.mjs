/*
===========================================================================

instance-prompt-layout.test.mjs - the quest instance prompt's box

7644E0 opens the quest instance prompt through
CGInterface_ShowSimpleMessageBox (6888C0), so it takes the simple message
box's geometry (CIFSimpleMessageBox_LayoutControls 52E720: at least
360x151, sized by its text, centred), not the 300x176 party proposal
frame #2334 had inferred (#4403). The test drives the real UI: the box it
draws and the box its drag moves are one frame.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";

// 52E720's floor: 0x168 x 0x97.
const SIMPLE_BOX_WIDTH = 360;
const SIMPLE_BOX_HEIGHT = 151;

test("the quest instance prompt is the centred simple message box, and its drag moves it (#4403)", t => {
	const f = uiFixture();
	t.after( () => f.dispose() );
	f.ui.step( f.state, 0 );
	Object.assign( f.state.gameplay, { social: { ...(f.state.gameplay.social ?? {}), instance: { gid: 7 } } } );
	let now = 0, frame, controls = [];
	const draw = () => {
		for ( let i = 0; i < 4; i++ ) frame = f.ui.step( f.state, now += 100 ) ?? frame;
		assert.ok( frame );
		controls = frame.controls;
		const body = frame.controls.find( control => control.id === "instance-body" );
		assert.ok( body, "the instance prompt draws" );
		return body.rect;
	};
	const rect = draw();
	assert.deepEqual( rect.slice( 2 ), [ SIMPLE_BOX_WIDTH, SIMPLE_BOX_HEIGHT ] );
	assert.deepEqual( rect.slice( 0, 2 ), [
		Math.floor( (f.state.width - SIMPLE_BOX_WIDTH) / 2 ),
		Math.floor( (f.state.height - SIMPLE_BOX_HEIGHT) / 2 )
	] );
	assert.ok( controls.some( control => control.id === "instance-accept" ) );
	assert.ok( controls.some( control => control.id === "instance-refuse" ) );
	f.ui.event( { kind: "drag", id: "instance-drag", dx: -40, dy: 25 } );
	assert.deepEqual( draw(), [ rect[0] - 40, rect[1] + 25, SIMPLE_BOX_WIDTH, SIMPLE_BOX_HEIGHT ] );
});
