/*
===========================================================================

friend-list-native.test.mjs - the friend window's buttons as the original

The BN button audit (research ui-button-audit/friends.md): the original
never disables Whisper or Delete, opens the add box with the selected
player's name (or a mounted pet's rider), and sorts by name ascending on
every header press.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

const { friendAddPrefill, friendNameOrder } = await import( "../../src/engine/foundation/gameplay/friends.ts" );
const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );

const LOCAL = 1;
const ENTITIES = [
	{ gid: LOCAL, kind: "player", name: "Player" },
	{ gid: 2, kind: "player", name: "Berk" },
	{ gid: 3, kind: "monster", name: "Tiger" },
	// A transport (gid 4) with Cale on it, and an empty one (gid 6).
	{ gid: 4, kind: "cos", name: "" },
	{ gid: 5, kind: "player", name: "Cale", mountedOn: 4 },
	{ gid: 6, kind: "cos", name: "" }
];

test("the add box takes the selected player or a mounted pet's rider (602010)", () => {
	assert.equal( friendAddPrefill( 2, ENTITIES, LOCAL ), "Berk" );
	assert.equal( friendAddPrefill( 4, ENTITIES, LOCAL ), "Cale", "a mounted transport gives its rider" );
	assert.equal( friendAddPrefill( 6, ENTITIES, LOCAL ), "", "an empty pet gives nothing" );
	assert.equal( friendAddPrefill( 3, ENTITIES, LOCAL ), "", "a monster gives nothing" );
	assert.equal( friendAddPrefill( LOCAL, ENTITIES, LOCAL ), "", "selecting yourself gives nothing" );
	assert.equal( friendAddPrefill( undefined, ENTITIES, LOCAL ), "" );
	assert.equal( friendAddPrefill( 99, ENTITIES, LOCAL ), "", "a target no longer in view gives nothing" );
});

/*
================
friendWindow

The Community window's Friend tab with three friends (Cale offline).
================
*/
function friendWindow( t ) {
	const f = uiFixture();
	t.after( () => f.dispose() );
	Object.assign( f.state.gameplay, {
		social: emptySocial( "Player" ),
		friends: {
			rows: [
				{ jid: 9, name: "Dora", modelRefId: 1907, online: true },
				{ jid: 7, name: "Berk", modelRefId: 1907, online: true },
				{ jid: 8, name: "Cale", modelRefId: 1907, online: false }
			],
			pending: false
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
	f.ui.event( { kind: "activate", id: "guild-tab:3" } );
	return { f, step };
}

test("Whisper and Delete stay enabled, as the original never disables them", t => {
	const { f, step } = friendWindow( t );
	let out = step();
	const button = id => defined( out.controls.find( c => c.id === id ), id );
	assert.equal( button( "friend-whisper" ).disabled, false, "no selection" );
	assert.equal( button( "friend-remove" ).disabled, false, "no selection" );
	f.ui.event( { kind: "activate", id: "friend-row:8" } );
	out = step();
	assert.equal( button( "friend-whisper" ).disabled, false, "an offline friend can be whispered (600740)" );
	f.ui.event( { kind: "activate", id: "friend-whisper" } );
	out = step();
	assert.equal( out.controls.find( c => c.id === "chat-text" )?.value, "$Cale " );
});

test("the header always sorts by name ascending (604FD0)", t => {
	const { f, step } = friendWindow( t );
	const order = out => out.controls.filter( c => c.id.startsWith( "friend-row:" ) ).map( c => c.label );
	assert.deepEqual( order( step() ), [ "Berk", "Cale", "Dora" ] );
	f.ui.event( { kind: "activate", id: "friend-sort" } );
	assert.deepEqual( order( step() ), [ "Berk", "Cale", "Dora" ], "a press re-sorts, it never reverses" );
});

test("names order by 78EC00's intended rule: a-z folded, code units, shorter first", () => {
	const sorted = names => [ ...names ].sort( friendNameOrder );
	assert.deepEqual( sorted( [ "berk", "Cale", "Abe" ] ), [ "Abe", "berk", "Cale" ], "case folds" );
	// '_' (0x5F) sorts after A-Z (0x41-0x5A) once a-z folds to them; digits
	// (0x30-0x39) sort before letters. localeCompare would order these otherwise.
	assert.deepEqual( sorted( [ "a_b", "aZ", "a1" ] ), [ "a1", "aZ", "a_b" ] );
	assert.deepEqual( sorted( [ "Berkeley", "Berk" ] ), [ "Berk", "Berkeley" ], "a prefix comes first" );
	assert.equal( friendNameOrder( "BERK", "berk" ), 0 );
});

test("Add opens with the selected player's name", t => {
	const { f, step } = friendWindow( t );
	f.state.entities.push( { ...f.state.entities[0], gid: 2, name: "Berk" } );
	f.state.gameplay = { ...f.state.gameplay, target: 2 };
	step();
	f.ui.event( { kind: "activate", id: "friend-add" } );
	const out = step();
	assert.equal( out.controls.find( c => c.id === "friend-name" )?.value, "Berk" );
});
