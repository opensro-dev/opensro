/*
===========================================================================

party-message-text.test.mjs - party, guild and exchange questions say who asks

The retail English cells read as if addressed to the wrong side (#3677): the
leader approving an applicant read "You have successfully joined this
party", an applicant's progress said "You" beside both names, and the
invitation's second line read "Party application success" under "[name]has".
The Korean cells say who asked whom; englishCorrections.json carries the
reviewed English, applied here through the build's own policy.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { correctedUiSystemCatalog } from "../helpers/corrected-text.mjs";

// The cells #3677 corrects.
const CORRECTED = [
	"UIIT_STT_PARTYMATCH_JOIN_REQUEST",
	"UIIT_STT_PARTYMATCH_JOIN_PROGRESS",
	"UIIT_STT_PARTY_PROPOSAL_ASK",
	"UIIT_STT_PARTY_SOMEUSER",
	"UIIT_MSG_GUILD_JOIN_REQUEST",
	"UIIT_MSG_DEAL_ASK"
];

/*
================
correctedFixture

A UI fixture whose UI system catalog carries the CORRECTED cells.
================
*/
function correctedFixture() {
	return uiFixture(
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		{},
		correctedUiSystemCatalog( CORRECTED )
	);
}

/*
================
invited

Steps the fixture into an invitation of the given type from entity gid 1.
================
*/
function invited( f, type ) {
	for ( let t = 0; t < 1000; t += 100 ) f.ui.step( f.state, t );
	f.state.entities = [ { ...f.state.entities[0], guildName: "Silkroad" } ];
	f.state.gameplay = { ...f.state.gameplay, social: { invitation: { type, gid: 1 } } };
	f.ui.step( f.state, 1100 );
}

test("a party invitation reads as one sentence across its two lines", () => {
	const f = correctedFixture();
	try {
		invited( f, 2 );
		assert.ok( f.hasText( "[Player] has" ) );
		assert.ok( f.hasText( "invited you to a party. Will you accept?" ) );
		assert.ok( !f.hasText( "Party application success" ) );
	} finally {
		f.dispose();
	}
});

test("an exchange request continues the same name line", () => {
	const f = correctedFixture();
	try {
		invited( f, 1 );
		assert.ok( f.hasText( "[Player] has" ) );
		assert.ok( f.hasText( "requested an exchange. Will you accept it?" ) );
	} finally {
		f.dispose();
	}
});

test("a guild invitation says the inviter invited us", () => {
	const f = correctedFixture();
	try {
		invited( f, 5 );
		assert.ok( f.hasText( "[Player] has invited you to join a guild." ) );
		assert.ok( !f.hasText( "has applied for your guild" ) );
	} finally {
		f.dispose();
	}
});

/*
================
matching

Steps the fixture into a party matching state over a settled HUD.
================
*/
function matching( f, partyMatching ) {
	for ( let t = 0; t < 1600; t += 100 ) f.ui.step( f.state, t );
	f.state.gameplay = {
		...f.state.gameplay,
		partyMatching: { page: 0, pages: 1, rows: [], own: null, result: null, auto: [], ...partyMatching }
	};
	f.ui.step( f.state, 2200 );
}

test("the leader approving an applicant reads that the applicant asked to join", () => {
	const f = correctedFixture();
	try {
		matching( f, {
			pending: null,
			request: {
				a: 7,
				b: 42,
				native7c0: 0,
				primary: 0,
				secondary: 0,
				flags: 0,
				member: {
					id: 77,
					name: "Peer",
					model: 1907,
					level: 20,
					region: 1,
					status: 0xaa,
					x: 0,
					y: 0,
					z: 0,
					war: 0
				},
				expires: 20000
			}
		} );
		assert.ok( f.hasText( "[Peer] has asked to join" ) );
		assert.ok( !f.hasText( "successfully joined" ) );
	} finally {
		f.dispose();
	}
});

test("an applicant's progress names the applicant and the leader, without a stray You", () => {
	const f = correctedFixture();
	try {
		matching( f, { request: null, pending: "join", joining: { name: "Peer", since: 2000 } } );
		assert.ok( f.hasText( "has applied to join" ) );
		assert.ok( f.hasText( "[Peer]'s party." ) );
		assert.ok( !f.hasText( "You" ) );
	} finally {
		f.dispose();
	}
});
