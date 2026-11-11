/*
===========================================================================

exchange-deception.test.mjs - the exchange window's anti-fraud warnings

#3135 (inference; the v1.150 client holds no display path for either key,
so the rule is the text rows' own wording): UIIT_MSG_EXCHANGE_DECEPTION_
COMPARE prints once when an exchange with a partner outside the player's
friends, guild and union opens; UIIT_MSG_EXCHANGE_DECEPTION_REAFFIRM
prints once when both offers stand locked and the next confirm approves.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const {
	exchangePartnerIsStranger,
	EXCHANGE_DECEPTION_COMPARE_KEY,
	EXCHANGE_DECEPTION_REAFFIRM_KEY
} = await import( sourceFileUrl( "src/engine/foundation/gameplay/exchange.ts" ).href );
const { createExchangeHud } = await import(
	sourceFileUrl( "src/engine/runtime/ui/hud/exchange-hud.ts" ).href
);

const RELATIONS = {
	friendNames: [ "Friend" ],
	guildMemberNames: [ "Guildmate" ],
	ownGuildId: 10,
	allianceGuildIds: [ 10, 20 ]
};

test("a partner matched by friend, guild or union is no stranger", () => {
	assert.equal( exchangePartnerIsStranger( "Friend", 99, RELATIONS ), false );
	assert.equal( exchangePartnerIsStranger( "Guildmate", 99, RELATIONS ), false );
	// The roster is authoritative; the entity's guild may lag it.
	assert.equal( exchangePartnerIsStranger( "Guildmate", 20, RELATIONS ), false );
	assert.equal( exchangePartnerIsStranger( "Ally", 20, RELATIONS ), false );
	assert.equal( exchangePartnerIsStranger( "Ally", 10, RELATIONS ), false );
});

test("a partner matched by nothing is a stranger", () => {
	assert.equal( exchangePartnerIsStranger( "Stranger", 99, RELATIONS ), true );
	assert.equal( exchangePartnerIsStranger( "Stranger", 0, RELATIONS ), true );
	// An unresolved guild matches nothing; an unresolved name can still
	// be cleared by the guild standing in the alliance.
	assert.equal( exchangePartnerIsStranger( "Stranger", undefined, RELATIONS ), true );
	assert.equal( exchangePartnerIsStranger( undefined, 99, RELATIONS ), true );
	assert.equal( exchangePartnerIsStranger( undefined, 20, RELATIONS ), false );
	assert.equal( exchangePartnerIsStranger( undefined, undefined, RELATIONS ), true );
});

test("the compare edge waits for the name, fires once, and re-arms on close", () => {
	const hud = createExchangeHud();
	assert.deepEqual( hud.warnings( true, null, false ), [] );
	assert.deepEqual( hud.warnings( true, null, false ), [] );
	assert.deepEqual( hud.warnings( true, true, false ), [ EXCHANGE_DECEPTION_COMPARE_KEY ] );
	assert.deepEqual( hud.warnings( true, true, false ), [] );
	hud.warnings( false, null, false );
	assert.deepEqual( hud.warnings( true, true, false ), [ EXCHANGE_DECEPTION_COMPARE_KEY ] );
});

test("a known partner never raises the compare line", () => {
	const hud = createExchangeHud();
	assert.deepEqual( hud.warnings( true, false, false ), [] );
	assert.deepEqual( hud.warnings( true, true, false ), [] );
});

test("the reaffirm edge fires once when both offers lock", () => {
	const hud = createExchangeHud();
	assert.deepEqual( hud.warnings( true, true, false ), [ EXCHANGE_DECEPTION_COMPARE_KEY ] );
	assert.deepEqual( hud.warnings( true, true, true ), [ EXCHANGE_DECEPTION_REAFFIRM_KEY ] );
	assert.deepEqual( hud.warnings( true, true, true ), [] );
	hud.warnings( false, null, false );
	assert.deepEqual( hud.warnings( true, false, true ), [ EXCHANGE_DECEPTION_REAFFIRM_KEY ] );
});

/*
================
fixture

A world in an open exchange with the partner entity standing beside the
local player, under the given relation rosters.
================
*/
/** @param {{ partner?: { guildId?: number }, friends?: { jid: number, name: string, modelRefId: number, online: boolean }[], guild?: { id: number, name: string, level: number, gp: number, subject: string, contents: string, members: object[] } | null, alliances?: { id: number, name: string, level: number, master: string, model: number, flags: number }[], locked?: boolean }} [options] */
function fixture( { partner = {}, friends = [], guild = null, alliances = [], locked = false } = {} ) {
	const f = uiFixture();
	Object.assign( f.state.gameplay, {
		exchange: {
			open: true,
			partner: 2,
			own: [],
			theirs: [],
			ownGold: 0,
			theirGold: 0,
			ownLocked: locked,
			theirLocked: locked,
			approved: false,
			requesting: false
		},
		friends: { rows: friends, prompt: null, pending: false, conclusion: null },
		social: {
			localName: "Player",
			self: 0,
			leader: 0,
			options: 0,
			members: [],
			guild,
			alliances,
			invitation: null
		}
	} );
	f.state.entities.push( {
		gid: 2,
		regionId: 1,
		x: 1,
		y: 0,
		z: 1,
		heading: 0,
		kind: "player",
		name: "Partner",
		mountedOn: 0,
		...partner
	} );
	return f;
}

// The chat column clips the lines' tails; assert on their heads.
// ("The other player is not registered in your guild/union/friend list." /
// "Please check the other player's ID, gold and equipment details once more.")
const COMPARE_TEXT = "The other player is not registered";
const REAFFIRM_TEXT = "Please check the other player's";
const member = name => ({
	id: 7,
	name,
	grade: 10,
	level: 1,
	donated: 0,
	permissions: 0,
	grant: "",
	model: 0,
	role: 0,
	offline: 0
});
const guildWith = names => ({
	id: 10,
	name: "OwnGuild",
	level: 1,
	gp: 0,
	subject: "",
	contents: "",
	members: names.map( member )
});

/*
================
settle

Step until the layout data lands and the window stands drawn.
================
*/
function settle( f ) {
	for ( let i = 0; i < 4; i++ ) f.ui.step( f.state, i * 1600 );
}

test("an exchange with a stranger prints the compare line", () => {
	const f = fixture();
	try {
		settle( f );
		assert.equal( f.hasText( COMPARE_TEXT ), true, "compare line missing for a stranger" );
	} finally {
		f.dispose();
	}
});

test("a friend, guildmate or union partner prints no compare line", () => {
	const cases = [
		fixture( { friends: [ { jid: 9, name: "Partner", modelRefId: 0, online: true } ] } ),
		fixture( { guild: guildWith( [ "Partner" ] ) } ),
		fixture( {
			partner: { guildId: 20 },
			guild: guildWith( [] ),
			alliances: [ { id: 20, name: "AllyGuild", level: 1, master: "Ally", model: 0, flags: 0 } ]
		} )
	];
	try {
		for ( const f of cases ) {
			settle( f );
			assert.equal( f.hasText( COMPARE_TEXT ), false, "compare line printed for a known partner" );
		}
	} finally {
		for ( const f of cases ) f.dispose();
	}
});

test("both offers locked print the reaffirm line", () => {
	const f = fixture( { locked: true } );
	try {
		settle( f );
		assert.equal( f.hasText( REAFFIRM_TEXT ), true, "reaffirm line missing at both-locked" );
	} finally {
		f.dispose();
	}
});

test("an unlocked exchange prints no reaffirm line", () => {
	const f = fixture();
	try {
		settle( f );
		assert.equal( f.hasText( REAFFIRM_TEXT ), false, "reaffirm line printed before both locked" );
	} finally {
		f.dispose();
	}
});
