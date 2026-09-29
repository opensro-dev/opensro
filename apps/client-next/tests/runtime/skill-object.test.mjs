/*
===========================================================================

skill-object.test.mjs - native dynamic skill-object wire and journal lifetime

The instruction-derived row checks the server's fixed byte layout without
pretending to be a captured packet. Journal tests exercise shipped decoding.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { decodeSkillObject } = await import( "../../src/engine/foundation/gameplay/skill-object.ts" );
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
const ROW = Buffer.from( "ffffffff5400c41b00000100000154640000c84200002041000048435a00", "hex" );

/*
================
flush

Release each reliable journal batch before admitting the next operation.
================
*/
function flush( entities ) {
	const batch = entities.take();
	if ( batch ) entities.ack( batch.sequence );
	return batch?.events ?? [];
}

test("skill object rows distinguish catalog sentinel, skill identity and world identity", () => {
	const entity = decodeSkillObject( ROW, false );
	assert.deepEqual( entity, {
		gid: 0x01000001,
		refObjId: 0xffffffff,
		kind: "skill-object",
		regionId: 0x6454,
		x: 100,
		y: 10,
		z: 200,
		heading: 90,
		name: "",
		skillObject: { skillId: 7108 }
	} );
	assert.equal( decodeSkillObject( Buffer.concat( [ ROW, Buffer.from( [ 1 ] ) ] ), true ).skillObject?.appear, 1 );
	assert.throws( () => decodeSkillObject( ROW, true ), /length/ );
	assert.throws( () => decodeSkillObject( Buffer.concat( [ ROW, Buffer.from( [ 1 ] ) ] ), false ), /length/ );
	for ( const offset of [ 0, 4, 6, 10 ] ) {
		const invalid = Buffer.from( ROW );
		invalid.fill( 0, offset, offset + (offset === 4 ? 2 : 4) );
		assert.throws( () => decodeSkillObject( invalid, false ) );
	}
	const invalid = Buffer.from( ROW );
	invalid.writeFloatLE( NaN, 20 );
	assert.throws( () => decodeSkillObject( invalid, false ), /position/ );
});

test("single spawn, despawn and object-list reentry use one skill-object journal owner", () => {
	const entities = createEntities();
	entities.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 0x6454, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	flush( entities );
	entities.receive( { opcode: 0x30d7, payload: Buffer.concat( [ ROW, Buffer.from( [ 1 ] ) ] ) }, 100 );
	const single = flush( entities ).filter( event => event.kind === "spawn" );
	assert.equal( single.length, 1 );
	assert.equal( single[0]?.entity.skillObject?.skillId, 7108 );
	entities.receive( { opcode: 0x36ab, payload: ROW.subarray( 10, 14 ) }, 101 );
	assert.deepEqual( flush( entities ).filter( event => event.kind === "despawn" ), [ {
		kind: "despawn",
		gid: 0x01000001
	} ] );
	entities.receive( { opcode: 0x30cb, payload: Buffer.from( [ 1, 1, 0 ] ) }, 102 );
	entities.receive( { opcode: 0x3417, payload: ROW }, 102 );
	entities.receive( { opcode: 0x330a, payload: Buffer.alloc( 0 ) }, 102 );
	const reentry = flush( entities ).filter( event => event.kind === "spawn" );
	assert.equal( reentry.length, 1 );
	assert.deepEqual( reentry[0]?.entity.skillObject, { skillId: 7108 } );
	entities.dispose();
});
