/*
===========================================================================

automob-disguise.test.mjs - the anti-bot monster's disguise (#4530)

The entity journal draws a MOB_AUTOMOB spawn as the last COS whose
selection the server granted, keeps its own look before any COS was
targeted (the native lookup of reference 0 faults instead), and marks
it to vanish without the despawn fade. Every other monster is untouched.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
const { autoMobSpawn, AUTOMOB_CODENAME } = await import( "../../src/engine/foundation/gameplay/automob.ts" );
const { createWorldCore } = await import( "../../src/engine/runtime/simulation/worker/session/world/core.ts" );

const AUTOMOB_REF = 2029, OTHER_MONSTER_REF = 2030, HORSE_REF = 3914, REGION = 25000;

/*
================
journal

A journal whose snapshot names MOB_AUTOMOB, another monster and a horse.
================
*/
function journal() {
	const e = createEntities();
	e.bootstrap( entry() );
	return e;
}

/*
================
entry

The enter-world bootstrap both owners read.
================
*/
function entry() {
	return {
		protocolVersion: 3,
		nativeResult: 1,
		refObjSnapshot: [
			{
				refObjId: AUTOMOB_REF,
				kind: "monster",
				tidWord: 0,
				codename: AUTOMOB_CODENAME,
				name: "Anti-bot",
				nameStrId: "SN_MOB_AUTOMOB"
			},
			{
				refObjId: OTHER_MONSTER_REF,
				kind: "monster",
				tidWord: 0,
				codename: "MOB_CH_MANGNYANG",
				name: "Mangnyang",
				nameStrId: "SN_MOB_CH_MANGNYANG"
			},
			{ refObjId: HORSE_REF, kind: "cos", tidWord: 0x11c6, name: "Horse" }
		],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: REGION, x: 1, y: 2, z: 3, angle: 0 } },
		character: { name: "Me", skills: [], quickSlots: [] }
	};
}

/*
================
monsterSpawn

A standing monster's 0x30D7 body.
================
*/
function monsterSpawn( refObjId, gid ) {
	const p = Buffer.alloc( 50 );
	p.writeUInt32LE( refObjId );
	p.writeUInt32LE( gid, 4 );
	p.writeUInt16LE( REGION, 8 );
	p.writeFloatLE( 100, 10 );
	p.writeFloatLE( 11, 14 );
	p.writeFloatLE( 40, 18 );
	p[25] = 3;
	p[29] = 1;
	p.writeFloatLE( 10, 32 );
	p.writeFloatLE( 20, 36 );
	p.writeFloatLE( 1, 40 );
	p[45] = 1;
	return { opcode: 0x30d7, payload: p };
}

/*
================
horseSpawn

A horse's 0x30D7 body.
================
*/
function horseSpawn( gid ) {
	const payload = Buffer.alloc( 57 );
	payload.writeUInt32LE( HORSE_REF );
	payload.writeUInt32LE( gid, 4 );
	payload.writeUInt16LE( REGION, 8 );
	payload[25] = 3;
	payload.writeFloatLE( 20, 32 );
	payload.writeFloatLE( 50, 36 );
	payload.writeFloatLE( 100, 40 );
	payload[45] = 1;
	payload.writeUInt32LE( 7, 52 );
	return { opcode: 0x30d7, payload };
}

test("MOB_AUTOMOB keeps its own look until a COS is targeted, and always vanishes", () => {
	const e = journal();
	e.receive( monsterSpawn( AUTOMOB_REF, 20 ), 0 );
	const bare = e.read( 20 );
	assert.equal( bare?.refObjId, AUTOMOB_REF );
	assert.equal( bare?.name, "Anti-bot" );
	assert.equal( bare?.vanishes, true );
});

test("a granted COS becomes the look of the next MOB_AUTOMOB spawn", () => {
	const e = journal();
	e.receive( horseSpawn( 8 ), 0 );
	e.receive( monsterSpawn( OTHER_MONSTER_REF, 9 ), 0 );
	e.noteTarget( 8 );
	// A monster target afterwards leaves the remembered COS alone (0x581851
	// writes only for a CICCos).
	e.noteTarget( 9 );
	e.receive( monsterSpawn( AUTOMOB_REF, 20 ), 0 );
	const disguised = e.read( 20 );
	assert.equal( disguised?.kind, "monster", "the class stays the monster's" );
	assert.equal( disguised?.refObjId, HORSE_REF );
	assert.equal( disguised?.name, "Horse" );
	assert.equal( disguised?.vanishes, true );
	const other = e.read( 9 );
	assert.equal( other?.refObjId, OTHER_MONSTER_REF );
	assert.equal( other?.vanishes, undefined, "another monster still fades" );
});

test("autoMobSpawn ignores an empty look", () => {
	const entity = { refObjId: AUTOMOB_REF, name: "Anti-bot" };
	assert.deepEqual( autoMobSpawn( entity, undefined ), { ...entity, vanishes: true } );
	assert.deepEqual( autoMobSpawn( entity, { refObjId: 0 } ), { ...entity, vanishes: true } );
	assert.deepEqual( autoMobSpawn( entity, { refObjId: HORSE_REF } ), {
		refObjId: HORSE_REF,
		name: "Anti-bot",
		vanishes: true
	} );
});

test("the world core hands the granted COS to the journal (0xB45A)", () => {
	const core = createWorldCore( () => {} );
	core.bootstrap( entry() );
	// The local identity (0x32A6), so a selection has a player behind it.
	core.receive( { opcode: 0x32a6, payload: Buffer.from( [ 7, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	core.receive( horseSpawn( 8 ), 0 );
	core.command( { kind: "select", gid: 8 }, 1 );
	const grant = Buffer.alloc( 14 );
	grant[0] = 1;
	grant.writeUInt32LE( 8, 1 );
	grant[5] = 1;
	core.receive( { opcode: 0xb45a, payload: grant }, 2 );
	core.receive( monsterSpawn( AUTOMOB_REF, 20 ), 3 );
	let spawned;
	for ( let batch = core.take(); batch; batch = core.take() ) {
		for ( const event of batch.events ) {
			if ( event.kind === "spawn" && event.entity.gid === 20 ) spawned = event.entity;
		}
		core.ack( batch.sequence );
	}
	assert.equal( spawned?.refObjId, HORSE_REF );
	assert.equal( spawned?.vanishes, true );
});
