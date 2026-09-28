/*
===========================================================================

party-options.test.mjs - formation choices survive empty social state

Check every sharing-bit combination across join and leave transitions, so
the fix cannot merely hard-code the reporter's item-and-experience choice.

===========================================================================
*/

import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const { effectivePartyOptions } = await import( "../../src/engine/foundation/ui/party-options.ts" );

test("formation settings govern missing and empty parties for every bit combination", () => {
	for ( let options = 0; options < 8; options++ ) {
		assert.equal( effectivePartyOptions( undefined, options ), options );
		assert.equal( effectivePartyOptions( emptySocial( "Player" ), options ), options );
	}
});

test("joining uses live rules and leaving restores formation choices", () => {
	for ( let options = 0; options < 8; options++ ) {
		const social = {
			...emptySocial( "Player" ),
			options,
			members: [ {
				id: 1,
				name: "Player",
				model: 1,
				level: 1,
				status: 0,
				region: 1,
				x: 0,
				y: 0,
				z: 0,
				war: 0
			} ]
		};
		assert.equal( effectivePartyOptions( social, 7 ^ options ), options );
		assert.equal( effectivePartyOptions( { ...social, members: [] }, 7 ^ options ), 7 ^ options );
	}
});
