/*
===========================================================================

name-color.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { refreshNameColor: color, equipmentHoldType, nameColorRgba } = await load(
	"src/engine/foundation/gameplay/name-color.ts"
);
const { createEntities } = await load( "src/engine/runtime/simulation/worker/session/world/entities/entities.ts" );
const { decodeCharacterSpawn } = await load( "src/engine/foundation/gameplay/character-spawn.ts" );
const { createCombat } = await load( "src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts" );
const local = { gid: 1, kind: "local-player", name: "Local", arenaTeam: 255 },
	peer = { gid: 2, kind: "player", name: "Peer", arenaTeam: 255 };
const social = { localName: "Local", self: 1, leader: 0, members: [], guild: null };
const fortress = {
	worldId: 1,
	worlds: [ { id: 1, code: "FORT" } ],
	fortresses: [ { id: 9, code: "FORT" } ],
	wars: [],
	registered: [],
	listId: 0
};
const context = { local, social, fortress, capeTeam: () => 1 };

test("accepted local casts latch a user name for twenty seconds, without duplicate rearming", () => {
	const rows = new Map( [ [ 1, local ], [ 2, peer ], [ 3, { ...peer, gid: 3, name: "Other" } ] ] ),
		combat = createCombat( gid => rows.get( gid ) );
	combat.cooldownReferences( 1, [] );
	combat.references( [ { id: 10, status: false, effectRider: false, nameHit: true }, {
		id: 11,
		status: false,
		effectRider: false,
		nameHit: false
	} ] );
	function cast( token, caster, target, skill = 10 ) {
		const p = Buffer.alloc( 19 );
		p[0] = 1;
		p.writeUInt32LE( skill, 2 );
		p.writeUInt32LE( caster, 6 );
		p.writeUInt32LE( token, 10 );
		p.writeUInt32LE( target, 14 );
		return p;
	}
	combat.receive( 0xb245, cast( 1, 1, 2 ), 100 );
	assert.equal( combat.nameAttack( 20099 ), "Peer" );
	combat.receive( 0xb245, cast( 1, 1, 2 ), 1000 );
	assert.equal( combat.nameAttack( 20100 ), undefined );
	combat.receive( 0xb245, cast( 2, 2, 3 ), 21000 );
	assert.equal( combat.nameAttack( 21000 ), undefined );
	combat.receive( 0xb245, cast( 3, 1, 2 ), 22000 );
	combat.receive( 0xb245, cast( 4, 1, 3, 11 ), 23000 );
	assert.equal( combat.nameAttack( 23000 ), "Peer" );
	combat.receive( 0xb245, cast( 5, 1, 3 ), 24000 );
	assert.equal( combat.nameAttack( 24000 ), "Other" );
	assert.throws( () => combat.receive( 0xb245, cast( 6, 1, 2 ).subarray( 0, 18 ), 25000 ), /Truncated/ );
	assert.equal( combat.nameAttack( 25000 ), "Other" );
	combat.clear();
	assert.equal( combat.nameAttack( 25000 ), undefined );
});
test("native early returns dominate party, PvP and fortress colors", () => {
	const c = {
		...context,
		local: { ...local, arenaTeam: 0 },
		social: { ...social, leader: 1, members: [ { name: "Peer" } ] }
	};
	assert.equal( color( { ...peer, name: "[GM]Peer", pvpState: 2, arenaTeam: 1 }, c ), 0xffffd87a );
	assert.equal( color( { ...peer, pvpState: 2, arenaTeam: 0 }, c ), 0xffffffff );
	assert.equal( color( { ...peer, pvpState: 0, arenaTeam: 1 }, c ), 0xffff6262 );
	assert.equal( color( peer, { ...context, social: c.social } ), 0xff62f5b1 );
	assert.equal( color( { ...peer, pvpState: 1 }, { ...context, social: c.social } ), 0xffff58fe );
	assert.equal( color( { ...peer, pvpState: 2 }, { ...context, social: c.social } ), 0xffff6262 );
	assert.equal( color( { ...peer, kind: "cos", pvpState: 2 }, context ), 0xffffff55 );
	assert.equal( color( { ...peer, kind: "monster", pvpState: 3, nameColor: 0xff123456 }, context ), 0xff123456 );
	assert.deepEqual( nameColorRgba( 0xffffd87a ), [ 1, 216 / 255, 122 / 255, 1 ] );
});
test("local player matches the roster by board name in job mode (8675F0)", () => {
	// Entering in job mode rewrites the local player's native name field
	// with the job alias, so the roster test (858810/8229B0) matches the
	// roster's alias row: own name turns party green while suited (#3214).
	const suited = { ...local, name: "RealHero", boardName: "Alias77" };
	assert.equal(
		color( suited, {
			...context,
			social: { ...social, leader: 1, members: [ { name: "Alias77" }, { name: "Peer" } ] }
		} ),
		0xff62f5b1
	);
	// The roster never carries a suited member's real name, so a roster
	// holding only it does not match: the reported white-name shape.
	assert.equal(
		color( suited, {
			...context,
			social: { ...social, leader: 1, members: [ { name: "RealHero" } ] }
		} ),
		0xffffffff
	);
	// Unsuited there is no board name and the real name matches as before.
	assert.equal(
		color( { ...local, name: "RealHero" }, {
			...context,
			social: { ...social, leader: 1, members: [ { name: "RealHero" } ] }
		} ),
		0xff62f5b1
	);
});
test("fortress registration matrix and party priority match 8567F0", () => {
	for (
		const [registered, allies, want] of [
			[ [ 10, 20 ], [], 0xffffffff ],
			[ [ 10 ], [], 0xffff6262 ],
			[ [ 20 ], [], 0xffff6262 ],
			[ [], [], 0xff1abaff ],
			[ [], [ { id: 20 } ], 0xffffffff ]
		]
	) {
		const c = {
			...context,
			social: { ...social, guild: { id: 10 }, alliances: allies },
			fortress: { ...fortress, wars: [ { id: 9, flags: 1 } ], registered }
		};
		assert.equal( color( { ...peer, guildId: 20, pvpState: 2 }, c ), want );
		assert.equal( color( { ...peer, kind: "npc", tidWord: 0x246, guildId: 20 }, c ), want );
		assert.equal(
			color( { ...peer, guildId: 20 }, {
				...c,
				social: { ...c.social, leader: 1, members: [ { name: "Peer" } ] }
			} ),
			0xff62f5b1
		);
	}
});
test("job and cape types are live; equal cape team 5 remains hostile", () => {
	const item = t => ({ slot: 8, refObjId: t, typeFlags: 0x3ac | (t << 11) });
	for ( const t of [ 1, 2, 3, 5 ] ) assert.equal( equipmentHoldType( item( t ).typeFlags ), t === 5 ? 4 : t );
	assert.equal(
		color( { ...peer, holdType: 1 }, { ...context, local: { ...local, holdType: 2 }, localItem: item( 2 ) } ),
		0xffff6262
	);
	for ( const [a, b, want] of [ [ 1, 1, 0xffffffff ], [ 1, 2, 0xffff6262 ], [ 5, 5, 0xffff6262 ] ] ) {
		const target = { ...peer, equipment: [ { ...item( 5 ), refObjId: 50 } ] };
		const c = { ...context, localItem: item( 5 ), capeTeam: id => id === 50 ? b : a };
		assert.equal( color( target, c ), want );
	}
	assert.throws(
		() =>
			color( { ...peer, equipment: [ item( 5 ) ] }, {
				...context,
				localItem: item( 5 ),
				capeTeam: () => undefined
			} ),
		/cape team/
	);
});
function npcPacket( tail = [] ) {
	const out = Buffer.alloc( 57 + tail.length );
	let o = 0;
	const u8 = n => out.writeUInt8( n, o++ ),
		u16 = n => {
			out.writeUInt16LE( n, o );
			o += 2;
		},
		u32 = n => {
			out.writeUInt32LE( n, o );
			o += 4;
		},
		f = n => {
			out.writeFloatLE( n, o );
			o += 4;
		};
	u32( 10 );
	u32( 2 );
	u16( 1 );
	f( 0 );
	f( 0 );
	f( 0 );
	u16( 0 );
	u8( 0 );
	u8( 1 );
	u8( 0 );
	u16( 0 );
	u8( 1 );
	u8( 0 );
	u8( 0 );
	f( 1 );
	f( 2 );
	f( 100 );
	u8( 0 );
	u8( 0 );
	return Buffer.concat( [ out.subarray( 0, o ), Buffer.from( tail ) ] );
}

test("world timer preserves COS spawn color until the native fortress sweep", async () => {
	const { createWorldCore } = await load( "src/engine/runtime/simulation/worker/session/world/core.ts" );
	const core = createWorldCore( () => {} ),
		drain = () => {
			const b = core.take();
			if ( b ) core.ack( b.sequence );
			return b?.events ?? [];
		};
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: 10, kind: "cos", tidWord: 0x21c6 } ],
		localPlayerEntry: {
			modelRef: 1933,
			fortressWorld: 1,
			startProfile: { regionId: 1, x: 0, y: 0, z: 0, angle: 0 }
		},
		gameWorldData: [ { gameWorldId: 1, warName: "FORT" } ],
		siegeFortressData: [ { fortressId: 9, codeName: "FORT" } ]
	} );
	drain();
	core.receive( { opcode: 0x32a6, payload: Buffer.from( [ 1, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	drain();
	core.receive( { opcode: 0x30d7, payload: npcPacket( [ 0, 0, 0, 0, 4, 1, 0, 0, 0, 0 ] ) }, 1 );
	assert.equal( drain().find( e => e.kind === "spawn" ).entity.nameColor, undefined, "band 4 skips 858B30 at spawn" );
	const war = Buffer.alloc( 31 );
	war[1] = 1;
	war.writeUInt32LE( 9, 2 );
	war[26] = 1;
	core.receive( { opcode: 0x3887, payload: war }, 2 );
	drain();
	core.step( 2999 );
	assert.ok( !drain().some( e => e.kind === "state" && e.entity.nameColor === 0xffffff55 ) );
	core.step( 3000 );
	assert.equal( drain().find( e => e.kind === "state" && e.entity.gid === 2 ).entity.nameColor, 0xffffff55 );
	core.clear();
	drain();
	core.step( 9000 );
	assert.ok( !drain().some( e => e.kind === "state" ) );
	core.dispose();
});

test("party dissolution temporarily overrides even GM gold, then the player timer restores it", async () => {
	const { createWorldCore } = await load( "src/engine/runtime/simulation/worker/session/world/core.ts" );
	const core = createWorldCore( () => {} ),
		drain = () => {
			const b = core.take();
			if ( b ) core.ack( b.sequence );
			return b?.events ?? [];
		},
		name = "[GM]Local";
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		character: { name },
		refObjSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 1, x: 0, y: 0, z: 0, angle: 0 } }
	} );
	drain();
	core.receive( { opcode: 0x32a6, payload: Buffer.from( [ 1, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	assert.equal( drain().find( e => e.kind === "spawn" ).entity.nameColor, 0xffffd87a );
	const roster = Buffer.alloc( 18 + name.length );
	roster[0] = 3;
	roster.writeUInt32LE( 1, 1 );
	roster[6] = 1;
	roster[7] = 17;
	roster.writeUInt32LE( 1, 8 );
	roster.writeUInt16LE( name.length, 12 );
	roster.write( name, 14 );
	roster.writeUInt32LE( 1933, 14 + name.length );
	core.receive( { opcode: 0x35d6, payload: roster }, 1 );
	drain();
	core.receive( { opcode: 0x3e58, payload: Buffer.from( [ 1, 0 ] ) }, 2 );
	assert.equal( drain().find( e => e.kind === "state" ).entity.nameColor, 0xffffffff );
	core.step( 2999 );
	assert.ok( !drain().some( e => e.kind === "state" && e.entity.nameColor === 0xffffd87a ) );
	core.step( 3000 );
	assert.equal( drain().find( e => e.kind === "state" ).entity.nameColor, 0xffffd87a );
	core.dispose();
});
test("guard decoder consumes guild identity after the inherited NPC prefix", () => {
	const tail = Buffer.alloc( 9 );
	tail.writeUInt32LE( 20 );
	tail.writeUInt16LE( 3, 4 );
	tail.write( "ABC", 6 );
	const e = decodeCharacterSpawn( npcPacket( tail ), "npc", 0x246, false );
	assert.equal( e.guildId, 20 );
	assert.equal( e.guildName, "ABC" );
	assert.throws( () => decodeCharacterSpawn( npcPacket( tail.subarray( 0, 8 ) ), "npc", 0x246, false ), /Truncated/ );
});
test("PvP setter recolors on repeated values; hold-only packet retains the color latch", () => {
	const owner = createEntities( undefined, undefined, () => context );
	owner.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: 10, kind: "npc" } ],
		localPlayerEntry: {}
	} );
	const flush = () => {
		const b = owner.take();
		if ( b ) owner.ack( b.sequence );
		return b;
	};
	flush();
	owner.receive( { opcode: 0x30d7, payload: Buffer.concat( [ npcPacket(), Buffer.from( [ 0 ] ) ] ) } );
	flush();
	const state = n => ({ opcode: 0x3122, payload: Buffer.from( [ 2, 0, 0, 0, 7, n ] ) });
	owner.receive( state( 1 ) );
	assert.equal( owner.read( 2 ).nameColor, 0xffff58fe );
	flush();
	owner.receive( { opcode: 0x3204, payload: Buffer.from( [ 2, 0, 0, 0, 1, 2 ] ) } );
	assert.equal( owner.read( 2 ).holdType, 2 );
	assert.equal( owner.read( 2 ).nameColor, 0xffff58fe );
	flush();
	owner.receive( { opcode: 0x3204, payload: Buffer.from( [ 2, 0, 0, 0, 3, 4, 0 ] ) } );
	assert.equal( owner.read( 2 ).nameColor, 0xffffffff );
	flush();
	owner.receive( state( 0 ) );
	assert.equal( flush().events.length, 1 );
	assert.throws( () => owner.receive( { opcode: 0x3204, payload: Buffer.from( [ 2, 0, 0, 0, 3, 1 ] ) } ), /length/ );
	assert.equal( owner.read( 2 ).holdType, 4, "truncated transaction cannot mutate hold type" );
	owner.dispose();
});

test("world reference admission treats only capes as cape teams", async () => {
	const { createWorldCore } = await load( "src/engine/runtime/simulation/worker/session/world/core.ts" );
	const core = createWorldCore( () => {} );
	let id = 20;
	const frame = ( typeFlags, value ) => ({
		opcode: 14,
		payload: Buffer.from(
			JSON.stringify( {
				version: 1,
				items: [ { refObjId: ++id, typeFlags, name: "Fixture", nativeFields: { itemParam2_2a0: value } } ]
			} )
		)
	});
	assert.doesNotThrow( () => core.receive( frame( 0x126c, -1.5 ), 0 ) );
	assert.throws( () => core.receive( frame( (5 << 11) | 0x3ac, -1.5 ), 0 ), /Invalid PvP cape/ );
	assert.doesNotThrow( () => core.receive( frame( (5 << 11) | 0x3ac, 5 ), 0 ) );
	core.dispose();
});
