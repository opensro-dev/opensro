/*
===========================================================================

skill-visual-fixture.mjs - published skill records through the effect owner

Share the deterministic asset host and actors used to verify authored skill
stages, lifetimes, sound cues and attachments without a second FX runtime.

===========================================================================
*/
import "./native-source-loader.mjs";
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );

/*
================
createSkillVisualFixture

Asset jobs return the real decoded catalog. Model residency is synchronous
here; program decoding and dependency publication are checked separately.
================
*/
export function createSkillVisualFixture( { records, durations = new Map() } ) {
	let serial = 0;
	const jobs = new Map(), sounds = [];
	const owner = createCharacterEffects(
		{
			progress: () => null,
			health: () => ({ phase: "running" }),
			/*
			================
			install
			================
			*/
			install() {},
			/*
			================
			dispose
			================
			*/
			dispose() {},
			available: () => 4,
			/*
			================
			request
			================
			*/
			request( url, limit, decode ) {
				jobs.set(
					++serial,
					decode === "effects" ? { kind: "effects", catalog: records } : {
						kind: "bytes",
						buffer: new TextEncoder().encode(
							JSON.stringify( { format: "sro-skill-stage-models", models: {} } )
						).buffer
					}
				);
				return serial;
			},
			/*
			================
			take
			================
			*/
			take( id ) {
				const result = jobs.get( id );
				jobs.delete( id );
				return result;
			},
			/*
			================
			cancel
			================
			*/
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://fixture.invalid",
		cue => sounds.push( cue ),
		createPresentationRandom( 1 )
	);
	/** @type {import('../../src/engine/contracts/world.ts').EntityState[]} */
	const entities = [ 1, 2 ].map( gid => ({
		gid,
		name: "fixture",
		kind: "player",
		refObjId: 1,
		regionId: 257,
		x: gid * 10,
		y: 20,
		z: 30,
		heading: 0
	}) );
	const bodies = entities.map( entity => ({
		gid: entity.gid,
		model: "body",
		pose: { ...entity, yaw: radians( 0 ) },
		scale: 1,
		height: 20,
		clip: "idle",
		time: 0,
		loop: false,
		pickable: false
	}) );
	const game = {
		localGid: 1,
		pose: { regionId: 257, x: 10, y: 20, z: 30, angle: 0 },
		vitals: [],
		inventory: [],
		/** @type {import('../../src/engine/contracts/gameplay.ts').CastState[]} */
		casts: [],
		/** @type {import('../../src/engine/foundation/gameplay/attached-effects.ts').AttachedEffect[]} */
		attachedEffects: []
	};
	/*
	================
	frame
	================
	*/
	function frame( now, triggers = [] ) {
		return owner.step(
			entities,
			game,
			now,
			() => true,
			model =>
				durations.get( decodeURIComponent( model.split( "#" )[1] ?? "" ) ) ??
					(model.includes( "damage_" ) ? .95 : 2),
			triggers,
			undefined,
			bodies
		);
	}
	frame( 0 );
	frame( .1 );
	frame( .2 );
	return { owner, game, sounds, frame };
}
