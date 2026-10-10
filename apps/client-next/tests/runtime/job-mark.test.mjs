/*
===========================================================================

job-mark.test.mjs - the overhead job mark and the suited player's own name

CICUser_RenderOverheadBoardPass (86B350) draws com_job_merchant, _thief or
_hunter by the active job class (+0x4F5), which 868D00 classifies from the
slot-8 suit when the character spawns: a peer at its spawn row (86AFB0), the
local player at world entry (869160), where 8675F0 also names the player's
own board with the job alias.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
const { defaultGameOptions } = await import( "../../src/engine/foundation/gameplay/game-options.ts" );
const { jobMark, overheadBoardVisible } = await import( "../../src/engine/foundation/ui/name-visibility.ts" );
const scenarios = JSON.parse( readFileSync(
	new URL( "../../../server/internal/game/world/simulation/testdata/peer_spawn_row_fixture.json", import.meta.url ),
	"utf8"
) ).scenarios;
const LOCAL_GID = 7, SUIT = 900, ALIAS = "ty4_goods";
// A thief suit (3/1/7/2): equipment band, group 7 (slot 8), job 2 on top.
const THIEF_SUIT_FLAGS = 0x3ac | 2 << 11;

/*
================
world

Enters the world, optionally wearing the thief suit, with every pinned peer
row's items catalogued.
================
*/
function world( { suited } ) {
	const owner = createEntities();
	const items = scenarios.flatMap( row => row.equipment ?? [] ).map( row => ({
		refObjId: row.refObjId,
		typeFlags: row.typeFlags
	}) );
	owner.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ {
			refObjId: scenarios[0].modelRefObjId,
			kind: "player",
			tidWord: scenarios[0].modelTidWord
		} ],
		refItemSnapshot: [ ...items, { refObjId: SUIT, typeFlags: THIEF_SUIT_FLAGS } ],
		character: { name: "Trader" },
		equipItems: suited ? [ { slot: 8, refObjId: SUIT, body: [] } ] : [],
		localPlayerEntry: {
			modelRef: scenarios[0].modelRefObjId,
			jobType: 2,
			jobAlias: ALIAS,
			startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 }
		}
	} );
	const payload = Buffer.alloc( 8 );
	payload.writeUInt32LE( LOCAL_GID );
	owner.receive( { opcode: 0x32a6, payload } );
	owner.ack( defined( owner.take() ).sequence );
	return owner;
}

test("a player entering in a job suit shows the alias and the job mark on its own board", () => {
	const owner = world( { suited: true } );
	const local = defined( owner.read( LOCAL_GID ) );
	assert.equal( local.activeJob, 2 );
	assert.equal( local.boardName, ALIAS );
	assert.equal( local.name, "Trader", "the character name itself is unchanged" );
	assert.equal( jobMark( local, defaultGameOptions() ), "com_job_thief" );
});

test("a joined player out of suit keeps the real name and shows no mark", () => {
	const owner = world( { suited: false } );
	const local = defined( owner.read( LOCAL_GID ) );
	assert.equal( local.activeJob, 4 );
	assert.equal( local.boardName, undefined );
	assert.equal( jobMark( local, defaultGameOptions() ), null );
});

test("a peer's mark comes from the suit in its spawn row", () => {
	const owner = world( { suited: false } );
	for ( const name of [ "job-suit-peer", "worn-set-peer" ] ) {
		const peer = defined( scenarios.find( row => row.name === name ) );
		owner.receive( { opcode: 0x30d7, payload: Buffer.from( peer.payloadHex, "hex" ) } );
		const spawned = defined( owner.read( peer.gid ) ), suited = name === "job-suit-peer";
		assert.equal( spawned.activeJob, suited ? 2 : 4 );
		assert.equal( spawned.name, peer.characterName );
		assert.equal( jobMark( spawned, defaultGameOptions() ), suited ? "com_job_thief" : null );
	}
});

test("the mark follows the icon option, and a far suited player shows its name with it", () => {
	const options = defaultGameOptions(),
		local = {
			gid: 1,
			kind: "local-player",
			name: "Me",
			regionId: 257,
			x: 0,
			y: 0,
			z: 0,
			heading: 0,
			refObjId: 1907
		},
		far = { ...local, gid: 2, kind: "player", name: "Hunter", x: 1000, activeJob: 3 };
	assert.equal( jobMark( far, options ), "com_job_hunter" );
	assert.equal( jobMark( { ...far, activeJob: 1 }, options ), "com_job_merchant" );
	assert.equal( jobMark( far, { ...options, ownName: false } ), null );
	assert.equal( jobMark( { ...far, kind: "npc" }, options ), null );
	assert.equal( overheadBoardVisible( far, local, false, options, undefined, false ), true );
	assert.equal( overheadBoardVisible( { ...far, activeJob: 4 }, local, false, options, undefined, false ), false );
});
