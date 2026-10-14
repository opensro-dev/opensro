/*
===========================================================================

bandit-imbue.test.mjs - a trade bandit wears its skill's imbue

CICMonster_InitializeTradeEquipmentAndSkill (861720) attaches the imbue of
the bandit's column-89 skill (+0x264) as a lasting deco: a magic bandit
glows with its element though the server sends no buff for it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const { createCharacterEffects, createCharacterResources, createPresentationRandom } = {
	...(await load( "src/engine/runtime/characters/effects/effects.ts" )),
	...(await load( "src/engine/runtime/characters/resources/resources.ts" )),
	...(await load( "src/engine/runtime/random/random.ts" ))
};

test("an imbued bandit draws its imbue skill's effect; a physical one draws none", () => {
	const stage = {
		phase: "ACT_L",
		resource: "test.efp",
		action: "AT_LOOP",
		move: "MOV_NONE",
		scripts: [],
		startEvent: 0,
		count: 1,
		offset: [ 0, 0, 0 ],
		life: 0
	};
	const model = { nodes: [], primitives: [], images: [], clips: [ { name: "effect", duration: 1, channels: [] } ] };
	let serial = 0;
	const jobs = new Map(), requested = [];
	const assets = {
		available: () => 4,
		request( url, limit, decode ) {
			requested.push( url );
			const result = decode === "effects" ?
				{ kind: "effects", catalog: { 1: { stages: [ stage ] } } } :
				decode === "effect" || decode === "character" ?
				{ kind: "character", model, images: [] } :
				{
					kind: "bytes",
					buffer:
						new TextEncoder().encode( JSON.stringify( { format: "sro-skill-stage-models", models: {} } ) )
							.buffer
				};
			jobs.set( ++serial, result );
			return serial;
		},
		take( id ) {
			const r = jobs.get( id );
			jobs.delete( id );
			return r;
		},
		cancel( id ) {
			jobs.delete( id );
		}
	};
	const resources = createCharacterResources(
		assets,
		{ setCharacterModel() {}, retainCharacterModels() {} },
		"http://fixture.invalid"
	);
	const effects = createCharacterEffects( assets, "http://fixture.invalid", () => {}, createPresentationRandom( 1 ) );
	const bandit = {
		gid: 1,
		kind: "monster",
		refObjId: 7585,
		tidWord: 0x18c6,
		tradeVariant: 4,
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0
	};
	const physical = { ...bandit, gid: 2, refObjId: 7586 };
	const bodies = [ bandit, physical ].map( e => ({ gid: e.gid, pose: { ...e, yaw: 0 }, scale: 1, height: 20 }) );
	const game = { casts: [], attachedEffects: [] };
	// Skill 1 stands for 7585's imbue (SKILL_CH_LIGHTNING_GIGONGTA_A_01).
	const bandits = new Map( [ [ 7585, { imbue: 1 } ] ] );
	let tick = 0, actors = [];
	try {
		for ( let i = 0; i < 8; i++ ) {
			resources.begin( tick );
			resources.poll();
			actors = effects.step(
				[ bandit, physical ],
				game,
				tick,
				resources.ready,
				resources.duration,
				[],
				undefined,
				bodies,
				2,
				true,
				bandits
			);
			tick += .001;
		}
		assert.deepEqual( actors.map( a => a.gid === actors[0]?.gid ), [ true ], "one lasting imbue actor" );
		assert.equal( effects.error(), null );
		assert.ok( requested.some( url => url.endsWith( "#test.efp" ) ), "the imbue's effect program loaded" );
	} finally {
		effects.dispose();
		resources.dispose();
	}
});
