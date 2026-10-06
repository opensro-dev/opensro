/*
===========================================================================

burn-effects.test.mjs - tests for effects.ts, effects.ts, combat.ts,
random.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const packet = ( gid, hp, abnormal ) => {
	const p = new Uint8Array( 15 ), v = new DataView( p.buffer );
	v.setUint32( 0, gid, true );
	v.setUint16( 4, 2, true );
	p[6] = 5;
	v.setUint32( 7, hp, true );
	v.setUint32( 11, abnormal, true );
	return p;
};
test("real burn vitals drive named EFP, native tint, cold admission, death and scope retirement", () => {
	const decode = createEffectDecoder(),
		named = decode.decode( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/namedEffectRecords.json" ) ),
		catalog = decode.decode( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/effectRecords.json" ) );
	let serial = 0;
	const jobs = new Map();
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, type ) {
				jobs.set(
					++serial,
					type === "effects" ?
						{ kind: "effects", catalog: url.includes( "namedEffect" ) ? named : catalog } :
						{
							kind: "bytes",
							buffer: new TextEncoder().encode(
								JSON.stringify( { format: "sro-skill-stage-models", models: {} } )
							).buffer
						}
				);
				return serial;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel() {}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 7 )
	);
	const entity = { gid: 2, kind: "monster", regionId: 257, x: 0, y: 0, z: 0, heading: 0 },
		entities = [ entity ],
		body = { gid: 2, pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 }, scale: 2, height: 40 };
	const combat = createCombat( gid => entities.find( e => e.gid === gid ) );
	let ready = false;
	const step = at => owner.step( entities, combat.state(), at, () => ready, () => 1, [], undefined, [ body ] );
	combat.receive( 0x33a6, packet( 2, 100, 8 ) );
	step( 0 );
	assert.deepEqual( owner.appearance( 2 ).materialTint, [ 1, Math.fround( 80 / 255 ), Math.fround( 80 / 255 ) ] );
	step( .01 );
	step( .02 );
	step( .03 );
	ready = true;
	const born = step( .04 );
	assert.equal( born.length, 1 );
	assert.ok( decodeURIComponent( born[0].model ).endsWith( "battle/status_bad_burn.efp" ) );
	assert.equal( defined( born[0].attachment ).bone, "Bip01" );
	assert.equal( born[0].scale, 2 );
	assert.equal( born[0].loop, true );
	combat.receive( 0x33a6, packet( 2, 92, 8 ) );
	assert.equal( step( .1 )[0].gid, born[0].gid, "same status must not restart" );
	combat.receive( 0x33a6, packet( 2, 0, 0 ) );
	step( .2 );
	assert.equal( owner.appearance( 2 ).materialTint, undefined );
	assert.equal( step( 1.3 ).length, 0 );
	combat.receive( 0x33a6, packet( 2, 100, 8 ) );
	assert.equal( step( 1.4 ).length, 1 );
	entities.length = 0;
	assert.equal( step( 1.5 ).length, 0 );
	assert.equal( owner.appearance( 2 ).materialTint, undefined );
	assert.equal( owner.error(), null );
	owner.dispose();
});
test("freeze, sleep and stun start from the abnormal mask and a cleared mask removes them", () => {
	const decode = createEffectDecoder(),
		named = decode.decode( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/namedEffectRecords.json" ) ),
		catalog = decode.decode( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/effectRecords.json" ) );
	let serial = 0;
	const jobs = new Map();
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, type ) {
				jobs.set(
					++serial,
					type === "effects" ?
						{ kind: "effects", catalog: url.includes( "namedEffect" ) ? named : catalog } :
						{
							kind: "bytes",
							buffer: new TextEncoder().encode(
								JSON.stringify( { format: "sro-skill-stage-models", models: {} } )
							).buffer
						}
				);
				return serial;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel() {}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 7 )
	);
	const entities = [ { gid: 2, kind: "monster", regionId: 257, x: 0, y: 0, z: 0, heading: 0 }, {
		gid: 3,
		kind: "player",
		regionId: 257,
		x: 1,
		y: 0,
		z: 0,
		heading: 0
	}, { gid: 4, kind: "cos", regionId: 257, x: 2, y: 0, z: 0, heading: 0 } ];
	const bodies = entities.map( entity => ({
		gid: entity.gid,
		pose: { regionId: 257, x: entity.x, y: 0, z: entity.z, yaw: 0 },
		scale: 1,
		height: 40,
		heightFactor: 1
	}) );
	const combat = createCombat( gid => entities.find( e => e.gid === gid ) );
	let ready = false;
	const step = at => owner.step( entities, combat.state(), at, () => ready, () => 1, [], undefined, bodies );
	const paths = actors => actors.map( actor => decodeURIComponent( actor.model ) );
	const vital = ( gid, hp, abnormal ) => {
		const extra = [];
		for ( let i = 0; i < 32; i++ ) if ( (abnormal & (2 ** i)) && ((2 ** i) & 0x017fcfc0) ) extra.push( 1 );
		const p = new Uint8Array( 15 + extra.length ), v = new DataView( p.buffer );
		v.setUint32( 0, gid, true );
		v.setUint16( 4, 2, true );
		p[6] = 5;
		v.setUint32( 7, hp, true );
		v.setUint32( 11, abnormal, true );
		extra.forEach( ( level, index ) => {
			p[15 + index] = level;
		} );
		return p;
	};
	combat.receive( 0x33a6, vital( 2, 100, 1 ) );
	combat.receive( 0x33a6, vital( 3, 100, 0x40 ) );
	combat.receive( 0x33a6, vital( 4, 100, 0x4000 ) );
	step( 0 );
	assert.equal( owner.appearance( 2 ).materialTint, undefined );
	assert.equal( owner.appearance( 3 ).materialTint, undefined );
	assert.deepEqual( owner.appearance( 4 ).materialTint, [ 1, 1, Math.fround( 64 / 255 ) ] );
	step( .01 );
	step( .02 );
	step( .03 );
	ready = true;
	const born = step( .05 );
	assert.ok(
		paths( born.filter( a => a.attachment?.gid === 2 ) ).some( path =>
			path.endsWith( "battle/status_bad_icing_on.efp" )
		)
	);
	assert.ok(
		paths( born.filter( a => a.attachment?.gid === 3 ) ).some( path =>
			path.endsWith( "battle/status_bad_sleep.efp" )
		)
	);
	assert.ok(
		paths( born.filter( a => a.attachment?.gid === 4 ) ).some( path =>
			path.endsWith( "battle/status_bad_stun.efp" )
		)
	);
	combat.receive( 0x33a6, vital( 2, 100, 0 ) );
	combat.receive( 0x33a6, vital( 3, 100, 0 ) );
	combat.receive( 0x33a6, vital( 4, 100, 0 ) );
	const released = step( .2 );
	assert.ok(
		paths( released.filter( a => a.attachment?.gid === 2 ) ).some( path =>
			path.endsWith( "battle/status_bad_icing_off.efp" )
		)
	);
	assert.equal( owner.appearance( 4 ).materialTint, undefined );
	assert.equal( owner.error(), null );
	owner.dispose();
});
test("a cleared tint resets the material while the other status decoration stays", () => {
	const decode = createEffectDecoder(),
		named = decode.decode( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/namedEffectRecords.json" ) ),
		catalog = decode.decode( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/effectRecords.json" ) );
	let serial = 0;
	const jobs = new Map();
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, type ) {
				jobs.set(
					++serial,
					type === "effects" ?
						{ kind: "effects", catalog: url.includes( "namedEffect" ) ? named : catalog } :
						{
							kind: "bytes",
							buffer: new TextEncoder().encode(
								JSON.stringify( { format: "sro-skill-stage-models", models: {} } )
							).buffer
						}
				);
				return serial;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel() {}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 7 )
	);
	const entities = [ { gid: 2, kind: "monster", regionId: 257, x: 0, y: 0, z: 0, heading: 0 } ];
	const bodies = [ {
		gid: 2,
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
		scale: 1,
		height: 40,
		heightFactor: 1
	} ];
	const combat = createCombat( gid => entities.find( e => e.gid === gid ) );
	let ready = false;
	const step = at => owner.step( entities, combat.state(), at, () => ready, () => 1, [], undefined, bodies );
	const vital = ( hp, abnormal ) => {
		const extra = [];
		for ( let i = 0; i < 32; i++ ) if ( (abnormal & (2 ** i)) && ((2 ** i) & 0x017fcfc0) ) extra.push( 1 );
		const p = new Uint8Array( 15 + extra.length ), v = new DataView( p.buffer );
		v.setUint32( 0, 2, true );
		v.setUint16( 4, 2, true );
		p[6] = 5;
		v.setUint32( 7, hp, true );
		v.setUint32( 11, abnormal, true );
		extra.forEach( ( level, index ) => {
			p[15 + index] = level;
		} );
		return p;
	};
	const paths = actors => actors.map( actor => decodeURIComponent( actor.model ) );
	combat.receive( 0x33a6, vital( 100, 0x8 ) );
	step( 0 );
	assert.deepEqual( owner.appearance( 2 ).materialTint, [ 1, Math.fround( 80 / 255 ), Math.fround( 80 / 255 ) ] );
	combat.receive( 0x33a6, vital( 100, 0x8 | 0x4000 ) );
	step( .01 );
	assert.deepEqual( owner.appearance( 2 ).materialTint, [ 1, 1, Math.fround( 64 / 255 ) ] );
	combat.receive( 0x33a6, vital( 100, 0x4000 ) );
	step( .02 );
	assert.equal( owner.appearance( 2 ).materialTint, undefined );
	step( .03 );
	ready = true;
	const born = step( .05 );
	assert.ok( paths( born ).some( path => path.endsWith( "battle/status_bad_stun.efp" ) ) );
	assert.ok( paths( born ).some( path => path.endsWith( "battle/status_bad_blind.efp" ) ) === false );
	combat.receive( 0x33a6, vital( 100, 0x2000 ) );
	step( .06 );
	ready = true;
	const dark = step( .08 );
	assert.ok( paths( dark ).some( path => path.endsWith( "battle/status_bad_blind.efp" ) ) );
	assert.equal( owner.error(), null );
	owner.dispose();
});
test("3128 periodic damage is private feedback, separate from authoritative HP and death", () => {
	const combat = createCombat( gid => gid === 2 ? { gid: 2, kind: "monster" } : undefined );
	combat.seed( 2, { hp: 100 } );
	const p = new Uint8Array( 8 ), v = new DataView( p.buffer );
	v.setUint32( 0, 2, true );
	v.setUint32( 4, 8, true );
	const before = combat.state();
	combat.receive( 0x3128, p, 100 );
	assert.equal( combat.state().vitals[0].hp, 100 );
	assert.equal( combat.state().environmentalDamage[0].damage, 8 );
	assert.equal( before.environmentalDamage.length, 0 );
	combat.receive( 0x33a6, packet( 2, 92, 8 ), 101 );
	assert.equal( combat.state().vitals[0].hp, 92 );
	assert.equal( combat.state().environmentalDamage.length, 1 );
	for ( const n of [ 0, 7, 9 ] ) assert.throws( () => combat.receive( 0x3128, new Uint8Array( n ) ) );
	v.setUint32( 0, 999, true );
	combat.receive( 0x3128, p, 102 );
	assert.equal( combat.state().environmentalDamage.length, 1 );
	combat.receive( 0x33a6, packet( 2, 0, 0 ), 103 );
	combat.step( 1100 );
	assert.equal( combat.state().environmentalDamage.length, 0 );
});
