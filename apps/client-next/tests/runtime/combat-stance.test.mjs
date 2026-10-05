/*
===========================================================================

combat-stance.test.mjs - attack-stance entry on casts and hits

Native 8E5ADE blends stance changes over 200 ms. These tests drive the
character presentation through casts and hits and check the stance it
enters and keeps.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { combatStanceOnCast, combatStanceOnHit } = await import(
	"../../src/engine/foundation/animation/combat-stance.ts"
);
const { createCharacterPresentation } = await import( "../../src/engine/runtime/characters/characters.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { skillMotionResolveAnimation } = await import( "../../src/engine/foundation/animation/skill-motion-resolve.ts" );
const impact = ( extra = {} ) => ({ type: 0, flags: 0, damage: 10, fatal: false, secondaryAmount: 0, ...extra });
const player = { kind: "player", tidWord: 0x26 };
const entity = ( gid, extra = {} ) => ({
	gid,
	refObjId: gid,
	kind: gid === 1 ? "local-player" : "player",
	tidWord: 0x26,
	regionId: 1,
	x: gid * 10,
	y: 0,
	z: 20,
	heading: 0,
	movementMode: 3,
	appearanceState: [ 1, 0, 0 ],
	...extra
});
const metadata = ( stateId ) => ({
	stateId,
	durationMs: 1000,
	loop: true,
	soundEvents: [],
	trackEvents: [],
	timeWarpCurve: { scale: 0, records: [] }
});
const cast = ( extra = {} ) => ({
	token: 10,
	caster: 1,
	target: 2,
	skill: 7,
	damage: 0,
	fatal: false,
	results: [],
	receivedAtMs: 1000,
	...extra
});
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
/*
================
ban
================
*/
function ban() {
	const rows = [],
		u32 = n => {
			const b = Buffer.alloc( 4 );
			b.writeUInt32LE( n );
			rows.push( b );
		},
		text = s => {
			u32( Buffer.byteLength( s ) );
			rows.push( Buffer.from( s ) );
		};
	rows.push( Buffer.from( "JMXVBAN 0102" ) );
	u32( 0 );
	u32( 0 );
	text( "combat" );
	u32( 1000 );
	u32( 0 );
	u32( 0 );
	u32( 2 );
	u32( 0 );
	u32( 1000 );
	u32( 1 );
	text( "root" );
	u32( 2 );
	for ( let i = 0; i < 2; i++ ) {
		for ( const n of [ 0, 0, 0, 1, 0, 0, 0 ] ) {
			const b = Buffer.alloc( 4 );
			b.writeFloatLE( n );
			rows.push( b );
		}
	}
	const out = Buffer.concat( rows );
	return out.buffer.slice( out.byteOffset, out.byteOffset + out.byteLength );
}
/*
================
fixture
================
*/
function fixture(
	{ wait = false, effectOnly = false, missingMotion = false, nativeBan = false, crossbow = false } = {}
) {
	let next = 0, actors = [];
	const pending = new Map(), requests = [], installed = [];
	const clips = [
		"stand",
		"walk",
		"run",
		"ride",
		"sit",
		"death",
		"skill_1",
		"wait",
		"idle122",
		"idle61",
		"idle81",
		"preview-state0-sword",
		...(!missingMotion && !nativeBan ? [ "combat" ] : [])
	];
	const states = Object.fromEntries(
		clips.map(
			name => [ name, metadata( name === "combat" ? 6 : name === "skill_1" ? 26 : name === "wait" ? 91 : 0 ) ]
		)
	);
	const json = value => ({ kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( value ) ).buffer });
	const model = () => ({
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		clips: clips.map( name => ({ name, duration: 1, channels: [] }) ),
		images: [],
		primitives: [ {
			name: "mesh",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			geometry: {
				positions: new Float32Array( 9 ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				transform: identity()
			}
		} ]
	});
	const effects = {
		7: {
			clips: [],
			phaseClips: effectOnly ? [ [], [], [] ] : [ [], wait ? [ "wait" ] : [], [ "skill_1" ] ],
			stages: []
		}
	};
	const assets = {
		available: () => 4,
		/*
		================
		request
		================
		*/
		request( url, limit, decode ) {
			const id = ++next;
			pending.set( id, { url, decode } );
			requests.push( url );
			return id;
		},
		/*
		================
		cancel
		================
		*/
		cancel( id ) {
			pending.delete( id );
		},
		/*
		================
		take
		================
		*/
		take( id ) {
			const job = pending.get( id );
			if ( !job ) return null;
			pending.delete( id );
			if ( job.url.endsWith( ".ban" ) ) return { kind: "bytes", buffer: ban() };
			if ( job.decode === "effects" ) return { kind: "effects", catalog: effects };
			if ( job.decode === "character" || job.decode === "effect" ) {
				return {
					kind: "character",
					model: model(),
					images: []
				};
			}
			if ( job.url.endsWith( "/anim/manifest.json" ) ) {
				return json( {
					models: nativeBan ?
						Object.fromEntries(
							[ 1, 2, 3 ].map(
								id => [ "CHAR_" + id, {
									animationSets: {
										default: { 6: { ...metadata( 6 ), url: "/assets/anim/combat.ban" } },
										bow: { 6: { ...metadata( 6 ), url: "/assets/anim/bow-ready.ban" } }
									}
								} ]
							)
						) :
						{}
				} );
			}
			if (
				job.url.endsWith( "/data/skillAudioData.json" ) || job.url.endsWith( "/data/characterActionData.json" )
			) {
				return json( { models: {} } );
			}
			if ( job.url.endsWith( "/skillfx/manifest.json" ) ) {
				return json( {
					format: "sro-skill-stage-models",
					models: {}
				} );
			}
			if ( job.url.endsWith( "/itemdrop/manifest.json" ) ) {
				return json( {
					format: "sro-mission-itemdrop-models",
					models: {}
				} );
			}
			if ( job.url.endsWith( "/audio/effectsound.json" ) ) return json( { rules: [] } );
			return json( {
				// The crossbow needs only a catalog entry; no visual socket.
				...(crossbow ? { dress: { equipment: { 9000: { slot: null, bodies: {} } } } } : {}),
				models: [ 1, 2, 3 ].map( refObjId => ({
					refObjId,
					codename: "CHAR_" + refObjId,
					glb: `/assets/${refObjId}.glb`,
					clips,
					animationStates: states
				}) )
			} );
		}
	};
	const renderer = {
		setCharacterModel() {},
		/*
		================
		setCharacterAnimation
		================
		*/
		setCharacterAnimation( body, name, source ) {
			installed.push( { body, name, source } );
			return 4096;
		},
		retainCharacterModels() {},
		/*
		================
		setCharacterActors
		================
		*/
		setCharacterActors( value ) {
			actors = value;
		},
		presentationNight: () => false,
		characterSocket: () => null
	};
	const presentation = createCharacterPresentation(
		assets,
		renderer,
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entities = [ entity( 1 ), entity( 2 ), entity( 3, { kind: "monster", tidWord: 0x46 } ) ];
	/*
	================
	step
	================
	*/
	function step( time, casts = [], rows = entities, simulationMs = time * 1000 ) {
		const local = rows.find( row => row.gid === 1 );
		presentation.step(
			rows,
			{
				localGid: 1,
				pose: local ? { ...local, angle: local.heading } : undefined,
				// A worn EU crossbow (item type 12 in bits 11..15) in the weapon slot.
				inventory: crossbow ? [ { slot: 6, refObjId: 9000, typeFlags: 12 << 11, plus: 0 } ] : [],
				casts,
				vitals: [],
				skills: [],
				moving: local?.moving ?? false
			},
			time,
			simulationMs
		);
		assert.equal( presentation.error(), null );
		return actors.find( row => row.gid === 1 );
	}
	for ( let i = 0; i < 60; i++ ) step( i / 100 );
	assert.equal( actors.filter( row => row.gid > 0 && row.gid <= 3 ).length, 3, "all fixture bodies admitted" );
	return {
		step,
		entities,
		presentation,
		requests,
		installed,
		actor: ( gid = 1 ) => actors.find( row => row.gid === gid ),
		dispose: () => presentation.dispose()
	};
}

for ( const kind of [ "player", "local-player" ] ) {
	test(`cast trigger accepts classified ${kind} without raw type`, () => {
		assert.equal( combatStanceOnCast( { kind }, true, false ), true );
	});
}
for (
	const [tidWord, expected] of [ [ 0x26, true ], [ 0x1c6, true ], [ 0x46, false ], [ 0x66, false ], [ 0x16, false ], [
		0,
		false
	] ]
) {
	test(`native cast family ${tidWord.toString( 16 )}`, () => {
		assert.equal( combatStanceOnCast( { kind: "player", tidWord }, true, false ), expected );
		assert.equal( combatStanceOnCast( { kind: "player", tidWord }, true, true ), false );
		assert.equal( combatStanceOnCast( { kind: "player", tidWord }, false, false ), false );
	});
}
for ( const type of [ 0, 1, 2, 3, 4, 5, 6, 7 ] ) {
	test(`hit subtype ${type}: exclusion rule and zero damage`, () => {
		assert.equal( combatStanceOnHit( player, true, impact( { type, damage: 0 } ) ), type !== 2 && type !== 7 );
		assert.equal( combatStanceOnHit( player, false, impact( { type } ) ), false );
		assert.equal( combatStanceOnHit( player, true, impact( { type, fatal: true } ) ), false );
		assert.equal( combatStanceOnHit( { kind: "monster", tidWord: 0x46 }, true, impact( { type } ) ), false );
	});
}
test("motion lookup does not substitute weapon preview state 0 for DEFAULT state 6", () => {
	const input = {
		role: "native:default:6",
		clips: [ "stand", "preview-state0-sword", "combat" ],
		bodyStates: { stand: metadata( 0 ), "preview-state0-sword": metadata( 0 ), combat: metadata( 6 ) }
	};
	assert.equal( defined( skillMotionResolveAnimation( input ) ).clip, "combat" );
	assert.equal( skillMotionResolveAnimation( { ...input, clips: [ "stand", "preview-state0-sword" ] } ), undefined );
});
test("cast holds combat idle after retirement, expires at five seconds, and blends for 200ms", () => {
	const f = fixture();
	try {
		assert.equal( f.step( 1, [ cast() ] ).clip, "combat" );
		assert.equal( f.step( 2.1 ).clip, "combat", "cast removal does not erase stance" );
		assert.equal( f.step( 5.99 ).clip, "combat" );
		assert.equal( f.step( 6 ).clip, "stand" );
		const layers = f.step( 6.1 ).layers;
		assert.ok( layers.some( layer => layer.clip === "combat" && layer.weight > 0 ) );
		assert.ok( layers.some( layer => layer.clip === "stand" && layer.weight > 0 ) );
		assert.ok( !f.step( 6.21 ).layers?.some( layer => layer.clip === "combat" ) );
	} finally {
		f.dispose();
	}
});
test("same cast on repeated snapshots does not refresh the deadline", () => {
	const f = fixture();
	try {
		for ( const time of [ 1, 2, 3, 4, 5 ] ) f.step( time, [ cast() ] );
		assert.equal( f.step( 6, [ cast() ] ).clip, "stand" );
	} finally {
		f.dispose();
	}
});
test("a new qualifying cast refreshes the deadline without restarting the same idle installation", () => {
	const f = fixture();
	try {
		f.step( 1, [ cast() ] );
		f.step( 1.5 );
		f.step( 4, [ cast( { token: 11, receivedAtMs: 4000 } ) ] );
		const row = f.step( 4.1, [ cast( { token: 11, receivedAtMs: 4000 } ) ] );
		assert.equal( row.layers.find( layer => layer.clip === "combat" ).activation.started, 1 );
		assert.equal( f.step( 6 ).clip, "combat" );
		assert.equal( f.step( 9 ).clip, "stand" );
	} finally {
		f.dispose();
	}
});
for ( const options of [ { wait: true }, { effectOnly: true } ] ) {
	test(`no stance for ${options.wait ? "secondary WAIT" : "effect-only"} instance`, () => {
		const f = fixture( options );
		try {
			assert.equal( f.step( 1, [ cast() ] ).clip, "stand" );
			assert.equal( f.step( 2 ).clip, "stand" );
		} finally {
			f.dispose();
		}
	});
}
test("late cast admission uses the original simulation timestamp, not five fresh seconds", () => {
	const f = fixture();
	try {
		assert.equal( f.step( 100, [ cast( { receivedAtMs: 1000 } ) ], f.entities, 6000 ).clip, "stand" );
	} finally {
		f.dispose();
	}
});
test("running and sitting take priority without discarding the remaining stance", () => {
	const f = fixture();
	try {
		f.step( 1, [ cast() ] );
		const moving = f.entities.map( e => e.gid === 1 ? { ...e, moving: true } : e );
		assert.equal( f.step( 2, [], moving ).clip, "run" );
		assert.equal( f.step( 3 ).clip, "combat" );
		const sitting = f.entities.map( e => e.gid === 1 ? { ...e, movementMode: 4 } : e );
		assert.equal( f.step( 3.5, [], sitting ).clip, "sit" );
		assert.equal( f.step( 4 ).clip, "combat" );
		assert.equal( f.step( 6 ).clip, "stand" );
	} finally {
		f.dispose();
	}
});
for ( const type of [ 0, 2, 7 ] ) {
	test(`applied zero-damage subtype ${type} ${type === 0 ? "refreshes" : "does not refresh"} victim stance`, () => {
		const f = fixture( { effectOnly: true } );
		try {
			const hit = impact( { type, damage: 0 } ),
				c = cast( { caster: 3, target: 1, results: [ { target: 1, impacts: [ hit ] } ], cancelledAtMs: 1000 } );
			assert.equal( f.step( 1, [ c ] ).clip, type === 0 ? "combat" : "stand" );
			assert.equal( f.step( 5 ).clip, type === 0 ? "combat" : "stand" );
			assert.equal( f.step( 6 ).clip, "stand" );
		} finally {
			f.dispose();
		}
	});
}
test("damage receipt alone does not start stance before result application", () => {
	const f = fixture( { effectOnly: true } );
	try {
		const c = cast( { caster: 3, target: 1, results: [ { target: 1, impacts: [ impact() ] } ] } );
		assert.equal( f.step( 1, [ c ] ).clip, "stand" );
		assert.equal( f.step( 2, [ { ...c, cancelledAtMs: 2000 } ] ).clip, "combat" );
		assert.equal( f.step( 6.9 ).clip, "combat" );
		assert.equal( f.step( 7 ).clip, "stand" );
	} finally {
		f.dispose();
	}
});
test("fidgets are suppressed in combat and for a full fifteen seconds after the expiry tick", () => {
	const f = fixture();
	try {
		f.step( 1, [ cast() ] );
		f.step( 2 );
		f.step( 5 );
		f.step( 6 );
		const noFidget = row => assert.ok( !row.layers?.some( layer => /^idle/.test( layer.clip ) ) );
		noFidget( f.step( 20.9 ) );
		noFidget( f.step( 20.99 ) );
		assert.ok( f.step( 21.1 ).layers.some( layer => /^idle/.test( layer.clip ) ) );
	} finally {
		f.dispose();
	}
});
test("missing state 6 falls back to stand, not preview, while retaining fidget suppression", () => {
	const f = fixture( { missingMotion: true } );
	try {
		assert.equal( f.step( 1, [ cast() ] ).clip, "stand" );
		f.step( 5 );
		f.step( 6 );
		assert.ok( !f.step( 20.9 ).layers?.some( layer => /^idle/.test( layer.clip ) ) );
	} finally {
		f.dispose();
	}
});
test("published DEFAULT state 6 BAN is admitted on the body even when absent from resident clips", () => {
	const f = fixture( { nativeBan: true } );
	try {
		for ( let i = 0; i < 15; i++ ) f.step( 1 + i / 100, [ cast() ] );
		assert.ok( f.requests.some( url => url.endsWith( "/anim/combat.ban" ) ) );
		assert.ok( f.installed.some( row => row.body === "/assets/1.glb" && row.name === "native:default:6" ) );
		assert.equal( f.actor().clip, "native:default:6" );
		assert.equal( f.step( 6 ).clip, "stand" );
	} finally {
		f.dispose();
	}
});
test("combat stance plays the weapon set's state 6: a crossbow holds its ready stance", () => {
	// 8E5ADE -> CCObjCharacter_PlayAnimation (8EADF0) resolves state 6 in the
	// active weapon prefix before DEFAULT; DEFAULT is the bare-hand stance.
	const f = fixture( { nativeBan: true, crossbow: true } );
	try {
		for ( let i = 0; i < 15; i++ ) f.step( 1 + i / 100, [ cast() ] );
		assert.ok( f.requests.some( url => url.endsWith( "/anim/bow-ready.ban" ) ) );
		assert.equal( f.actor().clip, "native:bow:6" );
	} finally {
		f.dispose();
	}
});
test("despawn/reuse and lifecycle reset discard stance independently of cast retirement", () => {
	for ( const event of [ { kind: "despawn", gid: 1 }, { kind: "spawn", entity: entity( 1 ) }, { kind: "reset" } ] ) {
		const f = fixture();
		try {
			f.step( 1, [ cast() ] );
			f.presentation.receiveLifecycle( [ event ] );
			assert.equal( f.step( 2 ).clip, "stand" );
		} finally {
			f.dispose();
		}
	}
});
test("death clears stance so revival does not inherit an old combat hold", () => {
	const f = fixture();
	try {
		f.step( 1, [ cast() ] );
		const dead = f.entities.map( e => e.gid === 1 ? { ...e, appearanceState: [ 2, 0, 0 ] } : e );
		assert.equal( f.step( 2, [], dead ).clip, "death" );
		assert.equal( f.step( 3 ).clip, "stand" );
	} finally {
		f.dispose();
	}
});

test("temporary linked results apply to the victim without starting a skill-0 action or attacker stance", () => {
	const f = fixture();
	try {
		const c = cast( {
			token: -1,
			skill: 0,
			resultOnly: true,
			cancelledAtMs: 1000,
			results: [ { target: 2, impacts: [ impact() ] } ]
		} );
		assert.equal( f.step( 1, [ c ] ).clip, "stand" );
		assert.equal( f.actor( 2 ).clip, "combat" );
		assert.equal( f.presentation.damageText().length, 1 );
		f.step( 1.1, [ c ] );
		assert.equal( f.presentation.damageText().length, 1 );
	} finally {
		f.dispose();
	}
});
test("network-only deferred cancellation leaves the real presenter motion running and does not flush damage", () => {
	const f = fixture();
	try {
		const c = cast( { cancellationDeferred: true, results: [ { target: 2, impacts: [ impact() ] } ] } );
		f.step( 1, [ c ] );
		const before = f.step( 1.1, [ c ] ).layers.find( layer => layer.clip === "skill_1" );
		assert.ok( before );
		const after = f.step( 1.2, [ { ...c, cancellationRequestedAtMs: 1200 } ] );
		assert.ok( after.layers.some( layer => layer.clip === "skill_1" && layer.weight > 0 ) );
		assert.equal(
			after.layers.find( layer => layer.clip === "skill_1" ).activation.started,
			before.activation.started
		);
		assert.equal( f.presentation.damageText().length, 0 );
	} finally {
		f.dispose();
	}
});
test("death exits an exempt caster motion but does not auto-flush the retained skill results", () => {
	const f = fixture();
	try {
		const c = cast( { cancellationDeferred: true, results: [ { target: 2, impacts: [ impact() ] } ] } );
		f.step( 1, [ c ] );
		const dead = f.entities.map( e => e.gid === 1 ? { ...e, appearanceState: [ 2, 0, 0 ] } : e );
		assert.equal( f.step( 1.2, [ c ], dead ).clip, "death" );
		assert.equal( f.presentation.damageText().length, 0 );
		const actor = f.step( 1.5, [ c ], dead );
		assert.ok( !actor.layers?.some( layer => layer.clip === "skill_1" && layer.weight > 0 ) );
		assert.equal( f.presentation.damageText().length, 0 );
	} finally {
		f.dispose();
	}
});
test("ordinary death still flushes pending cast results once", () => {
	const f = fixture();
	try {
		const c = cast( { results: [ { target: 2, impacts: [ impact() ] } ] } );
		f.step( 1, [ c ] );
		const dead = f.entities.map( e => e.gid === 1 ? { ...e, appearanceState: [ 2, 0, 0 ] } : e );
		f.step( 1.2, [ c ], dead );
		assert.equal( f.presentation.damageText().length, 1 );
		f.step( 1.5, [ c ], dead );
		assert.equal( f.presentation.damageText().length, 1 );
	} finally {
		f.dispose();
	}
});
test("actual linked worker snapshots neither restart the root motion nor refresh its stance", async () => {
	const { createCombat } = await import(
		"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
	);
	const worker = createCombat();
	worker.references( [ { id: 7, status: false, effectRider: false }, {
		id: 8,
		status: false,
		effectRider: false,
		linkedSkillId: 7
	} ] );
	const packet = ( skill, token ) => {
		const p = new Uint8Array( 19 ), v = new DataView( p.buffer );
		p[0] = 1;
		v.setUint32( 2, skill, true );
		v.setUint32( 6, 1, true );
		v.setUint32( 10, token, true );
		v.setUint32( 14, 2, true );
		return p;
	};
	const f = fixture();
	try {
		worker.receive( 0xb245, packet( 7, 10 ), 1000 );
		f.step( 1, worker.state().casts );
		const start = f.step( 1.1, worker.state().casts ).layers.find( layer =>
			layer.clip === "skill_1"
		).activation.started;
		worker.receive( 0xb245, packet( 8, 11 ), 1200 );
		assert.equal( worker.state().casts.length, 1 );
		const after = f.step( 1.2, worker.state().casts );
		assert.equal( after.layers.find( layer => layer.clip === "skill_1" ).activation.started, start );
		worker.receive( 0xb245, packet( 8, 12 ), 4000 );
		f.step( 4, worker.state().casts );
		assert.equal( f.step( 6, worker.state().casts ).clip, "stand" );
	} finally {
		f.dispose();
	}
});
