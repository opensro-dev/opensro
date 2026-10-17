/*
===========================================================================

academy-honor.test.mjs - Academy Honor Ranking, slot actions and Judge UI

Tests the storage NPC's Honor Ranking row 0x2E (0x72D0 -> 0xB2D0 ->
CIFApprenticeShipHonorRanking 6E8C10), the camp evaluation badge (+0x34 in
82AA10 and 0x3AC5 subtype 12), the Academy window's Dissolve button
(0x77C5 / 0xB7C5), the member slot's Exit/Graduate button 18
(CIFApprenticeShipSlot 5C6F20..5C7938), the Guardian Evaluation dialog
(CIFApprenticeShipJudge 6E97A0 -> 0x710A), and 0x3AC5 status notices.

===========================================================================
*/

import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { defined } from "../helpers/defined.mjs";

const {
	academyBootstrap,
	academyPacket,
	academyHonorRankRequest,
	academyDisbandRequest,
	academyLeaveRequest,
	academyGraduateRequest,
	academyKickRequest,
	academyCompulsoryGraduateRequest
} = await import( "../../src/engine/foundation/gameplay/academy.ts" );
const {
	academyHonorBadge,
	academyHonorPage,
	academyHonorPages,
	academySlotAction
} = await import( "../../src/engine/runtime/ui/hud/academy-hud.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { uiFixture } = await import( "../helpers/ui-fixture.mjs" );

const empty = { member: false, page: 0, pages: 0, rows: [], request: null, result: null };

/*
================
encodeStr

A length-prefixed Windows-1252 string for wire test fixtures.
================
*/
function encodeStr( text ) {
	const bytes = Buffer.from( text, "latin1" );
	const header = Buffer.alloc( 2 );
	header.writeUInt16LE( bytes.length, 0 );
	return Buffer.concat( [ header, bytes ] );
}

/*
================
encodeHonorList

A 0xB2D0 [1, count, ...] payload for the given rows.
================
*/
function encodeHonorList( rows ) {
	const parts = [ Buffer.from( [ 1, rows.length ] ) ];
	for ( const row of rows ) {
		const head = Buffer.alloc( 5 );
		head.writeUInt32LE( row.ranking, 0 );
		head[4] = row.rank;
		const mid = Buffer.alloc( 6 );
		mid[0] = row.level;
		mid[1] = row.maxLevel;
		mid.writeUInt32LE( row.graduates, 2 );
		parts.push( head, encodeStr( row.name ), mid, encodeStr( row.guild ) );
	}
	return new Uint8Array( Buffer.concat( parts ) );
}

/*
================
encodeMemberRow

One 8290E0 member record inside a campRecord.
================
*/
function encodeMemberRow( member ) {
	const head = Buffer.alloc( 12 );
	head.writeUInt32LE( 0, 0 );
	head.writeUInt32LE( member.id, 4 );
	head.writeUInt32LE( member.model ?? 1907, 8 );
	const tail = Buffer.alloc( 37 );
	tail[0] = member.kind;
	tail[18] = member.entryLevel ?? member.level;
	tail[19] = member.level;
	tail.writeUInt32LE( member.honor ?? 0, 20 );
	tail[24] = member.offline ? 1 : 0;
	return Buffer.concat( [ head, encodeStr( member.name ), tail, encodeStr( member.location ?? "Jangan" ) ] );
}

/*
================
encodeCampSeed

A 0x3AC5 [10, 1, ...] campRecord payload with evaluation at the 17th header byte.
================
*/
function encodeCampSeed( localMemberId, evaluation, members, subject = "Notice", contents = "Hello" ) {
	const header = Buffer.alloc( 2 + 4 + 17 );
	header[0] = 10;
	header[1] = 1;
	header.writeUInt32LE( localMemberId, 2 );
	header[2 + 4 + 16] = evaluation;
	const parts = [
		header,
		encodeStr( subject ),
		encodeStr( contents ),
		Buffer.from( [ members.length ] ),
		...members.map( encodeMemberRow )
	];
	return new Uint8Array( Buffer.concat( parts ) );
}

/*
================
storageTalk

Opens the storage keeper's talk menu in a UI fixture.
================
*/
function storageTalk( level, member, extraAcademy = {} ) {
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
		academy: { ...empty, member, ...extraAcademy },
		progression: { ...f.state.gameplay.progression, level }
	};
	return { f, commands, step };
}

test("0x72D0 / 0xB2D0 Honor Ranking wire codec and pagination (703070, 771750, 6E8C10)", () => {
	assert.deepEqual( academyHonorRankRequest( 0x11223344 ), {
		opcode: 0x72d0,
		payload: Uint8Array.of( 0x44, 0x33, 0x22, 0x11 )
	} );
	const rows = Array.from( { length: 12 }, ( _, i ) => ({
		ranking: i + 1,
		rank: (i % 5) + 1,
		name: `Mentor_${i + 1}`,
		level: 60 + i,
		maxLevel: 80,
		graduates: (i + 1) * 3,
		guild: i % 2 === 0 ? `Guild_${i + 1}` : ""
	}) );
	const base = defined( academyBootstrap( { academyMember: false } ) );
	const next = defined( academyPacket( base, { opcode: 0xb2d0, payload: encodeHonorList( rows ) } ) );
	assert.equal( next?.honorRanks?.sequence, 1 );
	assert.equal( next?.honorRanks?.rows.length, 12 );
	assert.deepEqual( next?.honorRanks?.rows[0], rows[0] );

	assert.equal( academyHonorPages( 0 ), 1 );
	assert.equal( academyHonorPages( 10 ), 1 );
	assert.equal( academyHonorPages( 12 ), 2 );

	const strings = { UIIT_STT_LEVEL_LV: "Lv", UIIT_STT_NO_GUILD: "None" };
	const page0 = academyHonorPage( rows, 0, strings );
	assert.deepEqual( page0[0], {
		rankingText: "1",
		badgePath: "interface/ifcommon/com_honor_level_1.png",
		nameText: "Mentor_1",
		levelText: "60(80)Lv",
		graduatesText: "3",
		guildText: "Guild_1"
	} );
	assert.equal( page0[1]?.guildText, "None", "empty guild falls back to UIIT_STT_NO_GUILD (6E9597)" );
	const page1 = academyHonorPage( rows, 1, strings );
	assert.equal( page1[0]?.nameText, "Mentor_11" );
	assert.equal( page1[1]?.nameText, "Mentor_12" );
	assert.equal( page1[2], null );

	// Refusal [2, code] leaves honorRanks untouched and routes through category 0x1D in gameplay.
	const refused = academyPacket( next, { opcode: 0xb2d0, payload: Uint8Array.of( 2, 0x05 ) } );
	assert.equal( refused?.honorRanks?.sequence, 1 );
});

test("storage keeper always lists Honor Ranking (0x2E); clicking sends 0x72D0 and 0xB2D0 opens the window", () => {
	for ( const [level, member] of [ [ 1, false ], [ 59, false ], [ 80, true ] ] ) {
		const { f, commands, step } = storageTalk( level, member );
		try {
			const controls = step();
			assert.ok( controls.has( "npc-academy-honor" ), `listed at level ${level}, member ${member}` );
			f.ui.event( { kind: "activate", id: "npc-academy-honor" } );
			assert.deepEqual(
				commands.filter( c => c.command?.kind === "academy-honor-rank" ).map( c => c.command ),
				[ { kind: "academy-honor-rank", gid: 77 } ]
			);
		} finally {
			f.dispose();
		}
	}

	const rows = Array.from( { length: 11 }, ( _, i ) => ({
		ranking: i + 1,
		rank: (i % 5) + 1,
		name: `Hero_${i + 1}`,
		level: 70,
		maxLevel: 80,
		graduates: 10 + i,
		guild: i === 0 ? "Order" : ""
	}) );
	const { f, step } = storageTalk( 40, false, { honorRanks: { rows, sequence: 1 } } );
	try {
		let controls = step();
		assert.ok( controls.has( "academy-honor-close" ), "0xB2D0 opened CIFApprenticeShipHonorRanking" );
		assert.ok( f.hasText( "Hero_1" ) && f.hasText( "70(80)" ) && f.hasText( "Order" ) );
		assert.equal( controls.get( "academy-honor-prev" )?.disabled, true );
		assert.equal( controls.get( "academy-honor-next" )?.disabled, false );

		f.ui.event( { kind: "activate", id: "academy-honor-next" } );
		controls = step();
		assert.ok( f.hasText( "Hero_11" ) && f.hasText( "2" ) );
		assert.equal( controls.get( "academy-honor-prev" )?.disabled, false );
		assert.equal( controls.get( "academy-honor-next" )?.disabled, true );
	} finally {
		f.dispose();
	}
});

test("campRecord +0x34 and 0x3AC5 subtype 12 update camp evaluation badge (82AA10, 774D78, 5C6572)", () => {
	const base = defined( academyBootstrap( { academyMember: true } ) );
	const seeded = defined( academyPacket( base, {
		opcode: 0x3ac5,
		payload: encodeCampSeed( 10, 4, [ { id: 10, name: "Master", kind: 0, level: 80 } ] )
	} ) );
	assert.equal( seeded?.evaluation, 4 );
	assert.equal( academyHonorBadge( seeded?.evaluation ), "interface/ifcommon/com_honor_level_4.png" );

	const promoted = academyPacket( seeded, { opcode: 0x3ac5, payload: Uint8Array.of( 12, 5 ) } );
	assert.equal( promoted?.evaluation, 5 );
	assert.equal( academyHonorBadge( promoted?.evaluation ), "interface/ifcommon/com_honor_level_5.png" );
	assert.equal( academyHonorBadge( 0 ), "" );
});

test("CIFApprenticeShipSlot button 18 enforces strict > for leader and >= for self (5C6F20..5C7938)", () => {
	const leader = {
		id: 1,
		name: "Master",
		kind: 0,
		level: 80,
		entryLevel: 80,
		model: 1907,
		offline: false,
		honor: 0,
		location: ""
	};
	const sub60 = { ...leader, id: 2, name: "Sub60", kind: 1, level: 60 };
	const sub61 = { ...leader, id: 3, name: "Sub61", kind: 1, level: 61 };
	const app39 = { ...leader, id: 4, name: "App39", kind: 2, level: 39 };
	const app40 = { ...leader, id: 5, name: "App40", kind: 2, level: 40 };
	const app41 = { ...leader, id: 6, name: "App41", kind: 2, level: 41 };
	const members = [ leader, sub60, sub61, app39, app40, app41 ];
	const baseCamp = { member: true, page: 0, pages: 0, rows: [], request: null, result: null, members };

	// Leader view: strict `>` (level > 40 for kind 2, level > 60 for kind 1).
	const asLeader = { ...baseCamp, localMemberId: 1 };
	assert.deepEqual( academySlotAction( asLeader, app40 ), {
		kind: "ban",
		labelKey: "UIIT_CTL_BAN_PARTY",
		name: "App40"
	} );
	assert.deepEqual( academySlotAction( asLeader, app41 ), {
		kind: "compulsory-graduate",
		labelKey: "UIIT_CTL_TC_GRADUATION",
		name: "App41"
	} );
	assert.deepEqual( academySlotAction( asLeader, sub60 ), {
		kind: "ban",
		labelKey: "UIIT_CTL_BAN_PARTY",
		name: "Sub60"
	} );
	assert.deepEqual( academySlotAction( asLeader, sub61 ), {
		kind: "compulsory-graduate",
		labelKey: "UIIT_CTL_TC_GRADUATION",
		name: "Sub61"
	} );

	// Non-leader view: `>=` on own row; null on every other row.
	assert.equal( academySlotAction( { ...baseCamp, localMemberId: 5 }, sub60 ), null );
	assert.deepEqual( academySlotAction( { ...baseCamp, localMemberId: 4 }, app39 ), {
		kind: "leave",
		labelKey: "UIIT_CTL_LEAVE_PARTY",
		name: "App39"
	} );
	assert.deepEqual( academySlotAction( { ...baseCamp, localMemberId: 5 }, app40 ), {
		kind: "apprentice-graduate",
		labelKey: "UIIT_CTL_TC_GRADUATION",
		name: "App40"
	} );
	assert.deepEqual( academySlotAction( { ...baseCamp, localMemberId: 2 }, sub60 ), {
		kind: "submentor-graduate",
		labelKey: "UIIT_CTL_TC_GRADUATION",
		name: "Sub60"
	} );
});

test("Academy UI disband, kick, compulsory graduate, leave and Judge graduation flows", () => {
	const leader = {
		id: 1,
		name: "Master",
		kind: 0,
		level: 80,
		entryLevel: 80,
		model: 1907,
		offline: false,
		honor: 0,
		location: ""
	};
	const app40 = { ...leader, id: 2, name: "App40", kind: 2, level: 40, entryLevel: 10 };
	const app41 = { ...leader, id: 3, name: "App41", kind: 2, level: 41, entryLevel: 10 };
	const commands = [], f = uiFixture( command => commands.push( command ) );
	let now = 1000, seen = null;
	const renderCamp = localMemberId => {
		f.state.gameplay = {
			...f.state.gameplay,
			academy: {
				...empty,
				member: true,
				localMemberId,
				evaluation: 3,
				members: [ leader, app40, app41 ]
			}
		};
		for ( let i = 0; i < 20; i++ ) seen = f.ui.step( f.state, now += 50 ) ?? seen;
		return new Map( (seen?.controls ?? []).map( control => [ control.id, control ] ) );
	};
	try {
		renderCamp( 1 );
		f.ui.event( { kind: "activate", id: "open-window:Academy" } );
		let controls = renderCamp( 1 );
		assert.equal( controls.get( "academy-dismiss" )?.disabled, false );
		assert.ok( controls.has( "academy-slot-action:2" ) && controls.has( "academy-slot-action:3" ) );

		// Leader Dissolve -> confirm -> academy-disband.
		f.ui.event( { kind: "activate", id: "academy-dismiss" } );
		controls = renderCamp( 1 );
		assert.ok( controls.has( "academy-confirm-yes" ) );
		f.ui.event( { kind: "activate", id: "academy-confirm-yes" } );
		assert.deepEqual( commands.at( -1 )?.command, { kind: "academy-disband" } );

		// Leader Ban on level-40 apprentice -> confirm -> academy-kick.
		f.ui.event( { kind: "activate", id: "academy-slot-action:2" } );
		controls = renderCamp( 1 );
		assert.ok( controls.has( "academy-confirm-yes" ) && f.hasText( "App40" ) );
		f.ui.event( { kind: "activate", id: "academy-confirm-yes" } );
		assert.deepEqual( commands.at( -1 )?.command, { kind: "academy-kick", name: "App40" } );

		// Leader Compulsory Graduate on level-41 apprentice -> confirm -> academy-compulsory-graduate.
		f.ui.event( { kind: "activate", id: "academy-slot-action:3" } );
		controls = renderCamp( 1 );
		assert.ok( controls.has( "academy-confirm-yes" ) && f.hasText( "App41" ) );
		f.ui.event( { kind: "activate", id: "academy-confirm-yes" } );
		assert.deepEqual( commands.at( -1 )?.command, { kind: "academy-compulsory-graduate", name: "App41" } );

		// Apprentice at level 40 -> opens CIFApprenticeShipJudge with default satisfaction 3; selecting 4..0 sends 0x710A.
		controls = renderCamp( 2 );
		assert.equal( controls.get( "academy-dismiss" )?.disabled, true );
		assert.ok( controls.has( "academy-slot-action:2" ) && !controls.has( "academy-slot-action:3" ) );
		f.ui.event( { kind: "activate", id: "academy-slot-action:2" } );
		controls = renderCamp( 2 );
		assert.equal( controls.get( "academy-judge-select:3" )?.selected, true, "default is radio index 1 (wire 3)" );
		f.ui.event( { kind: "activate", id: "academy-judge-select:0" } );
		controls = renderCamp( 2 );
		assert.equal( controls.get( "academy-judge-select:0" )?.selected, true );
		f.ui.event( { kind: "activate", id: "academy-judge-confirm" } );
		assert.deepEqual( commands.at( -1 )?.command, { kind: "academy-graduate", satisfaction: 0 } );
	} finally {
		f.dispose();
	}
});

test("gameplay wires 0x77C5, 0x7785, 0x710A, 0x74D4, 0x736D, 0xB7C5 wait code and 0x3AC5 notices", () => {
	assert.deepEqual( academyDisbandRequest(), { opcode: 0x77c5, payload: new Uint8Array( 0 ) } );
	assert.deepEqual( academyLeaveRequest(), { opcode: 0x7785, payload: new Uint8Array( 0 ) } );
	assert.deepEqual( academyGraduateRequest( 4 ), { opcode: 0x710a, payload: Uint8Array.of( 4 ) } );
	assert.deepEqual( academyKickRequest( "App" ), {
		opcode: 0x74d4,
		payload: Uint8Array.of( 3, 0, 0x41, 0x70, 0x70 )
	} );
	assert.deepEqual( academyCompulsoryGraduateRequest( "App" ), {
		opcode: 0x736d,
		payload: Uint8Array.of( 3, 0, 0x41, 0x70, 0x70 )
	} );

	const sent = [];
	const g = createGameplay( frame => {
		sent.push( frame );
		return true;
	} );
	g.bootstrap( {
		gid: 1,
		name: "Master",
		model: 1907,
		regionId: 25000,
		x: 0,
		y: 0,
		z: 0,
		level: 80,
		gold: "500000",
		academyMember: true,
		inventory: []
	} );
	g.receive(
		{
			opcode: 0x3ac5,
			payload: encodeCampSeed( 1, 2, [
				{ id: 1, name: "Master", kind: 0, level: 80 },
				{ id: 2, name: "Pupil", kind: 2, level: 40 }
			] )
		},
		1
	);
	g.take();

	// 0xB7C5 [2, 0x1d, u32] 6-byte disband wait refusal.
	g.receive( { opcode: 0xb7c5, payload: Uint8Array.of( 2, 0x1d, 1, 2, 3, 4 ) }, 2 );
	let state = defined( g.take() );
	assert.equal( defined( state.notices ).at( -1 )?.key, "UIIT_MSG_TC_ERROR_NOT_DISORGANIZATION_7D" );

	// 0x3AC5 subtype 4 graduation of another member prints UIIT_STT_TC_GRADUATION_ANNOUNCE with name.
	g.receive( { opcode: 0x3ac5, payload: Uint8Array.of( 4, 2, 0, 0, 0, 0 ) }, 3 );
	state = defined( g.take() );
	assert.deepEqual( { ...defined( state.notices ).at( -1 ), sequence: undefined }, {
		key: "UIIT_STT_TC_GRADUATION_ANNOUNCE",
		value: 0,
		arguments: [ "Pupil" ],
		sequence: undefined
	} );

	// 0x3AC5 subtype 1 dissolution prints UIIT_STT_TC_DISSOLUTION_SUCCESS on child 23.
	g.receive( { opcode: 0x3ac5, payload: Uint8Array.of( 1 ) }, 4 );
	state = defined( g.take() );
	assert.equal( state.academy?.member, false );
	assert.equal( defined( state.notices ).at( -1 )?.key, "UIIT_STT_TC_DISSOLUTION_SUCCESS" );
	assert.equal( defined( state.notices ).at( -1 )?.notificationBanner, true );
});
