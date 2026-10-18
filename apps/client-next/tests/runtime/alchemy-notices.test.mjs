/*
===========================================================================

alchemy-notices.test.mjs - refused reinforcements name their reason

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { alchemyNotice, alchemyProtectionStoneRefusal } = await import(
	"../../src/engine/foundation/gameplay/alchemy-notices.ts"
);

/*
================
astral refusal
================
*/
test("a refused Astral stone shows the Astral rule (0xB651 [2, 0x24])", () => {
	assert.equal( alchemyNotice( 0xb651, Uint8Array.of( 2, 0x24 ) )?.key, "UIIT_MSG_STRGERR_ASTRAL" );
});

/*
================
elixir refusal
================
*/
test("a refused elixir reinforcement shows its category 0x12 reason", () => {
	assert.equal( alchemyNotice( 0xb373, Uint8Array.of( 2, 0x14 ) )?.key, "UIIT_MSG_REINFORCERR_INVALID_ITEM_SLOT" );
});

/*
================
silent answers
================
*/
test("a failed magic roll, a success and other frames show no notice", () => {
	assert.equal( alchemyNotice( 0xb651, Uint8Array.of( 2, 0x23 ) ), null );
	assert.equal( alchemyNotice( 0xb651, Uint8Array.of( 1, 1, 13 ) ), null );
	assert.equal( alchemyNotice( 0xb06d, Uint8Array.of( 2, 0x24 ) ), null );
});

/*
================
protection stone precheck (620DD0)

On the Fuse click the native window refuses a protection stone whose
option is already at six charges, with that stone's own notice, and
sends nothing. Each tag pins to its specific (0x16, code) string.
================
*/
const OPTION_REFS = new Map( [
	[ 101, { paramId: 101, optionName: "MATTR_ATHANASIA", paramName: "MATTR_ATHANASIA+", degree: 1 } ],
	[ 102, { paramId: 102, optionName: "MATTR_LUCK", paramName: "MATTR_LUCK+", degree: 1 } ],
	[ 103, { paramId: 103, optionName: "MATTR_SOLID", paramName: "MATTR_SOLID+", degree: 1 } ],
	[ 104, { paramId: 104, optionName: "MATTR_ASTRAL", paramName: "MATTR_ASTRAL+", degree: 1 } ]
] );
const charge = ( paramId, amount ) => String( BigInt( paramId ) | (BigInt( amount ) << 32n) );

/** @type {readonly [string, number, string][]} */
const PROTECTION_CASES = [
	[ "ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA", 101, "UIIT_MSG_STRGERR_MAGICSTONE_ATHANASIA_MAX" ],
	[ "ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK", 102, "UIIT_MSG_STRGERR_MAGICSTONE_LUCK_MAX" ],
	[ "ITEM_ETC_ARCHEMY_MAGICSTONE_SOLID", 103, "UIIT_MSG_STRGERR_MAGICSTONE_SOLID_MAX" ]
];
for ( const [stone, option, key] of PROTECTION_CASES ) {
	test(`a maxed ${option} option refuses the ${stone} stone locally`, () => {
		assert.equal(
			alchemyProtectionStoneRefusal( stone, [ charge( option, 6 ) ], OPTION_REFS )?.key,
			key
		);
		// Five charges still fuse; the refusal names the stone's option
		// only, never another option sitting at six.
		assert.equal( alchemyProtectionStoneRefusal( stone, [ charge( option, 5 ) ], OPTION_REFS ), null );
		assert.equal(
			alchemyProtectionStoneRefusal( stone, [ charge( 104, 6 ), charge( option, 5 ) ], OPTION_REFS ),
			null
		);
	});
}

test("other stones and unknown items are never refused locally", () => {
	assert.equal(
		alchemyProtectionStoneRefusal( "ITEM_ETC_ARCHEMY_MAGICSTONE_ASTRAL", [ charge( 104, 6 ) ], OPTION_REFS ),
		null
	);
	assert.equal( alchemyProtectionStoneRefusal( "ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK", [], OPTION_REFS ), null );
	assert.equal( alchemyProtectionStoneRefusal( undefined, [ charge( 102, 6 ) ], OPTION_REFS ), null );
	// The codename compare is case-insensitive (620DD0).
	assert.equal(
		alchemyProtectionStoneRefusal( "item_etc_archemy_magicstone_luck", [ charge( 102, 6 ) ], OPTION_REFS )?.key,
		"UIIT_MSG_STRGERR_MAGICSTONE_LUCK_MAX"
	);
});
