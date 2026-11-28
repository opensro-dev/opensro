/*
===========================================================================

spirit-effects.test.mjs - the dark force a Devil's Spirit plays on its wearer

Port-only, not native (SRO_NASRUN). Each Spirit names a record for four
transitions (equip, unequip, awake, expire); a wake is a dormant clock
starting, a lapse is the lent skill strip emptying under a worn Spirit.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

/*
================
load
================
*/
async function load( entry ) {
	return import( sourceFileUrl( entry ).href );
}
const { spiritEffectRecord, spiritLapsed, spiritWoke } = await load(
	"src/engine/foundation/gameplay/spirit-effects.ts"
);

const TABLE = {
	ITEM_MALL_AVATAR_M_NASRUN: {
		equip: "ITEMSKILL_MALL_AVATAR_NASRUN_EQUIP",
		unequip: "ITEMSKILL_MALL_AVATAR_NASRUN_UNEQUIP",
		awake: "ITEMSKILL_MALL_AVATAR_NASRUN_AWAKE",
		expire: "ITEMSKILL_MALL_AVATAR_NASRUN_TIMEOUT"
	}
};

test("each transition plays the Spirit's own record, and nothing without the table", () => {
	assert.equal(
		spiritEffectRecord( TABLE, "ITEM_MALL_AVATAR_M_NASRUN", "equip" ),
		"ITEMSKILL_MALL_AVATAR_NASRUN_EQUIP"
	);
	assert.equal(
		spiritEffectRecord( TABLE, "ITEM_MALL_AVATAR_M_NASRUN", "expire" ),
		"ITEMSKILL_MALL_AVATAR_NASRUN_TIMEOUT"
	);
	assert.equal( spiritEffectRecord( TABLE, "ITEM_CH_SWORD_01_A", "equip" ), undefined );
	assert.equal( spiritEffectRecord( undefined, "ITEM_MALL_AVATAR_M_NASRUN", "equip" ), undefined );
	assert.equal( spiritEffectRecord( TABLE, undefined, "equip" ), undefined );
});

test("a wake is a dormant clock starting; an extension is none", () => {
	assert.equal( spiritWoke( 0, 1_800_000_000 ), true );
	assert.equal( spiritWoke( 1_800_000_000, 1_800_604_800 ), false );
	assert.equal( spiritWoke( 0, 0 ), false );
});

test("a lapse is the strip emptying under a worn Spirit, not a removal or a re-entry", () => {
	assert.equal( spiritLapsed( [ 31275, 31276 ], [], true ), true );
	// Taken off: the move result has already emptied the socket.
	assert.equal( spiritLapsed( [ 31275 ], [], false ), false );
	// An entry or a repeat with nothing lent before is no transition.
	assert.equal( spiritLapsed( [], [], true ), false );
	assert.equal( spiritLapsed( [ 31275 ], [ 31275 ], true ), false );
});
