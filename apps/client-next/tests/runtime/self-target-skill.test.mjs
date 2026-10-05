/*
===========================================================================

self-target-skill.test.mjs - tests for gameplay.ts and skill-catalog.ts:
a target-required skill that admits its caster

Mana Cycle and Discord Wave require a target that may be the caster
(targetSelf, column 26 of the skill row). With nothing selected the cast
goes out aimed at the local character; a target-required row without the
mark is still refused before anything is sent.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { skillCatalog } = await import( "../../src/engine/foundation/gameplay/skill-catalog.ts" );

const LOCAL_GID = 7;
const SELF_SKILL = 9726;
const ENEMY_SKILL = 3;
/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
const local = {
	gid: LOCAL_GID,
	refObjId: 14875,
	kind: "local-player",
	name: "bard",
	regionId: 0x61a8,
	x: 100,
	y: 20,
	z: 100,
	heading: 0
};

/*
================
skillRef

One published skill reference row: target-required, with the targetSelf
mark when self is true.
================
*/
function skillRef( id, self ) {
	return {
		id,
		group: id,
		level: 1,
		status: false,
		effectRider: false,
		ui: {
			name: `SKILL_${id}`,
			trainable: true,
			spCost: 1,
			targetRequired: true,
			...(self ? { targetSelf: true } : {}),
			cooldownMs: 0,
			masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
			prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
		}
	};
}

/*
================
seededGameplay

A gameplay owner whose local character LOCAL_GID knows both skills, and
the frames it sends.
================
*/
function seededGameplay() {
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [];
	const g = createGameplay( f => sent.push( f ) );
	g.bootstrap( {
		character: { skills: [ SELF_SKILL, ENEMY_SKILL ] },
		refSkillSnapshot: [ skillRef( SELF_SKILL, true ), skillRef( ENEMY_SKILL, false ) ]
	} );
	g.seed( local );
	return { g, sent };
}

test("the catalog reads targetSelf and defaults it to false", () => {
	const rows = skillCatalog( { refSkillSnapshot: [ skillRef( SELF_SKILL, true ), skillRef( ENEMY_SKILL, false ) ] } );
	assert.equal( rows.find( r => r.id === SELF_SKILL )?.targetSelf, true );
	assert.equal( rows.find( r => r.id === ENEMY_SKILL )?.targetSelf, false );
	const bad = skillRef( SELF_SKILL, true );
	assert.throws( () => skillCatalog( { refSkillSnapshot: [ { ...bad, ui: { ...bad.ui, targetSelf: 1 } } ] } ) );
});

test("a self-admitting skill with nothing selected is sent at the local character", () => {
	const { g, sent } = seededGameplay();
	g.command( { kind: "skill", skillId: SELF_SKILL }, 100, undefined );
	assert.equal( sent.length, 1 );
	const frame = sent[0];
	assert.ok( frame );
	const v = new DataView( frame.payload.buffer, frame.payload.byteOffset, frame.payload.byteLength );
	assert.equal( frame.opcode, 0x72cd );
	assert.equal( frame.payload.length, 11 );
	assert.equal( v.getUint32( 2, true ), SELF_SKILL );
	assert.equal( frame.payload[6], 1, "the cast carries an object target" );
	assert.equal( v.getUint32( 7, true ), LOCAL_GID );
	g.dispose();
});

test("a target-required skill without the self mark is still refused with nothing selected", () => {
	const { g, sent } = seededGameplay();
	assert.throws( () => g.command( { kind: "skill", skillId: ENEMY_SKILL }, 100, undefined ), /requires a target/ );
	assert.equal( sent.length, 0 );
	g.dispose();
});
