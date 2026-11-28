/*
===========================================================================

presentation-due-scene.test.mjs - skipped work changes nothing, on an invented scene

presentation-due.test.mjs proves this on the replay tool's settle scenario,
which reads the built game assets. This file proves it with none: the
scene of tests/helpers/presentation-scene.mjs (invented bodies, items, an
effect program and the smallest GLBs the decoder admits) is served to the
replay harness through its source hook. A standing crowd stays settled to
the end; one peer each walks, changes its equipment, sits and stands,
dies, rides a horse, takes a freeze and a slow, leaves view for one frame,
attacks a monster, emotes, picks up, hosts attached motions, goes berserk,
or wears what an arena team or a job-suit rule changes; two monsters
carry a glow particle, one leaves, and the camera pulls back past the
distance that hides the other's (sceneSession lists them).

The scene runs with the production rules (presentation-due.ts) and with
always-due rules that resolve, decide and build the actor of every entity
every frame: every frame's outputs must be the same, the forced run must
skip nothing and the production run must skip most of each rule's answers.
Each run's published actors also enter two of the renderer's snapshot
owners, one that copies only the clocks of a frozen actor whose other
fields did not change and one that copies every actor in full: every
frame's rows must be the same. Three contracts are checked every frame:
an attached actor's holder is published with it, a model particle hides
exactly when its holder is past nine tenths of the detail distance
(model-emission.ts; the scene's holders select no override), and every
published model is in the set the renderer was last handed to retain.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../../../.." );
const { sceneSession, sceneSources } = await import( "../helpers/presentation-scene.mjs" );
const { createPresentationHarness } = await import( "../../tools/perf/presentation/harness.mjs" );
const { PRESENTATION_DUE } = await import( "../../src/engine/runtime/characters/presentation-due.ts" );
const { createActorSnapshots } = await import( "../../src/engine/runtime/renderer/characters/actor-snapshots.ts" );
const { sameRow } = await import( "../helpers/renderer-row.mjs" );
const { clocked } = await import( "../../src/engine/foundation/animation/actor-clock.ts" );
const SEED = 1;
const RULES = [ "appearance", "motion", "record" ];
// The share of rule answers the production run must skip: the crowd
// stands still for most of the scene.
const SKIPPED_SHARE = .5;
// The detail fraction past which a model particle hides (model-emission.ts).
const PARTICLE_HIDDEN_FRACTION = Math.fround( .9 );
// The share of renderer copies that must copy only clocks: the crowd
// stands still for most of the scene, and a rebuilt actor keeps its
// unchanged value objects (actor-advance.ts carried) and every published
// actor is frozen (presentation-finalize.ts): 95% of copies here.
const CLOCK_COPY_SHARE = .93;
// The share of a body's publications that are its last actor object again:
// 75% on the scene, whose walkers and record refreshes rebuild the rest.
const STEADY_REPUBLISHED_SHARE = .72;

/*
================
freezeDeep

The publication contract a frozen actor makes (actor-advance.ts): nothing
it references changes after it is published but its clocks. Every object
a frozen published actor reaches is frozen here, so a producer that
changes one later throws, and the presentation reports the frame's error.
================
*/
function freezeDeep( actor ) {
	if ( !Object.isFrozen( actor ) ) return;
	const walk = value => {
		if ( value === null || typeof value !== "object" || ArrayBuffer.isView( value ) || Object.isFrozen( value ) ) {
			return;
		}
		Object.freeze( value );
		for ( const nested of Object.values( value ) ) walk( nested );
	};
	for ( const [key, value] of Object.entries( actor ) ) {
		if ( key !== "layers" && key !== "modelAnimation" ) walk( value );
	}
}

/*
================
replay

Every frame's outputs of one run of the scene, encoded; how often each
rule was asked and answered not due; the presentation errors seen; the
first frame whose two snapshot owners' rows differ, and the first frame
that breaks either contract.
================
*/
async function replay( rules ) {
	const asked = { appearance: 0, motion: 0, record: 0 }, skipped = { appearance: 0, motion: 0, record: 0 };
	const counted = kind => ( retained, stamp ) => {
		asked[kind]++;
		const due = rules[kind]( retained, stamp );
		if ( !due ) skipped[kind]++;
		return due;
	};
	const reused = createActorSnapshots(), full = createActorSnapshots( false );
	let frame = 0, rowsDiffer = -1, published = [];
	// Each frame's actors by gid, and how often a body's actor was the same
	// object as on the frame before (republished) or a new one (rebuilt).
	let lastByGid = new Map(), republishedCount = 0, rebuiltCount = 0;
	const sources = sceneSources();
	const harness = await createPresentationHarness( ROOT, sceneSession( SEED ), {
		due: { appearance: counted( "appearance" ), motion: counted( "motion" ), record: counted( "record" ) },
		source: pathname => sources.get( pathname ) ?? null,
		actors: ( actors, portraits = [], clocks = new Map() ) => {
			for ( const actor of actors ) freezeDeep( actor );
			for ( const clock of clocks.values() ) freezeDeep( clock );
			for ( const actor of actors ) {
				// Bodies only: stage effects and particles are rebuilt until
				// they adopt the table (#4751), a fade is a copy by design.
				if ( actor.effectEntity || actor.deferredParticle || actor.attachment || actor.opacity !== undefined ) {
					continue;
				}
				if ( lastByGid.get( actor.gid ) === actor ) republishedCount++;
				else if ( lastByGid.has( actor.gid ) ) rebuiltCount++;
			}
			lastByGid = new Map( actors.map( actor => [ actor.gid, actor ] ) );
			published = [ ...actors.map( actor => clocked( actor, clocks ) ), ...portraits ];
			if ( !sameRow( reused.update( actors, clocks ), full.update( actors, clocks ) ) && rowsDiffer < 0 ) {
				rowsDiffer = frame;
			}
		}
	} );
	const frames = [], errors = new Set(), broken = [];
	let retained = new Set();
	try {
		for ( ; frame < harness.frames; frame++ ) {
			published = [];
			const out = harness.step( frame );
			if ( out.reads.error !== null ) errors.add( out.reads.error );
			frames.push( JSON.stringify( out ) );
			// The harness records each call that changed the renderer's set.
			const calls = /** @type {{ retainCharacterModels?: string[][][] }} */ (out).retainCharacterModels;
			const handed = calls?.at( -1 );
			if ( handed ) retained = new Set( handed[0] );
			const gids = new Set( published.map( actor => actor.gid ) );
			const holders = new Map( published.map( actor => [ actor.gid, actor ] ) );
			const misplaced = published.find( actor => {
				const holder = actor.attachment?.basis === "bsr" ? holders.get( actor.attachment.gid ) : undefined;
				return holder?.animationLod && !holder.modelAnimation?.selected?.override &&
					actor.deferredParticle?.lodHidden !== holder.animationLod.fraction > PARTICLE_HIDDEN_FRACTION;
			} );
			const orphan = published.find( actor => actor.attachment && !gids.has( actor.attachment.gid ) ),
				unretained = published.find( actor => !retained.has( actor.model ) );
			if ( broken.length < 2 && orphan ) {
				broken.push( `frame ${frame}: ${orphan.gid} rides absent ${orphan.attachment.gid}` );
			}
			if ( broken.length < 2 && unretained ) broken.push( `frame ${frame}: ${unretained.model} not retained` );
			if ( broken.length < 2 && misplaced ) {
				broken.push( `frame ${frame}: particle ${misplaced.gid} hidden wrong` );
			}
		}
	} finally {
		harness.dispose();
	}
	return {
		frames,
		asked,
		skipped,
		errors: [ ...errors ],
		rowsDiffer,
		broken,
		copies: reused.stats(),
		republished: republishedCount / Math.max( 1, republishedCount + rebuiltCount )
	};
}

/*
================
TestSceneSkippedWorkMatchesForced
================
*/
test("on the invented scene, skipped resolves, decides and actor builds present every frame as doing them all does", async () => {
	const always = () => true;
	const forced = await replay( { appearance: always, motion: always, record: always } ),
		production = await replay( PRESENTATION_DUE );
	assert.deepEqual( forced.errors, [] );
	assert.deepEqual( forced.skipped, { appearance: 0, motion: 0, record: 0 } );
	assert.equal( production.frames.length, forced.frames.length );
	for ( let i = 0; i < forced.frames.length; i++ ) {
		assert.equal( production.frames[i], forced.frames[i], `frame ${i}` );
	}
	for ( const run of [ forced, production ] ) {
		assert.equal( run.rowsDiffer, -1, `renderer rows differ from frame ${run.rowsDiffer}` );
		assert.deepEqual( run.broken, [] );
	}
	const { copies, clockCopies } = production.copies;
	assert.ok(
		clockCopies > (copies + clockCopies) * CLOCK_COPY_SHARE,
		`${clockCopies} of ${copies + clockCopies} copied clocks only`
	);
	// A steady actor is the same object again, its clocks in the table
	// (actor-clock.ts); the forced run builds every actor every frame.
	assert.ok(
		production.republished >= STEADY_REPUBLISHED_SHARE,
		`${production.republished} of body publications republished the last object`
	);
	// The forced run builds every body each frame; only a body held while its
	// model loads (actor-appearance.ts) is its last object again.
	assert.ok( forced.republished < .001, `${forced.republished} of forced publications republished` );
	for ( const kind of RULES ) {
		assert.ok(
			production.skipped[kind] > production.asked[kind] * SKIPPED_SHARE,
			`${production.skipped[kind]} of ${production.asked[kind]} ${kind} rule answers skipped`
		);
	}
});
