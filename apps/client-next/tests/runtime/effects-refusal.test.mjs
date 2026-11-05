/*
===========================================================================

effects-refusal.test.mjs - a mover waits for both of its bones, and an
effect the port cannot play is skipped and logged, never an error

Issue 2440: the Blood Touch stage (SKILL_EU_WARLOCK_BLOODA_LIFEDRAIN_A,
SHOT/1) is a returning mover, AT_SOURCE / MOV_HWAN with bone and
targetBone both "Bip01". It launches from a victim bone and lands on the
caster's bone. The socket query answers only for presented actors, so a
caster whose model is still loading has no landing bone yet. The launch
bone already held the cast callback; the landing bone refused the stage
into the fatal unsupported set. These tests pin the symmetric hold, and
the refusal contract: skipped and logged, never an error. A spin range
wider than int32 plays with the native rotation instead (issue 2576).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );

function fixture() {
	let id = 0;
	const jobs = new Map(), sounds = [];
	const encode = value => new TextEncoder().encode( JSON.stringify( value ) );
	// The authored Blood Touch stage, as the asset worker decodes it.
	const stage = {
		resource: "skill/europe/warlock_blood_touch_a.efp",
		phase: "SHOT",
		damageEvent: false,
		startEvent: 1,
		action: "AT_SOURCE",
		move: "MOV_HWAN",
		bone: "Bip01",
		offset: [ 0, 0, 0 ],
		targetBone: "Bip01",
		targetOffset: [ 0, 0, 0 ],
		count: 1,
		scripts: [],
		sound: "launch.wav",
		soundEnd: null,
		arrivalResource: "skill/europe/warlock_blood_touch_hit_a.efp",
		movement: { delayMs: 0, startSpeed: 100, endSpeed: 100 }
	};
	const effects = createCharacterEffects(
		{
			available: () => 4,
			progress: () => null,
			health: () => ({ phase: "running" }),
			textureFeatures() {},
			install() {},
			dispose() {},
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog: { "9490": { clips: [ "attack1" ], stages: [ stage ] } } } :
						{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer }
				);
				return id;
			},
			take( jobId ) {
				const r = jobs.get( jobId );
				jobs.delete( jobId );
				return r;
			},
			cancel( jobId ) {
				jobs.delete( jobId );
			}
		},
		"http://localhost",
		event => sounds.push( event ),
		createPresentationRandom( 1 )
	);
	const entities = [
		{ gid: 1, refObjId: 1, kind: "player", name: "warlock", regionId: 257, x: 0, y: 0, z: 0, heading: 0 },
		{ gid: 2, refObjId: 1, kind: "monster", name: "victim", regionId: 258, x: 100, y: 0, z: 0, heading: 0 }
	];
	const displayed = new Set();
	const socket = gid => {
		if ( !displayed.has( gid ) ) return null;
		const entity = defined( entities.find( row => row.gid === gid ), "socket entity" );
		return { regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, yaw: radians( 0 ) };
	};
	const cast = { token: 1, caster: 1, target: 2, skill: 9490 }, castRows = [];
	const gameplay = { casts: castRows, localGid: 9, pose: null, vitals: [], inventory: [] };
	const step = ( time, triggers = [] ) =>
		effects.step( entities, gameplay, time, () => true, () => 0.1, triggers, socket );
	step( 0 );
	step( 0.01 );
	step( 0.02 );
	castRows.push( cast );
	return {
		effects,
		sounds,
		gameplay,
		displayed,
		step,
		trigger: { cast, phase: "SHOT", event: 1, at: 1 }
	};
}

test("a returning mover waits for the caster's landing bone, then flies", () => {
	const f = fixture();
	f.displayed.add( 2 );
	// The caster is resident but not presented yet: no landing bone.
	assert.deepEqual( f.step( 1, [ f.trigger ] ), [] );
	assert.equal( f.effects.error(), null );
	assert.equal( f.sounds.length, 0 );
	// Once the caster can be posed, the held callback launches the touch
	// from the victim, exactly as if the bone had never been cold.
	f.displayed.add( 1 );
	const launched = f.step( 1.05, [ f.trigger ] );
	assert.equal( f.effects.error(), null );
	assert.equal( launched.length, 1 );
	assert.equal( launched[0].pose.regionId, 258 );
	assert.equal( launched[0].pose.x, 100 );
	assert.equal( f.sounds.length, 1 );
	f.step( 12 );
	assert.equal( f.effects.error(), null );
});

test("a returning mover waits for the victim's launch bone", () => {
	const f = fixture();
	f.displayed.add( 1 );
	assert.deepEqual( f.step( 1, [ f.trigger ] ), [] );
	assert.equal( f.effects.error(), null );
	f.displayed.add( 2 );
	assert.equal( f.step( 1.05, [ f.trigger ] ).length, 1 );
	assert.equal( f.effects.error(), null );
});

function trapFixture( count ) {
	let id = 0;
	const jobs = new Map(), sounds = [];
	const encode = value => new TextEncoder().encode( JSON.stringify( value ) );
	// MSKILL_TQ_QINSHITRAP1_ATTACK02's ACT_S stage, as decoded.
	const stage = {
		resource: "monster/eu/etc/buff_critical_up.efp",
		phase: "ACT_S",
		damageEvent: false,
		startEvent: 0,
		action: "AT_ONE_FOLLOW",
		move: "MOV_NONE",
		bone: null,
		offset: [ 0, 0, 0 ],
		targetOffset: [ 0, 0, 0 ],
		count,
		scripts: [],
		sound: null,
		soundEnd: null,
		movement: { delayMs: 0, startSpeed: 0, endSpeed: 0 }
	};
	const effects = createCharacterEffects(
		{
			available: () => 4,
			progress: () => null,
			health: () => ({ phase: "running" }),
			textureFeatures() {},
			install() {},
			dispose() {},
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog: { "25048": { clips: [ "buff" ], stages: [ stage ] } } } :
						{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer }
				);
				return id;
			},
			take( jobId ) {
				const r = jobs.get( jobId );
				jobs.delete( jobId );
				return r;
			},
			cancel( jobId ) {
				jobs.delete( jobId );
			}
		},
		"http://localhost",
		event => sounds.push( event ),
		createPresentationRandom( 1 )
	);
	const entity = { gid: 5, refObjId: 1, kind: "monster", name: "trap", regionId: 257, x: 0, y: 0, z: 0, heading: 0 };
	const actor = {
		gid: 5,
		model: "body",
		clip: "stand",
		time: 0,
		loop: true,
		scale: 1,
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
		height: 10
	};
	const gameplay = {
		casts: [],
		localGid: 9,
		pose: null,
		vitals: [],
		inventory: [],
		attachedEffects: [ { gid: 5, skill: 25048, token: 7, phase: 0 } ]
	};
	const step = time => effects.step( [ entity ], gameplay, time, () => true, () => 1, [], undefined, [ actor ] );
	step( 0 );
	step( 0.1 );
	return { effects, sounds, step };
}

test("an attached stage with an authored count of zero creates nothing and stays silent", () => {
	// The Qin-Shi tomb trap buffs author a batch count of 0. 8DCA40
	// consumes the count (+0x9) as its creation-loop bound (8DCDBB), so
	// the native client creates nothing and refuses the value nowhere.
	// That used to throw out of the attached lane into the session-fatal
	// failure (#2440), then log a refusal (#2471).
	const f = trapFixture( 0 );
	const warnings = [], original = console.warn;
	console.warn = ( ...args ) => warnings.push( args.join( " " ) );
	try {
		assert.deepEqual( f.step( 0.2 ), [] );
		assert.deepEqual( f.step( 0.3 ), [] );
	} finally {
		console.warn = original;
	}
	assert.equal( f.effects.error(), null );
	assert.equal( warnings.length, 0 );
	assert.equal( f.effects.unsupported().length, 0 );
	// The same stage with a usable count still renders its visual.
	const rendered = trapFixture( 1 );
	const actors = rendered.step( 0.2 );
	assert.equal( rendered.effects.error(), null );
	assert.equal( actors.length, 1 );
	assert.ok( actors[0].model.includes( "buff_critical_up" ) );
});

function spinFixture() {
	let id = 0;
	const jobs = new Map(), sounds = [];
	const encode = value => new TextEncoder().encode( JSON.stringify( value ) );
	// A MOV_UPR stage whose authored spin endpoints are full int32 fields
	// spanning more than a signed 32-bit range, as the worker decodes it.
	const stage = {
		resource: "skill/spin.efp",
		phase: "SHOT",
		damageEvent: false,
		startEvent: 1,
		action: "AT_SOURCE",
		move: "MOV_UPR",
		bone: null,
		offset: [ 0, 0, 0 ],
		targetOffset: [ 0, 0, 0 ],
		count: 1,
		scripts: [],
		sound: null,
		soundEnd: null,
		movement: { delayMs: 0, startSpeed: 100, endSpeed: 100 },
		parameters: [ 0, -2000000000, 2000000000 ]
	};
	const effects = createCharacterEffects(
		{
			available: () => 4,
			progress: () => null,
			health: () => ({ phase: "running" }),
			textureFeatures() {},
			install() {},
			dispose() {},
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog: { "9490": { clips: [ "spin" ], stages: [ stage ] } } } :
						{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer }
				);
				return id;
			},
			take( jobId ) {
				const r = jobs.get( jobId );
				jobs.delete( jobId );
				return r;
			},
			cancel( jobId ) {
				jobs.delete( jobId );
			}
		},
		"http://localhost",
		event => sounds.push( event ),
		createPresentationRandom( 1 )
	);
	const entities = [
		{ gid: 1, refObjId: 1, kind: "player", name: "caster", regionId: 257, x: 0, y: 0, z: 0, heading: 0 },
		{ gid: 2, refObjId: 1, kind: "monster", name: "victim", regionId: 258, x: 100, y: 0, z: 0, heading: 0 }
	];
	const socket = gid => {
		const entity = defined( entities.find( row => row.gid === gid ), "socket entity" );
		return { regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, yaw: radians( 0 ) };
	};
	const cast = { token: 1, caster: 1, target: 2, skill: 9490 }, castRows = [];
	const gameplay = { casts: castRows, localGid: 9, pose: null, vitals: [], inventory: [] };
	const step = ( time, triggers = [] ) =>
		effects.step( entities, gameplay, time, () => true, () => 0.1, triggers, socket );
	step( 0 );
	step( 0.01 );
	step( 0.02 );
	castRows.push( cast );
	return { effects, step, trigger: { cast, phase: "SHOT", event: 1, at: 1 } };
}

test("a spin range wider than int32 plays with the native rotation, never an error", () => {
	// Issue 2576: the native range helper wraps the span to int32, reads
	// it non-positive and returns the lower endpoint without a draw. The
	// port threw out of the spawn and ended the session at world entry.
	const f = spinFixture();
	const launched = f.step( 1, [ f.trigger ] );
	assert.equal( f.effects.error(), null );
	assert.equal( launched.length, 1 );
	assert.equal( launched[0].pose.regionId, 258 );
	f.step( 12 );
	assert.equal( f.effects.error(), null );
});

test("a mover whose cast ends while a bone is cold is skipped and logged, never an error", () => {
	const f = fixture();
	f.displayed.add( 2 );
	assert.deepEqual( f.step( 1, [ f.trigger ] ), [] );
	const warnings = [], original = console.warn;
	console.warn = ( ...args ) => warnings.push( args.join( " " ) );
	try {
		// The cast is over; the held callback can no longer wait, so the
		// effect is skipped. Nobody's session ends over it.
		f.gameplay.casts = [];
		assert.deepEqual( f.step( 1.05 ), [] );
	} finally {
		console.warn = original;
	}
	assert.equal( f.effects.error(), null );
	assert.equal( warnings.length, 1 );
	assert.match( warnings[0], /warlock_blood_touch_a\.efp/ );
	assert.equal( f.effects.unsupported().length, 1 );
	assert.match( f.effects.unsupported()[0], /target-socket:1\/Bip01/ );
});

function staticFixture( stage ) {
	let id = 0;
	const jobs = new Map(), sounds = [];
	const encode = value => new TextEncoder().encode( JSON.stringify( value ) );
	const effects = createCharacterEffects(
		{
			available: () => 4,
			progress: () => null,
			health: () => ({ phase: "running" }),
			textureFeatures() {},
			install() {},
			dispose() {},
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog: { "9490": { clips: [ "attack1" ], stages: [ stage ] } } } :
						{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer }
				);
				return id;
			},
			take( jobId ) {
				const r = jobs.get( jobId );
				jobs.delete( jobId );
				return r;
			},
			cancel( jobId ) {
				jobs.delete( jobId );
			}
		},
		"http://localhost",
		event => sounds.push( event ),
		createPresentationRandom( 1 )
	);
	const entities = [
		{ gid: 1, refObjId: 1, kind: "player", name: "caster", regionId: 257, x: 0, y: 0, z: 0, heading: 0 },
		{ gid: 2, refObjId: 1, kind: "monster", name: "victim", regionId: 258, x: 100, y: 0, z: 0, heading: 0 }
	];
	const cast = { token: 1, caster: 1, target: 2, skill: 9490 }, castRows = [];
	const gameplay = { casts: castRows, localGid: 9, pose: null, vitals: [], inventory: [] };
	const step = ( time, triggers = [] ) => effects.step( entities, gameplay, time, () => true, () => 0.1, triggers );
	step( 0 );
	step( 0.01 );
	step( 0.02 );
	castRows.push( cast );
	return { effects, sounds, step, cast };
}

test("a cast stage with an authored count of zero creates nothing and stays silent", () => {
	// 8DE398 consumes the authored count (+0x9) as its creation-loop
	// bound: zero iterations create nothing, and the native client
	// refuses the value nowhere (#2471).
	const f = staticFixture( {
		resource: "skill/europe/wizard_fire_trap_hit.efp",
		phase: "SHOT",
		damageEvent: false,
		startEvent: 1,
		action: "AT_STOP",
		move: "MOV_NONE",
		bone: null,
		offset: [ 0, 0, 0 ],
		targetOffset: [ 0, 0, 0 ],
		count: 0,
		scripts: [],
		sound: null,
		soundEnd: null,
		movement: { delayMs: 0, startSpeed: 0, endSpeed: 0 }
	} );
	const warnings = [], original = console.warn;
	console.warn = ( ...args ) => warnings.push( args.join( " " ) );
	try {
		assert.deepEqual( f.step( 1, [ { cast: f.cast, phase: "SHOT", event: 1, at: 1 } ] ), [] );
	} finally {
		console.warn = original;
	}
	assert.equal( f.effects.error(), null );
	assert.equal( warnings.length, 0 );
	assert.equal( f.effects.unsupported().length, 0 );
});

test("a return stage anchored on the source plays at the caster", () => {
	// SKILL_EU_WIZARD_FIREA_TRAP_A's S_RETURN stage, as decoded. Native
	// runs AT_SOURCE through the visual block (8DDEF9); with MOV_NONE
	// there is no flight, and the stage ends in the caster-socket
	// transform (8DE210). The static cast set left AT_SOURCE out, so
	// the stage was refused (#2471).
	const f = staticFixture( {
		resource: "skill/europe/wizard_fire_trap_hit.efp",
		phase: "S_RETURN",
		damageEvent: false,
		startEvent: 0,
		action: "AT_SOURCE",
		move: "MOV_NONE",
		bone: null,
		offset: [ 0, 0, 0 ],
		targetBone: null,
		targetOffset: [ 0, 0, 0 ],
		count: 1,
		scripts: [],
		sound: "skill2/usk_wizard_fire_hit_b.wav",
		soundEnd: null,
		movement: { delayMs: 0, startSpeed: 0, endSpeed: 0 }
	} );
	const warnings = [], original = console.warn;
	console.warn = ( ...args ) => warnings.push( args.join( " " ) );
	let actors = [];
	try {
		actors = f.step( 1, [ { cast: f.cast, phase: "S_RETURN", event: 0, at: 1 } ] );
	} finally {
		console.warn = original;
	}
	assert.equal( f.effects.error(), null );
	assert.equal( warnings.length, 0 );
	assert.equal( actors.length, 1 );
	assert.equal( actors[0].pose.regionId, 257 );
	assert.equal( actors[0].pose.x, 0 );
	assert.equal( f.sounds.length, 1 );
});

function attachedFixture( stage ) {
	let id = 0;
	const jobs = new Map(), sounds = [];
	const encode = value => new TextEncoder().encode( JSON.stringify( value ) );
	const effects = createCharacterEffects(
		{
			available: () => 4,
			progress: () => null,
			health: () => ({ phase: "running" }),
			textureFeatures() {},
			install() {},
			dispose() {},
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog: { "25048": { clips: [ "buff" ], stages: [ stage ] } } } :
						{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer }
				);
				return id;
			},
			take( jobId ) {
				const r = jobs.get( jobId );
				jobs.delete( jobId );
				return r;
			},
			cancel( jobId ) {
				jobs.delete( jobId );
			}
		},
		"http://localhost",
		event => sounds.push( event ),
		createPresentationRandom( 1 )
	);
	const entity = { gid: 5, refObjId: 1, kind: "monster", name: "trap", regionId: 257, x: 0, y: 0, z: 0, heading: 0 };
	const actor = {
		gid: 5,
		model: "body",
		clip: "stand",
		time: 0,
		loop: true,
		scale: 1,
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
		height: 10
	};
	const gameplay = {
		casts: [],
		localGid: 9,
		pose: null,
		vitals: [],
		inventory: [],
		attachedEffects: [ { gid: 5, skill: 25048, token: 7, phase: 0 } ]
	};
	const step = time => effects.step( [ entity ], gameplay, time, () => true, () => 1, [], undefined, [ actor ] );
	step( 0 );
	step( 0.1 );
	return { effects, sounds, step };
}

test("an attached AT_STOP stage plays its sound and creates no visual, silently", () => {
	// ITEM_ETC_FIREWORK_BOOMB_G's ACT_S stage, as decoded. 8DCA40
	// dispatches on (action - 1), so AT_STOP (0) reaches only the sound
	// tail (8DCE2B) on the attached route and creates no visual; the
	// record's own route is the external item lane (#2471).
	const f = attachedFixture( {
		resource: "system/item_firework_bomb_g.efp",
		phase: "ACT_S",
		damageEvent: false,
		startEvent: 0,
		action: "AT_STOP",
		move: "MOV_NONE",
		bone: null,
		offset: [ 0, 0, 0 ],
		targetOffset: [ 0, 0, 0 ],
		count: 1,
		scripts: [],
		sound: "firework/firework_shot_01.wav",
		soundEnd: null,
		movement: { delayMs: 0, startSpeed: 0, endSpeed: 0 }
	} );
	const warnings = [], original = console.warn;
	console.warn = ( ...args ) => warnings.push( args.join( " " ) );
	try {
		assert.deepEqual( f.step( 0.2 ), [] );
	} finally {
		console.warn = original;
	}
	assert.equal( f.effects.error(), null );
	assert.equal( warnings.length, 0 );
	assert.equal( f.effects.unsupported().length, 0 );
	assert.equal( f.sounds.length, 1 );
	assert.equal( f.sounds[0].path, "firework/firework_shot_01.wav" );
});
