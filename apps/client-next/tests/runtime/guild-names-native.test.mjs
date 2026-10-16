/*
===========================================================================

guild-names-native.test.mjs - typed guild names checked as the original

The BN audit (research ui-button-audit/guild-union.md): create (5D3D10),
the nickname (5E35E0) and the war request (619620) check their text with
the filter's guild-name column (CStringCheck_IsAllowedGuildName 790A30)
and ask their native questions before anything is sent.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

const { creationNameRules, checkedNameError, NAME_FILTER_GUILD } = await import(
	"../../src/engine/foundation/ui/character-create.ts"
);
const { cp949Length, guildCreateNameCheck, guildCreateSendRefusal, guildGrantNameCheck, guildWarNameRefusal } =
	await import( "../../src/engine/foundation/gameplay/guild-names.ts" );

// One table of A-Z, a-z and digits; "rogue" is banned only from character
// names, "brigand" only from guild names (whole words, flag 1), "thief" from
// both (substring, flag 2).
const FILTER = [
	"#ALLOW_ID_TABLE\t0x0041\t0x005A",
	"#ALLOW_ID_TABLE\t0x0061\t0x007A",
	"#ALLOW_ID_TABLE\t0x0030\t0x0039",
	"rogue\t1\t0\t0\t0",
	"brigand\t0\t0\t1\t0",
	"thief\t2\t2\t2\t2"
].join( "\n" );
const bytes = new TextEncoder().encode( FILTER ).buffer;
const characterRules = creationNameRules( bytes );
const guildRules = creationNameRules( bytes, NAME_FILTER_GUILD );

test("each flag column is its own list (791660)", () => {
	assert.equal( checkedNameError( "Rogue", characterRules ) !== null, true );
	assert.equal( checkedNameError( "Rogue", guildRules ), null, "rogue is banned from character names only" );
	assert.equal( checkedNameError( "Brigand", characterRules ), null );
	assert.equal( checkedNameError( "Brigand", guildRules ) !== null, true, "brigand is banned from guild names" );
	assert.equal( checkedNameError( "Thiefdom", guildRules ) !== null, true );
	assert.equal( checkedNameError( "Thiefdom", characterRules ) !== null, true );
});

test("CP949 counts two bytes for every non-ASCII character", () => {
	assert.equal( cp949Length( "Holders" ), 7 );
	assert.equal( cp949Length( "가나" ), 4 );
});

test("create: a bad length keeps the box, a refused name closes it (5D3D10)", () => {
	const length = guildCreateNameCheck( "A", guildRules );
	assert.equal( length.notice?.key, "UIIT_MSG_GUILDERR_INVALID_GUILDNAME_LEN" );
	assert.deepEqual( [ length.keepOpen, length.ask ], [ true, false ] );
	assert.equal( guildCreateNameCheck( "ABCDEFGHIJKLM", guildRules ).keepOpen, true, "13 bytes is too long" );
	const refused = guildCreateNameCheck( "Brigand", guildRules );
	assert.equal( refused.notice?.key, "UIIT_MSG_GUILDERR_NOT_ALLOWED_GUILDNAME" );
	assert.deepEqual( [ refused.keepOpen, refused.ask ], [ false, false ] );
	assert.deepEqual( guildCreateNameCheck( "Holders", guildRules ), { notice: null, keepOpen: false, ask: true } );
	// 700590 checks the character column at the send.
	assert.equal( guildCreateSendRefusal( "Rogue", characterRules )?.key, "UIIT_MSG_GUILDERR_NOT_ALLOWED_GUILDNAME" );
	assert.equal( guildCreateSendRefusal( "Holders", characterRules ), null );
});

test("nickname: too long keeps the box, a refused name closes it (5E35E0)", () => {
	const long = guildGrantNameCheck( "ABCDEFGHIJKLM", guildRules );
	assert.equal( long.notice?.key, "UIIT_MSG_GUILD_ERROR_NAME_GRANT_SPECIAL_LETTER" );
	assert.equal( long.keepOpen, true );
	const refused = guildGrantNameCheck( "Brigand", guildRules );
	assert.equal( refused.notice?.key, "UIIT_MSG_GUILD_ERROR_NAME_GRANT_SPECIAL_LETTER" );
	assert.deepEqual( [ refused.keepOpen, refused.ask ], [ false, false ] );
	assert.deepEqual( guildGrantNameCheck( "Officer", guildRules ), { notice: null, keepOpen: false, ask: true } );
	assert.equal( guildGrantNameCheck( "", guildRules ).ask, true, "clearing a nickname is asked too" );
});

test("war request refuses in 619620's order", () => {
	const key = ( name, stake = 0 ) => guildWarNameRefusal( name, "Holders", stake, guildRules )?.key ?? null;
	assert.equal( key( "" ), "UIIT_MSG_GUILDWAR_HGN_INPUT" );
	assert.equal( key( "Holders" ), "UIIT_MSG_GUILDWARERR_SELFWAR" );
	assert.equal( key( "A" ), "UIIT_MSG_GUILDERR_INVALID_GUILDNAME_LEN" );
	assert.equal( key( "Brigand" ), "UIIT_MSG_GUILDWARERR_ERRINPUT" );
	assert.equal( key( "Rivals", 500_000_001 ), "UIIT_MSG_GUILDWARERR_LIMIT_COMPENSATION" );
	assert.equal( key( "Rivals", 500_000_000 ), null );
	assert.equal( guildWarNameRefusal( "", "Holders", 0, guildRules )?.banner, true, "shown in the centre too" );
});

/*
================
createDialog

A guild manager's talk menu with the create row's name box open.
================
*/
function createDialog( t ) {
	const sent = [];
	const f = uiFixture( message => {
		if ( message.kind === "gameplay" ) sent.push( message.command );
	} );
	t.after( () => f.dispose() );
	f.state.gameplay = { ...f.state.gameplay, target: 9, npcConversation: { phase: "menu", gid: 9 } };
	let now = 1000, last = null;
	const step = () => {
		for ( let i = 0; i < 6; i++ ) last = f.ui.step( f.state, now += 100 ) ?? last;
		return defined( last );
	};
	step();
	f.ui.event( { kind: "activate", id: "npc-guild:create" } );
	step();
	const type = value => {
		f.ui.event( {
			kind: "edit",
			id: "guild-manager-text",
			value,
			start: value.length,
			end: value.length,
			composing: false
		} );
		step();
	};
	return { f, sent, step, type };
}

test("create asks the name, then the cost, then sends 0x7663", t => {
	const { f, sent, step, type } = createDialog( t );
	type( "Holders" );
	f.ui.event( { kind: "activate", id: "guild-manager-yes" } );
	step();
	assert.ok( f.hasText( "Will you name your guild as [Holders]?" ), "box 0x11" );
	assert.equal( sent.length, 0 );
	f.ui.event( { kind: "activate", id: "guild-manager-yes" } );
	step();
	assert.ok( f.hasText( "If you pay 0.5 million gold, [Holders]guild will be created" ), "box 0x12" );
	assert.equal( sent.length, 0 );
	f.ui.event( { kind: "activate", id: "guild-manager-yes" } );
	step();
	assert.deepEqual( sent.pop(), { kind: "guild-create", gid: 9, name: "Holders" } );
});

test("create with one letter shows the length notice and keeps the box", t => {
	const { f, sent, step, type } = createDialog( t );
	type( "A" );
	f.ui.event( { kind: "activate", id: "guild-manager-yes" } );
	const out = step();
	assert.equal( sent.pop()?.notice?.key, "UIIT_MSG_GUILDERR_INVALID_GUILDNAME_LEN" );
	assert.ok( out.controls.some( c => c.id === "guild-manager-text" ), "the name box stays" );
});

test("a war request with no name shows HGN_INPUT and keeps the window (619620)", t => {
	const sent = [];
	const f = uiFixture( message => {
		if ( message.kind === "gameplay" ) sent.push( message.command );
	} );
	t.after( () => f.dispose() );
	const master = { id: 5, name: "Player", grade: 0, level: 40, permissions: 31, grant: "", donated: 0, model: 1907 };
	f.state.gameplay = {
		...f.state.gameplay,
		social: {
			localName: "Player",
			self: 5,
			leader: 0,
			options: 0,
			members: [],
			guild: {
				id: 9,
				name: "Holders",
				level: 3,
				gp: 0,
				subject: "",
				contents: "",
				crest: 0,
				members: [ master ]
			},
			invitation: null,
			error: null,
			wars: []
		}
	};
	let now = 1000, last = null;
	const step = () => {
		for ( let i = 0; i < 6; i++ ) last = f.ui.step( f.state, now += 100 ) ?? last;
		return defined( last );
	};
	step();
	f.ui.event( { kind: "activate", id: "open-window:Guild" } );
	step();
	f.ui.event( { kind: "activate", id: "guild-tab:2" } );
	step();
	f.ui.event( { kind: "activate", id: "war-declare" } );
	step();
	f.ui.event( { kind: "activate", id: "war-confirm" } );
	const out = step();
	assert.equal( sent.pop()?.notice?.key, "UIIT_MSG_GUILDWAR_HGN_INPUT" );
	assert.ok( out.controls.some( c => c.id === "war-name" ), "the request stays open" );
});

test("the worker shows a UI refusal as one of its notices", async () => {
	const { createGameplay } = await import(
		"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
	);
	const g = createGameplay( () => {} );
	g.bootstrap( {} );
	g.take();
	const notice = defined( guildWarNameRefusal( "", "Holders", 0, guildRules ) );
	assert.equal( g.command( { kind: "local-notice", notice }, 0, undefined ), null, "nothing is sent" );
	const shown = defined( defined( defined( g.take() ).notices ).at( -1 ) );
	assert.equal( shown.key, "UIIT_MSG_GUILDWAR_HGN_INPUT" );
	assert.equal( shown.banner, true );
	assert.equal( typeof shown.sequence, "number" );
});
