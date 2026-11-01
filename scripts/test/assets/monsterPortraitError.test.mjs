/*
===========================================================================

monsterPortraitError.test.mjs - a failed portrait names its row

renderMonsterPortrait drives one viewer page for the whole catalogue;
when a still failed, the build died with the viewer's bare "asset-load",
which names none of the 1919 monsters and cannot be reproduced. These
tests inject a failing viewer page and pin that the thrown error carries
the row (refObjId and codename), the model it was drawing, and the
viewer's own message, with the original error kept as the cause.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { monsterPortraitReferences, renderMonsterPortrait } from "../../build/char/buildMonsterPortraitAssets.mjs";

const PNG_STILL =
	"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";
const FLAMEMASTER = {
	refObjId: 14838,
	codename: "MOB_TQ_FLAMEMASTER",
	glb: "/assets/npc/char/china/shinmu.glb"
};

/*
================
fakeViewerPage

A viewer page stub: the __viewerNext probe answers hasNext, the still
evaluation resolves the PNG or rejects stillError, and navigations are
recorded. Playwright surfaces a page rejection as an Error carrying the
page error's message, so the stub rejects the same way.
================
*/
function fakeViewerPage( { hasNext = true, stillError = null } = {} ) {
	const gotos = [];
	return {
		gotos,
		async goto( url ) {
			gotos.push( url );
		},
		async waitForFunction() {},
		async evaluate( fn, arg ) {
			if ( arg === undefined ) return hasNext;
			if ( stillError ) throw stillError;
			return PNG_STILL;
		}
	};
}

/*
================
failed batch still names its row
================
*/
test("a failed batch still names its row and model", async () => {
	const viewerError = Error( "asset-load: /assets/npc/char/china/shinmu.glb: HTTP 404" );
	const page = fakeViewerPage( { stillError: viewerError } );
	await assert.rejects(
		() => renderMonsterPortrait( page, "http://127.0.0.1:9/", FLAMEMASTER.refObjId, FLAMEMASTER ),
		( error ) => {
			assert.match( error.message, /14838/ );
			assert.match( error.message, /MOB_TQ_FLAMEMASTER/ );
			assert.match( error.message, /shinmu\.glb/ );
			assert.match( error.message, /asset-load/ );
			assert.equal( error.cause, viewerError );
			return true;
		}
	);
	// The batch page is still reloaded after a failed draw.
	assert.deepEqual( page.gotos, [ "about:blank" ] );
});

/*
================
failed first load names its row
================
*/
test("a failed first load names its row and model", async () => {
	const page = fakeViewerPage( { hasNext: false, stillError: Error( "asset-load" ) } );
	await assert.rejects(
		() => renderMonsterPortrait( page, "http://127.0.0.1:9/", FLAMEMASTER.refObjId, FLAMEMASTER ),
		( error ) => {
			assert.match( error.message, /14838/ );
			assert.match( error.message, /MOB_TQ_FLAMEMASTER/ );
			assert.match( error.message, /asset-load/ );
			return true;
		}
	);
	assert.match( page.gotos[0], /viewer\.html\?monster=14838/ );
	assert.equal( page.gotos[1], "about:blank" );
});

/*
================
successful still is untouched
================
*/
test("a successful still returns its PNG bytes", async () => {
	const page = fakeViewerPage();
	const png = await renderMonsterPortrait( page, "http://127.0.0.1:9/", FLAMEMASTER.refObjId, FLAMEMASTER );
	assert.ok( Buffer.isBuffer( png ) );
	assert.equal( png[0], 0x89 );
	assert.equal( png[1], 0x50 );
	assert.deepEqual( page.gotos, [] );
});

/*
================
references carry the codename
================
*/
test("portrait references carry the codename beside the id", () => {
	const rows = monsterPortraitReferences( {
		models: {
			MOB_TQ_FLAMEMASTER: {
				codename: "MOB_TQ_FLAMEMASTER",
				refObjId: 14838,
				kind: "monster",
				glb: "/assets/npc/char/china/shinmu.glb"
			},
			NPC_TOWN: { codename: "NPC_TOWN", refObjId: 2000, kind: "npc", glb: "/assets/npc/town.glb" }
		}
	} );
	assert.deepEqual( rows, [ FLAMEMASTER ] );
});
