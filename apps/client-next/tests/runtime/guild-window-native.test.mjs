/*
===========================================================================

guild-window-native.test.mjs - the guild window's commands as the original

The BN button audit (research ui-button-audit/guild-union.md): Withdraw
bans on the click (5E4C30 → 703DD0) unless the default-on Experimental
"Confirm guild ban" row asks first (port-only, the owner's choice
2026-10-15); the worker refuses the master or oneself with notice
0x10/0x1F before any question; the authority button reads "Give authority" for the
master and "Check authority" for a member (5E1E80); an invite goes to the
selected player or a selected mounted pet's rider (700B50).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const { targetPlayer } = await import( "../../src/engine/foundation/gameplay/target-player.ts" );
const { guildBanRefused } = await import( "../../src/engine/foundation/gameplay/guild-manager.ts" );
const { constantNativeNotice } = await import( "../../src/engine/foundation/gameplay/native-notice.ts" );

const SELF = 5;

/*
================
member
================
*/
function member( id, name, grade ) {
	return { id, name, grade, level: 40, permissions: 31, grant: "", donated: 0, model: 1907, role: 0, offline: 0 };
}

/*
================
guildWindow

The Guild window's member list for a player of grade selfGrade, with Other
selected.
================
*/
function guildWindow( t, selfGrade ) {
	const sent = [];
	const f = uiFixture( message => {
		if ( message.kind === "gameplay" ) sent.push( message.command );
	} );
	t.after( () => f.dispose() );
	Object.assign( f.state.gameplay, {
		social: {
			...emptySocial( "Player" ),
			self: SELF,
			guild: {
				id: 9,
				name: "Holders",
				level: 3,
				gp: 0,
				subject: "",
				contents: "",
				crest: 0,
				members: [ member( SELF, "Player", selfGrade ), member( 6, "Boss", 0 ), member( 7, "Other", 2 ) ]
			}
		}
	} );
	let now = 1000, last = null;
	const step = () => {
		for ( let i = 0; i < 6; i++ ) last = f.ui.step( f.state, now += 100 ) ?? last;
		return defined( last );
	};
	step();
	f.ui.event( { kind: "activate", id: "open-window:Guild" } );
	step();
	return { f, sent, step };
}

test("Withdraw with Confirm guild ban off bans on the click, as 5E4C30", t => {
	const { f, sent, step } = guildWindow( t, 1 );
	f.ui.event( /** @type {any} */ ({ kind: "experimental-preferences", value: { confirmGuildBan: false } }) );
	for ( const [id, name] of [ [ 7, "Other" ], [ 6, "Boss" ], [ SELF, "Player" ] ] ) {
		f.ui.event( { kind: "activate", id: "social-member:" + id } );
		step();
		f.ui.event( { kind: "activate", id: "guild-kick" } );
		const out = step();
		assert.ok( !out.controls.some( c => c.id === "social-confirm-yes" ), "no question" );
		assert.deepEqual( sent.pop(), { kind: "guild-kick", name } );
	}
});

test("Withdraw with Confirm guild ban on (the default) asks first, port-only", t => {
	const { f, sent, step } = guildWindow( t, 1 );
	f.ui.event( { kind: "activate", id: "social-member:7" } );
	step();
	f.ui.event( { kind: "activate", id: "guild-kick" } );
	let out = step();
	assert.ok( out.controls.some( c => c.id === "social-confirm-yes" ), "the question shows" );
	assert.equal( sent.length, 0 );
	f.ui.event( { kind: "activate", id: "social-confirm-no" } );
	step();
	assert.equal( sent.length, 0, "No drops the ban" );
	f.ui.event( { kind: "activate", id: "guild-kick" } );
	step();
	f.ui.event( { kind: "activate", id: "social-confirm-yes" } );
	assert.deepEqual( sent.pop(), { kind: "guild-kick", name: "Other" } );
	// The master and oneself are refused before any question: the worker
	// answers the click with 0x10/0x1F.
	for ( const [id, name] of [ [ 6, "Boss" ], [ SELF, "Player" ] ] ) {
		f.ui.event( { kind: "activate", id: "social-member:" + id } );
		step();
		f.ui.event( { kind: "activate", id: "guild-kick" } );
		out = step();
		assert.ok( !out.controls.some( c => c.id === "social-confirm-yes" ), "no question for " + name );
		assert.deepEqual( sent.pop(), { kind: "guild-kick", name } );
	}
});

test("the worker refuses a ban of the master or oneself with notice 0x10/0x1F", () => {
	const guild = { members: [ member( 6, "Master", 0 ), member( SELF, "Player", 1 ), member( 7, "Other", 2 ) ] };
	assert.equal( guildBanRefused( guild, "Player", "Master" ), true );
	assert.equal( guildBanRefused( guild, "Player", "Player" ), true );
	assert.equal( guildBanRefused( guild, "Player", "Other" ), false );
	assert.equal( guildBanRefused( guild, "Player", "Gone" ), false, "the server answers a name no longer listed" );
	assert.equal( guildBanRefused( null, "Player", "Other" ), false );
	assert.equal( constantNativeNotice( 0x10, 0x1f )?.key, "UIIT_MSG_GUILD_EXPULSION_NOTEXP" );
});

test("the authority button reads Give for the master and Check for a member (5E1E80)", t => {
	// UIIT_CTL_GUILD_UNION_CHAT_GRANT / _CONFIRM; the layout's own
	// UIIT_CTL_AUTHORITY_GRANT ("Grant authority") never shows.
	for ( const [grade, label] of [ [ 0, "Give authority" ], [ 1, "Check authority" ] ] ) {
		const { step } = guildWindow( t, grade );
		const button = defined( step().controls.find( c => c.id === "guild-dialog:authority" ) );
		assert.equal( button.label, label );
	}
});

test("an invite goes to the selected player or a mounted pet's rider (700B50)", () => {
	const entities = [
		{ gid: 1, kind: "player", name: "Player" },
		{ gid: 2, kind: "player", name: "Berk" },
		{ gid: 4, kind: "cos" },
		{ gid: 5, kind: "player", name: "Cale", mountedOn: 4 },
		{ gid: 6, kind: "monster" }
	];
	assert.equal( targetPlayer( 2, entities, 1 )?.gid, 2 );
	assert.equal( targetPlayer( 4, entities, 1 )?.gid, 5, "the rider, not the transport" );
	assert.equal( targetPlayer( 6, entities, 1 ), undefined );
	assert.equal( targetPlayer( 1, entities, 1 ), undefined, "never oneself" );
});

test("Invite on a selected mounted pet invites its rider, and nothing for a monster", t => {
	const { f, sent, step } = guildWindow( t, 0 );
	const base = f.state.entities[0];
	f.state.entities.push(
		{ ...base, gid: 40, kind: "cos", name: "" },
		{ ...base, gid: 41, kind: "player", name: "Cale", mountedOn: 40 },
		{ ...base, gid: 42, kind: "monster", name: "Tiger" }
	);
	f.state.gameplay = { ...f.state.gameplay, target: 40 };
	step();
	f.ui.event( { kind: "activate", id: "guild-invite" } );
	assert.deepEqual( sent.pop(), { kind: "guild-invite", gid: 41 } );
	f.state.gameplay = { ...f.state.gameplay, target: 42 };
	step();
	f.ui.event( { kind: "activate", id: "guild-invite" } );
	assert.equal( sent.length, 0, "a monster is not invited" );
});
