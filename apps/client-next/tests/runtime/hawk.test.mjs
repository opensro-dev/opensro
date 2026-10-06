/*
===========================================================================

hawk.test.mjs - tests for hawk.ts, effects.ts, random.ts, effects.ts, ...

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
const { stepHawk, hawkEvent, hawkYaw } = await import( "../../src/engine/foundation/animation/hawk.ts" );
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { hawkResult } = await import( "../../src/engine/foundation/gameplay/attached-effects.ts" );
const { effectScript } = await import( "../../src/engine/foundation/animation/effect-script.ts" );
const reference = JSON.parse( readFileSync( "tests/fixtures/native/hawk-reference.json", "utf8" ) );
const manifest = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skillfx/manifest.json", "utf8" ) );
const raw = readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/effectRecords.json" );
const catalog = createEffectDecoder().decode( raw ), phases = [ "hover", "approach", "attack", "hold", "return" ];
const point = ( [x, y, z] ) => ({ x, y, z });

test("five native hawk modes match 35 original instruction cases, including missing targets and teleport threshold", () => {
	assert.equal( reference.binarySha256, "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a" );
	assert.equal( reference.cases.length, 35 );
	for ( const row of reference.cases ) {
		const state = {
			...row.before,
			phase: phases[row.before.mode],
			position: point( row.before.position ),
			animationMs: 0
		};
		const actual = stepHawk( state, {
			...row.frame,
			holder: point( row.frame.holder ),
			displayedHolder: point( row.frame.displayedHolder ),
			target: row.frame.target ?
				{
					...point( row.frame.target.position ),
					heightFactor: row.frame.target.heightFactor,
					dead: row.frame.target.dead
				} :
				undefined
		} );
		assert.equal( actual.phase, phases[row.after.mode] );
		assert.equal( actual.remainingMs, row.after.remainingMs );
		for ( const [key, i] of [ "x", "y", "z" ].map( ( key, i ) => [ key, i ] ) ) {
			assert.equal( actual.position[key], row.after.position[i], JSON.stringify( { row, actual } ) );
		}
		for ( const key of [ "yaw", "targetYaw" ] ) {
			assert.equal( actual[key], row.after[key], JSON.stringify( { row, actual } ) );
		}
	}
	for ( const row of reference.commands ) {
		const actual = hawkEvent( { phase: phases[row.beforeMode], animationMs: 123 }, {
			type: "command",
			target: 73,
			damage: 0x8123
		} );
		assert.equal( actual.phase, "approach" );
		assert.equal( actual.remainingMs, row.after.remainingMs );
		assert.equal( actual.damage, row.after.damage );
		assert.equal( actual.target, row.after.target );
		assert.equal( actual.animationMs, row.beforeMode === 1 ? 123 : 0 );
	}
});
function fixture( skill = 78, blood = false, bloodEnabled = true ) {
	let serial = 0, ready = true;
	const jobs = new Map();
	const effects = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, type ) {
				jobs.set(
					++serial,
					type === "effects" ?
						{ kind: "effects", catalog } :
						{ kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( manifest ) ).buffer }
				);
				return serial;
			},
			take( id ) {
				const row = jobs.get( id );
				jobs.delete( id );
				return row;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entities = [ { gid: 1, kind: "local-player", regionId: 257, x: 0, y: 0, z: 0, heading: 0 }, {
		gid: 2,
		kind: "monster",
		regionId: 257,
		x: 40,
		y: 0,
		z: 0,
		heading: 0
	} ];
	const game = {
		localGid: 1,
		casts: [],
		attachedEffects: [ { gid: 1, skill, token: 88, phase: 0, restored: true } ]
	};
	const step = now =>
		effects.step(
			entities,
			game,
			now,
			() => ready,
			() => 2,
			[],
			undefined,
			entities.map( entity => ({
				gid: entity.gid,
				model: "/assets/actor.glb",
				clip: "stand",
				time: 0,
				loop: true,
				scale: 1,
				heightFactor: 1,
				bloodEffects: blood ? [ "hiteffect/test-red.efp", "hiteffect/test-green.efp" ] : undefined,
				effectAnchor: { bone: null, offset: [ 0, 10, 1 ] },
				pose: { ...entity, yaw: Math.PI }
			}) ),
			2,
			bloodEnabled
		);
	step( 0 );
	step( .01 );
	step( .02 );
	return {
		effects,
		game,
		entities,
		step,
		setReady( value ) {
			ready = value;
		},
		command( revision = 1 ) {
			game.attachedEffects = [ { ...game.attachedEffects[0], hawk: { revision, target: 2, damage: 0x8123 } } ];
		}
	};
}
test("all 17 SCT_MOVER rows publish required native clips and keep a managed hawk past clip duration", () => {
	let count = 0;
	for ( const [id, record] of Object.entries( catalog ) ) {
		if ( record.stages.some( s => defined( s.script ).kind === "mover" ) ) {
			const f = fixture( +id ), rows = f.step( 30 );
			assert.equal( rows.length, 1 );
			assert.equal( rows[0].pose.y, 21 );
			assert.equal( rows[0].attachment, undefined );
			assert.equal( rows[0].clip, "state-0" );
			assert.equal( f.effects.error(), null );
			f.effects.dispose();
			count++;
		}
	}
	assert.equal( count, 17 );
	assert.throws( () => effectScript( [ "SCT_MOVER", "1" ] ) );
	for ( const path of [ "res/npc/animal/blackhawk.bsr", "res/npc/animal/lighthawk.bsr" ] ) {
		const states = manifest.models[path].states;
		assert.deepEqual( Object.keys( states ), [ "0", "2", "7" ] );
		assert.equal( states[2].durationMs, 933 );
		assert.equal( states[2].loop, false );
		assert.equal( states[2].trackEvents[0].cursorMs, 342 );
	}
});
test("hawk command travels, hits on authored callback exactly once, holds and returns; normal hit resource is used for packed fatal damage", () => {
	const f = fixture();
	f.command();
	let now = .03, rows = f.step( now );
	assert.equal( rows[0].clip, "state-7" );
	assert.deepEqual( f.effects.takeHawkImpacts(), [] );
	while ( rows[0].clip !== "state-2" && now < 2 ) {
		now = Math.round( (now + .01) * 1000 ) / 1000;
		rows = f.step( now );
	}
	assert.equal( rows[0].clip, "state-2" );
	const attack = now;
	f.step( attack + .342 );
	assert.deepEqual( f.effects.takeHawkImpacts(), [] );
	rows = f.step( attack + .343 );
	const hits = f.effects.takeHawkImpacts();
	assert.equal( hits.length, 1 );
	assert.equal( hits[0].damage, 0x8123 );
	assert.equal( hits[0].target, 2 );
	assert.equal( hits[0].holder, 1 );
	assert.ok( rows.some( a => a.model.endsWith( "hit_3_normal.efp" ) ) );
	f.step( attack + .5 );
	assert.deepEqual( f.effects.takeHawkImpacts(), [] );
	rows = f.step( attack + .934 );
	assert.equal( rows[0].clip, "state-0" );
	rows = f.step( attack + 6 );
	assert.ok( rows[0].clip === "state-7" || rows[0].clip === "state-0" );
	f.step( attack + 7 );
	rows = f.step( attack + 8 );
	assert.equal( rows[0].pose.y, 21 );
	assert.equal( f.effects.error(), null );
	f.effects.dispose();
});
test("retarget does not restart the active approach clip; disappearance returns and teardown cannot resurrect stale commands", () => {
	const f = fixture();
	f.command();
	let rows = f.step( .12 ), gid = rows[0].gid;
	f.command( 2 );
	rows = f.step( .15 );
	assert.equal( rows[0].gid, gid );
	assert.ok( rows[0].time >= .1 );
	f.entities.pop();
	const previous = { ...rows[0].pose };
	rows = f.step( .16 );
	assert.deepEqual( rows[0].pose, previous ); // missing target exits before motion/yaw publication
	assert.equal( rows[0].clip, "state-7" );
	f.game.attachedEffects = [];
	assert.deepEqual( f.step( .17 ), [] );
	assert.deepEqual( f.effects.takeHawkImpacts(), [] );
	f.effects.reset();
	assert.deepEqual( f.step( .2 ), [] );
	f.effects.dispose();
});
test("cold admission retains latest instance command and reset clears pending hit delivery", () => {
	const f = fixture();
	f.effects.reset();
	f.setReady( false );
	f.command();
	f.step( 0 );
	f.step( .01 );
	assert.deepEqual( f.step( .1 ), [] );
	f.setReady( true );
	assert.equal( f.step( .2 )[0].clip, "state-7" );
	f.effects.reset();
	assert.deepEqual( f.effects.takeHawkImpacts(), [] );
	f.effects.dispose();
});
test("357A decoder uses skill-instance token and packed u16, rejects malformed input atomically, ignores stale instances", () => {
	const c = createCombat();
	c.seedEffects( 1, [ { id: 78, token: 88, status: 0 } ] );
	const p = new Uint8Array( 10 ), v = new DataView( p.buffer );
	v.setUint32( 0, 88, true );
	v.setUint32( 4, 456, true );
	v.setUint16( 8, 0x8123, true );
	assert.equal( c.receive( 0x357a, p ), true );
	assert.equal( c.state().casts.length, 0 );
	const first = c.state().attachedEffects;
	assert.deepEqual( first[0].hawk, { revision: 1, target: 456, damage: 0x8123 } );
	assert.throws( () => c.receive( 0x357a, p.subarray( 0, 9 ) ) );
	assert.equal( c.state().attachedEffects, first );
	c.receive( 0x357a, p );
	assert.equal( defined( c.state().attachedEffects[0].hawk ).revision, 2 );
	v.setUint32( 0, 999, true );
	c.receive( 0x357a, p );
	assert.equal( defined( c.state().attachedEffects[0].hawk ).revision, 2 );
	c.remove( 1 );
	c.receive( 0x357a, p );
	assert.deepEqual( c.state().attachedEffects, [] );
	c.clear();
});

test("native callback high bit is fatal, not critical, and absent targets clear pending damage", () => {
	for ( const row of reference.callbacks ) {
		const hit = row.calls.find( call => call[0] === "hit" );
		if ( row.event === 1 ) {
			const actual = hawkEvent( { phase: "attack", damage: row.damage, target: 7 }, {
				type: "impact",
				targetExists: row.targetExists
			} );
			assert.equal( actual.damage, row.after.damage );
			assert.equal( actual.target, row.after.target );
			if ( row.targetExists ) {
				const result = hawkResult( row.damage );
				assert.equal( result.damage, hit[5] );
				assert.equal( Number( result.fatal ), hit[4][0] );
				assert.equal( result.flags, hit[4][2] );
				assert.equal( hit[3], 5678 );
				assert.equal( hit[6], 1234 );
			} else assert.equal( hit, undefined );
		} else if ( row.event === 100 ) {
			assert.equal( hawkEvent( { phase: "attack" }, { type: "animation-end" } ).phase, phases[row.after.mode] );
		} else assert.equal( row.after.damage, row.damage );
	}
	const c = createCombat();
	c.seedEffects( 1, [ { id: 78, token: 88, status: 0 } ] );
	c.seed( 456, { hp: 500 } );
	const p = new Uint8Array( 10 ), v = new DataView( p.buffer );
	v.setUint32( 0, 88, true );
	v.setUint32( 4, 456, true );
	v.setUint16( 8, 123, true );
	c.receive( 0x357a, p );
	assert.equal( c.state().vitals[0].hp, 377 );
	v.setUint16( 8, 0x8001, true );
	c.receive( 0x357a, p );
	assert.equal( c.state().vitals[0].hp, 0 );
});

test("native yaw retains zero-vector behavior, axes, quadrants and quotient float stores", () => {
	for ( const row of reference.directions ) {
		assert.equal( hawkYaw( row.vector[0], row.vector[2] ), row.yaw, JSON.stringify( row ) );
	}
});

test("hawk secondary copies primary placement and selects red or green without hit tint", () => {
	for ( const enabled of [ true, false ] ) {
		const f = fixture( 78, true, enabled );
		f.command();
		let now = .03, rows = f.step( now );
		while ( rows[0].clip !== "state-2" && now < 2 ) {
			now = Math.round( (now + .01) * 1000 ) / 1000;
			rows = f.step( now );
		}
		rows = f.step( now + .343 );
		const blood = rows.find( a => a.model.includes( enabled ? "test-red" : "test-green" ) ),
			primary = rows.find( a => a.gid !== rows[0].gid && a !== blood );
		assert.ok( blood );
		assert.ok( primary );
		assert.deepEqual( blood.pose, primary.pose );
		assert.equal( f.effects.appearance( 1 ).pointLight, undefined );
		assert.equal( f.effects.appearance( 2 ).pointLight, undefined );
		assert.equal( f.effects.error(), null );
		f.effects.dispose();
	}
});
