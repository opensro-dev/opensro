/*
===========================================================================
invite-made.test.mjs - the console never offers a grant's code to copy

A key linked to a Discord member is collected from the bot; the console
says so instead of showing keys. Plain keys keep their copy prompt.
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { madeStatus } from "../public/invite-made.js";

test("a grant tells the operator the bot delivers it", () => {
	assert.equal(
		madeStatus( { keys: [], granted: true } ),
		"Granted: the member gets the code from the bot's Get my beta key button."
	);
});

test("plain keys are counted for copying", () => {
	assert.equal( madeStatus( { keys: [ {} ], granted: false } ), "Made 1 key. Copy them now." );
	assert.equal( madeStatus( { keys: [ {}, {} ], granted: false } ), "Made 2 keys. Copy them now." );
});
