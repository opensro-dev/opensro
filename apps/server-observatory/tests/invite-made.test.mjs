/*
===========================================================================
invite-made.test.mjs - the console never offers a grant's code to copy

A key linked to a Discord member is collected from the bot; the console
says so instead of showing keys. Plain keys keep their copy prompt. A
grant always asks for one key, whatever batch size the form holds.
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { createRequest, madeStatus } from "../public/invite-made.js";

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

test("a grant asks for one key even when How many holds a batch size", () => {
	const form = {
		count: "15",
		maxUses: "1",
		expiresInDays: "14",
		note: " teste ",
		discordUserId: " 123456789012345678 ",
		discordName: " member "
	};
	assert.deepEqual( createRequest( form ), {
		count: 1,
		maxUses: 1,
		expiresInDays: 14,
		note: "teste",
		discordUserId: "123456789012345678",
		discordName: "member"
	} );
});

test("without a member the batch size is sent as typed", () => {
	const form = { count: "15", maxUses: "2", expiresInDays: "0", note: "", discordUserId: "  ", discordName: "x" };
	assert.deepEqual( createRequest( form ), { count: 15, maxUses: 2, expiresInDays: 0, note: "" } );
});
