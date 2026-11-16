/*
===========================================================================
arena-chest-ui.test.mjs - visible frames never poll the worker for a chest.
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

test("a visible world waits for the server chest seed without posting probes", () => {
	const commands = [];
	const f = uiFixture( command => commands.push( command ) );
	try {
		for ( let i = 0; i < 32; i++ ) f.ui.step( f.state, i * 100 );
		assert.equal( commands.some( c => c.kind === "gameplay" && c.command.kind === "arena-decision" ), false );
	} finally {
		f.dispose();
	}
});
