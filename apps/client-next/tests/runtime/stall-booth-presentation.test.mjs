/*
===========================================================================

stall-booth-presentation.test.mjs - booth selection, loading and child lifetime

Connects manifest admission to the production auxiliary presenter. Resource
readiness and renderer assembly calls are explicit fixtures; no GPU is needed.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createPresentationCatalog } = await import( "../../src/engine/runtime/characters/presentation-catalog.ts" );
const { createAuxiliaryPresentation } = await import( "../../src/engine/runtime/characters/presentation-auxiliary.ts" );
const { CHARACTER_ACTORS } = await import( "../../src/engine/foundation/animation/character-budget.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );

const CHINA = "res/item/china/item/cj_store.bsr";
const EUROPE = "res/item/europe/item/euro_streetstall01.bsr";
const DECORATION = "res/item/etc/booth.bsr";
const MANIFEST = "/assets/npc/manifest.json";
const PARTICLE = "system/booth.efp";
const PROGRAM = "/assets/effects/programs.json#" + encodeURIComponent( PARTICLE );

/*
================
resource
================
*/
function resource( name, particles = false ) {
	const entry = {
		field00: 1,
		effectPath: PARTICLE,
		boneName: "",
		vector3c: [ 0, 0, 0 ],
		field4c: 0,
		flags50: [ 0, 0, 0 ],
		flag53: 0
	};
	return {
		glb: `/assets/npc/${name}.glb`,
		clips: [ "stand" ],
		animationStates: { stand: { stateId: 0, durationMs: 1000, loop: true } },
		animationBindings: [ { set: "default", stateId: 0, clip: "stand" } ],
		modifierSets: [ { kind: 1, stateId: 0, animationSetName: "default", count: 1, firstBaseWord4: 0 } ],
		particleModifiers: particles ?
			[
				{ kind: 2, stateId: -1, animationSetName: "ambient", entries: [ entry ] },
				{
					kind: 1,
					stateId: 0,
					animationSetName: "default",
					baseWords: [ 0, 0, 0, 0, 0, 0 ],
					entries: [ entry ]
				}
			] :
			[]
	};
}

/*
================
fixture
================
*/
function fixture() {
	const accepted = [], rejected = [], plans = [], assemblies = [];
	const cold = new Set();
	let nextId = -1;
	const published = createPresentationCatalog( {
		resources: {
			accepted: path => accepted.push( path ),
			rejected: ( path, error ) => rejected.push( { path, error } )
		},
		sounds: { catalog() {} },
		referenceAppearances: { setReferences() {} }
	} );
	const auxiliary = createAuxiliaryPresentation( () => nextId-- );
	const bindings = {
		boothModels: published.boothModels,
		items: {
			"17": { codename: "ITEM_MALL_BOOTH_FIXTURE", wornModelPath: "item\\etc\\BOOTH.bsr" },
			"18": { codename: "ITEM_WITHOUT_MODEL", wornModelPath: null }
		},
		resources: {
			ready: path => !cold.has( path ),
			/*
			================
			plan
			================
			*/
			plan( paths ) {
				plans.push( paths );
				return paths.every( path => !cold.has( path ) );
			},
			duration: () => 1
		},
		renderer: {
			/*
			================
			setCharacterAssembly
			================
			*/
			setCharacterAssembly( ...args ) {
				assemblies.push( args );
			}
		}
	};
	const entity = {
		gid: 7,
		refObjId: 1,
		name: "Stall owner",
		kind: /** @type {const} */ ("player"),
		regionId: 257,
		x: 10,
		y: 20,
		z: 30,
		heading: 0,
		countryByte9c: 0,
		appearanceState: [ 1, 0, 0, 0, 0, 0, 4 ],
		titleId: 0
	};
	const owner = {
		gid: entity.gid,
		model: "character",
		pose: { regionId: 257, x: 10, y: 20, z: 30, yaw: radians( 1 ) },
		clip: "stall",
		time: 99,
		loop: true,
		scale: 2,
		opacity: .75
	};
	/*
	================
	admit
	================
	*/
	function admit( manifest ) {
		published.admit( { path: MANIFEST, buffer: new TextEncoder().encode( JSON.stringify( manifest ) ).buffer } );
	}
	admit( {
		resources: {},
		boothModels: {
			[CHINA]: resource( "china" ),
			[EUROPE]: resource( "europe" ),
			[DECORATION]: resource( "decoration", true )
		},
		models: { booth: { kind: "booth", codename: CHINA, bsr: CHINA } }
	} );
	assert.deepEqual( rejected, [] );
	/*
	================
	step
	================
	*/
	/**
	 * @param {number} seconds
	 * @param {import('../../src/engine/contracts/world.ts').EntityState[]} [entities]
	 * @param {import('../../src/engine/contracts/character.ts').CharacterActor[]} [actors]
	 */
	function step( seconds, entities = [ entity ], actors = [ owner ] ) {
		/** @type {Parameters<typeof auxiliary.presentBooths>[0]} */
		const frame = {
			entities,
			seconds,
			next: new Map( actors.map( actor => [ actor.gid, actor ] ) ),
			animationHolders: [],
			particleHolders: []
		};
		auxiliary.presentBooths( frame, bindings );
		return { ...frame, children: [ ...frame.next.values() ].filter( actor => actor.gid < 0 ) };
	}
	return { published, auxiliary, bindings, entity, owner, step, admit, cold, plans, assemblies, accepted, rejected };
}

test("race defaults and item decoration load separate assemblies without changing the owner", () => {
	for (
		const [race, titleId, name]
			of /** @type {const} */ ([ [ 0, 0, "china" ], [ 1, 0, "europe" ], [ 0, 17, "decoration" ] ])
	) {
		const f = fixture();
		const frame = f.step( 3, [ { ...f.entity, countryByte9c: race, titleId } ] );
		const child = defined( frame.children[0] );
		assert.equal( frame.next.get( f.owner.gid ), f.owner );
		assert.deepEqual( f.assemblies, [ [ child.model, `/assets/npc/${name}.glb`, [] ] ] );
		assert.equal( child.pose, f.owner.pose );
		assert.deepEqual( child.attachment, { gid: f.owner.gid, bone: "", root: true, offset: [ 0, 0, 0 ] } );
		assert.equal( child.scale, 1 );
		assert.equal( child.absoluteEffectScale, undefined );
		assert.equal( child.pickable, false );
		assert.equal( child.clip, "stand" );
		assert.equal( child.time, 0 );
		assert.equal( child.opacity, undefined, "inherit current owner alpha without squaring it" );
		const renderer = createCharacters();
		assert.deepEqual(
			defined( renderer.matrix( [ f.owner, child ], child.gid ) ),
			defined( renderer.matrix( [ f.owner ], f.owner.gid ) ),
			"booth copies full body matrix, including the fixture's scale 2"
		);
	}
});

test("a static booth stays in its bind pose without borrowing the character's stall motion", () => {
	const f = fixture(), model = defined( f.published.boothModels.get( CHINA ) );
	f.published.boothModels.set( CHINA, { ...model, clips: [], animationStates: {} } );
	const child = defined( f.step( 0 ).children[0] );
	assert.equal( child.clip, "" );
	assert.deepEqual( child.layers, [] );
	assert.equal( child.modelAnimation?.selected, null );
});

test("missing item records use the race default while existing records without models do not", () => {
	for ( const [race, name] of /** @type {const} */ ([ [ 0, "china" ], [ 1, "europe" ] ]) ) {
		const f = fixture();
		const child = defined( f.step( 0, [ { ...f.entity, countryByte9c: race, titleId: 999 } ] ).children[0] );
		assert.deepEqual( f.assemblies, [ [ child.model, `/assets/npc/${name}.glb`, [] ] ] );
	}
	const f = fixture();
	assert.deepEqual( f.step( 0, [ { ...f.entity, titleId: 18 } ] ).children, [] );
});

test("invalid races, nonplayers and closed stalls do not fabricate booths", () => {
	const f = fixture();
	for (
		const current of [
			{ ...f.entity, countryByte9c: 2 },
			{ ...f.entity, kind: /** @type {const} */ ("monster") },
			{ ...f.entity, appearanceState: [ 1, 0, 0 ] }
		]
	) assert.deepEqual( f.step( 0, [ current ] ).children, [] );
	assert.deepEqual( f.assemblies, [] );
});

test("cold model and particle dependencies hold the booth clock; admission publishes both particle planes", () => {
	const f = fixture(), entity = { ...f.entity, titleId: 17 };
	f.cold.add( "/assets/npc/decoration.glb" );
	assert.deepEqual( f.step( 1, [ entity ] ).children, [] );
	f.cold.clear();
	f.cold.add( PROGRAM );
	assert.deepEqual( f.step( 2, [ entity ] ).children, [] );
	assert.equal( f.assemblies.length, 0 );
	f.cold.clear();
	const first = f.step( 10, [ entity ] ), child = defined( first.children[0] );
	assert.equal( child.time, 0 );
	assert.equal( first.particleHolders.length, 1 );
	assert.equal( first.animationHolders.length, 1 );
	assert.equal( first.particleHolders[0].actor, child );
	assert.equal( first.animationHolders[0].actor, child );
	assert.deepEqual( child.modelAnimation?.selected, { set: "default", state: 0 } );
	const later = defined( f.step( 10.25, [ entity ] ).children[0] );
	assert.equal( later.gid, child.gid );
	assert.equal( later.time, .25 );
	assert.equal( later.layers?.[0]?.activation, child.layers?.[0]?.activation );
});

test("the native cached booth survives title changes until close, then reopening selects the new decoration", () => {
	const f = fixture(), first = defined( f.step( 0 ).children[0] );
	const decorated = { ...f.entity, titleId: 17 };
	const cached = defined( f.step( 1, [ decorated ] ).children[0] );
	assert.equal( cached.gid, first.gid );
	assert.equal( cached.model, first.model );
	assert.equal( cached.time, 1 );
	assert.deepEqual( f.step( 2, [ { ...decorated, appearanceState: [ 1, 0, 0 ] } ] ).children, [] );
	const reopened = defined( f.step( 3, [ decorated ] ).children[0] );
	assert.notEqual( reopened.gid, first.gid );
	assert.equal( reopened.time, 0 );
	assert.equal( f.assemblies.at( -1 )?.[1], "/assets/npc/decoration.glb" );
});

test("despawn, reset, reused gids and close/reopen between frames retire the cached booth", () => {
	for ( const boundary of [ "absent", "resetStages", "reset", "despawn", "spawn", "close" ] ) {
		const f = fixture(), first = defined( f.step( 0 ).children[0] );
		if ( boundary === "absent" ) assert.deepEqual( f.step( 1, [], [] ).children, [] );
		else if ( boundary === "resetStages" ) f.auxiliary.resetStages();
		else if ( boundary === "reset" ) f.auxiliary.receiveBooths( [ { kind: "reset", epoch: 1 } ] );
		else if ( boundary === "despawn" ) f.auxiliary.receiveBooths( [ { kind: "despawn", gid: f.entity.gid } ] );
		else if ( boundary === "spawn" ) f.auxiliary.receiveBooths( [ { kind: "spawn", entity: f.entity } ] );
		else f.auxiliary.receiveBooths( [ { kind: "state", entity: { ...f.entity, appearanceState: [ 1, 0, 0 ] } } ] );
		const fresh = defined( f.step( 2 ).children[0] );
		assert.notEqual( fresh.gid, first.gid, boundary );
		assert.equal( fresh.time, 0, boundary );
	}
});

test("missing owners and full actor budgets do not publish orphan booths or consume their first animation sample", () => {
	const f = fixture();
	assert.deepEqual( f.step( 0, [ f.entity ], [] ).children, [] );
	const full = Array.from( { length: CHARACTER_ACTORS }, ( _, gid ) => ({ ...f.owner, gid: gid + 1 }) );
	assert.deepEqual( f.step( 1, [ f.entity ], full ).children, [] );
	assert.equal( defined( f.step( 2 ).children[0] ).time, 0 );
});

test("malformed booth publications are rejected atomically and disposal releases the booth catalog", () => {
	const f = fixture(), original = f.published.boothModels.get( CHINA );
	for (
		const boothModels of [
			{ [CHINA]: resource( "replacement" ), [DECORATION]: { glb: "/outside.glb", clips: [] } },
			{ "res/../escape.bsr": resource( "escape" ) },
			{ [DECORATION]: { ...resource( "bad" ), particleModifiers: [ { kind: 9 } ] } }
		]
	) {
		f.admit( { boothModels } );
		assert.equal( f.published.boothModels.get( CHINA ), original );
	}
	assert.equal( f.rejected.length, 3 );
	assert.equal( f.accepted.length, 1 );
	f.published.dispose();
	assert.equal( f.published.boothModels.size, 0 );
});
