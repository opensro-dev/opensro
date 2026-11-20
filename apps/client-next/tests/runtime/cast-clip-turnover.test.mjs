/*
===========================================================================

cast-clip-turnover.test.mjs - a cast clock re-checks BAN admission per frame

Native 8E7150 resolves (set, state) on the caster's current model at
install and installs nothing when the branch is absent. A cast clock
freezes its phases at creation, when every BAN phase was admitted. When a
crowd pushes the caster out of the presented selection, its body is no
longer planned: the admitted catalogue turns over (residency retirement
and a fresh decode reset the renderer's clips and the duration table),
while the cast stays published and its clock lives on. Back in the
selection, the frozen phases would name a clip the fresh body does not
hold. The clock must re-check admission against the current body before
it publishes a layer, as the combat stance and stall motions do on every
frame, and re-arm the load while it is not admitted.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { presentationRig } = await import( "../helpers/presentation-fixture.mjs" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );

const FILLERS = 520;
const CASTER = 2, TARGET = 3;
const entity = ( gid, x, extra = {} ) => ({
	gid,
	refObjId: gid,
	kind: gid === 1 ? "local-player" : "player",
	tidWord: 0x26,
	regionId: 1,
	x,
	y: 0,
	z: 20,
	heading: 0,
	movementMode: 3,
	appearanceState: [ 1, 0, 0 ],
	...extra
});
const metadata = ( stateId, durationMs = 1000 ) => ({
	stateId,
	durationMs,
	loop: false,
	soundEvents: [],
	trackEvents: [],
	timeWarpCurve: { scale: 0, records: [] }
});
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
/*
================
ban

One BAN clip: two keys on the "root" bone, as in combat-stance.test.mjs.
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

test("a cast clock re-arms its BAN after the caster body catalogue turns over", () => {
	let next = 0;
	const pending = new Map(), requests = [];
	const clips = [ "stand", "walk", "run", "ride", "sit", "death", "skill_1" ];
	const states = Object.fromEntries( clips.map( name => [ name, metadata( 0 ) ] ) );
	const json = value => ({ kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( value ) ).buffer });
	// One shared megabyte of geometry: every filler body costs a megabyte of
	// resident budget without the test allocating it five hundred times.
	const fillerGeometry = new Float32Array( 262143 );
	// The caster's own body, just under the single-source decode budget:
	// unselected and inactive, it is the body the cache retires.
	const casterGeometry = new Float32Array( 4188000 );
	const model = ( url = "" ) => ({
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
				positions: url.endsWith( "/2.glb" ) ?
					casterGeometry :
					url.endsWith( "/3.glb" ) || /\/f\d+\.glb$/.test( url ) ?
					fillerGeometry :
					new Float32Array( 9 ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				transform: identity()
			}
		} ]
	});
	const BAN_URL = "/assets/anim/ready.ban";
	const effects = {
		7: {
			clips: [],
			phaseClips: [ [ "native:default:42" ], [ "native:default:42" ], [ "native:default:42" ] ],
			stages: []
		}
	};
	const assets = {
		available: () => 100000,
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
				return { kind: "character", model: model( job.url ), images: [] };
			}
			if ( job.url.endsWith( "/anim/manifest.json" ) ) {
				return json( {
					models: {
						CHAR_2: {
							animationSets: { default: { 42: { ...metadata( 42, 30000 ), url: BAN_URL } } }
						}
					}
				} );
			}
			if (
				job.url.endsWith( "/data/skillAudioData.json" ) || job.url.endsWith( "/data/characterActionData.json" )
			) return json( { models: {} } );
			if ( job.url.endsWith( "/skillfx/manifest.json" ) ) {
				return json( { format: "sro-skill-stage-models", models: {} } );
			}
			if ( job.url.endsWith( "/itemdrop/manifest.json" ) ) {
				return json( { format: "sro-mission-itemdrop-models", models: {} } );
			}
			if ( job.url.endsWith( "/audio/effectsound.json" ) ) return json( { rules: [] } );
			return json( {
				models: [
					{ refObjId: 1, codename: "CHAR_1", glb: "/assets/1.glb", clips, animationStates: states },
					{ refObjId: CASTER, codename: "CHAR_2", glb: "/assets/2.glb", clips, animationStates: states },
					{ refObjId: TARGET, codename: "CHAR_3", glb: "/assets/3.glb", clips, animationStates: states },
					...Array.from( { length: FILLERS }, ( _, i ) => ({
						refObjId: 100 + i,
						codename: "CHAR_F" + i,
						glb: `/assets/f${i}.glb`,
						clips,
						animationStates: states
					}) )
				]
			} );
		}
	};
	const rig = presentationRig( {
		assets,
		random: createPresentationRandom( 1 ),
		play: () => {},
		port: characters => ({ setCharacterAnimation: characters.animation })
	} );
	const cast = () => ({
		token: 900,
		caster: CASTER,
		target: TARGET,
		skill: 7,
		damage: 0,
		fatal: false,
		results: [],
		receivedAtMs: 1000
	});
	const home = [
		entity( 1, 10 ),
		entity( CASTER, 5200 ),
		entity( TARGET, 30, { kind: "monster", tidWord: 0x46 } )
	];
	const crowd = [
		entity( 1, 10 ),
		entity( TARGET, 30, { kind: "monster", tidWord: 0x46 } ),
		...Array.from( { length: FILLERS }, ( _, i ) => entity( 100 + i, 100 + i * 9 ) ),
		entity( CASTER, 5200 )
	];
	/*
	================
	step
	================
	*/
	function step( time, casts, rows ) {
		rig.presentation.step(
			rows,
			{
				localGid: 1,
				pose: { ...rows.find( row => row.gid === 1 ), angle: 0 },
				inventory: [],
				casts,
				vitals: [],
				skills: [],
				moving: false
			},
			time,
			time * 1000
		);
		rig.prepare();
		return rig.characters.takeDegradations();
	}
	const banRequests = () => requests.filter( url => url.endsWith( ".ban" ) ).length;
	const casterGlbRequests = () => requests.filter( url => url.endsWith( "/2.glb" ) ).length;
	const casterRow = () => rig.actors.find( row => row.gid === CASTER );
	const casterLayer = () => casterRow()?.layers?.some( layer => layer.clip === "native:default:42" ) ?? false;
	try {
		const seen = [];
		for ( let i = 0; i < 60; i++ ) seen.push( ...step( i / 100, [], home ) );
		// The cast clock forms once the BAN is admitted on the caster's body.
		for ( let i = 0; i < 30; i++ ) seen.push( ...step( 1 + i / 100, [ cast() ], home ) );
		assert.equal( casterLayer(), true, "cast layer published after admission" );
		assert.equal( banRequests(), 1, "one BAN fetch admitted the phase clip" );
		assert.equal( casterGlbRequests(), 1 );
		// A nearer crowd fills the presented selection: the caster leaves it
		// while its cast stays published, and its body catalogue turns over.
		for ( let i = 0; i < 250; i++ ) seen.push( ...step( 2 + i / 100, [ cast() ], crowd ) );
		// The crowd leaves; the caster is presented again under the same cast.
		for ( let i = 0; i < 60; i++ ) seen.push( ...step( 5 + i / 100, [ cast() ], home ) );
		assert.equal( casterGlbRequests(), 2, "the caster body turned over while unselected" );
		assert.equal( banRequests(), 2, "the clock re-armed the BAN on the fresh body" );
		assert.equal( casterLayer(), true, "the cast layer resumed after re-admission" );
		assert.deepEqual(
			seen.filter( d => d.code === "animation-clip-missing" ),
			[],
			"no layer ever named a clip the current body does not hold"
		);
	} finally {
		rig.dispose();
	}
});
