/*
===========================================================================

title-credit.test.mjs - the page shell's login credit follows the login phase

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createUi } = await import( "../../src/engine/runtime/ui/ui.ts" );

/*
================
step
================
*/
function step( ui, phase ) {
	return ui.step( {
		frontend: { phase, generation: 1, elapsed: 1, alpha: 1, logoAlpha: 0, error: null },
		session: { phase: "signed-out", revision: 1 },
		gameplay: null,
		entities: [],
		width: 1024,
		height: 768,
		worldReady: false
	}, 0 );
}

test("the credit shows on the login screen and nowhere else", () => {
	const ui = createUi(
		{ available: () => 0, take: () => null, request: () => 0, cancel: () => {} },
		() => {},
		() => {},
		() => {},
		"https://fixture.invalid",
		"https://fixture.invalid"
	);
	try {
		for ( const phase of [ "intro", "login-reveal", "login-accepted", "dock", "world" ] ) {
			assert.notEqual( step( ui, phase )?.titleCredit, true, phase );
		}
		assert.equal( step( ui, "login" )?.titleCredit, true );
	} finally {
		ui.dispose();
	}
});
