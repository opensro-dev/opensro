/*
===========================================================================

blind-hide.test.mjs - holding V hides a character whole (2570)

The blind rule (foundation/ui/name-visibility.ts) marks which characters
V hides. This suite pins how publishCharacters applies it: the body, the
attachments riding on it and the staged effects chained through
attachment.gid all go invisible together, and nothing else does.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const { publishCharacters } = await import(
	"../../src/engine/runtime/characters/presentation-finalize.ts"
);

/*
================
actor
================
*/
function actor( gid, fields = {} ) {
	return {
		gid,
		model: "model-" + gid,
		pose: { regionId: 250, x: 0, y: 0, z: 0, yaw: 0 },
		clip: "stand",
		time: 0,
		loop: true,
		...fields
	};
}

/*
================
fixture

Publishes one frame and returns the presented actors by gid. The frame
holds a blindable unique (42) with an attached aura (9001) and a particle
chained to the aura (9002), the local player (1) with its own attached
buff (9003), a free-standing effect with no holder (9004), another
player's companion (7) and the effect riding on it (9005).
================
*/
function fixture( { blindHeld, vHidesCompanions = false } ) {
	const next = new Map();
	for (
		const entry of [
			actor( 42, { blindable: true } ),
			actor( 9001, { effectEntity: true, attachment: { gid: 42, joint: null, offset: [ 0, 0, 0 ] } } ),
			actor( 9002, { effectEntity: true, attachment: { gid: 9001, joint: null, offset: [ 0, 0, 0 ] } } ),
			actor( 1 ),
			actor( 9003, { effectEntity: true, attachment: { gid: 1, joint: null, offset: [ 0, 0, 0 ] } } ),
			actor( 9004, { effectEntity: true } ),
			actor( 7, { blindableCompanion: true } ),
			actor( 9005, { effectEntity: true, attachment: { gid: 7, joint: null, offset: [ 0, 0, 0 ] } } )
		]
	) next.set( entry.gid, entry );
	const presented = [];
	const bindings = {
		renderer: {
			/*
			================
			setCharacterActors
			================
			*/
			setCharacterActors( actors ) {
				presented.push( ...actors );
			},
			setCharacterAssembly() {},
			vHidesCompanions: () => vHidesCompanions
		},
		resources: {
			ready: () => true,
			plan: () => true,
			retainWanted() {}
		},
		mallPreview: {
			skin: () => null,
			/*
			================
			step
			================
			*/
			step() {
				return [];
			}
		}
	};
	const frame = { local: undefined, gameplay: null, next, seconds: 0, blindHeld };
	publishCharacters( frame, { displayed: next }, { catalog: new Map(), dress: {}, manifest: 0 }, bindings );
	return new Map( presented.map( entry => [ entry.gid, entry ] ) );
}

/*
================
hidden

True when the published actor went out invisible and unpickable, the
same shape the finalize hide has always emitted for a blinded body.
================
*/
function hidden( presented, gid ) {
	const entry = presented.get( gid );
	assert.ok( entry, "actor " + gid + " was published" );
	return entry.opacity === 0 && entry.pickable === false;
}

test("holding V hides a blinded character's attached effects with it", () => {
	const presented = fixture( { blindHeld: true } );
	assert.equal( hidden( presented, 42 ), true, "the unique's body hides" );
	assert.equal( hidden( presented, 9001 ), true, "its attached aura hides with it" );
	assert.equal( hidden( presented, 9002 ), true, "an effect chained to the aura hides too" );
	assert.equal( hidden( presented, 1 ), false, "the local player stays visible" );
	assert.equal( hidden( presented, 9003 ), false, "the local player's buff stays visible" );
	assert.equal( hidden( presented, 9004 ), false, "a holderless effect stays visible" );
});

test("releasing V leaves every actor visible", () => {
	const presented = fixture( { blindHeld: false } );
	for ( const gid of [ 42, 9001, 9002, 1, 9003, 9004, 7, 9005 ] ) {
		assert.equal( hidden( presented, gid ), false, "actor " + gid + " stays visible" );
	}
});

test("the companion row hides a companion and its effects only when armed", () => {
	const armed = fixture( { blindHeld: true, vHidesCompanions: true } );
	assert.equal( hidden( armed, 7 ), true, "the companion hides" );
	assert.equal( hidden( armed, 9005 ), true, "its effect hides with it" );
	const disarmed = fixture( { blindHeld: true } );
	assert.equal( hidden( disarmed, 7 ), false, "unarmed, the companion stays visible" );
	assert.equal( hidden( disarmed, 9005 ), false, "unarmed, its effect stays visible" );
});
