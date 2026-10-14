/*
===========================================================================

effect-budget.test.mjs - a crowd's skill effects all draw

The original keeps each character's decorations in an unbounded list
(CICharactor_AppendOwnedDecoration 85C020). The port's effect budget is
sized above a crowd's load: a crowd of casts all draws, and past the
budget the farthest attached effects give way instead of the frame
drawing nothing.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { trimToEffectBudget, sharedCharacterRoom } = await import(
	"../../src/engine/foundation/animation/effect-room.ts"
);
const { createCharacterSelection } = await import( "../../src/engine/foundation/animation/character-selection.ts" );

/*
================
effectOwner

An effect owner whose one skill (1) is a single AT_TARGET stage.
================
*/
function effectOwner() {
	let requestId = 0;
	const jobs = new Map();
	return createCharacterEffects(
		{
			available: () => 4,
			progress: () => null,
			health: () => ({ phase: "running" }),
			install() {},
			textureFeatures() {},
			dispose() {
				jobs.clear();
			},
			request( url, limit, decode ) {
				const result = decode === "effects" ?
					{
						kind: "effects",
						catalog: {
							"1": {
								clips: [ "attack1" ],
								stages: [ {
									resource: "hit.efp",
									damageEvent: false,
									startEvent: 1,
									action: "AT_TARGET",
									move: "MOV_NONE",
									bone: null,
									offset: [ 0, 0, 0 ],
									targetBone: null,
									targetOffset: [ 0, 0, 0 ],
									life: 0,
									sound: null,
									count: 1,
									scripts: []
								} ]
							}
						}
					} :
					{
						kind: "bytes",
						buffer: new TextEncoder().encode( JSON.stringify( {
							format: "sro-skill-stage-models",
							models: {}
						} ) ).buffer
					};
				jobs.set( ++requestId, result );
				return requestId;
			},
			take( id ) {
				const result = jobs.get( id );
				jobs.delete( id );
				return result;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
}

/*
================
character
================
*/
function character( gid, kind, x ) {
	return { gid, refObjId: 1, kind, name: `c${gid}`, regionId: 257, x, y: 0, z: 0, heading: 0 };
}

test("a crowd of 200 casts all draw, past the old 128 cast cap", () => {
	const effect = effectOwner();
	const crowd = 200;
	const entities = [], casts = [];
	for ( let i = 0; i < crowd; i++ ) {
		entities.push( character( 1000 + i, "player", i ), character( 5000 + i, "monster", i + 0.5 ) );
		casts.push( { token: i + 1, caster: 1000 + i, target: 5000 + i, skill: 1, damage: 0, fatal: false } );
	}
	const gameplay = {
		casts,
		localGid: 1000,
		revision: 0,
		pose: null,
		authoritativePose: null,
		pendingMoves: 0,
		acknowledgedMove: 0,
		target: 0,
		targetPending: 0,
		inventory: [],
		inventoryPending: false,
		vitals: [],
		error: null
	};
	let drawn = [];
	for ( const time of [ 0, 0.1, 0.2, 0.3 ] ) {
		drawn = effect.step(
			entities,
			gameplay,
			time,
			() => true,
			() => 0.5,
			casts.map( cast => ({ cast, phase: "SHOT", event: 1, at: time }) )
		);
		if ( drawn.length ) break;
	}
	assert.equal( effect.error(), null );
	assert.equal( drawn.length, crowd, "every cast draws its effect" );
	assert.deepEqual(
		effect.takeBudget(),
		{ actors: crowd, gaveWay: 0 },
		"the probe sees the crowd, nothing gave way"
	);
	effect.dispose();
});

test("past the budget the farthest attached effects give way, nearest and order kept", () => {
	const at = ( x, regionId = 257 ) => ({ pose: { regionId, x, z: 0 }, x });
	const actors = [ at( 5 ), at( 900 ), at( 1 ), at( 0, 258 ), at( 2 ) ];
	assert.equal( trimToEffectBudget( actors, 3, { regionId: 257, x: 0, z: 0 } ), 2, "two gave way" );
	// Region 258 lies 1920 east: it and x 900 are the two farthest.
	assert.deepEqual( actors.map( a => a.x ), [ 5, 1, 2 ] );
	const unplaced = [ at( 1 ), at( 2 ), at( 3 ) ];
	trimToEffectBudget( unplaced, 2, undefined );
	assert.deepEqual( unplaced.map( a => a.x ), [ 1, 2 ], "without a player pose the newest give way" );
	const within = [ at( 1 ) ];
	assert.equal( trimToEffectBudget( within, 4, { regionId: 257, x: 0, z: 0 } ), 0 );
	assert.equal( within.length, 1 );
});

test("a full item-effect queue skips the newest and counts it for the probe", () => {
	const effect = effectOwner();
	const source = character( 1, "player", 0 );
	for ( let i = 0; i < 2049; i++ ) effect.item( { kind: "item-effect", source, item: 1, typeFlags: 0 } );
	assert.equal( effect.takeBudget().gaveWay, 1, "the 2049th visual gave way" );
	assert.equal( effect.takeBudget().gaveWay, 0, "each call consumes the count" );
	effect.dispose();
});

test("past the actor room the nearest effects draw, and the probe hears how many gave way", async () => {
	const { createAuxiliaryPresentation } = await import(
		"../../src/engine/runtime/characters/presentation-auxiliary.ts"
	);
	const { CHARACTER_ACTORS } = await import( "../../src/engine/foundation/animation/character-budget.ts" );
	const auxiliary = createAuxiliaryPresentation( () => 0 );
	const next = new Map();
	for ( let gid = 1; next.size < CHARACTER_ACTORS - 2; gid++ ) next.set( gid, { gid } );
	/** @type {( gid: number, x: number ) => any} */
	const effect = ( gid, x ) => ({
		gid,
		model: "fx",
		clip: "effect",
		time: 0,
		pose: { regionId: 257, x, y: 0, z: 0 }
	});
	const effectActors = [ effect( -1, 900 ), effect( -2, 1 ), effect( -3, 500 ), effect( -4, 2 ) ];
	const counts = {};
	auxiliary.presentStages(
		{ orbActors: [], effectActors, next, seconds: 0, animationHolders: [], player: { regionId: 257, x: 0, z: 0 } },
		{
			renderer: { setCharacterAssembly() {} },
			resources: { duration: () => 1 },
			effects: { animationModel: () => undefined, takeBudget: () => ({ actors: 4, gaveWay: 0 }) },
			probe: { characterCount: ( name, value = 1 ) => counts[name] = (counts[name] ?? 0) + value }
		}
	);
	assert.deepEqual( counts, { "effect-actors": 4, "effect-budget-gave-way": 0, "effect-actor-room-gave-way": 2 } );
	assert.equal( next.size, CHARACTER_ACTORS );
	assert.ok( next.has( -2 ) && next.has( -4 ), "the two nearest effects draw" );
	assert.ok( !next.has( -1 ) && !next.has( -3 ), "the two farthest give way" );
});

test("in the shared actor room a near effect takes a far character's place; the player always stays", () => {
	const ranked = [ { priority: 0, distance: 900 }, { priority: 2, distance: 1 }, { priority: 2, distance: 25 } ];
	// Room 3: the player (always first), the character at 1, then the
	// effect at 4 beats the character at 25.
	assert.equal( sharedCharacterRoom( ranked, [ 4 ], 3 ), 2 );
	assert.equal( sharedCharacterRoom( ranked, [ 100 ], 3 ), 3, "a far effect leaves every character" );
	assert.equal( sharedCharacterRoom( ranked, [], 3 ), 3 );
	assert.equal( sharedCharacterRoom( ranked, [ 0, 0, 0 ], 3 ), 1, "the player keeps its slot" );
	/** @type {( gid: number, x: number ) => any} */
	const at = ( gid, x ) => ({ gid, regionId: 257, x, y: 0, z: 0 });
	const entities = [ at( 1, 0 ), at( 2, 1 ), at( 3, 5 ) ];
	let effects;
	const selection = createCharacterSelection( 3, () => effects );
	const anchor = { regionId: 257, x: 0, y: 0, z: 0, angle: 0 };
	assert.deepEqual( selection.select( entities, anchor, 1, undefined ).map( e => e.gid ), [ 1, 2, 3 ] );
	effects = [ 4 ];
	assert.deepEqual(
		selection.select( entities, anchor, 1, undefined ).map( e => e.gid ),
		[ 1, 2 ],
		"the effect at distance 2 outranks the character at distance 5"
	);
	effects = undefined;
	assert.deepEqual(
		selection.select( entities, anchor, 1, undefined ).map( e => e.gid ),
		[ 1, 2, 3 ],
		"off is the old rule"
	);
});
