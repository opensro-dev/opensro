/*
===========================================================================

crest-register-box.test.mjs - the crest register confirm box's controls

#3141: confirming "Register guild emblem?" froze the client. The box
drew, but its Yes/No buttons were registered on a stale frame control
list, so the published semantics carried no crest-register-yes/no and
no click could ever reach them while the box's full-screen block ate
every other click. These tests drive the production UI: use the
register item, read the published controls, and click through them.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

// TID 3/3/12/2 packed as the client's type word (see crest-register.test).
const CREST_FLAGS = 3 << 2 | 3 << 5 | 12 << 7 | 2 << 11;
const PNG_HEAD = Uint8Array.from( [ 0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4 ] );
const BAG_SLOT = 20;

/*
================
fixture

A world whose bag holds the register item named by refObjId, with the
given picker standing behind the owner's contract.
================
*/
function fixture( refObjId, pickCrestImage ) {
	const sent = [];
	const extensions = pickCrestImage ? { pickCrestImage } : {};
	const f = uiFixture(
		command => sent.push( command ),
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		extensions
	);
	Object.assign( f.state.gameplay, {
		inventorySlotCount: 45,
		inventory: [ { slot: BAG_SLOT, refObjId, typeFlags: CREST_FLAGS, quantity: 1 } ]
	} );
	f.ui.step( f.state, 0 );
	f.ui.event( { kind: "activate", id: "open-window:Inventory" } );
	f.ui.step( f.state, 1 );
	f.ui.event( { kind: "double-activate", id: "slot:" + BAG_SLOT } );
	return { f, sent };
}

/*
================
boxControls

The published controls while the register box stands.
================
*/
function boxControls( f, now ) {
	const semantics = f.ui.step( f.state, now );
	assert.ok( semantics, "no frame published" );
	return semantics.controls;
}

/*
================
flushPicker

Let the picker's promise land before reading what it sent.
================
*/
async function flushPicker() {
	for ( let i = 0; i < 8; i++ ) await Promise.resolve();
}

test("the register box publishes its Yes and No buttons", () => {
	const { f } = fixture( 9268, () => Promise.resolve( null ) );
	try {
		const controls = boxControls( f, 2 );
		assert.ok(
			controls.some( control => control.id === "crest-register-yes" ),
			"crest-register-yes missing from the published controls"
		);
		assert.ok(
			controls.some( control => control.id === "crest-register-no" ),
			"crest-register-no missing from the published controls"
		);
	} finally {
		f.dispose();
	}
});

test("No closes the register box and hands control back", () => {
	let picks = 0;
	const { f, sent } = fixture( 9268, () => {
		picks++;
		return Promise.resolve( PNG_HEAD );
	} );
	try {
		boxControls( f, 2 );
		f.ui.event( { kind: "activate", id: "crest-register-no" } );
		const controls = boxControls( f, 3 );
		assert.equal( picks, 0, "No must not open the picker" );
		assert.equal(
			sent.filter( command => command.command?.kind === "crest-register" ).length,
			0,
			"No must not upload"
		);
		assert.ok(
			!controls.some( control => control.id.startsWith( "crest-register-" ) ),
			"the box is still up after No"
		);
		assert.ok( controls.length > 0, "no controls came back after No" );
	} finally {
		f.dispose();
	}
});

test("Yes picks the image and uploads it for the guild crest", async () => {
	let picks = 0;
	const { f, sent } = fixture( 9268, () => {
		picks++;
		return Promise.resolve( PNG_HEAD );
	} );
	try {
		boxControls( f, 2 );
		f.ui.event( { kind: "activate", id: "crest-register-yes" } );
		assert.equal( picks, 1, "Yes did not open the picker" );
		await flushPicker();
		const uploads = sent.filter( command => command.command?.kind === "crest-register" );
		assert.equal( uploads.length, 1, "the picked image was not uploaded" );
		assert.equal( uploads[0].command.slot, BAG_SLOT );
		assert.equal( uploads[0].command.crestKind, "guild" );
		assert.deepEqual( uploads[0].command.image, PNG_HEAD );
		const controls = boxControls( f, 3 );
		assert.ok(
			!controls.some( control => control.id.startsWith( "crest-register-" ) ),
			"the box is still up after Yes"
		);
	} finally {
		f.dispose();
	}
});

test("a cancelled picker uploads nothing and hands control back", async () => {
	const { f, sent } = fixture( 9268, () => Promise.resolve( null ) );
	try {
		boxControls( f, 2 );
		f.ui.event( { kind: "activate", id: "crest-register-yes" } );
		await flushPicker();
		assert.equal(
			sent.filter( command => command.command?.kind === "crest-register" ).length,
			0,
			"a cancelled pick uploaded"
		);
		const controls = boxControls( f, 3 );
		assert.ok(
			!controls.some( control => control.id.startsWith( "crest-register-" ) ),
			"the box is still up after a cancelled pick"
		);
		assert.ok( controls.length > 0, "no controls came back after a cancelled pick" );
	} finally {
		f.dispose();
	}
});

test("the union register box behaves the same", async () => {
	const { f, sent } = fixture( 9270, () => Promise.resolve( PNG_HEAD ) );
	try {
		const controls = boxControls( f, 2 );
		assert.ok( controls.some( control => control.id === "crest-register-yes" ) );
		f.ui.event( { kind: "activate", id: "crest-register-yes" } );
		await flushPicker();
		const uploads = sent.filter( command => command.command?.kind === "crest-register" );
		assert.equal( uploads.length, 1 );
		assert.equal( uploads[0].command.crestKind, "union" );
	} finally {
		f.dispose();
	}
});
