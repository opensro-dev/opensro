/*
===========================================================================

fortress-production.test.mjs - the smith's and trainer's production window

The talk rows (5D8C86), the 0x71E1 query whose answer opens
CIFFortressMakeItemWnd, the make and cancel boxes (MsgBoxMakeItem /
MsgBoxMakeItemCancel), the local countdown (65A5E0) and the collect
request, through the production HUD.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const fortress = await import( "../../src/engine/foundation/gameplay/fortress.ts" );
const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const production = await import( "../../src/engine/foundation/gameplay/fortress-production.ts" );

const SMITH = 31;
const SELF = 5;
const ITEM = 9001;
const SMITH_CAPABILITY = 0x2000000;
const COPY = {
	UIIT_STT_REMAIN_TIME: "Remaining time",
	UIIT_STT_HOUR: "Hours",
	UIIT_STT_MINUTE: "Minutes",
	PARAM_DAY: "Day",
	PARAM_SECOND: "Second"
};

/*
================
smithFixture

An open talk menu with a fortress smith; the local guild holds Jangan when
holder is "Holders". Role 1 is the fortress commander, 8 the smith.
================
*/
function smithFixture( t, holder, member = { grade: 2, role: 1 } ) {
	const sent = [];
	const f = uiFixture( message => {
		if ( message.kind === "gameplay" ) sent.push( message.command );
	} );
	t.after( () => f.dispose() );
	f.state.entities.push( { ...f.state.entities[0], gid: SMITH, refObjId: 2101, kind: "npc", name: "Smith" } );
	Object.assign( f.state.gameplay, {
		target: SMITH,
		targetCapabilities: SMITH_CAPABILITY,
		npcConversation: { phase: "menu", gid: SMITH },
		fortressForge: [
			{ refObjId: ITEM, gold: 100, gp: 10, minutes: 2, staff: "smith", name: "Siege Ram", maxStack: 5 },
			{ refObjId: 9002, gold: 100, gp: 10, minutes: 2, staff: "trainer", name: "Cart" }
		],
		fortress: {
			...fortress.fortressBootstrap( {
				gameWorldData: [ { gameWorldId: 7, warName: "FORTRESS_JANGAN" } ],
				siegeFortressData: [ {
					fortressId: 1,
					codeName: "FORTRESS_JANGAN",
					nameStrId: "SN_FORTRESS_JANGAN",
					requestFee: 1,
					taxTargets: 63
				} ]
			} ),
			worldId: 7,
			wars: [ { id: 1, name: holder, flags: 0 } ],
			serviceSequence: 0
		},
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
				members: [ { id: SELF, name: "Player", level: 60, permissions: 31, ...member } ]
			}
		}
	} );
	let now = 0, last = null;
	const step = ( ms = 100 ) => {
		for ( let i = 0; i < 16; i++ ) last = f.ui.step( f.state, now += ms ) ?? last;
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
control
================
*/
function control( presentation, id ) {
	return presentation.controls.find( c => c.id === id );
}

test("the remaining time reads days, hours, rounded-up minutes, then seconds", () => {
	const copy = key => COPY[key] ?? key;
	assert.equal( production.fortressProductionTimeText( 90061, copy ), "Remaining time : 1Day 1Hours 1Minutes " );
	assert.equal( production.fortressProductionTimeText( 3660, copy ), "Remaining time : 1Hours 1Minutes " );
	assert.equal( production.fortressProductionTimeText( 61, copy ), "Remaining time : 2 Minutes" );
	assert.equal( production.fortressProductionTimeText( 59, copy ), "Remaining time : 59 Second" );
});

test("the count edit keeps two digits capped at twenty", () => {
	assert.equal( production.fortressProductionCount( "7" ), "7" );
	assert.equal( production.fortressProductionCount( "35" ), "20" );
	assert.equal( production.fortressProductionCount( "0a5" ), "5" );
	assert.equal( production.fortressProductionCount( "" ), "" );
});

test("the commander or the exact staff role may operate the window", () => {
	assert.equal( production.fortressProductionMayOperate( { role: 1 }, "smith" ), true );
	// 659D50 never reads the guild grade: a leader without a role may not.
	assert.equal( production.fortressProductionMayOperate( { role: 0 }, "smith" ), false );
	assert.equal( production.fortressProductionMayOperate( { role: 8 }, "smith" ), true );
	assert.equal( production.fortressProductionMayOperate( { role: 8 }, "trainer" ), false );
	assert.equal( production.fortressProductionMayOperate( { role: 0x18 }, "smith" ), false );
	assert.equal( production.fortressProductionMayOperate( undefined, "smith" ), false );
});

test("the smith's row refuses a guild that does not hold the fortress", t => {
	const { f, sent, step } = smithFixture( t, "Strangers" );
	assert.ok( control( step(), "npc-fortress-production:smith" ) );
	f.ui.event( { kind: "activate", id: "npc-fortress-production:smith" } );
	step();
	assert.equal( sent.length, 0 );
	assert.ok( f.hasText( "You are not authorized." ), "notice 0x10/0x1E" );
});

test("the query answer opens the window, and make, cancel and collect follow the order", t => {
	const { f, sent, step, answer } = smithFixture( t, "Holders" );
	step();
	f.ui.event( { kind: "activate", id: "npc-fortress-production:smith" } );
	assert.deepEqual( sent.at( -1 ), { kind: "fortress-production", gid: SMITH, fortress: 1, action: 0x0d } );
	// Unlike the tax window, nothing shows before the answer.
	assert.equal( control( step(), "fortress-production-make:" + ITEM ), undefined );
	let shown = answer( { action: 0x0d, result: 1, fortress: 1, producing: false } );
	assert.equal( control( shown, "fortress-production-make:" + ITEM )?.disabled, false );
	assert.equal( control( shown, "fortress-production-make:9002" ), undefined, "the trainer's rows stay out" );

	f.ui.event( { kind: "activate", id: "fortress-production-make:" + ITEM } );
	step();
	f.ui.event( { kind: "edit", id: "fortress-production-count", value: "3", start: 1, end: 1, composing: false } );
	f.ui.event( { kind: "activate", id: "fortress-production-yes" } );
	assert.deepEqual( sent.at( -1 ), {
		kind: "fortress-production",
		gid: SMITH,
		fortress: 1,
		action: 0x0e,
		reference: ITEM,
		count: 3
	} );
	shown = answer( { action: 0x0e, result: 1, fortress: 1, reference: ITEM, quantity: 3, productionTime: "30" } );
	assert.equal( control( shown, "fortress-production-make:" + ITEM )?.disabled, true );
	assert.equal( control( shown, "fortress-production-cancel" )?.disabled, false );
	assert.equal( control( shown, "fortress-production-complete" ), undefined );

	f.ui.event( { kind: "activate", id: "fortress-production-cancel" } );
	step();
	f.ui.event( { kind: "activate", id: "fortress-production-yes" } );
	assert.deepEqual( sent.at( -1 ), {
		kind: "fortress-production",
		gid: SMITH,
		fortress: 1,
		action: 0x0f,
		reference: ITEM
	} );

	// 65A5E0 completes the order locally once its time runs out.
	step( 1000 );
	step( 1000 );
	shown = step( 1000 );
	f.ui.event( { kind: "activate", id: "fortress-production-complete" } );
	assert.deepEqual( sent.at( -1 ), {
		kind: "fortress-production",
		gid: SMITH,
		fortress: 1,
		action: 0x10,
		reference: ITEM,
		count: 3,
		stackLimit: 5
	} );
	answer( { action: 0x10, result: 1, fortress: 1, reference: ITEM, quantity: 3 } );
	shown = step();
	assert.equal( control( shown, "fortress-production-make:" + ITEM )?.disabled, false );
});

test("a member without the staff role sees the window but cannot make", t => {
	const { f, step, answer } = smithFixture( t, "Holders", { grade: 0, role: 0 } );
	step();
	f.ui.event( { kind: "activate", id: "npc-fortress-production:smith" } );
	const shown = answer( { action: 0x0d, result: 1, fortress: 1, producing: false } );
	assert.equal( control( shown, "fortress-production-make:" + ITEM )?.disabled, true );
});

test("a refused query opens nothing and leaving the smith closes the window", t => {
	const { f, step, answer } = smithFixture( t, "Holders" );
	step();
	f.ui.event( { kind: "activate", id: "npc-fortress-production:smith" } );
	assert.equal( control( answer( { action: 0x0d, result: 2, code: 1 } ), "fortress-production-close" ), undefined );
	f.ui.event( { kind: "activate", id: "npc-fortress-production:smith" } );
	assert.ok(
		control( answer( { action: 0x0d, result: 1, fortress: 1, producing: false } ), "fortress-production-close" )
	);
	f.state.gameplay.target = 0;
	assert.equal( control( step(), "fortress-production-close" ), undefined );
});
