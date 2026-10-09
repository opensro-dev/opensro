/*
===========================================================================

mouse-modes.test.mjs - the native mouse modes' buttons and labels

67CCA0: mode 0 uses the mouse quickslot on a wheel click and orbits on the
right button; mode 1 swaps them. The options window lists mode 0 as
UIIT_STT_USE_WHEEL_TO_USE_SKILL (5CC9D4).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { pathToFileURL as sourceFileUrl } from "node:url";

const { cameraDragButtons, shortcutButton, mouseModeLabel } = await import(
	sourceFileUrl( "src/engine/foundation/ui/mouse-modes.ts" ).href
);

test("mode 0: the wheel click is the shortcut, the right button orbits", () => {
	assert.equal( shortcutButton( 0 ), 1 );
	assert.equal( cameraDragButtons( 0 ), 2 );
	assert.equal( mouseModeLabel( 0 ), "UIIT_STT_USE_WHEEL_TO_USE_SKILL" );
});

test("mode 1: the right click is the shortcut, the wheel button orbits", () => {
	assert.equal( shortcutButton( 1 ), 2 );
	assert.equal( cameraDragButtons( 1 ), 4 );
	assert.equal( mouseModeLabel( 1 ), "UIIT_STT_USE_WHEEL_TO_CHANGE_SIGHT" );
});
