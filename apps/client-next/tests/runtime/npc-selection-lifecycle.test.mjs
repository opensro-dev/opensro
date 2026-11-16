/*
===========================================================================

npc-selection-lifecycle.test.mjs - selection and conversation lifetime

Drives the shipped gameplay dispatcher rather than testing only the target
or dialogue owner in isolation. NPCs and teleport buildings share this
boundary, including service windows that retain an authoritative selection.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { talkPaneOpens } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/targeting/targeting.ts"
);

const SELECT = 0x745a;
const SELECT_RESULT = 0xb45a;
const RELEASE_RESULT = 0xb4b3;
const RESTORATION_OPEN = 0x3230;
const DIALOGUE = 0x3773;
const NPC_GID = 7;
const MOVEMENT_REQUEST = 9;
const MOVEMENT_RESULT = 10;
const REGION_SIZE = 1920;
const TILE_SIZE = 20;
const TILES_PER_AXIS = REGION_SIZE / TILE_SIZE;

/*
================
fixture
================
*/
function fixture( kind = "npc", fortress = false ) {
	/** @type {import("../../src/engine/contracts/network.ts").WireFrame[]} */
	const sent = [];
	const transport = { fail: false };
	const game = createGameplay( frame => {
		if ( transport.fail ) throw Error( "transport unavailable" );
		sent.push( frame );
	} );
	const entity = {
		refObjId: 1,
		gid: NPC_GID,
		name: "NPC fixture",
		kind,
		regionId: 1,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		...(kind === "teleport" ? { teleport: { radius: 10, height: 25, fortressId: fortress ? 1 : 0 } } : {})
	};
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( { ...entity, gid: 1, kind: "player" } );
	/*
	================
	grant
	================
	*/
	function grant( gid = NPC_GID, capabilities = kind === "teleport" ? 0xc0 : 3 ) {
		const payload = Buffer.alloc( kind === "teleport" && !fortress ? 9 : 11 );
		payload[0] = 1;
		payload.writeUInt32LE( gid, 1 );
		payload.writeUInt32LE( capabilities, kind === "teleport" ? 5 : 6 );
		if ( fortress ) payload.writeInt16LE( 17, 9 );
		game.receive( { opcode: SELECT_RESULT, payload }, 1 );
	}
	/*
	================
	vitalsGrant

	A monster's or COS's grant: [u8 vitals=1][u32 hp][u32 word].
	================
	*/
	function vitalsGrant( gid = NPC_GID, word = 0 ) {
		const payload = Buffer.alloc( 14 );
		payload[0] = 1;
		payload.writeUInt32LE( gid, 1 );
		payload[5] = 1;
		payload.writeUInt32LE( 100, 6 );
		payload.writeUInt32LE( word, 10 );
		game.receive( { opcode: SELECT_RESULT, payload }, 1 );
	}
	/*
	================
	select
	================
	*/
	function select( now = 0 ) {
		return game.command( { kind: "select", gid: NPC_GID }, now, entity );
	}
	/*
	================
	snapshot

	A command under test must publish its gameplay and conversation state.
	================
	*/
	function snapshot() {
		const state = defined( game.take(), "gameplay snapshot" );
		return { ...state, npcConversation: defined( state.npcConversation, "NPC conversation" ) };
	}
	return { game, sent, transport, entity, grant, vitalsGrant, select, snapshot };
}

test("a zero-capability NPC opens an empty talk pane, as native", () => {
	// CPSMission_OnNpcSelectResponse0xB45A shows a CICNPC's pane on every
	// grant and builds its rows from the word (5D9100): no bits, no rows.
	const f = fixture();
	try {
		f.select();
		f.grant( NPC_GID, 0 );
		const state = f.snapshot();
		assert.equal( state.npcConversation.phase, "menu" );
		assert.equal( state.targetCapabilities, undefined, "The pane carries no service rows" );
		assert.equal( f.select( 2 ), null, "The visible pane suppresses a repeat selection" );
	} finally {
		f.game.dispose();
	}
});

test("a teleport gate opens its talk pane whatever its grant word", () => {
	// The CITeleportGate branch (0x7650b8) shows the pane with the gate
	// caption as its greeting; the word only decides the rows.
	const f = fixture( "teleport" );
	try {
		f.select();
		f.grant( NPC_GID, 0 );
		assert.equal( f.snapshot().npcConversation.phase, "menu" );
	} finally {
		f.game.dispose();
	}
});

test("a monster opens the talk pane only for the talk bit without 0x10", () => {
	// CICMonster and CICCos are the only gated kinds:
	// (bits & 0x10) == 0 && (bits & 2) != 0.
	for (
		const { word, phase } of [
			{ word: 0, phase: "closed" },
			{ word: 2, phase: "menu" },
			{ word: 0x12, phase: "closed" },
			{ word: 0x10, phase: "closed" }
		]
	) {
		const f = fixture( "monster" );
		try {
			f.select();
			f.vitalsGrant( NPC_GID, word );
			assert.equal( f.snapshot().npcConversation.phase, phase, `word ${word}` );
		} finally {
			f.game.dispose();
		}
	}
});

test("an unspawned target's talk pane follows the grant's shape", () => {
	const npc = Buffer.alloc( 11 );
	npc[0] = 1;
	const vitals = Buffer.alloc( 14 );
	vitals[0] = 1;
	vitals[5] = 1;
	assert.equal( talkPaneOpens( undefined, false, npc ), true, "The NPC arm always opens" );
	assert.equal( talkPaneOpens( undefined, false, vitals ), false, "The vitals arm takes the monster rule" );
	vitals.writeUInt32LE( 2, 10 );
	assert.equal( talkPaneOpens( undefined, false, vitals ), true );
	assert.equal( talkPaneOpens( "cos", false, vitals ), true, "A COS shares the monster rule" );
	assert.equal( talkPaneOpens( "player", false, npc ), false, "A player never opens the pane" );
});

test("the Bandit Den gate's return-point grant alone opens the talk menu", () => {
	// The den gate has no link out, so its word carries 0x100 (row 0x10,
	// "Teleport to return point") and no teleport bit.
	const f = fixture( "teleport" );
	try {
		f.select();
		f.grant( NPC_GID, 0x100 );
		assert.equal( f.snapshot().npcConversation.phase, "menu" );
	} finally {
		f.game.dispose();
	}
});

/*
================
navigation

An empty outdoor cell lets the dispatcher exercise its real movement owner.
================
*/
function navigation() {
	const tileCount = TILES_PER_AXIS * TILES_PER_AXIS;
	return {
		navmesh: {
			regionSize: REGION_SIZE,
			tileSize: TILE_SIZE,
			tilesPerAxis: TILES_PER_AXIS,
			regions: [ {
				dx: 0,
				dz: 0,
				blockedTiles: Buffer.alloc( tileCount ).toString( "base64" ),
				tileCellIds: Buffer.alloc( tileCount * Uint32Array.BYTES_PER_ELEMENT ).toString( "base64" ),
				cells: { count: 1 },
				objects: []
			} ]
		}
	};
}

/*
================
arrive

Commit the server receipt for an actual dispatched movement request.
================
*/
function arrive( f, request, x, now ) {
	assert.equal( request.opcode, MOVEMENT_REQUEST );
	const id = new DataView( request.payload.buffer, request.payload.byteOffset ).getUint32( 1, true );
	f.game.receive( {
		opcode: MOVEMENT_RESULT,
		payload: Buffer.from( JSON.stringify( {
			v: 1,
			id,
			gid: 1,
			accepted: true,
			serverTimeMs: now,
			world: { spawn: { regionId: 1, x, y: 0, z: 0, angle: 0 } }
		} ) )
	}, now );
}

test("gate approach reacquires a retained gate selection after the conversation closes", () => {
	for ( const fortress of [ false, true ] ) {
		const f = fixture( "teleport", fortress );
		const local = { ...f.entity, gid: 1, kind: "player" };
		try {
			f.game.command( { kind: "navigation", regionId: 1, bundle: navigation() }, 0, undefined );
			f.select();
			f.grant();
			const away = f.game.command(
				{ kind: "move", destination: { regionId: 1, x: 1200, y: 0, z: 0, angle: 0 } },
				2,
				undefined
			);
			arrive( f, defined( away ), 1200, 3 );
			const approach = f.select( 4 );
			assert.equal( f.snapshot().npcConversation.phase, "closed" );
			arrive( f, defined( approach ), 640, 5 );
			f.game.step( 6, local );
			assert.equal( f.sent.at( -1 )?.opcode, SELECT, "Arrival must request a fresh grant for the retained gate" );
			f.grant();
			assert.equal( f.snapshot().npcConversation.phase, "menu" );
		} finally {
			f.game.dispose();
		}
	}
});

test("coalesced selection retains the acknowledged NPC interaction lock", () => {
	const f = fixture();
	const text = Buffer.from( "SN_TALK_COMMON_END" );
	const notice = Buffer.alloc( Uint16Array.BYTES_PER_ELEMENT + text.length );
	notice.writeUInt16LE( text.length );
	text.copy( notice, Uint16Array.BYTES_PER_ELEMENT );
	try {
		f.select();
		f.grant();
		f.game.receive( { opcode: 0xb338, payload: Uint8Array.of( 1, 2, 0, 0, 0 ) }, 2 );
		f.game.receive( { opcode: 0x36bf, payload: notice }, 3 );
		const before = f.snapshot().notices;
		f.select( 4 );
		f.game.receive( { opcode: 0x36bf, payload: notice }, 5 );
		assert.deepEqual(
			f.snapshot().notices,
			before,
			"Repeated quest notices stay suppressed while the NPC owns the lock"
		);
	} finally {
		f.game.dispose();
	}
});

test("repeated granted NPC and gate selection preserves the menu without another transaction", () => {
	for (
		const { kind, fortress } of [ { kind: "npc", fortress: false }, { kind: "teleport", fortress: false }, {
			kind: "teleport",
			fortress: true
		} ]
	) {
		const f = fixture( kind, fortress );
		try {
			f.select();
			f.grant();
			const before = f.snapshot();
			assert.equal( before.npcConversation.phase, "menu" );
			assert.equal( f.select( 2 ), null );
			const after = f.snapshot();
			assert.deepEqual( after.npcConversation, before.npcConversation, kind );
			assert.equal( after.target, NPC_GID );
			assert.equal( after.targetPending, 0 );
			assert.equal( after.targetTaxRate, before.targetTaxRate );
			assert.equal( f.sent.length, 1 );
		} finally {
			f.game.dispose();
		}
	}
});

test("repeated selection preserves waiting, uncertain and ready dialogue transactions", () => {
	for ( const phase of [ "waiting", "uncertain", "ready" ] ) {
		const f = fixture();
		try {
			f.select();
			f.grant();
			f.game.command( { kind: "npc-talk" }, 2, undefined );
			if ( phase === "uncertain" ) f.game.step( 10002 );
			if ( phase === "ready" ) f.game.receive( { opcode: DIALOGUE, payload: Buffer.from( [ 1, 0, 0 ] ) }, 3 );
			const before = f.snapshot().npcConversation;
			assert.equal( before.phase, phase );
			const count = f.sent.length;
			f.select( 10003 );
			assert.deepEqual( f.snapshot().npcConversation, before, phase );
			assert.equal( f.sent.length, count );
		} finally {
			f.game.dispose();
		}
	}
});

test("a service window can close the talk pane while retaining a target that must be selectable again", () => {
	const f = fixture();
	try {
		f.select();
		f.grant();
		f.game.command( { kind: "npc-talk" }, 2, undefined );
		f.game.receive( { opcode: RESTORATION_OPEN, payload: new Uint8Array() }, 3 );
		const closed = f.snapshot();
		assert.equal( closed.npcConversation.phase, "closed" );
		assert.equal( closed.target, NPC_GID );
		assert.equal( f.select( 4 )?.opcode, SELECT, "Closed talk window requires a fresh native grant" );
		assert.equal( f.select( 5 ), null, "Pending reselect still coalesces" );
		f.grant();
		assert.equal( f.snapshot().npcConversation.phase, "menu" );
	} finally {
		f.game.dispose();
	}
});

test("an open NPC function blocks world-click movement until the window closes", () => {
	// 698740 opens with CGInterface_IsInteractionBlocked (67D090): the shop's
	// interaction lock refuses the click, so the player cannot walk away from
	// an open shop and keep trading. Closing the window releases the lock.
	const f = fixture();
	const move = now =>
		f.game.command( { kind: "move", destination: { regionId: 1, x: 600, y: 0, z: 0, angle: 0 } }, now, undefined );
	try {
		f.game.command( { kind: "navigation", regionId: 1, bundle: navigation() }, 0, undefined );
		f.select();
		f.grant();
		f.game.receive( { opcode: 0xb338, payload: Uint8Array.of( 1, 1, 0, 0, 0 ) }, 2 );
		const before = f.sent.length;
		assert.equal( move( 3 ), null, "a click is ignored while the shop holds the lock" );
		assert.equal( f.sent.length, before );
		f.game.command( { kind: "npc-close" }, 4, undefined );
		f.game.receive( { opcode: RELEASE_RESULT, payload: Uint8Array.of( 1 ) }, 5 );
		assert.equal( move( 6 )?.opcode, MOVEMENT_REQUEST, "the closed window lets the player walk" );
	} finally {
		f.game.dispose();
	}
});

test("an open alchemy window blocks world-click movement until it closes", () => {
	// 67D090 refuses the click while the alchemy box exists (67D0A3), not
	// only under the NPC interaction lock.
	const f = fixture();
	const move = now =>
		f.game.command( { kind: "move", destination: { regionId: 1, x: 600, y: 0, z: 0, angle: 0 } }, now, undefined );
	try {
		f.game.command( { kind: "navigation", regionId: 1, bundle: navigation() }, 0, undefined );
		f.game.command( { kind: "alchemy-open" }, 1, undefined );
		const before = f.sent.length;
		assert.equal( move( 2 ), null, "a click is ignored while the alchemy box is open" );
		assert.equal( f.sent.length, before );
		f.game.command( { kind: "alchemy-close" }, 3, undefined );
		assert.equal( move( 4 )?.opcode, MOVEMENT_REQUEST, "the closed window lets the player walk" );
	} finally {
		f.game.dispose();
	}
});

test("a stall or an open exchange pins the player: no walk and no skill until it closes", () => {
	// #3433: the server refuses both (action PlayerInteractionLocked), so the
	// client sends neither and predicts no walk or teleport.
	const STALL_CREATE_RESULT = 0xb049, STALL_CLOSE_RESULT = 0xb42c;
	const EXCHANGE_OPENED = 0x3219, EXCHANGE_CANCEL_RESULT = 0xb2db;
	const cases = [
		{
			name: "stall",
			open: { opcode: STALL_CREATE_RESULT, payload: Uint8Array.of( 1 ) },
			close: { opcode: STALL_CLOSE_RESULT, payload: Uint8Array.of( 1 ) }
		},
		{
			name: "exchange",
			open: { opcode: EXCHANGE_OPENED, payload: Uint8Array.of( 9, 0, 0, 0 ) },
			close: { opcode: EXCHANGE_CANCEL_RESULT, payload: Uint8Array.of( 1 ) }
		}
	];
	for ( const { name, open, close } of cases ) {
		const f = fixture();
		const move = now =>
			f.game.command(
				{ kind: "move", destination: { regionId: 1, x: 600, y: 0, z: 0, angle: 0 } },
				now,
				undefined
			);
		const press = now => f.game.command( { kind: "skill", skillId: 1 }, now, undefined );
		try {
			f.game.command( { kind: "navigation", regionId: 1, bundle: navigation() }, 0, undefined );
			f.game.receive( open, 1 );
			const before = f.sent.length;
			assert.equal( move( 2 ), null, `${name}: a click is ignored` );
			assert.equal( press( 3 ), null, `${name}: a skill press is ignored` );
			assert.equal( f.sent.length, before, `${name}: nothing is sent` );
			f.game.receive( close, 4 );
			assert.equal( move( 5 )?.opcode, MOVEMENT_REQUEST, `${name}: the closed window lets the player walk` );
			// The press now reaches the skill path, which refuses the unlearned row.
			assert.throws( () => press( 6 ), /not learned/, `${name}: the closed window lets the press through` );
		} finally {
			f.game.dispose();
		}
	}
});

test("close and release barrier retire a conversation before the same NPC opens again", () => {
	// The NPC window's close sends the release (693790). The target window's
	// X sends nothing (582020): it retires the conversation and no barrier.
	{
		const f = fixture();
		try {
			f.select();
			f.grant();
			const before = f.sent.length;
			f.game.command( { kind: "release-target" }, 2, undefined );
			assert.equal( f.snapshot().npcConversation.phase, "closed" );
			assert.equal( f.sent.length, before, "the target window's X sends no release" );
		} finally {
			f.game.dispose();
		}
	}
	{
		const f = fixture();
		try {
			f.select();
			f.grant();
			f.game.command( { kind: "npc-close" }, 2, undefined );
			assert.equal( f.snapshot().npcConversation.phase, "closed" );
			assert.throws( () => f.select( 3 ), /pending/ );
			f.grant();
			assert.equal( f.snapshot().npcConversation.phase, "closed", "Late select cannot cross release barrier" );
			f.game.receive( { opcode: RELEASE_RESULT, payload: Uint8Array.of( 1 ) }, 4 );
			assert.equal( f.select( 5 )?.opcode, SELECT );
			f.grant();
			assert.equal( f.snapshot().npcConversation.phase, "menu" );
		} finally {
			f.game.dispose();
		}
	}
});

test("failed target switch preserves the existing menu and a successful switch retires it", () => {
	const f = fixture();
	try {
		f.select();
		f.grant();
		const before = f.snapshot().npcConversation;
		const next = { ...f.entity, gid: 8 };
		f.transport.fail = true;
		assert.throws( () => f.game.command( { kind: "select", gid: 8 }, 2, next ), /transport unavailable/ );
		assert.deepEqual( f.snapshot().npcConversation, before );
		f.transport.fail = false;
		f.game.command( { kind: "select", gid: 8 }, 3, next );
		assert.equal( f.snapshot().npcConversation.phase, "closed" );
		f.grant( 8 );
		const conversation = f.snapshot().npcConversation;
		assert.equal( conversation.phase, "menu" );
		assert.equal( conversation.gid, 8 );
	} finally {
		f.game.dispose();
	}
});
