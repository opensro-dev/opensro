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
const { createActorMotion } = await import( "../../src/engine/runtime/characters/actor-motion.ts" );
const { createCharacterPose } = await import( "../../src/engine/foundation/animation/animation-pose.ts" );

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
		name: "Fixture",
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

test("actor-motion drops outgoing live-body locomotion on death-model swap and clamps future hit timestamps", () => {
	const liveResource = {
		codename: "MOB_FIXTURE",
		refObjId: 1,
		glb: BODY,
		clips: [ "stand", "walk", "run", "hit1", "death" ]
	};
	const corpseResource = {
		codename: "MOB_FIXTURE",
		refObjId: 1,
		glb: CORPSE,
		clips: [ "death", "deathLoop" ]
	};
	const states = new Map();
	const idleStates = new Map();
	const displayed = new Map();
	let nextId = 1;
	const motion = createActorMotion( {
		activeSkin: () => undefined,
		allocateActor: () => nextId++,
		appearances: new Map(),
		avatarOverrides: new Map(),
		committedAuxiliary: new Map(),
		concealmentSkills: () => () => undefined,
		displayedDependencies: new Map(),
		effects: {
			appearance: () => ({ weaponHidden: false, materialTint: undefined, scale: 1 }),
			hostMotions: () => []
		},
		entityLod: { crowded: () => false, distance: () => 0, fraction: () => 1 },
		footContact() {},
		groundClocks: new Map(),
		health: undefined,
		output: {
			displayed,
			failure: null,
			cameraTarget: null,
			cameraFade: null,
			previewReady: false,
			dockReady: false,
			commonReady: false
		},
		posePresentation: {
			moving: () => false,
			pose: ( _gid, target ) => target,
			carry() {}
		},
		published: {
			animationStates: new Map(),
			bloodEffects: new Map(),
			dress: {},
			dropModels: {},
			effectAnchors: new Map(),
			heightFactors: new Map(),
			heights: new Map(),
			itemIds: new Map(),
			items: {},
			manifest: 1,
			manifests: [ MANIFEST ],
			nativeMotionUrls: new Map(),
			riderModes: new Map(),
			ridesByRider: new Map(),
			shadowSizes: new Map(),
			skillSounds: new Map(),
			soundProfiles: new Map(),
			catalog: new Map( [ [ 1, liveResource ] ] ),
			deathModels: new Map( [ [ "MOB_FIXTURE", corpseResource ] ] ),
			recoveryByCodename: new Map()
		},
		referenceAppearances: { get: () => undefined, skin: () => undefined },
		renderer: {},
		resourceFor: () => liveResource,
		resources: {
			duration: () => 1,
			animation: () => false
		},
		skillObjects: { frame: () => null },
		soundSurface: () => undefined,
		sounds: { advance() {}, retainActivations() {} },
		states,
		statusOwner: { view: () => ({ poseLocked: false, rate: 1 }) },
		wornEquipment: () => [],
		presentationState: { idleStates, combatStanceEnds: new Map() }
	} );
	const makePose = clips =>
		createCharacterPose( {
			nodes: [ {
				name: "root",
				parent: -1,
				translation: [ 0, 0, 0 ],
				rotation: [ 0, 0, 0, 1 ],
				scale: [ 1, 1, 1 ]
			} ],
			clips: clips.map( name => ({ name, duration: 1, channels: [] }) ),
			images: [],
			primitives: []
		} );
	const livePose = makePose( liveResource.clips );
	const corpsePose = makePose( corpseResource.clips );

	/*
	================
	presentFrame
	================
	*/
	function presentFrame( entity, resource, seconds, hitByActor = new Map() ) {
		const next = new Map();
		motion.present(
			entity,
			{
				resource,
				model: resource.glb,
				dependencies: [ resource.glb ],
				auxiliaryCommit: [],
				particleCommit: [],
				overrideCommit: [],
				defaultWearCommit: []
			},
			{
				actionLayersByActor: new Map(),
				animationDeltaMs: 16,
				castByActor: new Map(),
				entitiesByGid: new Map( [ [ entity.gid, entity ] ] ),
				gameplay: null,
				hitByActor,
				localMover: () => false,
				logicalPose: value => ({ regionId: value.regionId, x: value.x, y: value.y, z: value.z, angle: 0 }),
				next,
				normalFortressClothes: false,
				pendingDeaths: new Set(),
				probe: undefined,
				sampleActorDetails: false,
				seconds,
				soundContext: () => ({ player: false, weapon: "PUNCH", critical: false, berserk: false }),
				vitalsByGid: new Map(),
				waitingActors: new Set()
			},
			{ animationHolders: [], localEntity: undefined, particleHolders: [] }
		);
		const actor = next.get( entity.gid );
		assert.ok( actor );
		displayed.set( entity.gid, actor );
		return actor;
	}

	const baseEntity = {
		gid: 7,
		refObjId: 1,
		kind: /** @type {const} */ ("monster"),
		regionId: 257,
		x: 10,
		y: 20,
		z: 30,
		heading: 0,
		movementMode: 1
	};
	// Hit with a slightly future callback timestamp (at > seconds by 1e-9s) must
	// clamp age >= 0 and evaluate without throwing Invalid animation layer.
	const hitActor = presentFrame(
		{ ...baseEntity, moving: false, appearanceState: [ 1, 0, 0 ] },
		liveResource,
		1.0,
		new Map( [ [ 7, { token: "1:7:0", at: 1.0 + 1e-9, damage: 50, critical: false } ] ] )
	);
	assert.doesNotThrow( () => livePose.evaluate( hitActor.clip, hitActor.time, hitActor.loop, hitActor.layers ) );

	// Start running on the live body, then swap to the death model (8E64F0):
	// outgoing "run" from the live body must not leak onto the corpse mesh.
	presentFrame(
		{ ...baseEntity, moving: true, movementRevision: 1, appearanceState: [ 1, 0, 0 ] },
		liveResource,
		1.1
	);
	idleStates.set( 7, {
		x: 10,
		z: 30,
		region: 257,
		idle: { remaining: 15, previous: 1.2, started: 1.2, clip: "stand" },
		deathEntered: true,
		deathModel: true,
		deathAction: true
	} );
	const deadActor = presentFrame(
		{ ...baseEntity, moving: false, movementRevision: 2, appearanceState: [ 2, 0, 0 ] },
		corpseResource,
		1.2
	);
	assert.equal( deadActor.model, CORPSE );
	for ( const layer of deadActor.layers ?? [] ) {
		assert.ok( corpseResource.clips.includes( layer.clip ), `unexpected clip ${layer.clip} on corpse` );
	}
	assert.doesNotThrow( () =>
		corpsePose.evaluate( deadActor.clip, deadActor.time, deadActor.loop, deadActor.layers )
	);
});
