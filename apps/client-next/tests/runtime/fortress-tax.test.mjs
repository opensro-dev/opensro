/*
===========================================================================

fortress-tax.test.mjs - the fortress manager's tax management window

The manager's rows (5D7AD0 action 0x33), the 0x71E1 query, rate and
collection requests, CIFTaxManagement's fill (665470) and its two confirm
boxes (665BA0, 664CE0), through the production HUD and the 0xB1E1 notices.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const fortress = await import( "../../src/engine/foundation/gameplay/fortress.ts" );
const services = await import( "../../src/engine/foundation/gameplay/fortress-services.ts" );
const { noticeText } = await import( "../../src/engine/foundation/ui/notice-text.ts" );
const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const { fortressTaxAmount } = await import( "../../src/engine/runtime/ui/hud/fortress-tax-hud.ts" );

const MANAGER = 23;
const SELF = 5;
const COPY = {
	SN_FORTRESS_JANGAN: "Jangan",
	UIIT_MSG_FORT_MANAGER_TAXCHANGE_COMPLETE: "Tax rate of [%s] has been changed to [%d]%%.",
	UIIT_MSG_FORT_MANAGER_TAXLEVY_COMPLETE: "[%d] gold was levied as the tax."
};

/*
================
taxState

A world at Jangan whose fortress the local guild (or its ally) holds.
================
*/
function taxState( holder ) {
	return {
		...fortress.fortressBootstrap( {
			gameWorldData: [ { gameWorldId: 7, warName: "FORTRESS_JANGAN" } ],
			siegeFortressData: [ {
				fortressId: 1,
				codeName: "FORTRESS_JANGAN",
				nameStrId: "SN_FORTRESS_JANGAN",
				requestFee: 5000000,
				taxTargets: 63
			} ]
		} ),
		worldId: 7,
		wars: holder ? [ { id: 1, name: holder, flags: 0 } ] : [],
		serviceSequence: 0
	};
}

/*
================
managerFixture

An open talk menu with a fortress manager (capability 0x400000).
================
*/
function managerFixture( t, holder, alliances = [], grade = 0 ) {
	const sent = [];
	const f = uiFixture( message => {
		if ( message.kind === "gameplay" ) sent.push( message.command );
	} );
	t.after( () => f.dispose() );
	f.state.entities.push( { ...f.state.entities[0], gid: MANAGER, refObjId: 2100, kind: "npc", name: "Manager" } );
	Object.assign( f.state.gameplay, {
		target: MANAGER,
		targetCapabilities: 0x400000,
		npcConversation: { phase: "menu", gid: MANAGER },
		fortress: taxState( holder ),
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
				members: [ { id: SELF, name: "Player", grade, level: 60, permissions: 31 } ]
			},
			alliances
		}
	} );
	let now = 0, last = null;
	const step = () => {
		for ( let i = 0; i < 16; i++ ) last = f.ui.step( f.state, now += 100 ) ?? last;
		return last;
	};
	const answer = service => {
		const state = f.state.gameplay.fortress;
		f.state.gameplay.fortress = { ...state, serviceSequence: state.serviceSequence + 1, service };
		return step();
	};
	return { f, sent, step, answer };
}

/*
================
rows
================
*/
function rows( presentation ) {
	return presentation.controls.filter( c => c.id.startsWith( "npc-fortress-" ) ).map( c => [ c.id, !!c.disabled ] );
}

test("the manager lists tax, staff and service for the holder and its allies only", t => {
	const holder = managerFixture( t, "Holders" );
	assert.deepEqual( rows( holder.step() ), [
		[ "npc-fortress-tax", false ],
		[ "npc-fortress-staff", false ],
		[ "npc-fortress-schedule", false ]
	] );
	// 5D7AD0 draws an ally's staff row red and 5D8930 ignores it.
	const ally = managerFixture( t, "Friends", [ {
		id: 4,
		name: "Friends",
		level: 1,
		master: "",
		model: 0,
		flags: 0
	} ] );
	assert.deepEqual( rows( ally.step() ), [
		[ "npc-fortress-tax", false ],
		[ "npc-fortress-staff", true ],
		[ "npc-fortress-schedule", false ]
	] );
	assert.deepEqual( rows( managerFixture( t, "Strangers" ).step() ), [] );
});

test("the tax row queries the fortress and the answer fills the window", t => {
	const { f, sent, step, answer } = managerFixture( t, "Holders" );
	step();
	f.ui.event( { kind: "activate", id: "npc-fortress-tax" } );
	assert.deepEqual( sent.at( -1 ), { kind: "fortress-tax", gid: MANAGER, fortress: 1 } );
	// Native shows the window before the answer, its buttons inert.
	let shown = step();
	assert.equal( shown.controls.find( c => c.id === "fortress-tax-modify" )?.disabled, true );
	shown = answer( { action: 0, result: 1, fortress: 1, taxRate: 10, gold: "4294967396" } );
	assert.equal( shown.controls.find( c => c.id === "fortress-tax-modify" )?.disabled, false );
	assert.equal( shown.controls.find( c => c.id === "fortress-tax-rate" )?.value, "30" );
	// The whole treasury, not 665470's low 32 bits (port-only).
	assert.ok( f.hasText( "4294967396" ) );
	assert.ok( f.hasText( "10" ) );
	assert.ok( shown.controls.some( c => c.id === "fortress-tax-help:19" && c.helpText ) );
});

test("an unchanged rate is refused locally and a moved slider sends action 1", t => {
	const { f, sent, step, answer } = managerFixture( t, "Holders" );
	step();
	f.ui.event( { kind: "activate", id: "npc-fortress-tax" } );
	answer( { action: 0, result: 1, fortress: 1, taxRate: 10, gold: "500" } );
	const before = sent.length;
	f.ui.event( { kind: "activate", id: "fortress-tax-modify" } );
	step();
	f.ui.event( { kind: "activate", id: "fortress-tax-yes" } );
	step();
	assert.equal( sent.length, before, "664CE0 never sends an unchanged ratio" );
	f.ui.event( { kind: "edit", id: "fortress-tax-rate", value: "35", start: 2, end: 2, composing: false } );
	f.ui.event( { kind: "activate", id: "fortress-tax-modify" } );
	step();
	assert.ok( f.hasText( "Current tax rate [10]%" ) && f.hasText( "Changed tax rate [15]%" ) );
	f.ui.event( { kind: "activate", id: "fortress-tax-yes" } );
	assert.deepEqual( sent.at( -1 ), { kind: "fortress-tax-rate", gid: MANAGER, fortress: 1, rate: 15 } );
	const shown = answer( { action: 1, result: 1, taxRate: 15 } );
	assert.equal( shown.controls.find( c => c.id === "fortress-tax-rate" )?.value, "35" );
});

test("collection asks for an amount capped at the treasury and sends action 2", t => {
	const { f, sent, step, answer } = managerFixture( t, "Holders" );
	step();
	f.ui.event( { kind: "activate", id: "npc-fortress-tax" } );
	answer( { action: 0, result: 1, fortress: 1, taxRate: 0, gold: "1234" } );
	f.ui.event( { kind: "activate", id: "fortress-tax-collect" } );
	let shown = step();
	assert.equal( shown.controls.find( c => c.id === "fortress-tax-amount" )?.value, "1234" );
	f.ui.event( { kind: "edit", id: "fortress-tax-amount", value: "99999", start: 5, end: 5, composing: false } );
	shown = step();
	assert.equal( shown.controls.find( c => c.id === "fortress-tax-amount" )?.value, "1234" );
	f.ui.event( { kind: "edit", id: "fortress-tax-amount", value: "200", start: 3, end: 3, composing: false } );
	f.ui.event( { kind: "activate", id: "fortress-tax-yes" } );
	assert.deepEqual( sent.at( -1 ), { kind: "fortress-tax-collect", gid: MANAGER, fortress: 1, gold: "200" } );
	answer( { action: 2, result: 1, gold: "200" } );
	assert.ok( f.hasText( "1034" ) );
});

test("only the guild master may press the tax buttons", t => {
	const { f, step, answer } = managerFixture( t, "Holders", [], 1 );
	step();
	f.ui.event( { kind: "activate", id: "npc-fortress-tax" } );
	const shown = answer( { action: 0, result: 1, fortress: 1, taxRate: 0, gold: "1" } );
	assert.equal( shown.controls.find( c => c.id === "fortress-tax-modify" )?.disabled, true );
	assert.equal( shown.controls.find( c => c.id === "fortress-tax-collect" )?.disabled, true );
});

test("leaving the manager closes the window", t => {
	const { f, step, answer } = managerFixture( t, "Holders" );
	step();
	f.ui.event( { kind: "activate", id: "npc-fortress-tax" } );
	answer( { action: 0, result: 1, fortress: 1, taxRate: 0, gold: "1" } );
	f.state.gameplay.npcConversation = null;
	const shown = step();
	assert.ok( !shown.controls.some( c => c.id === "fortress-tax-modify" ) );
});

test("the levy edit keeps digits and never exceeds the treasury", () => {
	assert.equal( fortressTaxAmount( "12a3", "500" ), "123" );
	assert.equal( fortressTaxAmount( "0007", "500" ), "7" );
	assert.equal( fortressTaxAmount( "9999", "500" ), "500" );
	assert.equal( fortressTaxAmount( "", "500" ), "" );
});

test("rate and levy answers report through the native messages", () => {
	const copy = key => COPY[key] ?? key;
	/** @type {import("../../src/engine/foundation/gameplay/fortress.ts").FortressState} */
	let state = taxState( "Holders" );
	/** @type {number | null} */
	let tracked = null;
	const fold = bytes => {
		state = {
			...state,
			...fortress.fortressPacket( state, { opcode: 0xb1e1, payload: Uint8Array.from( bytes ) } )
		};
		const result = fortress.fortressTaxNotice( state, tracked );
		tracked = result.fortress;
		return result.notice && noticeText( copy, result.notice );
	};
	// A rate answer before any query fills no window: 665730 stays silent.
	assert.equal( fold( [ 1, 1, 5, 0 ] ), null );
	assert.equal( fold( [ 0, 1, 1, 0, 0, 0, 10, 0, 0x10, 0x27, 0, 0, 0, 0, 0, 0 ] ), null );
	assert.equal( tracked, 1 );
	assert.equal( fold( [ 1, 1, 0xfb, 0xff ] ), "Tax rate of [Jangan] has been changed to [-5]%." );
	assert.equal( fold( [ 2, 1, 0x10, 0x27, 0, 0, 0, 0, 0, 0 ] ), "[10000] gold was levied as the tax." );
});

test("the tax requests are 0x71E1 actions 0, 1 and 2", () => {
	const frame = request => [ ...services.fortressServiceRequest( request ).payload ];
	assert.deepEqual( frame( { target: MANAGER, action: 0, fortress: 1 } ), [ 23, 0, 0, 0, 0, 1, 0, 0, 0 ] );
	assert.deepEqual( frame( { target: MANAGER, action: 1, fortress: 1, word: -5 } ), [
		23,
		0,
		0,
		0,
		1,
		1,
		0,
		0,
		0,
		0xfb,
		0xff
	] );
	assert.deepEqual( frame( { target: MANAGER, action: 2, fortress: 1, gold: "200" } ), [
		23,
		0,
		0,
		0,
		2,
		1,
		0,
		0,
		0,
		200,
		0,
		0,
		0,
		0,
		0,
		0,
		0
	] );
});
