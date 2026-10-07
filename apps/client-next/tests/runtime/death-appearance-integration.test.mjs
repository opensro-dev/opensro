/*
===========================================================================
death-appearance-integration.test.mjs - death resource ownership through revival

Connects production JSON catalog admission, death entry and appearance lookup.
The resource selected for actor assembly must change without replacing the
character's identity, scale or sound profile, then restore after revival.
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createPresentationCatalog } = await import( "../../src/engine/runtime/characters/presentation-catalog.ts" );
const { createPresentationState } = await import( "../../src/engine/runtime/characters/presentation-state.ts" );
const { createAppearanceLookup } = await import( "../../src/engine/runtime/characters/appearance-lookup.ts" );

const MANIFEST = "/assets/npc/manifest.json";
const BODY = "/assets/npc/body.glb";
const CORPSE = "/assets/npc/corpse.glb";

test("admitted death resources replace body appearance and revival restores its authored variants", () => {
	const accepted = [], rejected = [];
	const referenceAppearances = {
		setReferences() {},
		skin: () => undefined,
		get: () => undefined
	};
	const published = createPresentationCatalog( {
		resources: {
			accepted: path => accepted.push( path ),
			rejected: ( path, error ) => rejected.push( { path, error } )
		},
		sounds: { catalog() {} },
		referenceAppearances
	} );
	const state = createPresentationState();
	const lookup = createAppearanceLookup( {
		published,
		referenceAppearances,
		structureVisuals: { appearance: () => undefined },
		deathShown: gid => state.idleStates.get( gid )?.deathModel === true
	} );
	const body = {
		codename: "MOB_FIXTURE",
		refObjId: 1,
		glb: BODY,
		clips: [ "stand", "death" ],
		scalePercent: 125,
		soundProfileName: "original-body-sounds",
		previewGlb: "/assets/npc/body-preview.glb",
		previewClips: [ "stand" ],
		materialVariants: { 1: "/assets/npc/body-champion.glb" }
	};
	const death = {
		codename: "res/mob/corpse.bsr",
		kind: "death",
		glb: CORPSE,
		clips: [ "death", "deathLoop" ],
		requiredBy: [ body.codename ]
	};
	published.admit( {
		path: MANIFEST,
		buffer: new TextEncoder().encode( JSON.stringify( {
			format: "sro-mission-npc-models",
			version: 8,
			models: { [body.codename]: body, [death.codename]: death }
		} ) ).buffer
	} );
	assert.deepEqual( rejected, [] );
	assert.deepEqual( accepted, [ MANIFEST ] );
	const entity = {
		gid: 7,
		refObjId: 1,
		kind: /** @type {const} */ ("monster"),
		regionId: 257,
		x: 10,
		y: 20,
		z: 30,
		heading: 0,
		movementMode: 2
	};
	let seconds = 0;
	/*
	================
	step

	The same order as characters.ts: enter state, then select assembly resource.
	================
	*/
	function step( dead, pending = false ) {
		seconds += .1;
		const current = { ...entity, appearanceState: [ dead ? 2 : 1, 0, 0 ] };
		const output = { failure: null };
		state.step(
			{
				entities: [ current ],
				seconds,
				simulationMs: seconds * 1000,
				logicalPose: value => ({ regionId: value.regionId, x: value.x, y: value.y, z: value.z, angle: 0 }),
				appearanceRef: lookup.appearanceRef,
				states: new Map( [ [ entity.gid, { actionMask: 8 } ] ] ),
				health: undefined,
				deadGids: new Set(),
				hitByActor: new Map(),
				castByActor: new Map(),
				resources: { duration: () => 1 },
				random: { range: () => 0 },
				active: new Set( [ entity.gid ] ),
				pendingDeaths: new Set( pending ? [ entity.gid ] : [] ),
				uncensored: false
			},
			output,
			published
		);
		assert.equal( output.failure, null );
		const resource = lookup.resourceFor( current );
		assert.ok( resource );
		return resource;
	}
	try {
		const alive = step( false );
		assert.equal( alive.glb, BODY );
		assert.equal( step( true, true ).glb, BODY, "Pending killing hit must keep the body" );
		const dying = step( true );
		assert.equal( dying.glb, CORPSE );
		assert.deepEqual( dying.clips, death.clips );
		assert.equal( dying.refObjId, body.refObjId );
		assert.equal( dying.codename, body.codename );
		assert.equal( dying.scalePercent, body.scalePercent );
		assert.equal( dying.soundProfileName, body.soundProfileName );
		assert.equal( dying.materialVariants, undefined, "Body grade variants must not override the corpse" );
		assert.equal( dying.previewGlb, undefined );
		assert.equal( dying.previewClips, undefined );
		assert.equal( step( true ).glb, CORPSE );
		const revived = step( false );
		assert.equal( revived, alive, "Revival selects the original admitted body resource" );
		assert.deepEqual( revived.materialVariants, body.materialVariants );
		assert.equal( revived.previewGlb, body.previewGlb );
		assert.deepEqual( published.catalog.get( 1 ), alive, "The death overlay must not mutate the body catalog" );
	} finally {
		published.dispose();
	}
});
