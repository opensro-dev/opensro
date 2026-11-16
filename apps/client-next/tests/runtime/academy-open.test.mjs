/*
===========================================================================

academy-open.test.mjs - opening an Academy at the storage NPC

5D9100 lists "Academy Establishment" (row 0x2D) under a capability-4 NPC for
a campless player of level 60 or more. The row raises the fee question
(5D3880 case 6); its Yes prints OPEN_GOLD under 100,000 gold and otherwise
sends 0x72EC (5D26F0 case 6 -> 702BD0). 0xB2EC [1] carries the camp record
(82AA10), prints OPEN_SUCCESS and opens the Academy tab (76FBA0); [2] is a
category-0x1D refusal, with four more bytes only for 0x1C.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { academyPacket, academyOpenRequest, ACADEMY_OPEN_FEE } = await import(
	"../../src/engine/foundation/gameplay/academy.ts"
);
const { constantNativeNotice } = await import( "../../src/engine/foundation/gameplay/native-notice.ts" );
const { uiFixture } = await import( "../helpers/ui-fixture.mjs" );

const MASTER = 0x1234;

/*
================
campRecord

The 82AA10 record with the master as its one member: {u32 localId}{17 bytes}
{str subject}{str contents}{u8 count} and the master's 8290E0 row.
================
*/
function campRecord() {
	const bytes = [];
	const u32 = v => bytes.push( v & 255, v >> 8 & 255, v >> 16 & 255, v >>> 24 );
	const str = s => bytes.push( s.length & 255, s.length >> 8, ...Array.from( s, c => c.charCodeAt( 0 ) ) );
	u32( MASTER );
	bytes.push( ...new Array( 17 ).fill( 0 ) );
	str( "" );
	str( "" );
	bytes.push( 1 );
	u32( MASTER );
	u32( MASTER );
	u32( 0 );
	str( "Mentor" );
	bytes.push( 0, 0, ...new Array( 16 ).fill( 0 ), 60, 60 );
	u32( 0 );
	bytes.push( 0 );
	u32( 0 );
	bytes.push( 0, 0, 0, 0, 0, 0, 0, 0 );
	str( "" );
	return bytes;
}

const opened = () => ({ opcode: 0xb2ec, payload: Uint8Array.from( [ 1, ...campRecord() ] ) });
const empty = { member: false, page: 0, pages: 0, rows: [], request: null, result: null };

test("0xB2EC [1] seats the master in the new camp; [2] changes nothing (76FBA0)", () => {
	const camp = defined( academyPacket( empty, opened() ) );
	assert.equal( camp.member, true );
	assert.equal( camp.localMemberId, MASTER );
	assert.deepEqual( camp.members?.map( m => [ m.name, m.kind, m.level ] ), [ [ "Mentor", 0, 60 ] ] );
	assert.equal( camp.opened, 1, "an opening counts" );
	assert.equal( defined( academyPacket( camp, opened() ) ).opened, 2 );
	for ( const payload of [ [ 2, 0x0a ], [ 2, 0x12 ], [ 2, 0x1c, 1, 0, 0, 0 ] ] ) {
		assert.equal( academyPacket( empty, { opcode: 0xb2ec, payload: Uint8Array.from( payload ) } ), empty );
	}
	assert.throws( () => academyPacket( empty, { opcode: 0xb2ec, payload: Uint8Array.of( 2, 0x1c ) } ) );
	assert.throws( () => academyPacket( empty, { opcode: 0xb2ec, payload: Uint8Array.of( 2, 0x0a, 0 ) } ), /Trailing/ );
	assert.throws( () => academyPacket( empty, { opcode: 0xb2ec, payload: Uint8Array.of( 3 ) } ) );
	assert.deepEqual( academyOpenRequest(), { opcode: 0x72ec, payload: new Uint8Array( 4 ) } );
});

/*
================
gameplayWithGold

A world gameplay owner holding gold (the 0x30B3 type-1 balance).
================
*/
function gameplayWithGold( gold, sent ) {
	const g = createGameplay( frame => sent.push( frame ) );
	g.bootstrap( { academyMember: false } );
	g.seed( /** @type {any} */ ({ gid: 1, regionId: 1, x: 0, y: 0, z: 0, heading: 0 }) );
	const balance = new Uint8Array( 10 );
	balance[0] = 1;
	new DataView( balance.buffer ).setBigUint64( 1, BigInt( gold ), true );
	g.receive( { opcode: 0x30b3, payload: balance }, 0 );
	g.take();
	return g;
}

test("Yes sends 0x72EC only with the fee in hand, else prints OPEN_GOLD (5D26F0 case 6)", () => {
	const short = [], poor = gameplayWithGold( ACADEMY_OPEN_FEE - 1, short );
	assert.equal( poor.command( { kind: "academy-open" }, 0, undefined ), null );
	assert.equal( short.length, 0, "nothing is sent short of the fee" );
	assert.equal( defined( defined( poor.take() ).notices ).at( -1 )?.key, "UIIT_MSG_TC_ERROR_OPEN_GOLD" );
	const sent = [], rich = gameplayWithGold( ACADEMY_OPEN_FEE, sent );
	rich.command( { kind: "academy-open" }, 0, undefined );
	assert.deepEqual( sent, [ academyOpenRequest() ] );
});

test("the answer prints OPEN_SUCCESS, or the category-0x1D refusal", () => {
	const g = gameplayWithGold( ACADEMY_OPEN_FEE, [] );
	g.receive( { opcode: 0xb2ec, payload: Uint8Array.of( 2, 0x12 ) }, 0 );
	let state = defined( g.take() );
	assert.deepEqual( { ...defined( state.notices ).at( -1 ), sequence: undefined }, {
		...constantNativeNotice( 0x1d, 0x12 ),
		sequence: undefined
	} );
	assert.equal( defined( state.academy ).member, false );
	g.receive( opened(), 0 );
	state = defined( g.take() );
	assert.equal( defined( state.notices ).at( -1 )?.key, "UIIT_MSG_TC_OPEN_SUCCESS" );
	assert.equal( defined( state.academy ).member, true );
});

/*
================
storageTalk

A UI talking to a storage keeper (capability 4) as a player of level, with
or without a camp, with the commands it sends and a step returning the
controls by id.
================
*/
function storageTalk( level, member ) {
	const commands = [], f = uiFixture( command => commands.push( command ) );
	let now = 1000, seen = null;
	const step = () => {
		for ( let i = 0; i < 20; i++ ) seen = f.ui.step( f.state, now += 50 ) ?? seen;
		return new Map( (seen?.controls ?? []).map( control => [ control.id, control ] ) );
	};
	f.state.gameplay = {
		...f.state.gameplay,
		npcConversation: { gid: 77, phase: "menu" },
		target: 77,
		targetCapabilities: 4,
		academy: { ...empty, member },
		progression: { ...f.state.gameplay.progression, level }
	};
	return { f, commands, step };
}

const sentOpen = commands => commands.filter( c => c.command?.kind === "academy-open" ).length;

test("the storage keeper lists the row only for a campless player of 60 or more (5D9100)", () => {
	for ( const [level, member, listed] of [ [ 60, false, true ], [ 59, false, false ], [ 80, true, false ] ] ) {
		const { f, step } = storageTalk( level, member );
		try {
			const controls = step();
			assert.ok( controls.has( "storage-open" ), "the storage row" );
			assert.equal( controls.has( "npc-academy-open" ), listed, `level ${level}, member ${member}` );
		} finally {
			f.dispose();
		}
	}
});

test("the row asks the fee question; No sends nothing, Yes sends the opening", () => {
	const { f, commands, step } = storageTalk( 60, false );
	try {
		step();
		f.ui.event( { kind: "activate", id: "npc-academy-open" } );
		let controls = step();
		assert.ok( controls.has( "job-confirm-yes" ) && controls.has( "job-confirm-no" ), "the question is up" );
		assert.ok( f.hasText( "100,000" ), "it names the fee" );
		f.ui.event( { kind: "activate", id: "job-confirm-no" } );
		controls = step();
		assert.ok( !controls.has( "job-confirm-yes" ) );
		assert.equal( sentOpen( commands ), 0 );
		f.ui.event( { kind: "activate", id: "npc-academy-open" } );
		step();
		f.ui.event( { kind: "activate", id: "job-confirm-yes" } );
		step();
		assert.equal( sentOpen( commands ), 1 );
	} finally {
		f.dispose();
	}
});

test("an accepted opening opens the Academy tab (CGInterface_OpenAcademyTab 69C810)", () => {
	const { f, step } = storageTalk( 60, false );
	try {
		step();
		assert.ok( !f.hasText( "Mentor" ), "no Academy tab yet" );
		const camp = defined( academyPacket( f.state.gameplay.academy, opened() ) );
		f.state.gameplay = { ...f.state.gameplay, academy: camp };
		step();
		assert.ok( f.hasText( "Mentor" ), "the Academy tab lists the new master" );
	} finally {
		f.dispose();
	}
});

test("the fee question draws each line whole in a box sized to it (52DB20, #3550)", () => {
	const { f, step } = storageTalk( 60, false );
	try {
		step();
		f.ui.event( { kind: "activate", id: "npc-academy-open" } );
		step();
		assert.ok( f.hasText( "Fee of 100,000 gold is needed to establish an Academy." ), "the first line broke" );
		assert.ok( f.hasText( "Would you like to establish an Academy?" ), "the second line broke" );
	} finally {
		f.dispose();
	}
});
