/*
===========================================================================

spirit-skills.test.mjs - a worn Devil's Spirit's lent skills (#4384)

The server names the skills a worn, awake Spirit lends in the bootstrap
(spiritSkills) and in the port-only 0x3FC9 frame; the client casts them
as learned and shows them in the equipment window's strip.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { decodeSpiritSkills, spiritSkillList, SPIRIT_SKILLS_OPCODE } = await import(
	"../../src/engine/foundation/gameplay/spirit-skills.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const SPIRIT_SKILL = 31555;
const local = {
	gid: 7,
	refObjId: 1907,
	kind: "local-player",
	name: "wearer",
	regionId: 257,
	x: 100,
	y: 0,
	z: 100,
	heading: 0
};

/*
================
frame
================
*/
function frame( ...ids ) {
	const p = Buffer.alloc( 1 + 4 * ids.length );
	p[0] = ids.length;
	ids.forEach( ( id, i ) => p.writeUInt32LE( id, 1 + 4 * i ) );
	return { opcode: SPIRIT_SKILLS_OPCODE, payload: p };
}

/*
================
skillRef
================
*/
function skillRef( id ) {
	return {
		id,
		group: id,
		level: 1,
		status: false,
		effectRider: false,
		ui: {
			name: `SKILL_${id}`,
			trainable: false,
			spCost: 0,
			targetRequired: false,
			admit: { weaponKinds: [ 255, 255 ] },
			cooldownMs: 0,
			masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
			prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
		}
	};
}

test("the Spirit skill frame decodes strictly", () => {
	assert.deepEqual( decodeSpiritSkills( frame( SPIRIT_SKILL, 31560 ).payload ), [ SPIRIT_SKILL, 31560 ] );
	assert.deepEqual( decodeSpiritSkills( frame().payload ), [] );
	assert.throws( () => decodeSpiritSkills( frame( SPIRIT_SKILL ).payload.subarray( 0, 4 ) ) );
	assert.throws( () => decodeSpiritSkills( frame( 1, 2, 3, 4, 5 ).payload ) );
	assert.throws( () => decodeSpiritSkills( frame( 7, 7 ).payload ) );
	assert.deepEqual( spiritSkillList( undefined ), [] );
	assert.throws( () => spiritSkillList( [ 0 ] ) );
});

test("a worn Spirit's lent skill casts and binds as learned until the server takes it back", () => {
	const sent = [], game = createGameplay( f => sent.push( f ) );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: { skills: [] },
		refSkillSnapshot: [ skillRef( SPIRIT_SKILL ) ],
		spiritSkills: [ SPIRIT_SKILL ]
	} );
	game.seed( local );
	assert.deepEqual( game.take()?.spiritSkills, [ SPIRIT_SKILL ] );
	game.command( { kind: "quickslot-bind", slot: 0, skillId: SPIRIT_SKILL }, 0, undefined, local );
	// The Spirit comes off: the strip empties and the skill is unlearned again.
	assert.equal( game.receive( frame(), 10 ), true );
	assert.deepEqual( game.take()?.spiritSkills, [] );
	assert.throws(
		() => game.command( { kind: "quickslot-bind", slot: 1, skillId: SPIRIT_SKILL }, 20, undefined, local ),
		/not learned/
	);
	assert.throws(
		() => game.command( { kind: "skill", skillId: SPIRIT_SKILL }, 20, undefined, local ),
		/not learned/
	);
	// Worn again (0x3FC9 with the list), the skill binds again.
	assert.equal( game.receive( frame( SPIRIT_SKILL ), 30 ), true );
	game.command( { kind: "quickslot-bind", slot: 1, skillId: SPIRIT_SKILL }, 40, undefined, local );
});

test("a worn, awake Spirit's socket loops the later clients' sixteen-cell edge", async () => {
	const { spiritActiveOverlay } = await import( "../../src/engine/foundation/ui/item-slot-effects.ts" );
	const rect = [ 10, 20, 32, 32 ];
	const first = spiritActiveOverlay( rect, 0, 0 ), next = spiritActiveOverlay( rect, 0, 50 );
	assert.equal( first.path, "/assets/images/Media_extracted/icon/item/etc/icon_edge_nasrun.png" );
	assert.deepEqual( first.rect, rect );
	assert.equal( first.uv[2], 1 / 16 );
	assert.equal( next.uv[0] - first.uv[0], 1 / 16, "one cell per 50 ms" );
	assert.deepEqual( spiritActiveOverlay( rect, 0, 16 * 50 ).uv, first.uv, "sixteen cells, then it loops" );
});

test("a Devil's Spirit's tooltip names its awakening: dormant, the time left, or over", async () => {
	const { itemTooltip } = await import( "../../src/engine/foundation/ui/item-tooltip.ts" );
	const english = {
			UIIT_STT_NASRUN_NOT_AWAKE: "This devil's spirit is not awaken.",
			UIIT_STT_NASRUN_AWAKE_TIME: "Awaken period.",
			UIIT_STT_NASRUN_AWAKE_TIMEOVER: "Awaken period is over.",
			PARAM_DAY: "day(s)",
			PARAM_HOUR: "hour(s)",
			PARAM_MINUTE: "min(s)"
		},
		text = s => english[s] ?? s;
	// 3/1/14/1: the Spirit; its clock rides the durability field (#4381).
	const spiritFlags = (3 << 2) | (1 << 5) | (14 << 7) | (1 << 11);
	const item = {
		slot: 13,
		refObjId: 51681,
		name: "Devil's Spirit",
		typeFlags: spiritFlags,
		quantity: 1,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		tooltip: { fields: {} }
	};
	const lines = durability => itemTooltip( { ...item, durability }, { masteries: [] }, text ).map( r => r.value );
	assert.ok( lines( 0 ).includes( "This devil's spirit is not awaken." ) );
	const awake = lines( Math.floor( Date.now() / 1000 ) + 27 * 86400 + 5 * 3600 + 30 * 60 );
	assert.ok( awake.includes( "Awaken period." ) );
	assert.ok( awake.some( line => /^27 day\(s\) 5 hour\(s\) (29|30) min\(s\)$/.test( line ) ), awake.join( "|" ) );
	assert.ok( lines( Math.floor( Date.now() / 1000 ) - 60 ).includes( "Awaken period is over." ) );
});
