/*
===========================================================================

pet-assist.test.mjs - attack pets join the local player's own attacks

776830: the local player's own cast broadcast (B245) of a skill whose
motion record has +0xBD set sends each aggressive attack pet without an
assist target the command bar's attack order, 0x769E [pet][2][target].
854B35 forgets the target once it is dead, so the next attack orders the
pet again.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const LOCAL = {
	gid: 1,
	refObjId: 1,
	kind: "local-player",
	regionId: 0x6b4f,
	x: 0,
	y: 0,
	z: 0,
	heading: 0,
	name: "Owner"
};
const PET = 2, MONSTER = 40, PEER = 7;
// A basic attack (assistsCompanions) and a buff (not).
const ATTACK_SKILL = 70, BUFF_SKILL = 27;
// The attack pet record's command mode offset (830EC0 band 3).
const COMMAND_MODE_OFFSET = 27;

/*
================
withPet

A gameplay owner with one summoned attack pet in the given command mode,
a monster in the world, and the skill references the server projects.
================
*/
function withPet( mode ) {
	const sent = [], entities = new Map();
	const gameplay = createGameplay( frame => sent.push( frame ), () => {}, () => {}, gid => entities.get( gid ) );
	gameplay.bootstrap( {
		refObjSnapshot: [ { kind: "cos", refObjId: 9, tidWord: 0x19c6 } ],
		refSkillSnapshot: [
			{ id: ATTACK_SKILL, group: 1, level: 1, status: false, effectRider: false, assistsCompanions: true },
			{ id: BUFF_SKILL, group: 2, level: 1, status: false, effectRider: false }
		]
	} );
	gameplay.worldReady();
	gameplay.seed( LOCAL );
	const record = new Uint8Array( 39 ), view = new DataView( record.buffer );
	view.setUint32( 0, PET, true );
	view.setUint32( 4, 9, true );
	view.setUint32( 8, 100, true );
	view.setUint32( COMMAND_MODE_OFFSET, mode, true );
	gameplay.receive( { opcode: 0x3158, payload: record }, 0 );
	entities.set( PET, { ...LOCAL, gid: PET, refObjId: 9, kind: "cos", ownerGid: 1 } );
	entities.set( MONSTER, { ...LOCAL, gid: MONSTER, kind: "monster" } );
	return { gameplay, sent, entities };
}

/*
================
cast

A B245 cast-open broadcast: result 1, sub 0, skill, caster, token, then a
phase naming the target with no result batch.
================
*/
function cast( skill, caster, target, token ) {
	const p = new Uint8Array( 19 ), v = new DataView( p.buffer );
	p[0] = 1;
	v.setUint32( 2, skill, true );
	v.setUint32( 6, caster, true );
	v.setUint32( 10, token, true );
	v.setUint32( 14, target, true );
	return { opcode: 0xb245, payload: p };
}

/*
================
orders
================
*/
function orders( sent ) {
	return sent.filter( frame => frame.opcode === 0x769e ).map( frame => [ ...frame.payload ] );
}

const ATTACK_ORDER = [ PET, 0, 0, 0, 2, MONSTER, 0, 0, 0 ];

test("the owner's attack sends an aggressive attack pet onto the target once", () => {
	const { gameplay, sent } = withPet( 1 );
	gameplay.receive( cast( ATTACK_SKILL, LOCAL.gid, MONSTER, 100 ), 1 );
	assert.deepEqual( orders( sent ), [ ATTACK_ORDER ] );
	// The pet holds its assist target: the next swing orders nothing.
	gameplay.receive( cast( ATTACK_SKILL, LOCAL.gid, MONSTER, 101 ), 2 );
	assert.equal( orders( sent ).length, 1 );
});

test("a defensive pet, a buff, and a peer's cast send nothing", () => {
	const defensive = withPet( 0 );
	defensive.gameplay.receive( cast( ATTACK_SKILL, LOCAL.gid, MONSTER, 100 ), 1 );
	assert.deepEqual( orders( defensive.sent ), [], "mode 0 does not assist" );
	const aggressive = withPet( 1 );
	aggressive.gameplay.receive( cast( BUFF_SKILL, LOCAL.gid, MONSTER, 100 ), 1 );
	aggressive.gameplay.receive( cast( ATTACK_SKILL, PEER, MONSTER, 101 ), 2 );
	assert.deepEqual( orders( aggressive.sent ), [], "only the owner's own attack skill" );
});

test("a dead target is forgotten, so the next attack orders the pet again", () => {
	const { gameplay, sent, entities } = withPet( 1 );
	gameplay.receive( cast( ATTACK_SKILL, LOCAL.gid, MONSTER, 100 ), 1 );
	entities.set( MONSTER, { ...entities.get( MONSTER ), appearanceState: [ 2 ] } );
	gameplay.step( 2, LOCAL );
	entities.set( MONSTER, { ...LOCAL, gid: MONSTER, kind: "monster" } );
	gameplay.receive( cast( ATTACK_SKILL, LOCAL.gid, MONSTER, 102 ), 3 );
	assert.deepEqual( orders( sent ), [ ATTACK_ORDER, ATTACK_ORDER ] );
});

test("the command bar's follow clears the assist target", () => {
	const { gameplay, sent, entities } = withPet( 1 );
	gameplay.receive( cast( ATTACK_SKILL, LOCAL.gid, MONSTER, 100 ), 1 );
	gameplay.command( { kind: "cos-follow", gid: PET }, 2, entities.get( PET ), LOCAL );
	gameplay.receive( cast( ATTACK_SKILL, LOCAL.gid, MONSTER, 101 ), 3 );
	assert.deepEqual( orders( sent ).filter( order => order[4] === 2 ), [ ATTACK_ORDER, ATTACK_ORDER ] );
});
