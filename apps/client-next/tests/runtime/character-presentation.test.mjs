/*
===========================================================================

character-presentation.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { goldDropModels } from "../helpers/gold-drop-models.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { publicRoot } from "../../../../scripts/build/world/paths.mjs";
/*
================
load
================
*/
async function load( entry ) {
	return import( sourceFileUrl( entry ).href );
}
const { createCharacterPresentation } = await load( "src/engine/runtime/characters/characters.ts" );
const { createCharacters } = await load( "src/engine/runtime/renderer/characters/characters.ts" );
const { createPresentationRandom } = await load( "src/engine/runtime/random/random.ts" );
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
/*
================
model

A minimal decoded character source; positionFloats sizes its geometry so a
test can charge the residency byte budget.
================
*/
function model( positionFloats = 9 ) {
	return {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		clips: [ "stand", "walk", "run", "pick", "death" ].map( name => ({ name, duration: 1, channels: [] }) ),
		images: [],
		primitives: [ {
			name: "mesh",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			geometry: {
				positions: new Float32Array( positionFloats ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				transform: identity()
			}
		} ]
	};
}
const entity = ( gid, extra = {} ) => ({
	gid,
	refObjId: gid,
	regionId: 1,
	x: 10,
	y: 20,
	z: 30,
	heading: 16384,
	movementMode: 2,
	...extra
});

test("BSR particles cross real manifest admission for drops and NPCs and follow holder lifetime", () => {
	const modifiers = [ {
		kind: 2,
		stateId: -1,
		animationSetName: "ambient",
		entries: [ {
			field00: 1,
			effectPath: "system/item_drop_equip_rare.efp",
			boneName: "",
			vector3c: [ 0, 2, 0 ],
			field4c: 0,
			flags50: [ 0, 0, 0 ],
			flag53: 0
		} ]
	}, { kind: 0, stateId: -1, animationSetName: "firework_spray", entries: [] } ];
	for ( const drops of [ true, false ] ) {
		const f = fixture( {}, 2, drops, false, false, false, false, false, false, false, modifiers );
		const body = entity(
			1,
			drops ? { kind: "ground-item", groundItem: { typeFlags: 0x8ac, tint: 0 } } : { kind: "monster" }
		);
		for ( let i = 0; i < 50; i++ ) f.step( [ body ], i / 100 );
		assert.equal( f.presentation.error(), null );
		const child = f.actors.find( a => a.attachment?.gid === 1 );
		assert.ok( child, "modifier must reach an actual effect actor" );
		assert.match( child.model, /item_drop_equip_rare/ );
		assert.equal( child.loop, true );
		assert.equal( child.pickable, false );
		assert.equal( child.attachment.root, true );
		// CIDecoAppear (spawn-fade.ts) has finished before the holder changes.
		f.step( [ { ...body, x: 40, movement: { ...body, x: 40, speed: 10 } } ], 3 );
		const advanced = f.actors.find( a => a.gid === child.gid );
		assert.ok( advanced );
		assert.ok( advanced.time > child.time, "pose changes do not restart ambient emission" );
		if ( drops ) {
			f.step( [ { ...body, groundItem: { ...body.groundItem, claimantGid: 2 } } ], 3.1 );
			assert.ok( !f.actors.some( a => a.attachment?.gid === 1 ) );
		} else {
			f.presentation.receiveLifecycle( [ { kind: "despawn", gid: 1 } ] );
			f.step( [], 3.1 );
			const transferred = f.actors.find( a => a.gid === child.gid );
			assert.ok( transferred );
			assert.notEqual( transferred.attachment.gid, 1 );
			assert.ok( transferred.time > advanced.time );
			f.step( [], 3.85 );
			const fadingChild = f.actors.find( a => a.gid === child.gid );
			assert.equal( fadingChild.opacity, undefined );
			assert.ok( Math.abs( f.actors.find( a => a.gid === fadingChild.attachment.gid ).opacity - .5 ) < 1e-7 );
			f.step( [], 4.61 );
			assert.ok( !f.actors.some( a => a.gid === child.gid ) );
		}
		f.step( [], 4 );
		assert.ok( !f.actors.some( a => a.attachment?.gid === 1 ) );
		f.presentation.reset();
		assert.equal( f.actors.length, 0 );
		f.dispose();
	}
});

test("spawned characters fade in over two seconds and NPCs appear at once", () => {
	const f = fixture();
	f.warm();
	const alpha = ( kind, at ) => {
		f.step( [ entity( 1, { kind } ) ], at );
		const actor = f.actors.find( a => a.gid === 1 );
		assert.ok( actor, kind + " is presented" );
		return actor.opacity ?? 1;
	};
	// CIDecoAppear (8D4B60): the ramp starts at the first drawable frame.
	assert.equal( alpha( "monster", 1 ), 0 );
	assert.equal( alpha( "monster", 2 ), 127 / 255 );
	assert.equal( alpha( "monster", 3 ), 1 );
	f.presentation.receiveLifecycle( [ { kind: "despawn", gid: 1 } ] );
	f.step( [], 3.5 );
	f.presentation.receiveLifecycle( [ { kind: "spawn", entity: entity( 1, { kind: "npc" } ) } ] );
	// 86EE85 clears the NPC's spawn-fade flag.
	assert.equal( alpha( "npc", 4 ), 1 );
	f.dispose();
});

test("another character keeps full alpha through every body status without a hide buff", () => {
	// 85EC00 writes body 4's 0x50, but 85D890 (CICUser_OnUpdate, every update,
	// every character but the local player) restores 0xFF when no hide buff
	// gives it concealment levels.
	const f = fixture();
	f.warm();
	// Let CIDecoAppear (spawn-fade.ts) finish: this test pins 85D890 alone.
	f.step( [ entity( 1, { kind: "player" } ) ], 1 );
	for ( const status of [ 4, 4, 3, 6, 7, 0, 4, 0 ] ) {
		f.step( [ entity( 1, { kind: "player", appearanceState: [ 1, 0, status ] } ) ], 3 );
		assert.equal( f.actors.length, 1 );
		assert.equal( f.actors[0].opacity ?? 1, 1 );
		assert.equal( f.actors[0].pickable, true );
	}
	f.dispose();
});

test("a stealthed character is hidden from strangers, translucent to its party and to a detecting viewer", () => {
	const f = fixture();
	f.warm();
	const skillCatalog = [
		{
			id: 7929,
			group: 1,
			level: 1,
			name: "SKILL_EU_ROG_STEALTHA_HIDING_A_01",
			hide: { mask: 1, level: 3 },
			spCost: 0,
			trainable: false,
			targetRequired: false,
			cooldownMs: 0,
			masteries: [],
			prerequisites: []
		},
		{
			id: 7122,
			group: 2,
			level: 1,
			name: "SKILL_ETC_DETECT_01_01",
			sight: { mask: 7, level: 3 },
			detectRange: 0,
			spCost: 0,
			trainable: false,
			targetRequired: false,
			cooldownMs: 0,
			masteries: [],
			prerequisites: []
		}
	];
	const stealthed = entity( 2, { kind: "player", name: "rogue", appearanceState: [ 1, 0, 6 ] } ),
		local = entity( 1, { kind: "local-player", name: "me" } );
	const base = {
		localGid: 1,
		pose: { regionId: 1, x: 10, y: 20, z: 30, angle: 0 },
		inventory: [],
		vitals: [],
		casts: [],
		skillCatalog
	};
	const hide = { gid: 2, skill: 7929, token: 9, phase: 2 };
	let clock = 1;
	// Let CIDecoAppear (spawn-fade.ts) finish before measuring concealment.
	for ( let i = 0; i < 80; i++ ) {
		f.presentation.step( [ local, stealthed ], base, clock += 0.1 );
		const actor = f.actors.find( value => value.gid === 2 );
		if ( actor && (actor.opacity ?? 1) === 1 ) break;
	}
	const admitted = f.actors.find( actor => actor.gid === 2 );
	assert.ok( admitted, "the peer finished resource admission" );
	assert.equal( admitted.opacity ?? 1, 1, "spawn fade has finished" );
	const opacity = gameplay => {
		for ( let i = 0; i < 5; i++ ) f.presentation.step( [ local, stealthed ], gameplay, clock += 0.1 );
		const actor = f.actors.find( a => a.gid === 2 );
		assert.ok( actor, "the stealthed actor is presented" );
		return actor.opacity ?? 1;
	};
	assert.equal( opacity( { ...base, attachedEffects: [ hide ] } ), 0, "undetected and outside the party" );
	assert.equal(
		opacity( { ...base, attachedEffects: [ hide ], social: { members: [ { id: 7, name: "rogue" } ] } } ),
		80 / 255,
		"a party member stays translucent"
	);
	assert.equal(
		opacity( { ...base, attachedEffects: [ hide, { gid: 1, skill: 7122, token: 10, phase: 2 } ] } ),
		80 / 255,
		"the local dtt covers level 3"
	);
	assert.equal( opacity( { ...base, attachedEffects: [] } ), 1, "no hide buff, no concealment" );
	f.dispose();
});

test("local body transparency survives settled camera state and stays off the mount", () => {
	const f = fixture();
	f.warm();
	const entities = [
		entity( 1, { kind: "local-player", appearanceState: [ 1, 0, 4 ], mountedOn: 2 } ),
		entity( 2, { kind: "cos" } )
	];
	const gameplay = {
		localGid: 1,
		pose: { regionId: 1, x: 10, y: 20, z: 30, angle: 16384 },
		inventory: [],
		vitals: [],
		casts: []
	};
	// This fixture has no rider socket; inspect the presentation owner's actors
	// without asking the renderer to build an unrelated mount attachment.
	// Let CIDecoAppear (spawn-fade.ts) finish before measuring body alpha.
	for ( let i = 0; i < 5; i++ ) f.presentation.step( entities, gameplay, 1 + i / 10 );
	for ( let i = 0; i < 5; i++ ) f.presentation.step( entities, gameplay, 3.5 + i / 10 );
	assert.equal( f.actors.find( a => a.gid === 1 ).opacity, 80 / 255 );
	// The mount is another character: its own 85D890 restores 0xFF.
	assert.equal( f.actors.find( a => a.gid === 2 ).opacity ?? 1, 1 );
	f.presentation.step( entities, gameplay, 4.5, undefined, -1 );
	assert.equal(
		f.actors.find( a => a.gid === 1 ).opacity,
		0,
		"active camera interpolation wins even on its completion frame"
	);
	f.presentation.step( entities, gameplay, 4.6, undefined, -1 );
	assert.equal(
		f.actors.find( a => a.gid === 1 ).opacity,
		80 / 255,
		"settled camera interpolation yields to native body alpha"
	);
	f.presentation.step( entities.map( e => ({ ...e, appearanceState: [ 1, 0, 0 ] }) ), gameplay, 5.5 );
	assert.equal( f.actors.find( a => a.gid === 1 ).opacity, 1 );
	assert.equal( f.actors.find( a => a.gid === 2 ).opacity ?? 1, 1 );
	f.dispose();
});
/*
================
fixture
================
*/
function fixture(
	effectCatalog = {},
	modelCount = 2,
	drops = false,
	eventRain = false,
	postures = false,
	idles = false,
	gear = false,
	audio = false,
	host = false,
	reference = false,
	modifiers = undefined,
	animationAudio = undefined,
	animationBindings = undefined,
	stageModels = {},
	metadataAdmission = {},
	options = {}
) {
	const characters = createCharacters(),
		pending = new Map(),
		requests = [],
		assemblies = [],
		played = [],
		failures = new Set();
	let next = 0, actors = [], instances = [], released = 0;
	const footprints = [];
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
			requests.push( { url, decode } );
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
			if ( !job || metadataAdmission.blockedPaths?.has( job.url ) ) return null;
			if ( metadataAdmission.failOnce && job.url.endsWith( "/data/characterActionData.json" ) ) {
				metadataAdmission.failOnce = false;
				pending.delete( id );
				return { kind: "error", error: "Metadata temporarily unavailable" };
			}
			if ( metadataAdmission.pending && job.url.endsWith( "/data/characterActionData.json" ) ) return null;
			pending.delete( id );
			if ( metadataAdmission.appearance ) {
				const value = job.url.endsWith( "/char/roster.json" ) ?
					metadataAdmission.appearance.roster :
					job.url.endsWith( "/data/missionPresentation.json" ) ?
					{ itemsByRefObjId: metadataAdmission.appearance.items } :
					undefined;
				if ( value ) {
					return { kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( value ) ).buffer };
				}
			}
			if ( failures.has( job.url ) ) return { kind: "error", error: "Unavailable " + job.url };
			if ( gear && job.url.endsWith( "/char/roster.json" ) ) {
				return {
					kind: "bytes",
					buffer: new TextEncoder().encode( JSON.stringify( {
						models: [],
						dress: {
							equipment: {
								100: { slot: 6, bodies: { "": { glb: "/assets/sword.glb", parts: [ "mesh" ] } } },
								101: { slot: 7, bodies: { "": { glb: "/assets/shield.glb", parts: [ "mesh" ] } } }
							}
						}
					} ) ).buffer
				};
			}
			if ( gear && job.url.endsWith( "/data/missionPresentation.json" ) ) {
				return {
					kind: "bytes",
					buffer: new TextEncoder().encode(
						JSON.stringify( {
							itemsByRefObjId: {
								100: { codename: "ITEM_CH_SWORD_01" },
								101: { codename: "ITEM_CH_SHIELD_01" }
							}
						} )
					).buffer
				};
			}
			if ( metadataAdmission.rows && job.url.endsWith( "/data/characterActionData.json" ) ) {
				return {
					kind: "bytes",
					buffer: new TextEncoder().encode(
						JSON.stringify( { characterActionEffectRows: metadataAdmission.rows } )
					).buffer
				};
			}
			if ( reference && job.url.endsWith( "/data/characterActionData.json" ) ) {
				const data = JSON.parse(
					readFileSync( path.join( publicRoot, "assets/data/characterActionData.json" ), "utf8" )
				);
				data.effectAppearanceStores = [ [ 2 ], [ 2 ] ];
				return { kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( data ) ).buffer };
			}
			if ( audio && job.url.endsWith( "/data/skillAudioData.json" ) ) {
				return {
					kind: "bytes",
					buffer: new TextEncoder().encode(
						JSON.stringify( { skillAudioRows: [ "7\t0\tSKILL_TEST_01\tSKILL_TEST" ] } )
					).buffer
				};
			}
			if ( animationAudio && job.url.endsWith( "/audio/effectsound.json" ) ) {
				return {
					kind: "bytes",
					buffer: new TextEncoder().encode( JSON.stringify( { rules: animationAudio.rules } ) ).buffer
				};
			}
			if ( audio && job.url.endsWith( "/audio/effectsound.json" ) ) {
				return {
					kind: "bytes",
					buffer: new TextEncoder().encode(
						JSON.stringify( {
							rules: [ {
								object: "PLAYER",
								handle: "SND_ACTIVATE",
								skillId: "SKILL_TEST",
								event1: "-",
								publicPath: "/assets/audio/activate.wav"
							} ]
						} )
					).buffer
				};
			}
			if (
				job.url.endsWith( "/anim/manifest.json" ) || job.url.endsWith( "/data/skillAudioData.json" ) ||
				job.url.endsWith( "/data/characterActionData.json" )
			) {
				return { kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( { models: {} } ) ).buffer };
			}
			if ( job.url.endsWith( "/itemdrop/manifest.json" ) ) {
				return {
					kind: "bytes",
					buffer: new TextEncoder().encode( JSON.stringify( {
						format: "sro-mission-itemdrop-models",
						models: metadataAdmission.dropModels ?? {
							...goldDropModels(),
							...(drops ?
								{
									"item/drop.bsr": {
										glb: "/assets/itemdrop/drop.glb",
										clips: [ "stand" ],
										clipLoop: false,
										particleModifiers: modifiers
									}
								} :
								{})
						}
					} ) ).buffer
				};
			}
			if ( drops && job.url.endsWith( "/data/missionPresentation.json" ) ) {
				return {
					kind: "bytes",
					buffer: new TextEncoder().encode(
						JSON.stringify( {
							itemsByRefObjId: { 1: { codename: "ITEM_GOLD", dropModelPath: "item/drop.bsr" } }
						} )
					).buffer
				};
			}
			if ( job.decode === "effects" ) return { kind: "effects", catalog: effectCatalog };
			if ( job.url.endsWith( "/skillfx/manifest.json" ) ) {
				return {
					kind: "bytes",
					buffer: new TextEncoder().encode(
						JSON.stringify( { format: "sro-skill-stage-models", models: stageModels } )
					).buffer
				};
			}
			if ( job.decode === "character" || job.decode === "effect" ) {
				const value = model( options.sourceFloats );
				for ( const clip of value.clips ) clip.duration = options.clipDurations?.[clip.name] ?? clip.duration;
				if ( metadataAdmission.appearance?.overrideTest ) {
					value.clips.push( { name: "native:avatar_wing:7", duration: 1, channels: [] } );
				}
				if ( host ) value.clips.push( { name: "attached-default-188", duration: 1, channels: [] } );
				if ( idles ) {
					value.clips.push(
						...[ "idle122", "idle61", "idle81" ].map( name => ({ name, duration: 1, channels: [] }) )
					);
				}
				if ( postures ) {
					value.clips.push(
						...[
							"charselect-state13",
							"charselect-state14",
							"charselect-state15",
							"deathloop",
							"down",
							"downwait",
							"downdamage",
							"wakeup",
							"deathquick",
							"emote0",
							"emote2",
							"emote6"
						].map( name => ({ name, duration: 1, channels: [] }) )
					);
				}
				if ( job.url.endsWith( "interface_lizard.glb" ) ) {
					value.clips.push( { name: "move", duration: 1, channels: [] } );
				}
				if ( job.decode === "effect" ) value.clips.push( { name: "effect", duration: 1, channels: [] } );
				return { kind: "character", model: value, images: [] };
			}
			return {
				kind: "bytes",
				buffer: new TextEncoder().encode( JSON.stringify( {
					recoveryByCodename: postures ? { NPC_1: 2000, NPC_2: 2000 } : undefined,
					models: [
						...(options.rides ?? []),
						...Array.from( { length: modelCount }, ( _, i ) => i + 1 ).map( refObjId => ({
							refObjId,
							eventRain,
							codename: metadataAdmission.appearance?.roster.models.find( r =>
								r.refObjId === refObjId
							)?.codename ?? "NPC_" + refObjId,
							particleModifiers: drops ? undefined : modifiers,
							modifierSets: modifiers?.map( m => ({
								kind: m.kind,
								stateId: m.stateId,
								animationSetName: m.animationSetName,
								count: 1,
								firstBaseWord4: m.baseWords?.[4] ?? 0
							}) ),
							animationBindings,
							glb: `/assets/${refObjId}.glb`,
							animationStates: metadataAdmission.appearance?.roster.models.find( r =>
								r.refObjId === refObjId
							)?.animationStates ?? animationAudio?.states ?? (host ?
								{
									"attached-default-188": {
										loop: true,
										durationMs: 1000,
										soundEvents: [],
										trackEvents: [],
										timeWarpCurve: { scale: 0, records: [] }
									}
								} :
								undefined),
							clips: [
								"stand",
								"walk",
								"run",
								"pick",
								"death",
								...(metadataAdmission.appearance?.overrideTest ? [ "native:avatar_wing:7" ] : []),
								...(metadataAdmission.appearance?.extraClips ?? []),
								...(host ? [ "attached-default-188" ] : []),
								...(idles ? [ "idle122", "idle61", "idle81" ] : []),
								...(postures ?
									[
										"charselect-state13",
										"charselect-state14",
										"charselect-state15",
										"deathloop",
										"down",
										"downwait",
										"downdamage",
										"wakeup",
										"deathquick",
										"emote0",
										"emote2",
										"emote6"
									] :
									[])
							]
						}) )
					]
				} ) ).buffer
			};
		}
	};
	const renderer = {
		/*
		================
		setFootprints
		================
		*/
		setFootprints( rows ) {
			footprints.push( rows );
		},
		characterParticleSnapshot: characters.particleSnapshot,
		characterMatrix: characters.matrix,
		characterParticleTime: characters.particleTime,
		presentationNight: () => true,
		characterSocket: () => null,
		setCharacterModel: characters.model,
		/*
		================
		setCharacterAssembly
		================
		*/
		setCharacterAssembly( id, base, parts ) {
			assemblies.push( { id, base, parts } );
			characters.assembly( id, base, parts );
		},
		retainCharacterModels: characters.retain,
		/*
		================
		setCharacterActors
		================
		*/
		setCharacterActors( value, portraits = [] ) {
			actors = value;
			characters.actors( value, portraits );
		}
	};
	const gpu = {
		/*
		================
		upload
		================
		*/
		upload( data ) {
			instances.push( data.instances.slice() );
			return {};
		},
		/*
		================
		updateInstances
		================
		*/
		updateInstances( draw, value ) {
			instances.push( value.slice() );
			return draw;
		},
		/*
		================
		updateBones
		================
		*/
		updateBones() {},
		/*
		================
		release
		================
		*/
		release() {
			released++;
		}
	};
	const presentation = createCharacterPresentation(
		assets,
		renderer,
		"http://localhost",
		event => played.push( event ),
		createPresentationRandom( 1 ),
		metadataAdmission.surface ?? (() => undefined)
	);
	return {
		footprints,
		portraitSource: characters.portraitSource,
		presentation,
		renderer,
		requests,
		assemblies,
		played,
		failures,
		pending,
		get actors() {
			return actors;
		},
		get instances() {
			return instances;
		},
		get released() {
			return released;
		},
		/*
		================
		step
		================
		*/
		/** @param {Record<string, unknown> | null} [gameplay] Partial gameplay fixture for this owner. */
		step( entities, time, gameplay = null ) {
			presentation.step( entities, gameplay, time );
			return characters.prepare( gpu, {
				/*
				================
				upload
				================
				*/
				upload() {
					return {};
				},
				/*
				================
				release
				================
				*/
				release() {}
			}, 1 );
		},
		/*
		================
		warm
		================
		*/
		warm() {
			for ( let t = 0; t < 30 && actors.length === 0; t++ ) this.step( [ entity( 1 ) ], t / 100 );
			assert.equal( actors.length, 1 );
			assert.equal( presentation.error(), null );
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			presentation.dispose();
			characters.dispose( gpu, null );
		}
	};
}
test("a released buff never leaves an empty pose after a stall or equipment change", () => {
	const f = fixture(
		{ 7: { clips: [ "walk", "run" ], phaseClips: [ [], [ "walk" ], [ "run" ] ], stages: [] } },
		2,
		false,
		false,
		false,
		false,
		true,
		false,
		false,
		false,
		undefined,
		{
			states: {
				walk: { durationMs: 1000, loop: true, trackEvents: [] },
				run: { durationMs: 1000, loop: false, trackEvents: [] }
			},
			rules: []
		}
	);
	f.warm();
	f.presentation.simulationOrigin( 0 );
	const player = entity( 1, { kind: "local-player" } );
	const game = {
		localGid: 1,
		inventory: [],
		vitals: [],
		casts: [ { token: 1, caster: 1, target: 0, skill: 7, damage: 0, fatal: false, receivedAtMs: 1000 } ]
	};
	f.step( [ player ], 1, game );
	f.step( [ player ], 1.5, game );
	game.casts[0].shotAtMs = 1500;
	f.step( [ player ], 1.6, game );
	game.inventory = [ { slot: 6, refObjId: 100, typeFlags: 6 << 11, plus: 0 } ];
	for ( const now of [ 5, 5.016, 5.1, 6 ] ) {
		f.step( [ player ], now, game );
		const actor = f.actors.find( actor => actor.gid === 1 );
		assert.ok( actor );
		assert.ok(
			actor.layers === undefined || actor.layers.some( layer => layer.weight > 0 ),
			`empty pose at ${now}`
		);
	}
	f.dispose();
});

for ( const local of [ false, true ] ) {
	for ( const displacement of local ? [ false, true, "after-WAIT" ] : [ false, true, "after-WAIT", "at-arrival" ] ) {
		test(`cast WAIT preserves displacement translation ${displacement}, local ${local}`, () => {
			const f = fixture(
				{ 7: { clips: [ "stand" ], phaseClips: [ [], [ "stand" ], [] ], stages: [] } },
				2,
				false,
				false,
				false,
				false,
				false,
				false,
				false,
				false,
				undefined,
				{ states: { stand: { durationMs: 1000, loop: true, trackEvents: [] } }, rules: [] }
			);
			f.warm();
			f.presentation.simulationOrigin( 0 );
			const from = { regionId: 1, x: 10, y: 20, z: 30, angle: 16384 };
			const to = { ...from, x: 110 };
			const cast = { token: 1, caster: 1, target: 0, skill: 7, damage: 0, fatal: false };
			let travelling = true;
			const frame = ( x, seconds, casting, moving ) => {
				const path = {
					from: travelling ? from : to,
					to: travelling ? to : { ...to, x: 210 },
					durationMs: travelling ? 200 : 2000,
					displacement: travelling && (displacement === true ||
						displacement === "after-WAIT" && seconds >= 1.1 ||
						displacement === "at-arrival" && seconds >= 1.3)
				};
				const player = entity( 1, {
					kind: local ? "local-player" : "player",
					x,
					moving,
					movementMode: 3,
					movementRevision: travelling ? 1 : 2,
					movementPath: local && !moving ? undefined : path,
					poseAtMs: seconds * 1000
				} );
				f.step( [ player ], seconds, {
					localGid: local ? 1 : 2,
					pose: { ...from, x },
					moving,
					movementRevision: travelling ? 1 : 2,
					movementPath: local && !moving ? undefined : path,
					poseAtMs: seconds * 1000,
					inventory: [],
					vitals: [],
					casts: casting ? [ { ...cast, token: travelling ? 1 : 2 } ] : []
				} );
				assert.equal( f.presentation.error(), null );
				return { ...f.actors.find( actor => actor.gid === 1 ).pose };
			};
			frame( 10, 1, false, true );
			const entered = frame( 20, 1.02, true, true );
			frame( 60, 1.1, true, true );
			const settled = frame( 110, 1.3, false, false );
			const later = frame( 110, 3, false, false );
			if ( displacement ) {
				assert.equal( settled.x, 110, "skill travel must reach its displayed endpoint" );
				assert.equal( later.x, 110, "cast retirement must not restore a navigation pin" );
			} else {
				assert.equal( settled.x, entered.x, "ordinary navigation still stops on the WAIT transition" );
				assert.equal( later.x, entered.x );
			}
			travelling = false;
			frame( 110, 3.02, false, true );
			const nextHold = frame( 111, 3.04, true, true );
			assert.equal(
				frame( 113, 3.08, true, true ).x,
				nextHold.x,
				"a new ordinary walk must hold during WAIT after the displacement has settled"
			);
			f.dispose();
		});
	}
}

test("gecko starts the walk immediately, fades out over 200ms at its end and resets behind the dock gate", () => {
	const f = fixture();
	f.warm();
	const step = ( time, visible = true ) => f.presentation.step( [], null, time, undefined, 0, [], null, visible );
	for ( let i = 0; i < 8 && !f.actors.some( a => a.gid === -1 ); i++ ) step( 1 );
	assert.equal( f.actors[0].gid, -1 );
	assert.equal( f.actors[0].clip, "move" );
	assert.deepEqual( f.actors[0].layers.map( l => [ l.clip, l.weight ] ), [ [ "move", 1 ], [ "stand", 1 ] ] );
	step( 1.1 );
	assert.equal( f.actors[0].layers[0].weight, 1 );
	const placement = f.actors[0].pose;
	step( 2.1 );
	assert.ok( Math.abs( f.actors[0].layers[0].weight - .5 ) < 1e-6 );
	assert.equal( f.actors[0].layers[0].time, 1 );
	step( 2.3 );
	assert.equal( f.actors[0].clip, "stand" );
	assert.equal( f.actors[0].loop, true );
	assert.deepEqual( f.actors[0].pose, placement );
	step( 3, false );
	assert.deepEqual( f.actors, [] );
	for ( let i = 0; i < 8 && !f.actors.length; i++ ) step( 4 );
	assert.equal( f.actors[0].time, 0 );
	f.dispose();
});
test("level-up admits a cold one-shot effect, follows the entity, and expires without a combat cast", () => {
	const stage = {
		phase: "ACT_S",
		startEvent: 0,
		move: "MOV_NONE",
		scripts: [],
		action: "AT_ONE_FOLLOW",
		resource: "system/system_levelup.efp",
		count: 1,
		offset: [ 0, 0, 0 ],
		life: 1
	};
	const f = fixture( { "-2147483642": { stages: [ stage ] } } );
	f.warm();
	const entities = [ entity( 1, { kind: "local-player" } ) ];
	f.presentation.receiveFeedback( [ { kind: "level-up", gid: 1 } ], entities );
	for ( let i = 0; i < 30 && !f.actors.some( a => a.model.includes( "system_levelup" ) ); i++ ) {
		f.step( entities, 1 + i / 100 );
	}
	const effect = f.actors.find( a => a.model.includes( "system_levelup" ) );
	assert.ok( effect );
	assert.equal( effect.loop, false );
	assert.equal( effect.pickable, false );
	assert.equal( effect.attachment.gid, 1 );
	f.step( [ entity( 1, { kind: "local-player", x: 50 } ) ], 1.5 );
	assert.equal( f.actors.find( a => a.gid === effect.gid ).attachment.gid, 1 );
	assert.equal( f.actors.find( a => a.gid === 1 ).pose.x, 10, "a stalled frame retains the displayed pose" );
	f.step( [ entity( 1, { kind: "local-player", x: 50 } ) ], 1.6 );
	assert.equal( f.actors.find( a => a.gid === 1 ).pose.x, 50 );
	f.step( entities, 3 );
	assert.ok( !f.actors.some( a => a.model.includes( "system_levelup" ) ) );
	f.step( entities, 4 );
	assert.equal( f.actors.length, 1 );
	f.dispose();
});

test("reward feedback received after deathloop begins still launches from the corpse", () => {
	const f = fixture( {}, 2, false, false, true );
	f.warm();
	const entities = [
		entity( 1, { kind: "monster", appearanceState: [ 2, 0, 0 ] } ),
		entity( 2, { kind: "local-player" } )
	];
	for ( let i = 0; i < 10; i++ ) f.step( entities, 1 + i / 100 );
	f.step( entities, 2.5 );
	assert.equal( f.actors.find( a => a.gid === 1 ).clip, "deathloop" );
	f.presentation.receiveFeedback( [ { kind: "orb-feedback", source: 1, target: 2, color: 2, count: 1 } ], entities );
	for ( let i = 0; i < 10; i++ ) f.step( entities, 2.6 + i / 100 );
	assert.ok(
		f.requests.some( r => /indraft/i.test( r.url ) ),
		"settled corpse must release late reward feedback into model admission"
	);
	f.dispose();
});

test("monster grade changes update actual rendered size without respawning the actor", () => {
	const f = fixture();
	f.warm();
	for (
		const [rarity, scale] of [ [ 0, 1 ], [ 1, 1.5 ], [ 2, 1 ], [ 3, 1 ], [ 4, 3 ], [ 5, 1 ], [
			6,
			Math.fround( 1.7 )
		], [ 7, 1 ] ]
	) {
		f.step( [ entity( 1, { kind: "monster", rarity, tidWord: 0xc6 } ) ], 1 + rarity );
		assert.equal( f.actors[0].scale, scale );
		assert.equal( Math.abs( f.instances.at( -1 )[5] ), scale );
	}
	f.dispose();
});

test("native entity headings travel through the real presenter and renderer exactly once", () => {
	const f = fixture();
	f.warm();
	let now = 1;
	for ( const heading of [ 0, 16384, 32768, 49151, 65535 ] ) {
		f.step( [ entity( 1, { heading } ) ], now++ );
		const matrix = f.instances.at( -1 ), yaw = heading / 65535 * 2 * Math.PI + Math.PI / 2;
		assert.ok( Math.abs( matrix[0] + Math.cos( yaw ) ) < 1e-6 );
		assert.ok( Math.abs( matrix[8] - Math.sin( yaw ) ) < 1e-6 );
		assert.ok( Math.abs( matrix[10] + Math.cos( yaw ) ) < 1e-6, "imported model forward follows native -Z" );
	}
	assert.equal( f.presentation.error(), null );
	f.dispose();
});

test("automatic idle reaches the rendered event lane and movement/despawn reset its lifecycle", () => {
	const f = fixture( {}, 2, false, false, false, true );
	f.warm();
	f.step( [ entity( 1 ) ], 14 );
	assert.equal( f.actors[0].layers, undefined );
	f.step( [ entity( 1 ) ], 16 );
	assert.equal( f.actors[0].layers[0].clip, "idle81" );
	f.step( [ entity( 1, { x: 11 } ) ], 16.1 );
	assert.ok( !f.actors[0].layers?.some( layer => layer.clip.startsWith( "idle" ) ) );
	f.step( [], 17 );
	for ( let i = 0; i < 5 && !f.actors.length; i++ ) f.step( [ entity( 1 ) ], 18 + i / 100 );
	assert.equal( f.actors.length, 1 );
	assert.ok( !f.actors[0].layers?.some( layer => layer.clip.startsWith( "idle" ) ) );
	f.dispose();
});

test("local prediction and remote destination headings face travel through all quadrants and a second click", async () => {
	const { movementHeading } = await load( "src/engine/foundation/gameplay/native-movement.ts" );
	const f = fixture();
	f.warm();
	let from = { regionId: 1, x: 100, y: 20, z: 100, angle: 0 }, now = 2;
	for (
		const [dx, dz] of [ [ 0, -20 ], [ 20, 0 ], [ 0, 20 ], [ -20, 0 ], [ 20, -20 ], [ -20, -20 ], [ 20, 20 ], [
			-20,
			20
		] ]
	) {
		const to = { ...from, x: from.x + dx, z: from.z + dz };
		to.angle = movementHeading( from, to );
		for ( const local of [ false, true ] ) {
			const remote = entity( 1, { ...to, heading: local ? 1234 : to.angle } );
			f.step( [ remote ], now, local ? { localGid: 1, pose: to, inventory: [], vitals: [], casts: [] } : null );
			f.step(
				[ remote ],
				now + .1,
				local ? { localGid: 1, pose: to, inventory: [], vitals: [], casts: [] } : null
			);
			now += .5;
			const m = f.instances.at( -1 );
			assert.ok(
				(m[8] * dx + m[10] * dz) / Math.hypot( dx, dz ) > .999999,
				"model forward must point along travel"
			);
			assert.equal( f.actors[0].pose.x, to.x );
			assert.equal( f.actors[0].pose.z, to.z );
		}
		from = to;
	}
	f.dispose();
});
test("failed model loading permits movement, despawn and a paced successful retry", () => {
	const f = fixture();
	f.warm();
	f.failures.add( "http://localhost/assets/2.glb" );
	f.step( [ entity( 1 ), entity( 2 ) ], 1 );
	f.step( [ entity( 1, { x: 25 } ), entity( 2 ) ], 1.05 );
	assert.equal( f.actors[0].clip, "walk" );
	f.step( [ entity( 1, { x: 25 } ), entity( 2 ) ], 1.1 );
	assert.equal( f.actors[0].pose.x, 25 );
	assert.equal( f.actors[0].clip, "stand", "arrival stops locomotion even when another model failed" );
	assert.match( f.presentation.error(), /Unavailable/ );
	f.step( [ entity( 1, { x: 30 } ), entity( 2 ) ], 1.2 );
	assert.equal( f.requests.filter( r => r.url.endsWith( "/2.glb" ) ).length, 1 );
	f.failures.clear();
	f.step( [ entity( 1 ), entity( 2 ) ], 3.2 );
	f.step( [ entity( 1 ), entity( 2 ) ], 3.3 );
	assert.deepEqual( f.actors.map( a => a.gid ), [ 1, 2 ] );
	assert.equal( f.presentation.error(), null );
	f.failures.add( "http://localhost/assets/1.glb" );
	f.step( [], 4 );
	assert.deepEqual( f.actors, [] );
	assert.ok( f.released > 0 );
	f.dispose();
});
test("despawn during an asset failure never retains a ghost actor", () => {
	const f = fixture();
	f.warm();
	f.failures.add( "http://localhost/assets/2.glb" );
	f.step( [ entity( 1 ), entity( 2 ) ], 1 );
	f.step( [ entity( 1 ), entity( 2 ) ], 1.1 );
	f.step( [], 1.2 );
	assert.deepEqual( f.actors, [] );
	assert.equal( f.pending.size, 0 );
	f.dispose();
});
test("missing equipment metadata does not stop current poses or unrelated actors", () => {
	const f = fixture();
	f.warm();
	const gameplay = {
		localGid: 1,
		pose: { regionId: 1, x: 90, y: 20, z: 30, angle: 16384 },
		inventory: [ { slot: 0, refObjId: 999 } ],
		vitals: [],
		casts: []
	};
	f.step( [ entity( 1 ) ], 1, gameplay );
	assert.equal( f.actors[0].pose.x, 10, "asset failure does not bypass stall recovery" );
	f.step( [ entity( 1 ) ], 1.1, gameplay );
	assert.equal( f.actors[0].pose.x, 90 );
	assert.match( f.presentation.error(), /Missing native equipment visual catalog/ );
	f.step( [], 2, gameplay );
	assert.deepEqual( f.actors, [] );
	f.dispose();
});
test("reset cancels loading and can rebuild characters from retained manifests", () => {
	const f = fixture();
	f.warm();
	f.step( [ entity( 1 ), entity( 2 ) ], 1 );
	f.presentation.reset();
	assert.equal( f.pending.size, 0 );
	f.step( [ entity( 1 ) ], 2 );
	f.step( [ entity( 1 ) ], 2.1 );
	assert.equal( f.actors.length, 1 );
	f.dispose();
});

test("special COS action 1 renders emote 0 even without an emote 1 resource", () => {
	const f = fixture( {}, 2, false, false, true );
	f.warm();
	const pet = entity( 1, { kind: "cos", tidWord: 0x19c6, emote: { action: 1, revision: 1, atMs: 1000 } } );
	f.step( [ pet ], 1 );
	f.step( [ pet ], 1.1 );
	assert.equal( f.actors[0].layers[0].clip, "emote0" );
	f.step( [ pet ], 2.5 );
	assert.ok( !f.actors[0].layers?.some( l => l.clip === "emote0" ) );
	f.dispose();
});

test("gesture hand suppression is actor-local and restores current peer equipment on exit", () => {
	const f = fixture( {}, 2, false, false, true, false, true );
	f.warm();
	const equipment = [ { slot: 6, refObjId: 100, plus: 0 }, { slot: 7, refObjId: 101, plus: 0 } ];
	const player = entity( 1, { kind: "player", equipment } ), peer = entity( 2, { kind: "player", equipment } );
	for ( let i = 0; i < 20; i++ ) f.step( [ player, peer ], .4 + i / 100 );
	assert.match( f.actors[0].model, /sword/ );
	assert.match( f.actors[1].model, /shield/ );
	player.emote = { action: 0, revision: 1, atMs: 1000 };
	f.step( [ player, peer ], 1 );
	assert.equal( f.actors[0].model, "/assets/1.glb", "body stays visible" );
	assert.match( f.actors[1].model, /sword/, "shared source model is unchanged" );
	player.equipment = [ { slot: 7, refObjId: 101, plus: 0 } ];
	f.step( [ player, peer ], 1.1 );
	assert.equal( f.actors[0].model, "/assets/1.glb" );
	f.step( [ player, peer ], 2.5 );
	assert.match( f.actors[0].model, /shield/ );
	assert.doesNotMatch( f.actors[0].model, /sword/ );
	player.emote = { action: 2, revision: 2, atMs: 3000 };
	f.step( [ player, peer ], 3 );
	assert.equal( f.actors[0].layers[0].clip, "emote2" );
	assert.match( f.actors[0].model, /shield/ );
	f.dispose();
});

test("player emote drives authored pose, ends, and is interrupted by movement", () => {
	const f = fixture( {}, 2, false, false, true );
	f.warm();
	for ( let i = 0; i < 20; i++ ) f.step( [ entity( 1 ) ], .3 + i / 100 );
	const player = entity( 1, { kind: "local-player", emote: { action: 6, revision: 1, atMs: 1000 } } );
	f.step( [ player ], 1 );
	f.step( [ player ], 1.1 );
	assert.equal( f.actors[0].layers[0].clip, "emote6" );
	assert.equal( f.actors[0].layers[0].time, 0 );
	f.step( [ player ], 1.5 );
	assert.ok( Math.abs( f.actors[0].layers[0].time - .3 ) < 1e-6 );
	f.step( [ player ], 2.5 );
	assert.ok( !f.actors[0].layers?.some( l => l.clip === "emote6" ) );
	player.emote = { action: 0, revision: 2, atMs: 3000 };
	f.step( [ player ], 3 );
	assert.equal( f.actors[0].layers[0].clip, "emote0" );
	f.step( [ { ...player, x: 12 } ], 3.1 );
	assert.ok( !f.actors[0].layers?.some( l => l.clip === "emote0" ) );
	f.dispose();
});

test("type-4 callback presents down, held-down and automatic wake without replaying the cast", () => {
	const stage = {
		resource: null,
		damageEvent: true,
		startEvent: 0,
		action: "AT_DMG_POS",
		move: "MOV_NONE",
		bone: null,
		offset: [ 0, 0, 0 ],
		life: 0,
		sound: null,
		count: 1,
		scripts: []
	};
	const f = fixture( { "1": { clips: [], phaseClips: [ [], [], [] ], stages: [ stage ] } }, 2, false, false, true );
	const actors = [ entity( 1 ), entity( 2 ) ];
	for ( let i = 0; i < 40; i++ ) f.step( actors, i / 100 );
	const cast = {
			token: 1,
			caster: 1,
			target: 2,
			skill: 1,
			results: [ { target: 2, impacts: [ { type: 4, damage: 0, fatal: false, flags: 0, secondaryAmount: 0 } ] } ]
		},
		gameplay = { localGid: 1, inventory: [], vitals: [], casts: [ cast ] };
	const target = () => f.actors.find( a => a.gid === 2 );
	f.step( actors, 1, gameplay );
	assert.ok( target().layers.some( l => l.clip === "down" ) );
	gameplay.casts = [];
	f.step( actors, 2.1, gameplay );
	assert.deepEqual( target().layers.map( l => l.clip ), [ "downwait" ] );
	f.step( actors, 3.501, gameplay );
	assert.equal( target().layers[0].clip, "wakeup" );
	f.step( actors, 4.8, gameplay );
	assert.ok( !target().layers?.some( l => l.clip === "wakeup" || l.clip === "downwait" ) );
	f.dispose();
});

test("death during knockdown chooses down-death and never plays a delayed wake after revival", () => {
	const stage = {
		resource: null,
		damageEvent: true,
		startEvent: 0,
		action: "AT_DMG_POS",
		move: "MOV_NONE",
		bone: null,
		offset: [ 0, 0, 0 ],
		life: 0,
		sound: null,
		count: 1,
		scripts: []
	};
	const f = fixture( { "1": { clips: [], phaseClips: [ [], [], [] ], stages: [ stage ] } }, 2, false, false, true );
	let actors = [ entity( 1 ), entity( 2 ) ];
	for ( let i = 0; i < 40; i++ ) f.step( actors, i / 100 );
	const gameplay = {
		localGid: 1,
		inventory: [],
		vitals: [],
		casts: [ {
			token: 1,
			caster: 1,
			target: 2,
			skill: 1,
			results: [ { target: 2, impacts: [ { type: 4, damage: 0, fatal: false, flags: 0, secondaryAmount: 0 } ] } ]
		} ]
	};
	f.step( actors, 1, gameplay );
	gameplay.casts = [];
	actors = [ actors[0], { ...actors[1], appearanceState: [ 2, 0, 0 ] } ];
	f.step( actors, 1.1, gameplay );
	assert.equal( f.actors.find( a => a.gid === 2 ).clip, "deathquick" );
	assert.equal( f.actors.find( a => a.gid === 2 ).loop, false );
	actors = [ actors[0], { ...actors[1], appearanceState: [ 1, 0, 0 ] } ];
	f.step( actors, 2, gameplay );
	f.step( actors, 4, gameplay );
	assert.equal( f.actors.find( a => a.gid === 2 ).clip, "stand" );
	assert.ok( !f.actors.find( a => a.gid === 2 ).layers?.some( l => l.clip === "wakeup" ) );
	f.dispose();
});

test("crowd admission preserves local player and nearest actors without stopping movement", () => {
	const f = fixture();
	f.warm();
	const entities = Array.from( { length: 700 }, ( _, i ) => entity( i + 1, { refObjId: 1, x: i } ) );
	const gameplay = {
		localGid: 700,
		pose: { regionId: 1, x: 690, y: 20, z: 30, angle: 0 },
		inventory: [],
		vitals: [],
		casts: []
	};
	assert.doesNotThrow( () => f.step( entities, 1, gameplay ) );
	assert.equal( f.actors.length, 512 );
	assert.equal( f.actors[0].gid, 700 );
	assert.ok( f.actors.some( a => a.gid === 691 ) );
	assert.ok( !f.actors.some( a => a.gid === 1 ) );
	gameplay.pose.x = 800;
	f.step( entities, 2, gameplay );
	assert.equal( f.actors[0].pose.x, 690, "crowd admission retains presentation continuity after a stall" );
	f.step( entities, 2.1, gameplay );
	assert.equal( f.actors[0].pose.x, 800 );
	f.step( [], 3, gameplay );
	assert.equal( f.actors.length, 0 );
	f.dispose();
});
test("rejected manifest publishes no rows and repaired retry commits completely", () => {
	let id = 0, bad = true;
	const jobs = new Map(), requested = [], modelRequests = [];
	const row = i => ({
		refObjId: i,
		codename: "NPC_" + i,
		glb: "/assets/" + i + ".glb",
		clips: [ "stand" ],
		animationStates: i === 2 ? { stand: { durationMs: bad ? -1 : 1000 } } : undefined
	});
	const assets = {
		available: () => 4,
		/*
		================
		request
		================
		*/
		request( url, limit, decode ) {
			jobs.set( ++id, { url, decode } );
			requested.push( url );
			return id;
		},
		/*
		================
		cancel
		================
		*/
		cancel( id ) {
			jobs.delete( id );
		},
		/*
		================
		take
		================
		*/
		take( id ) {
			const job = jobs.get( id );
			jobs.delete( id );
			if ( !job ) return null;
			if ( job.url.endsWith( "/itemdrop/manifest.json" ) ) {
				return {
					kind: "bytes",
					buffer: new TextEncoder().encode(
						JSON.stringify( { format: "sro-mission-itemdrop-models", models: {} } )
					).buffer
				};
			}
			if ( job.decode === "effects" ) return { kind: "effects", catalog: {} };
			if ( job.decode === "character" ) {
				modelRequests.push( job.url );
				return { kind: "character", model: model(), images: [] };
			}
			return {
				kind: "bytes",
				buffer: new TextEncoder().encode( JSON.stringify(
					job.url.includes( "skillfx" ) ?
						{ format: "sro-skill-stage-models", models: {} } :
						job.url.includes( "/anim/" ) ?
						{ models: {} } :
						{ models: [ row( 1 ), row( 2 ) ] }
				) ).buffer
			};
		}
	};
	const c = createCharacters(),
		p = createCharacterPresentation(
			assets,
			{
				setCharacterModel: c.model,
				setCharacterAssembly: c.assembly,
				setCharacterActors: c.actors,
				retainCharacterModels: c.retain
			},
			"http://localhost",
			event => played.push( event ),
			createPresentationRandom( 1 )
		);
	p.step( [ entity( 1 ), entity( 2 ) ], null, 0 );
	p.step( [ entity( 1 ), entity( 2 ) ], null, 0.1 );
	assert.match( p.error(), /animation duration/ );
	assert.equal( requested.filter( url => url.endsWith( ".glb" ) ).length, 0 );
	bad = false;
	for ( let i = 0; i < 12; i++ ) p.step( [ entity( 1 ), entity( 2 ) ], null, 3 + i / 10 );
	assert.equal( c.stats().actors, 2 );
	assert.equal( p.error(), null );
	p.dispose();
	c.dispose( null, null );
});

test("effects cannot push a full character population over renderer capacity", () => {
	const stage = {
		resource: "hit.efp",
		damageEvent: true,
		startEvent: 1,
		action: "AT_DMG_POS",
		move: "MOV_NONE",
		bone: null,
		offset: [ 0, 0, 0 ],
		life: 10,
		sound: null,
		count: 1,
		scripts: []
	};
	const f = fixture( { "1": { clips: [ "stand" ], stages: [ stage ] } } );
	f.warm();
	const entities = Array.from( { length: 512 }, ( _, i ) => entity( i + 1, { refObjId: 1 } ) ),
		gameplay = { localGid: 1, inventory: [], vitals: [], casts: [ { token: 1, caster: 1, target: 2, skill: 1 } ] };
	for ( let i = 0; i < 5; i++ ) assert.doesNotThrow( () => f.step( entities, 1 + i / 10, gameplay ) );
	assert.equal( f.actors.length, 512 );
	assert.ok( f.actors.every( actor => actor.gid > 0 ) );
	f.dispose();
});

test("a cold attack effect never strips equipment, drops a fighter or refetches a decoded model", () => {
	const stage = resource => ({
		resource,
		damageEvent: false,
		startEvent: 0,
		action: "AT_DMG_POS",
		move: "MOV_NONE",
		bone: null,
		offset: [ 0, 0, 0 ],
		life: 10,
		sound: null,
		count: 1,
		scripts: []
	});
	const f = fixture(
		{ "1": { clips: [], phaseClips: [ [], [], [] ], stages: [ stage( "cast.efp" ), stage( "hit.efp" ) ] } },
		3,
		false,
		false,
		false,
		false,
		true
	);
	f.warm();
	const gameplay = {
		localGid: 1,
		pose: { regionId: 1, x: 10, y: 20, z: 30, angle: 16384 },
		inventory: [ { slot: 6, refObjId: 100, plus: 0 } ],
		vitals: [],
		casts: []
	};
	const fighters = [ entity( 1, { kind: "local-player" } ), entity( 2, { kind: "monster", refObjId: 2 } ) ],
		reinforcement = entity( 3, { kind: "monster", refObjId: 3 } );
	for ( let i = 0; i < 80; i++ ) f.step( [ ...fighters, reinforcement ], .4 + i / 100, gameplay );
	assert.deepEqual( f.actors.map( actor => actor.gid ).sort( ( a, b ) => a - b ), [ 1, 2, 3 ] );
	assert.match( f.actors.find( actor => actor.gid === 1 ).model, /sword/ );
	for ( let i = 0; i < 10; i++ ) f.step( fighters, 1.3 + i / 100, gameplay );
	// Basic attack: two effect programs are wanted in the same frame and neither is
	// decoded. Speculation for them must not unadmit the fighters, strip equipment
	// that is already resident, or evict the monster that just left the frame.
	gameplay.casts = [ { token: 1, caster: 1, target: 2, skill: 1 } ];
	for ( let i = 0; i < 20; i++ ) {
		f.step( [ ...fighters, reinforcement ], 1.5 + i / 100, gameplay );
		for ( const gid of [ 1, 2, 3 ] ) {
			assert.ok(
				f.actors.some( actor => actor.gid === gid ),
				`fighter ${gid} stays admitted while an attack effect decodes`
			);
		}
		assert.match(
			f.actors.find( actor => actor.gid === 1 ).model,
			/sword/,
			"a cold effect never strips admitted equipment"
		);
	}
	assert.equal(
		f.requests.filter( row => row.url.endsWith( "/3.glb" ) ).length,
		1,
		"a decoded source is reused, not refetched, when its actor returns"
	);
	assert.equal(
		f.actors.filter( actor => actor.gid < 0 ).length,
		2,
		"both attack effects are admitted, one load slot at a time"
	);
	assert.equal(
		f.requests.filter( row => row.url.includes( "programs.json#" ) ).length,
		2,
		"a decoded effect is not refetched for the next attack"
	);
	assert.equal( f.presentation.error(), null );
	f.dispose();
});

test("model priority evicts lower priority residents and never starves a late local player", async () => {
	// Heavy sources make the residency byte budget bind with a handful of
	// actors. More entities than the budget can hold compete; the local
	// player arrives last and must still be admitted by eviction.
	const { CHARACTER_RESIDENT_BYTES, CHARACTER_SOURCE_BYTES } = await load(
		"src/engine/foundation/animation/character-budget.ts"
	);
	const sourceBytes = CHARACTER_SOURCE_BYTES - 1024 * 1024;
	const most = Math.floor( CHARACTER_RESIDENT_BYTES / sourceBytes );
	const count = most + 2;
	const f = fixture(
		{},
		count,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		undefined,
		undefined,
		undefined,
		{},
		{},
		{ sourceFloats: Math.floor( sourceBytes / 12 ) * 3 }
	);
	const entities = Array.from( { length: count }, ( _, i ) => entity( i + 1 ) );
	const others = entities.slice( 0, count - 1 );
	let t = 0;
	for ( let i = 0; i < 4 * count; i++ ) f.step( others, t += .1 );
	assert.ok( f.actors.length > 0 && f.actors.length < others.length, "the byte budget binds" );
	const gameplay = {
		localGid: count,
		pose: { regionId: 1, x: 10, y: 20, z: 30, angle: 0 },
		inventory: [],
		vitals: [],
		casts: []
	};
	for ( let i = 0; i < 4 * count && !f.actors.some( a => a.gid === count ); i++ ) {
		f.step( entities, t += .1, gameplay );
	}
	assert.ok( f.actors.some( a => a.gid === count ), "the late local player is admitted" );
	assert.ok( f.actors.length <= most, "residency stays within the byte budget" );
	assert.equal( f.requests.filter( r => r.url.endsWith( `/${count}.glb` ) ).length, 1 );
	assert.equal( f.presentation.error(), null );
	const evicted = others.find( e => !f.actors.some( a => a.gid === e.gid ) );
	assert.ok( evicted, "a lower-priority resident made room" );
	gameplay.localGid = evicted.gid;
	for ( let i = 0; i < 4 * count && !f.actors.some( a => a.gid === evicted.gid ); i++ ) {
		f.step( entities, t += .1, gameplay );
	}
	assert.ok( f.actors.some( a => a.gid === evicted.gid ), "a new local player evicts again" );
	assert.equal( f.presentation.error(), null );
	f.dispose();
});

test("ground items use authored drop models rather than a colliding character reference", () => {
	const f = fixture( {}, 2, true );
	for ( let i = 0; i < 20; i++ ) {
		f.step(
			[ entity( 1, { kind: "ground-item", groundItem: { typeFlags: 0x2ec, goldAmount: 10, tint: 0 } } ) ],
			i / 10
		);
	}
	assert.equal( f.actors.length, 1 );
	assert.equal( f.actors[0].model, "/assets/itemdrop/drop.glb" );
	assert.equal( f.actors[0].loop, false );
	assert.equal( f.presentation.error(), null );
	f.step( [], 3 );
	assert.deepEqual( f.actors, [] );
	f.dispose();
});
test("pickup event plays once and mounted riders ignore it", () => {
	const f = fixture();
	f.warm();
	f.step( [ entity( 1, { pickupRevision: 1 } ) ], 1 );
	assert.equal( f.actors[0].layers[0].clip, "pick" );
	f.step( [ entity( 1, { pickupRevision: 1 } ) ], 2.1 );
	assert.equal( f.actors[0].layers, undefined );
	f.step( [ entity( 1, { pickupRevision: 2, mountedOn: 2 } ), entity( 2 ) ], 3 );
	assert.equal( f.actors[0].layers, undefined );
	f.dispose();
});

test("published gecko starts at the walk-on pose, never at the stand pose, across repeated entry", async () => {
	const { readFile } = await import( "node:fs/promises" ),
		{ createModelDecoder } = await load( "src/engine/runtime/assets/worker/model/model.ts" ),
		{ createCharacterPose } = await load( "src/engine/foundation/animation/animation-pose.ts" ),
		{ oneShotLayers } = await load( "src/engine/foundation/animation/one-shot-layers.ts" );
	const decoder = createModelDecoder(),
		bytes = await readFile( CLIENT_PUBLIC_ROOT + "/assets/character-select/interface_lizard.glb" ),
		m = decoder.character( decoder.decode( bytes ) ),
		pose = createCharacterPose( m ),
		reference = createCharacterPose( m ),
		duration = m.clips.find( c => c.name === "move" ).duration;
	reference.evaluate( "stand", 0 );
	const stand = reference.socket( "Bip01" );
	reference.evaluate( "move", 0, false );
	const move = reference.socket( "Bip01" );
	assert.notDeepEqual( move, stand );
	for ( let entry = 0; entry < 3; entry++ ) {
		for ( const t of [ 0, .001, .033, .1, .199, .3 ] ) {
			pose.evaluate( "move", t, false, oneShotLayers( "move", "stand", t, duration, .2 ) );
			reference.evaluate( "move", t, false );
			assert.deepEqual( pose.socket( "Bip01" ), reference.socket( "Bip01" ) );
		}
	}
	pose.evaluate( "move", duration + .1, false, oneShotLayers( "move", "stand", duration + .1, duration, .2 ) );
	assert.ok( pose.socket( "Bip01" ).every( Number.isFinite ) );
	pose.evaluate( "stand", duration + .2, true, oneShotLayers( "move", "stand", duration + .2, duration, .2 ) );
	reference.evaluate( "stand", duration + .2, true );
	assert.deepEqual( pose.socket( "Bip01" ), reference.socket( "Bip01" ) );
});

test("customization prepares both genders and swaps every figure without a new request", async () => {
	const { readFileSync } = await import( "node:fs" );
	const { initialCreation, creationLoadout, creationRange } = await load(
		"src/engine/foundation/ui/character-create.ts"
	);
	const roster = readFileSync( CLIENT_PUBLIC_ROOT + "/assets/char/roster.json" ),
		pending = new Map(),
		requests = [];
	let serial = 0, allow = false, actors = [];
	const assets = {
		available: () => 4,
		/*
		================
		request
		================
		*/
		request( url, limit, decode ) {
			pending.set( ++serial, { url, decode } );
			requests.push( url );
			return serial;
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
			if ( !job || !allow ) return null;
			pending.delete( id );
			return job.decode === "character" ?
				{ kind: "character", model: model(), images: [] } :
				{
					kind: "bytes",
					buffer: roster.buffer.slice( roster.byteOffset, roster.byteOffset + roster.byteLength )
				};
		}
	};
	const renderer = {
		/*
		================
		setCharacterModel
		================
		*/
		setCharacterModel() {},
		/*
		================
		setCharacterAssembly
		================
		*/
		setCharacterAssembly() {},
		/*
		================
		retainCharacterModels
		================
		*/
		retainCharacterModels() {},
		/*
		================
		setCharacterActors
		================
		*/
		setCharacterActors( value ) {
			actors = value;
		}
	};
	const owner = createCharacterPresentation(
			assets,
			renderer,
			"http://localhost",
			event => played.push( event ),
			createPresentationRandom( 1 )
		),
		selection = initialCreation( 0, 0 );
	const step = () => owner.step( [], null, 1, undefined, 0, undefined, { selection, yaw: 0, protectorFloor: 0 } );
	step();
	assert.equal( owner.previewReady(), false );
	allow = true;
	for ( let i = 0; i < 100 && !owner.previewReady(); i++ ) step();
	assert.equal( owner.error(), null );
	assert.equal( owner.previewReady(), true );
	const warmed = requests.length;
	allow = false;
	const { createItemCodenameIndex } = await load( "src/engine/foundation/animation/equipment-appearance.ts" );
	const itemIds = createItemCodenameIndex( JSON.parse( roster.toString( "utf8" ) ).dress );
	// No protector: the ownerless preview wears the native default clothing.
	const defaultClothing = ':["clothes_BA","clothes_LA"]';
	for ( const gender of [ 0, 1, 0 ] ) {
		for ( let figure = 1; figure <= 13; figure++ ) {
			Object.assign( selection, { gender, figure } );
			step();
			assert.equal( owner.previewReady(), true );
			assert.equal( actors.length, 1 );
			const { heightScale, volumeScale, ...assembly } = creationLoadout( selection, itemIds );
			assert.equal( actors[0].model, "creation:0:" + JSON.stringify( assembly ) + defaultClothing );
			assert.equal( requests.length, warmed );
		}
	}
	for ( const gender of [ 0, 1 ] ) {
		selection.gender = gender;
		const maxWeapon = creationRange( selection, "weapon", 0 )[1];
		for ( let weapon = 0; weapon <= maxWeapon; weapon++ ) {
			selection.weapon = weapon;
			const maxProtector = creationRange( selection, "protector", 0 )[1];
			for ( let protector = 0; protector <= maxProtector; protector++ ) {
				selection.protector = protector;
				step();
				assert.equal( owner.previewReady(), true, "Every offered wardrobe is resident before reveal" );
				assert.equal( actors.length, 1 );
				const { heightScale, volumeScale, ...assembly } = creationLoadout( selection, itemIds );
				assert.ok(
					actors[0].model.startsWith( "creation:0:" + JSON.stringify( assembly ) ),
					"Preview wears the choice's items"
				);
				assert.equal( requests.length, warmed, "Equipment changes never start another download" );
			}
		}
	}
	const stableModel = actors[0].model;
	selection.height = 4;
	selection.volume = 4;
	step();
	assert.equal( actors[0].scale, 1 );
	assert.equal( actors[0].bodyVolume.index, 2 );
	owner.step( [], null, 1.5, undefined, 0, undefined, { selection, yaw: 0, protectorFloor: 0 } );
	assert.ok( Math.abs( actors[0].scale - 1.03 ) < 1e-6 );
	assert.equal( actors[0].bodyVolume.index, 3 );
	assert.equal( actors[0].model, stableModel );
	owner.step( [], null, 2, undefined, 0, undefined, { selection, yaw: 0, protectorFloor: 0 } );
	assert.ok( Math.abs( actors[0].scale - 1.06 ) < 1e-6 );
	assert.equal( actors[0].bodyVolume.index, 4 );
	assert.equal( requests.length, warmed );
	owner.reset();
	assert.equal( owner.previewReady(), false );
	assert.equal( pending.size, 0 );
	owner.dispose();
});

test("native weather follows ordered lifecycle events, including same-batch GID reuse", () => {
	const f = fixture( {}, 2, false, true ), p = f.presentation;
	const send = ( ...events ) => {
		p.receiveLifecycle( events );
		return p.eventRain();
	};
	const spawn = gid => ({ kind: "spawn", entity: entity( gid ) }), drop = gid => ({ kind: "despawn", gid });
	assert.equal( send( spawn( 1 ) ), false );
	f.warm();
	assert.equal( p.eventRain(), true );
	assert.equal( send( spawn( 2 ), drop( 1 ) ), false );
	assert.equal( send( { kind: "state", entity: entity( 2 ) } ), false );
	assert.equal( send( spawn( 1 ), drop( 1 ) ), false );
	assert.equal( send( drop( 2 ), spawn( 2 ) ), true );
	assert.equal( send( { kind: "reset", epoch: 1 }, spawn( 1 ), drop( 1 ) ), false );
	assert.equal( send( spawn( 1 ) ), true );
	p.reset();
	assert.equal( p.eventRain(), false );
	f.dispose();
});

test("life state drives non-looping death and revival overrides stale HP", () => {
	const f = fixture();
	f.warm();
	const gameplay = { localGid: 1, inventory: [], vitals: [ { gid: 1, hp: 999 } ], casts: [] };
	f.step( [ entity( 1, { appearanceState: [ 2, 0, 0 ] } ) ], 1, gameplay );
	assert.equal( f.actors[0].clip, "death" );
	assert.equal( f.actors[0].loop, false );
	assert.equal( f.actors[0].pickable, true, "own corpse remains selectable for the rebirth prompt" );
	f.step( [ entity( 1, { appearanceState: [ 2, 0, 0 ] } ) ], 1.2, gameplay );
	assert.ok( Math.abs( f.actors[0].time - .2 ) < 1e-8 );
	gameplay.vitals[0].hp = 0;
	f.step( [ entity( 1, { appearanceState: [ 1, 0, 0 ] } ) ], 2, gameplay );
	assert.equal( f.actors[0].clip, "stand" );
	assert.equal( f.actors[0].loop, true );
	f.step( [ entity( 1, { appearanceState: [ 2, 0, 0 ] } ) ], 3, { ...gameplay, localGid: 2 } );
	assert.equal( f.actors[0].pickable, false, "other corpses remain excluded from normal picking" );
	f.dispose();
});

test("revival during the death one-shot releases the death layer while movement resumes", () => {
	const f = fixture( {}, 2, false, false, true );
	f.warm();
	const game = { localGid: 1, inventory: [], vitals: [ { gid: 1, hp: 0 } ], casts: [], moving: false };
	for ( const base of [ 1, 3 ] ) {
		f.step( [ entity( 1, { appearanceState: [ 2, 0, 0 ] } ) ], base, game );
		assert.ok( f.actors[0].layers.some( layer => layer.clip === "death" ) );
		game.moving = true;
		game.pose = { regionId: 1, x: 20, y: 20, z: 30, angle: 0 };
		f.step( [ entity( 1, { appearanceState: [ 1, 0, 0 ], movementMode: 3 } ) ], base + .05, game );
		f.step( [ entity( 1, { appearanceState: [ 1, 0, 0 ], movementMode: 3 } ) ], base + .3, game );
		assert.equal( f.actors[0].clip, "run" );
		assert.ok( !(f.actors[0].layers ?? []).some( layer => layer.weight > 0 && /death|die/.test( layer.clip ) ) );
		game.moving = false;
	}
	f.dispose();
});
test("wire despawn retires identity immediately but preserves the final rendered model for its fade", () => {
	const f = fixture();
	f.warm();
	f.step( [ entity( 1, { appearanceState: [ 2, 0, 0 ] } ) ], 1 );
	f.presentation.receiveLifecycle( [ { kind: "despawn", gid: 1 } ] );
	f.step( [], 1.3 );
	assert.equal( f.presentation.ready( 1 ), false );
	assert.equal( f.actors.length, 1 );
	assert.ok( f.actors[0].gid < 0 );
	assert.equal( f.actors[0].pickable, false );
	assert.equal( f.actors[0].clip, "death" );
	f.step( [], 2.05 );
	assert.ok( Math.abs( f.actors[0].opacity - .5 ) < 1e-7 );
	f.step( [], 2.81 );
	assert.equal( f.actors.length, 0 );
	f.dispose();
});

test("sit and stand transitions retain the underlying pose; death and revival interrupt them", () => {
	const f = fixture( {}, 2, false, false, true );
	f.warm();
	const actor = extra => entity( 1, { appearanceState: [ 1, 0, 0 ], ...extra } );
	f.step( [ actor( { movementMode: 4 } ) ], 1 );
	assert.equal( f.actors[0].clip, "charselect-state14" );
	assert.equal( f.actors[0].layers[0].clip, "charselect-state13" );
	f.step( [ actor( { movementMode: 4 } ) ], 2.3 );
	assert.equal( f.actors[0].clip, "charselect-state14" );
	assert.equal( f.actors[0].layers, undefined );
	f.step( [ actor( { movementMode: 0 } ) ], 3 );
	assert.equal( f.actors[0].layers[0].clip, "charselect-state15" );
	assert.equal( f.actors[0].layers[0].time, 0 );
	f.step( [ actor( { appearanceState: [ 2, 0, 0 ] } ) ], 3.2 );
	assert.equal( f.actors[0].clip, "deathloop" );
	assert.equal( f.actors[0].layers[0].clip, "death" );
	f.step( [ actor( { appearanceState: [ 2, 0, 0 ] } ) ], 4.5 );
	assert.equal( f.actors[0].clip, "deathloop" );
	assert.equal( f.actors[0].loop, true );
	f.step( [ actor( { movementMode: 0 } ) ], 5 );
	assert.equal( f.actors[0].clip, "stand" );
	assert.ok( f.actors[0].layers.some( layer => layer.clip === "deathloop" && layer.weight > 0 ) );
	f.step( [ actor( { movementMode: 0 } ) ], 5.1 );
	assert.ok( f.actors[0].layers.some( layer => layer.clip === "deathloop" && layer.weight > 0 && layer.weight < 1 ) );
	f.step( [ actor( { movementMode: 0 } ) ], 5.21 );
	assert.equal( f.actors[0].layers, undefined );
	f.dispose();
});

test("run phase survives delayed position batches and direction changes until explicit arrival", () => {
	for ( const local of [ false, true ] ) {
		const f = fixture();
		f.warm();
		let previous = -1;
		const advances = [];
		for ( let i = 0; i < 180; i++ ) {
			const now = 1 + i / 150,
				actor = entity( 1, {
					movementMode: 3,
					moving: true,
					x: 10 + Math.floor( i / 19 ),
					heading: i < 90 ? 0 : 32768
				} );
			const game = local ?
				{
					localGid: 1,
					pose: { ...actor, angle: actor.heading },
					moving: true,
					inventory: [],
					vitals: [],
					casts: []
				} :
				null;
			f.step( [ actor ], now, game );
			const shown = f.actors[0];
			assert.ok( shown, JSON.stringify( { local, i, error: f.presentation.error() } ) );
			assert.equal( shown.clip, "run" );
			if ( i > 35 ) {
				const advance = Math.round( ((shown.time - previous + 1) % 1) * 1000 );
				assert.ok(
					advance === 6 || advance === 7,
					"the cursor advances whole milliseconds at 150 Hz, including wrap"
				);
				advances.push( advance );
			}
			previous = shown.time;
		}
		// 150 Hz is 6.667 ms per frame: whole-millisecond steps that average the real
		// interval rather than truncating to 6 ms and running the clip slow.
		assert.ok( Math.abs( advances.reduce( ( a, b ) => a + b, 0 ) / advances.length - 1000 / 150 ) < .05 );
		const actor = entity( 1, { movementMode: 3, moving: false, x: 19, heading: 32768 } );
		f.step(
			[ actor ],
			2.3,
			local ?
				{
					localGid: 1,
					pose: { ...actor, angle: actor.heading },
					moving: false,
					inventory: [],
					vitals: [],
					casts: []
				} :
				null
		);
		f.step(
			[ actor ],
			2.5,
			local ?
				{
					localGid: 1,
					pose: { ...actor, angle: actor.heading },
					moving: false,
					inventory: [],
					vitals: [],
					casts: []
				} :
				null
		);
		assert.equal( f.actors[0].clip, "stand" );
		f.dispose();
	}
});

test("appearance topology survives pose updates but in-place equipment edits invalidate it", () => {
	const f = fixture( {}, 2, false, false, true, false, true );
	f.warm();
	const equipment = [ { slot: 6, refObjId: 100, plus: 0 } ], player = entity( 1, { kind: "player", equipment } );
	for ( let i = 0; i < 20; i++ ) f.step( [ player ], .4 + i / 100 );
	const parts = f.assemblies.at( -1 ).parts;
	f.step( [ { ...player, x: player.x + 1, equipment: equipment.map( item => ({ ...item }) ) } ], 1 );
	assert.equal( f.assemblies.at( -1 ).parts, parts );
	assert.match( f.actors[0].model, /sword/ );
	equipment[0].plus = 9;
	f.step( [ player ], 1.05 );
	assert.notEqual( f.assemblies.at( -1 ).parts, parts );
	assert.equal( f.assemblies.at( -1 ).parts.find( p => p.equipment )?.equipment.plus, 9 );
	equipment[0] = { slot: 7, refObjId: 101, plus: 0 };
	for ( let i = 0; i < 5; i++ ) f.step( [ player ], 1.1 + i / 100 );
	assert.notEqual( f.assemblies.at( -1 ).parts, parts );
	assert.match( f.actors[0].model, /shield/ );
	assert.doesNotMatch( f.actors[0].model, /sword/ );
	assert.equal( f.presentation.error(), null );
	f.dispose();
});

test("damage delivery drives local-caster flash once, independently of victim HWAN state", () => {
	for (
		const [local, hwan, targetHwan, flags, amplitude] of [
			[ true, false, true, 0, 0 ],
			[ true, true, false, 0, 50 ],
			[ true, false, true, 2, 50 ],
			[ true, true, false, 2, 200 ],
			[ false, true, true, 2, 0 ]
		]
	) {
		const f = fixture( { 7: { clips: [], phaseClips: [ [], [], [] ], stages: [] } } );
		f.warm();
		const entities = [
			entity( 1, { kind: "local-player", appearanceState: [ 1, 0, Number( hwan ) ] } ),
			entity( 2, { appearanceState: [ 1, 0, Number( targetHwan ) ] } )
		];
		const cast = {
			token: 99,
			caster: 1,
			target: 2,
			skill: 7,
			cancelledAtMs: 1000,
			results: [ { target: 2, impacts: [ { type: 0, damage: 1, flags, fatal: false, secondaryAmount: 0 } ] } ]
		};
		const game = { localGid: local ? 1 : 2, inventory: [], casts: [ cast ], vitals: [] };
		f.step( entities, 1, game );
		const events = f.presentation.takeCameraScripts();
		assert.deepEqual( events.map( e => e.amplitude ), amplitude ? [ amplitude ] : [] );
		f.step( entities, 1.1, game );
		assert.deepEqual( f.presentation.takeCameraScripts(), [] );
		f.dispose();
	}
});

test("attached activation reaches the parent sound owner with the skill selector and current pose exactly once", () => {
	const f = fixture( { 7: { clips: [], stages: [] } }, 2, false, false, false, false, false, true );
	f.warm();
	const player = entity( 1, { kind: "local-player" } ),
		game = {
			localGid: 1,
			inventory: [],
			casts: [],
			vitals: [],
			pose: { regionId: 257, x: 80, y: 90, z: 100, angle: 0 },
			attachedEffects: [ { gid: 1, skill: 7, token: 1, phase: 2, restored: true } ]
		};
	f.step( [ player ], 1, game );
	assert.equal( f.presentation.error(), null );
	assert.equal( f.played.length, 1 );
	assert.equal( f.played[0].path, "/assets/audio/activate.wav" );
	assert.deepEqual( [ f.played[0].x, f.played[0].y, f.played[0].z ], [ 2000, 90, 2020 ] );
	f.step( [ player ], 1.1, game );
	assert.equal( f.played.length, 1 );
	game.attachedEffects = [];
	f.step( [ player ], 2, game );
	assert.equal( f.played.length, 1 );
	f.dispose();
});

test("parent attached host timed motion survives looping and releases its own lane", () => {
	const f = fixture(
		{ 7: { clips: [], stages: [], attachedMotion: { set: "default", id: 188 } } },
		2,
		false,
		false,
		false,
		false,
		false,
		false,
		true
	);
	f.warm();
	const game = {
			inventory: [],
			vitals: [],
			casts: [],
			attachedEffects: [ { gid: 1, skill: 7, token: 1, phase: 1 } ]
		},
		entities = [ entity( 1 ) ];
	for ( let i = 0; i < 10; i++ ) f.step( entities, 1 + i / 100, game );
	f.step( entities, 2.5, game );
	const layers = f.actors.find( a => a.gid === 1 ).layers;
	assert.equal( layers.find( l => l.clip === "attached-default-188" ).lane, "timed" );
	assert.equal( layers.find( l => l.clip === "attached-default-188" ).loop, true );
	assert.equal( layers.find( l => l.clip === "attached-default-188" ).weight, 1 );
	assert.ok( layers.some( l => l.clip === "stand" ) );
	game.attachedEffects = [];
	f.step( entities, 3, game );
	f.step( entities, 3.1, game );
	assert.ok(
		Math.abs(
			f.actors.find( a => a.gid === 1 ).layers.find( l => l.clip === "attached-default-188" ).weight - .5
		) < 1e-6
	);
	f.step( entities, 3.21, game );
	assert.ok( !f.actors.find( a => a.gid === 1 ).layers?.some( l => l.clip === "attached-default-188" ) );
	assert.equal( f.presentation.error(), null );
	f.dispose();
});

test("published reference appearance reaches parent model assembly, survives cold loading and restores on stop", () => {
	const f = fixture( { 9452: { clips: [], stages: [] } }, 2, false, false, false, false, false, false, false, true );
	f.warm();
	const entities = [ entity( 1 ) ], game = { inventory: [], vitals: [], casts: [], attachedEffects: [] };
	for ( let i = 0; i < 30; i++ ) f.step( entities, .3 + i / 100, game );
	const original = f.actors.find( a => a.gid === 1 ).model;
	game.attachedEffects = [ { gid: 1, skill: 9452, token: 1, phase: 1 } ];
	f.step( entities, 1, game );
	assert.equal(
		f.actors.find( a => a.gid === 1 ).model,
		original,
		"cold replacement must keep the old model visible"
	);
	for ( let i = 1; i < 20; i++ ) f.step( entities, 1 + i / 100, game );
	assert.match( f.actors.find( a => a.gid === 1 ).model, /assembly:disguise:\/assets\/2.glb/ );
	assert.equal( entities[0].refObjId, 1 );
	assert.ok( f.assemblies.some( a => a.base === "/assets/2.glb" ) );
	game.attachedEffects = [];
	for ( let i = 0; i < 5; i++ ) f.step( entities, 2 + i / 100, game );
	assert.equal( f.actors.find( a => a.gid === 1 ).model, original );
	assert.equal( f.presentation.error(), null );
	f.dispose();
});

test("ground gold uses the native one-shot fanfare, freezes on claim and hands off to the authored heap", () => {
	const f = fixture( {}, 2, true ),
		item = entity( 1, {
			kind: "ground-item",
			groundItem: { typeFlags: 0x2ec, goldAmount: 10, tint: 0, appear: 1 }
		} );
	try {
		for ( let i = 0; i < 25; i++ ) f.step( [ item ], 0 );
		assert.equal( f.presentation.error(), null );
		assert.equal( f.actors[0].model, "/assets/itemdrop/fanfare.glb" );
		assert.equal( f.actors[0].groundItem, true );
		f.step( [ item ], .5 );
		assert.equal( f.actors[0].model, "/assets/itemdrop/fanfare.glb" );
		f.step( [ { ...item, groundItem: { ...item.groundItem, claimantGid: 7 } } ], .6 );
		assert.equal( f.actors.length, 0 );
		f.step( [ { ...item, groundItem: { ...item.groundItem, claimantGid: 7 } } ], 2 );
		assert.equal( f.actors.length, 0 );
		f.step( [ item ], 2.1 );
		assert.equal( f.actors[0].model, "/assets/itemdrop/fanfare.glb" );
		f.step( [ item ], 2.7 );
		assert.equal(
			f.actors[0].model,
			"/assets/itemdrop/fanfare.glb",
			"completion registers a timer rather than backdating the swap"
		);
		f.step( [ item ], 2.702 );
		assert.equal( f.actors[0].model, "/assets/itemdrop/drop.glb" );
		assert.equal( f.actors[0].loop, false );
		assert.ok( f.actors[0].time < .01, "auxiliary animation did not run during the fanfare" );
		f.step( [], 3 );
		assert.equal( f.actors.length, 0 );
	} finally {
		f.dispose();
	}
});

test("scenery effects cannot overwrite a retiring corpse in the composed actor publication", () => {
	const f = fixture();
	try {
		f.warm();
		f.renderer.scenery = () => ({
			night: true,
			emitters: [ { id: "torch", model: "/assets/1.glb", pose: { regionId: 1, x: 40, y: 20, z: 30, yaw: 0 } } ]
		});
		f.presentation.step( [ entity( 1, { appearanceState: [ 2, 0, 0 ] } ) ], null, 1 );
		const scenery = f.actors.find( a => a.clip === "effect" );
		assert.ok( scenery );
		f.presentation.receiveLifecycle( [ { kind: "despawn", gid: 1 } ] );
		f.presentation.step( [], null, 1.3 );
		assert.equal( f.actors.length, 2, "corpse and scenery must both survive map composition" );
		const corpse = f.actors.find( a => a.clip === "death" );
		assert.ok( corpse );
		assert.notEqual( corpse.gid, scenery.gid );
		f.presentation.step( [], null, 2.05 );
		assert.ok( Math.abs( f.actors.find( a => a.gid === corpse.gid ).opacity - .5 ) < 1e-7 );
		f.presentation.step( [], null, 2.81 );
		assert.deepEqual( f.actors.map( a => a.gid ), [ scenery.gid ] );
	} finally {
		f.dispose();
	}
});

test("caster death retires committed hit feedback before a later release can replay it", () => {
	const f = fixture( { 7: { clips: [], phaseClips: [ [], [], [] ], stages: [] } } );
	f.warm();
	const cast = {
		token: 99,
		caster: 1,
		target: 2,
		skill: 7,
		results: [ { target: 2, impacts: [ { type: 0, damage: 17, flags: 0, fatal: false, secondaryAmount: 0 } ] } ]
	};
	const game = {
		localGid: 2,
		pose: { regionId: 1, x: 10, y: 20, z: 30, angle: 0 },
		inventory: [],
		casts: [ cast ],
		vitals: []
	};
	const alive = [
		entity( 1, { appearanceState: [ 1, 0, 0 ] } ),
		entity( 2, { kind: "local-player", appearanceState: [ 1, 0, 0 ] } )
	];
	f.step( alive, 1, game );
	assert.equal( f.presentation.damageText().length, 0 );
	const dead = [ { ...alive[0], appearanceState: [ 2, 0, 0 ] }, alive[1] ];
	f.step( dead, 1.1, game );
	assert.equal(
		f.presentation.damageText().length,
		1,
		"Death flushes the already committed result instead of letting the wind-up finish later"
	);
	const started = f.presentation.damageText()[0];
	f.step( dead, 1.5, { ...game, casts: [ { ...cast, shotAtMs: 1500 } ] } );
	assert.equal( f.presentation.damageText().length, 1 );
	assert.deepEqual( f.presentation.damageText()[0], started );
	assert.equal( cast.cancelledAtMs, undefined, "Presentation cannot mutate the authoritative cast" );
	f.dispose();
});

test("held native blind masks players and monsters without retiring presentation state", () => {
	const f = fixture();
	f.warm();
	const local = entity( 1, { kind: "player" } );
	// Let CIDecoAppear (spawn-fade.ts) finish: this test pins the blind mask.
	for ( let i = 0; i < 5; i++ ) f.presentation.step( [ local ], null, 1 + i / 10 );
	f.presentation.step(
		[ local ],
		{ localGid: 1, pose: { ...local, angle: local.heading }, casts: [], inventory: [], vitals: [] },
		4,
		undefined,
		undefined,
		undefined,
		undefined,
		false,
		2,
		true
	);
	assert.ok(
		(f.actors.find( a => a.gid === 1 ).opacity ?? 1) > 0,
		"local identity is exempt even when its kind is player"
	);
	for ( const kind of [ "monster", "player", "local-player", "npc", "cos", "ground-item" ] ) {
		const body = entity( 1, { kind } );
		f.presentation.step( [ body ], null, 4, undefined, undefined, undefined, undefined, false, 2, true );
		const actor = f.actors.find( a => a.gid === 1 );
		assert.ok( actor, kind + " remains resident" );
		const hidden = [ "monster", "player" ].includes( kind );
		assert.equal( actor.opacity ?? 1, hidden ? 0 : 1, kind );
		if ( hidden ) assert.equal( actor.pickable, false );
		assert.equal( f.presentation.ready( 1 ), true );
		f.presentation.step( [ body ], null, 5 );
		assert.equal( f.actors.find( a => a.gid === 1 ).opacity ?? 1, 1, kind + " restores on release" );
	}
	f.dispose();
});

test("production blends retain separate outgoing and incoming sound installations through rapid reversal", () => {
	const definition = ( cue, at ) => ({
		durationMs: 1000,
		trackEvents: [],
		timeWarpCurve: { scale: 1, records: [] },
		soundEvents: [ { cursorMs: at, cue } ]
	});
	const audio = {
		states: { stand: definition( "snd_stand", 450 ), run: definition( "snd_run", 450 ) },
		rules: [ "STAND", "RUN" ].map( handle => ({
			object: "NPC_1",
			handle: "SND_" + handle,
			event1: "-",
			publicPath: "/assets/audio/" + handle + ".wav"
		}) )
	};
	const f = fixture( {}, 2, false, false, false, false, false, false, false, false, undefined, audio );
	f.warm();
	f.step( [ entity( 1, { kind: "monster" } ) ], .3 );
	const start = .3 - f.actors[0].time;
	f.step( [ entity( 1, { kind: "monster", moving: true, movementMode: 3 } ) ], start + .4 );
	f.step( [ entity( 1, { kind: "monster", moving: true, movementMode: 3 } ) ], start + .46 );
	assert.equal( f.presentation.error(), null );
	// ADFFB0 constructs an installation at cursor 0, so the run clip has only just
	// begun and its 450 ms cue is not yet due. Only the stand clip reaches its own.
	assert.deepEqual( f.played.map( e => e.path ).sort(), [ "/assets/audio/STAND.wav" ] );
	const old = f.actors[0].layers.find( l => l.clip === "stand" ).activation;
	f.step( [ entity( 1, { kind: "monster", moving: false } ) ], start + .47 );
	f.step( [ entity( 1, { kind: "monster", moving: false } ) ], start + .48 );
	const sameClip = f.actors[0].layers.filter( l => l.clip === "stand" );
	assert.equal( sameClip.length, 2 );
	assert.equal( sameClip[0].activation, old );
	assert.notEqual( sameClip[1].activation, old );
	assert.equal( f.played.length, 1, "a freshly installed clip starts at its own beginning and replays nothing" );
	assert.equal( new Set( f.played.map( e => e.id ) ).size, 1 );
	f.dispose();
});

for ( const zeroClip of [ "stand", "run" ] ) {
	test(`production blend keeps ${zeroClip} when its zero duration omits a dispatch row`, () => {
		const audio = {
			states: Object.fromEntries(
				[ "stand", "run" ].filter( clip => clip !== zeroClip ).map( clip => [ clip, {
					durationMs: 1000,
					trackEvents: [],
					soundEvents: [ { cursorMs: 45, cue: "snd_" + clip } ]
				} ] )
			),
			rules: [ "STAND", "RUN" ].map( handle => ({
				object: "NPC_1",
				handle: "SND_" + handle,
				event1: "-",
				publicPath: "/assets/audio/" + handle + ".wav"
			}) )
		};
		// Zero-duration fallback comes from a decoded clip without BAN metadata;
		// published BAN duration zero is correctly rejected during admission.
		const f = fixture(
			{},
			2,
			false,
			false,
			false,
			false,
			false,
			false,
			false,
			false,
			undefined,
			audio,
			undefined,
			{},
			{},
			{ clipDurations: { [zeroClip]: 0 } }
		);
		try {
			f.warm();
			f.step( [ entity( 1, { kind: "monster" } ) ], .3 );
			f.played.length = 0;
			f.step( [ entity( 1, { kind: "monster", moving: true, movementMode: 3 } ) ], .4 );
			f.step( [ entity( 1, { kind: "monster", moving: true, movementMode: 3 } ) ], .46 );
			assert.equal( f.presentation.error(), null );
			const layers = f.actors[0].layers;
			assert.deepEqual( layers.map( layer => layer.clip ).sort(), [ "run", "stand" ] );
			assert.notEqual( layers[0].activation, layers[1].activation );
			assert.deepEqual(
				f.played.map( event => event.path ),
				zeroClip === "stand" ? [ "/assets/audio/RUN.wav" ] : [],
				"only the installation with a dispatch cursor can emit its due sound"
			);
		} finally {
			f.dispose();
		}
	});
}

test("cold production animation particles dispatch both blend installations and retain inactive wrappers", () => {
	const entry = ( path, key ) => ({
		field00: 1,
		effectPath: path,
		boneName: "",
		vector3c: [ 0, 0, 0 ],
		field4c: key,
		flags50: [ 0, 0, 0 ],
		flag53: 0
	});
	const modifiers = [ {
		kind: 1,
		stateId: 0,
		animationSetName: "default",
		baseWords: [ 1056964608, 1, 48, 4294967295, 0, 0 ],
		entries: [ entry( "monster/dust_earthghost_down.efp", 0 ) ]
	}, {
		kind: 1,
		stateId: 2,
		animationSetName: "default",
		baseWords: [ 1056964608, 1, 48, 4294967295, 0, 0 ],
		entries: [ entry( "monster/dust_earthghost_down.efp", 100 ) ]
	} ];
	const bindings = [ { set: "default", stateId: 0, clip: "stand" }, { set: "default", stateId: 2, clip: "run" } ];
	const f = fixture( {}, 2, false, false, false, false, false, false, false, false, modifiers, undefined, bindings ),
		body = entity( 1, { kind: "monster" } );
	for ( let i = 0; i < 50; i++ ) f.step( [ body ], i / 100 );
	assert.equal( f.presentation.error(), null );
	const first = f.actors.find( a => a.attachment?.gid === 1 );
	assert.ok( first, "cold load must not lose the zero key" );
	assert.equal( first.loop, false );
	f.step( [ { ...body, moving: true, movementMode: 3 } ], .5 );
	for ( let i = 1; i < 20; i++ ) f.step( [ { ...body, moving: true, movementMode: 3 } ], .5 + i / 100 );
	assert.equal( f.presentation.error(), null );
	assert.ok( f.actors.find( a => a.gid === first.gid ), "outgoing modifier instance remains retained" );
	assert.equal(
		f.actors.filter( a => a.attachment?.gid === 1 ).length,
		2,
		"incoming blend installation dispatches its own key"
	);
	assert.equal( f.actors.find( a => a.gid === first.gid ).deferredParticle.lodHidden, true );
	f.presentation.receiveLifecycle( [ { kind: "despawn", gid: 1 } ] );
	f.step( [], .7 );
	const retained = f.actors.find( a => a.gid === first.gid );
	assert.ok( retained );
	assert.notEqual( retained.attachment.gid, 1 );
	assert.ok(
		f.actors.some( a => a.gid === retained.attachment.gid ),
		"private fading model retains the modifier owner"
	);
	f.step( [], 2.21 );
	assert.ok( !f.actors.some( a => a.gid === first.gid ) );
	f.presentation.reset();
	assert.equal( f.actors.length, 0 );
	f.dispose();
});

test("cold skill BSR publication dispatches authored state keys through the production holder", () => {
	const particleModifiers = [ {
		kind: 1,
		stateId: 2,
		animationSetName: "default",
		baseWords: [ 1056964608, 1, 48, 4294967295, 0, 0 ],
		entries: [ {
			field00: 1,
			effectPath: "monster/dust_earthghost_down.efp",
			boneName: "",
			vector3c: [ 0, 0, 0 ],
			field4c: 0,
			flags50: [ 0, 0, 0 ],
			flag53: 0
		} ]
	} ];
	const stage = {
		phase: "ACT_S",
		startEvent: 0,
		move: "MOV_NONE",
		scripts: [],
		action: "AT_ONE_FOLLOW",
		resource: "res/mob/test.bsr",
		count: 1,
		offset: [ 0, 0, 0 ],
		life: 1
	};
	const models = {
		"res/mob/test.bsr": {
			glb: "/assets/skillfx/test.glb",
			clips: [ "stand" ],
			clipLoop: false,
			particleModifiers,
			modifierSets: [ { kind: 1, stateId: 2, animationSetName: "default", count: 1, firstBaseWord4: 0 } ],
			animationBindings: [ { set: "default", stateId: 2, clip: "stand" } ]
		}
	};
	const f = fixture(
		{ "-2147483642": { stages: [ stage ] } },
		2,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		undefined,
		undefined,
		undefined,
		models
	);
	f.warm();
	const entities = [ entity( 1, { kind: "local-player" } ) ];
	f.presentation.receiveFeedback( [ { kind: "level-up", gid: 1 } ], entities );
	for ( let i = 0; i < 50; i++ ) f.step( entities, 1 + i / 100 );
	assert.equal( f.presentation.error(), null );
	const holder = f.actors.find( a => a.model.includes( "skillfx/test.glb" ) );
	assert.ok( holder );
	assert.deepEqual( holder.modelAnimation.selected, { set: "default", state: 2 } );
	const child = f.actors.find( a => a.attachment?.gid === holder.gid );
	assert.ok( child, "state-zero time key survives model and EFP admission" );
	assert.equal( child.loop, false );
	f.presentation.reset();
	assert.equal( f.actors.length, 0 );
	f.dispose();
});

test("quest marker waits for character metadata admission without reporting a missing native height", () => {
	const metadata = {
		pending: true,
		failOnce: true,
		rows: [ { codename: "NPC_1", soundProfileName: "PCM_ADVENTURER", heightFactor: 1 } ]
	};
	const record = {
		stages: [ {
			phase: "ACT_L",
			startEvent: 0,
			action: "AT_LOOP",
			move: "MOV_NONE",
			scripts: [],
			count: 1,
			resource: "marker.bsr",
			life: 0,
			// sub_91e720 decodes '*' into no bone plus binding +0x09.
			bone: null,
			addHeight: true,
			offset: [ 0, 5, 0 ]
		} ]
	};
	const f = fixture(
		{ "-2147483622": record },
		2,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		undefined,
		undefined,
		undefined,
		{ "marker.bsr": { glb: "/assets/marker.glb", clips: [ "stand" ], clipLoop: true } },
		metadata
	);
	const game = {
		casts: [],
		vitals: [],
		inventory: [],
		questMarkers: [ { refId: 1, flags: 2, valueA: 1, optional: 1 } ]
	};
	for ( let i = 0; i < 35; i++ ) {
		f.step( [ entity( 1, { kind: "npc" } ) ], i / 100, game );
		assert.ok(
			!f.presentation.error()?.includes( "Missing native effect anchor height" ),
			"cold metadata must be pending, not an invalid actor: " + f.presentation.error()
		);
	}
	assert.ok( !f.actors.some( a => a.attachment?.gid === 1 ) );
	metadata.pending = false;
	for ( let i = 235; i < 265; i++ ) {
		f.step( [ entity( 1, { kind: "npc" } ) ], i / 100, game );
		assert.ok( !f.presentation.error()?.includes( "Missing native effect anchor height" ), f.presentation.error() );
	}
	assert.equal( f.presentation.error(), null );
	// Stage offsets keep their authored sign for every resource kind.
	assert.deepEqual( f.actors.find( a => a.attachment?.gid === 1 )?.attachment.offset, [ 0, 25, 0 ] );
	f.dispose();
});

test("all CH/EU armor families and degrees assemble by native reference for local and remote players", () => {
	const roster = JSON.parse( readFileSync( path.join( publicRoot, "assets/char/roster.json" ), "utf8" ) );
	const items = JSON.parse( readFileSync( path.join( publicRoot, "assets/data/missionPresentation.json" ), "utf8" ) )
		.itemsByRefObjId;
	const byCode = new Map( Object.entries( items ).map( ( [id, row] ) => [ row.codename, Number( id ) ] ) );
	for ( const race of [ "CH", "EU" ] ) {
		for ( const sex of [ "M", "W" ] ) {
			for ( const family of [ "HEAVY", "LIGHT", "CLOTHES" ] ) {
				for ( const local of [ false, true ] ) {
					const appearance = {
						items,
						roster: {
							models: [ {
								refObjId: 1,
								codename: `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_TEST`,
								glb: "/assets/1.glb",
								clips: [ "stand" ]
							} ],
							dress: roster.dress
						}
					};
					const f = fixture(
						{},
						2,
						false,
						false,
						false,
						false,
						false,
						false,
						false,
						false,
						undefined,
						undefined,
						undefined,
						{},
						{ appearance }
					);
					for ( let degree = 1; degree <= 9; degree++ ) {
						for ( const head of [ "HA", "CA" ] ) {
							const equipment = [ head, "BA", "SA", "AA", "LA", "FA" ].map( ( part, slot ) => ({
								slot,
								refObjId: byCode.get(
									`ITEM_${race}_${sex}_${family}_${String( degree ).padStart( 2, "0" )}_${part}_A`
								)
							}) ).filter( i => i.refObjId );
							const body = entity( 1, { kind: local ? "local-player" : "player", equipment } );
							for ( let i = 0; i < 30; i++ ) {
								f.step(
									[ body ],
									degree + i / 100,
									local ? { localGid: 1, inventory: equipment, vitals: [], casts: [] } : null
								);
							}
							assert.equal( f.presentation.error(), null, `${race}/${sex}/${family}/${degree}/${head}` );
							const expected = equipment.flatMap( i => {
								const entry = roster.dress.equipment[i.refObjId].bodies[`${race}_${sex}`];
								return entry ? [ entry ] : [];
							} );
							assert.deepEqual(
								f.assemblies.at( -1 ).parts.map( p => [ p.model, p.parts ] ),
								expected.map( e => [ e.glb, e.parts ] )
							);
						}
					}
					f.dispose();
				}
			}
		}
	}
});

test("event avatars and mall avatars use the same native reference catalog", () => {
	const roster = JSON.parse( readFileSync( path.join( publicRoot, "assets/char/roster.json" ), "utf8" ) );
	const items = JSON.parse( readFileSync( path.join( publicRoot, "assets/data/missionPresentation.json" ), "utf8" ) )
		.itemsByRefObjId;
	let checked = 0;
	for ( const prefix of [ "CH_M", "CH_W", "EU_M", "EU_W" ] ) {
		const [race, sex] = prefix.split( "_" ),
			appearance = {
				items,
				roster: {
					models: [ {
						refObjId: 1,
						codename: `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_TEST`,
						glb: "/assets/1.glb",
						clips: [ "stand" ]
					} ],
					dress: roster.dress
				}
			};
		const f = fixture(
			{},
			2,
			false,
			false,
			false,
			false,
			false,
			false,
			false,
			false,
			undefined,
			undefined,
			undefined,
			{},
			{ appearance }
		);
		for ( const [id, visual] of Object.entries( roster.dress.equipment ) ) {
			const entry = visual.bodies[prefix];
			if ( visual.avatarSlot === undefined || !entry ) continue;
			for ( let i = 0; i < 30; i++ ) {
				f.presentation.step(
					[ entity( 1, { kind: "player", avatars: [ { refObjId: Number( id ) } ] } ) ],
					null,
					checked + i / 100
				);
			}
			assert.equal( f.presentation.error(), null, `${prefix}/${id}` );
			assert.ok( f.assemblies.at( -1 ).parts.some( p => p.model === entry.glb ) );
			checked++;
		}
		f.dispose();
	}
	assert.ok( checked >= 132 );
});

test("default clothing assembles and retires through repeated raw equipment and avatar changes", () => {
	const roster = JSON.parse( readFileSync( path.join( publicRoot, "assets/char/roster.json" ), "utf8" ) );
	const items = JSON.parse( readFileSync( path.join( publicRoot, "assets/data/missionPresentation.json" ), "utf8" ) )
		.itemsByRefObjId;
	for ( const prefix of [ "CH_M", "CH_W", "EU_M", "EU_W" ] ) {
		const [race, sex] = prefix.split( "_" ),
			appearance = {
				items,
				roster: {
					models: [ {
						refObjId: 1,
						codename: `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_TEST`,
						glb: "/assets/1.glb",
						clips: [ "stand" ]
					} ],
					dress: roster.dress
				}
			};
		const f = fixture(
			{},
			2,
			false,
			false,
			false,
			false,
			false,
			false,
			false,
			false,
			undefined,
			undefined,
			undefined,
			{},
			{ appearance }
		);
		const find = predicate =>
			Number(
				Object.entries( roster.dress.equipment ).find( ( [id, row] ) =>
					row.bodies[prefix] && predicate( row, items[id] )
				)?.[0]
			);
		const shoulder = find( r => r.slot === 2 && r.armorClass === 3 ),
			thief = find( r => r.thiefSuit ),
			dress = find( r => r.avatarSlot === 1 && (r.visualMask & 18) === 18 );
		assert.ok( shoulder && thief && dress );
		let time = 0;
		for ( let cycle = 0; cycle < 3; cycle++ ) {
			for (
				const [equipment, avatars, family] of [
					[ [], [], "clothes" ],
					[ [ { slot: 2, refObjId: shoulder } ], [], "light" ],
					[ [ { slot: 8, refObjId: thief } ], [], null ],
					[ [], [ { refObjId: dress } ], null ],
					[ [], [], "clothes" ]
				]
			) {
				for ( let i = 0; i < 30; i++ ) {
					f.step( [ entity( 1, { kind: "player", equipment, avatars } ) ], time += .01 );
				}
				assert.equal( f.presentation.error(), null, prefix );
				const actor = f.actors.find( a => a.gid === 1 ),
					parts = actor.model.startsWith( "assembly:" ) ? f.assemblies.at( -1 ).parts : [];
				const defaults = parts.filter( p => /\/ch_[mw]_(clothes|light)_(ba|la)\.glb$/.test( p.model ) );
				assert.deepEqual(
					defaults.map( p => p.model ).sort(),
					race === "CH" && family ?
						[ "BA", "LA" ].map( p => roster.dress.defaultWear[prefix + "_" + family + "_" + p].glb )
							.sort() :
						[]
				);
			}
		}
		f.dispose();
	}
});

test("mounting hides native weapon and shield visuals and dismount restores both", () => {
	const f = fixture( {}, 2, false, false, false, false, true );
	const equipment = [ { slot: 6, refObjId: 100, plus: 0 }, { slot: 7, refObjId: 101, plus: 0 } ];
	for ( let i = 0; i < 30; i++ ) f.presentation.step( [ entity( 1, { kind: "player", equipment } ) ], null, i / 100 );
	assert.equal( f.presentation.error(), null );
	assert.equal( f.assemblies.at( -1 ).parts.length, 2 );
	f.presentation.step( [ entity( 1, { kind: "player", equipment, mountedOn: 2 } ) ], null, 1 );
	assert.equal( f.presentation.error(), null );
	assert.equal( f.actors.find( a => a.gid === 1 ).model, "/assets/1.glb" );
	f.presentation.step( [ entity( 1, { kind: "player", equipment } ) ], null, 2 );
	assert.equal( f.presentation.error(), null );
	assert.equal( f.assemblies.at( -1 ).parts.length, 2 );
	f.dispose();
});

test("auxiliary avatar wings keep independent tracks and retire across cold replacement, masking, unequip, despawn and reset", () => {
	const roster = JSON.parse( readFileSync( path.join( publicRoot, "assets/char/roster.json" ), "utf8" ) );
	const items = JSON.parse( readFileSync( path.join( publicRoot, "assets/data/missionPresentation.json" ), "utf8" ) )
		.itemsByRefObjId;
	for ( const prefix of [ "CH_M", "CH_W", "EU_M", "EU_W" ] ) {
		const [race, sex] = prefix.split( "_" ),
			appearance = {
				items,
				roster: {
					models: [ {
						refObjId: 1,
						codename: `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_TEST`,
						glb: "/assets/1.glb",
						clips: [ "stand", "walk", "run" ]
					} ],
					dress: roster.dress
				}
			};
		const blockedPaths = new Set(),
			f = fixture(
				{},
				2,
				false,
				false,
				false,
				false,
				false,
				false,
				false,
				false,
				undefined,
				undefined,
				undefined,
				{},
				{ appearance, blockedPaths }
			);
		const ids = Object.keys( roster.dress.avatarAuxiliary ).map( Number ).filter( id =>
			roster.dress.equipment[id].bodies[prefix]
		);
		assert.equal( ids.length, 2 );
		let now = 0;
		const children = () => f.actors.filter( a => a.attachment && a.model.includes( "/avatar_aux_" ) );
		const step = ( id, extra = {} ) => {
			for ( let i = 0; i < 30; i++ ) {
				f.presentation.step(
					[
						entity( 1, {
							kind: "player",
							equipment: [],
							avatars: id ? [ { refObjId: id } ] : [],
							...extra
						} )
					],
					null,
					now += .01
				);
			}
		};
		step( ids[0] );
		assert.equal(
			children()[0].attachment.basis,
			"compound",
			"private avatar skeleton uses native attach-root space"
		);
		const committed = children()[0];
		blockedPaths.add( "http://localhost" + roster.dress.avatarAuxiliary[ids[1]].glb );
		step( ids[1] );
		assert.equal( children().length, 1 );
		assert.equal( children()[0].gid, committed.gid, "cold replacement retains old wing transaction" );
		blockedPaths.clear();
		step( ids[1] );
		assert.equal( children().length, 1 );
		assert.notEqual( children()[0].gid, committed.gid );
		const blocker = Number(
			Object.entries( roster.dress.equipment ).find( ( [id, r] ) =>
				r.slot === 8 && r.bodies[prefix] && (r.visualMask & roster.dress.equipment[ids[0]].visualMask) !== 0
			)?.[0]
		);
		assert.ok( blocker );
		step( ids[0], { equipment: [ { slot: 8, refObjId: blocker } ] } );
		assert.equal( children().length, 0, "native job mask suppresses primary and auxiliary together" );
		for ( let cycle = 0; cycle < 3; cycle++ ) {
			step( ids[0] );
			assert.equal( f.presentation.error(), null );
			assert.equal( children().length, 1 );
			const first = children()[0];
			assert.equal( first.clip, "stand" );
			assert.equal( first.attachment.bone, "Bip01 Spine1" );
			step( ids[0] );
			assert.equal( children()[0].gid, first.gid );
			assert.ok( children()[0].time > first.time, "unchanged equipment does not restart wings" );
			step( ids[0], { moving: true, movementMode: 3 } );
			assert.equal( children()[0].clip, "run" );
			const run = children()[0];
			step( ids[0], { moving: true, movementMode: 2 } );
			assert.equal( children()[0].clip, "run", "missing walk track retains current native track" );
			assert.ok( children()[0].time > run.time );
			step( ids[0], { moving: true, movementMode: 3, mountedOn: 2 } );
			assert.equal( children()[0].clip, "stand" );
			step( ids[1] );
			assert.equal( children().length, 1 );
			assert.notEqual( children()[0].gid, first.gid );
			assert.equal( children()[0].model, roster.dress.avatarAuxiliary[ids[1]].glb );
			step( null );
			assert.equal( children().length, 0 );
		}
		step( ids[0] );
		const retired = children()[0];
		f.presentation.receiveLifecycle( [ { kind: "despawn", gid: 1 } ] );
		f.presentation.step( [], null, now += .1 );
		assert.equal( children().length, 1 );
		assert.equal( children()[0].gid, retired.gid );
		assert.notEqual( children()[0].attachment.gid, 1 );
		f.presentation.step( [], null, now += .75 );
		assert.equal( children()[0].opacity, undefined );
		assert.ok( Math.abs( f.actors.find( a => a.gid === children()[0].attachment.gid ).opacity - .5 ) < 1e-6 );
		f.presentation.step( [], null, now += .76 );
		assert.equal( children().length, 0 );
		step( ids[0] );
		f.presentation.reset();
		assert.equal( f.actors.length, 0 );
		f.dispose();
	}
});

test("body avatar override restores normal run and restarts same-name replacements only once", () => {
	const roster = JSON.parse( readFileSync( path.join( publicRoot, "assets/char/roster.json" ), "utf8" ) ),
		items = JSON.parse( readFileSync( path.join( publicRoot, "assets/data/missionPresentation.json" ), "utf8" ) )
			.itemsByRefObjId;
	for ( const prefix of [ "CH_M", "CH_W", "EU_M", "EU_W" ] ) {
		const [race, sex] = prefix.split( "_" ), role = "native:avatar_wing:7";
		const appearance = {
			overrideTest: true,
			items,
			roster: {
				models: [ {
					refObjId: 1,
					codename: `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_TEST`,
					glb: "/assets/1.glb",
					clips: [
						"stand",
						"walk",
						"run",
						"charselect-state13",
						"charselect-state14",
						"charselect-state15",
						role
					],
					animationStates: { [role]: { durationMs: 1000, loop: true } }
				} ],
				dress: roster.dress
			}
		};
		const f = fixture(
			{},
			2,
			false,
			false,
			true,
			false,
			false,
			false,
			false,
			false,
			undefined,
			undefined,
			undefined,
			{},
			{ appearance }
		);
		const ids = Object.keys( roster.dress.avatarAuxiliary ).map( Number ).filter( id =>
			roster.dress.equipment[id].bodies[prefix]
		);
		let now = 0, movementRevision = 0;
		const step = ( avatars, movementMode = 3, moving = true ) => {
			for ( let i = 0; i < 40; i++ ) {
				f.presentation.step(
					[
						entity( 1, {
							kind: "player",
							equipment: [],
							avatars: avatars.map( refObjId => ({ refObjId }) ),
							movementMode,
							moving,
							movementRevision
						} )
					],
					null,
					now += .01
				);
			}
			return f.actors.find( a => a.gid === 1 );
		};
		let a = step( [ ids[0] ] );
		assert.equal( f.presentation.error(), null );
		movementRevision++;
		a = step( [ ids[0] ] );
		assert.equal( a.clip, role );
		const age = a.time;
		a = step( [ ids[0] ] );
		assert.ok( a.time > age, "unchanged override keeps clock" );
		a = step( [ ids[1] ] );
		assert.equal( a.clip, "stand", "native re-entry rejects movement before restoring standing" );
		movementRevision++;
		a = step( [ ids[1] ] );
		assert.equal( a.clip, role );
		assert.ok( a.time < .5, "fresh movement command installs the replacement clip" );
		const replacedAge = a.time;
		a = step( [ ids[1] ] );
		assert.ok( a.time > replacedAge );
		assert.equal( step( [ ids[1] ], 2 ).clip, "walk", "missing override state retains ordinary body track" );
		assert.equal( step( [ ids[1] ], 3, false ).clip, "stand" );
		const standAge = step( [ ids[1] ], 3, false ).time;
		assert.ok( standAge > .5 );
		assert.ok( step( [ ids[0] ], 3, false ).time < .5, "standing base restarts once on replacement" );
		assert.ok( step( [ ids[0] ], 3, false ).time > .5, "unchanged standing override does not restart" );
		step( [ ids[0] ], 4, false );
		step( [ ids[0] ], 4, false );
		step( [ ids[0] ], 4, false );
		const sitChanged = step( [ ids[1] ], 4, false );
		assert.equal( sitChanged.clip, "charselect-state14" );
		assert.ok( sitChanged.time < .5, "sitting base restarts on replacement" );
		assert.ok(
			!(sitChanged.layers ?? []).some( l => l.clip === "sitdown" || l.clip === "charselect-state13" ),
			"refresh does not replay sitting transition"
		);
		step( [], 3, true );
		movementRevision++;
		assert.equal( step( [], 3, true ).clip, "run", "fresh movement after unequip restores normal set" );
		step( [ ids[0] ], 3, true );
		movementRevision++;
		assert.equal( step( [ ids[0] ], 3, true ).clip, role );
		f.presentation.reset();
		assert.equal( f.actors.length, 0 );
		f.dispose();
	}
});

test("the equipped weapon's animation set drives stand and run, falling back per state", () => {
	// CCObjCharacter_ResolveWeaponAnimationPrefix (8E83F0): a spear (band 4)
	// plays the spear set's two-handed stand/run; a state the set lacks keeps
	// the default clip. Reported: a spear ran holding the weapon in one hand.
	const items = JSON.parse( readFileSync( path.join( publicRoot, "assets/data/missionPresentation.json" ), "utf8" ) )
		.itemsByRefObjId;
	const roster = JSON.parse( readFileSync( path.join( publicRoot, "assets/char/roster.json" ), "utf8" ) );
	const run = "native:spear:7", stand = "native:spear:0";
	const appearance = {
		items,
		extraClips: [ run, stand ],
		roster: {
			models: [ {
				refObjId: 1,
				codename: "CHAR_CH_MAN_TEST",
				glb: "/assets/1.glb",
				clips: [ "stand", "walk", "run", run, stand ],
				animationStates: { [run]: { durationMs: 800, loop: true }, [stand]: { durationMs: 2000, loop: true } }
			} ],
			dress: roster.dress
		}
	};
	const f = fixture(
		{},
		2,
		false,
		false,
		true,
		false,
		false,
		false,
		false,
		false,
		undefined,
		undefined,
		undefined,
		{},
		{ appearance }
	);
	const spear = { slot: 6, refObjId: 0, typeFlags: (4 << 11) | 0x6c, plus: 0 };
	let now = 0, movementRevision = 0;
	const step = ( equipment, movementMode = 3, moving = true ) => {
		for ( let i = 0; i < 40; i++ ) {
			f.presentation.step(
				[ entity( 1, { kind: "player", equipment, movementMode, moving, movementRevision } ) ],
				null,
				now += .01
			);
		}
		return f.actors.find( a => a.gid === 1 );
	};
	step( [ spear ] );
	movementRevision++;
	assert.equal( step( [ spear ] ).clip, run, "a spear runs with the spear set" );
	assert.equal( step( [ spear ], 3, false ).clip, stand, "and stands with it" );
	movementRevision++;
	assert.equal( step( [ spear ], 2 ).clip, "walk", "the spear set's missing walk keeps the default" );
	movementRevision++;
	assert.equal( step( [] ).clip, "run", "without a weapon the default set runs" );
	f.dispose();
});

test("native Korean wear freeze preserves committed default handles while equipment still changes", () => {
	const roster = JSON.parse( readFileSync( path.join( publicRoot, "assets/char/roster.json" ), "utf8" ) );
	const items = JSON.parse( readFileSync( path.join( publicRoot, "assets/data/missionPresentation.json" ), "utf8" ) )
		.itemsByRefObjId;
	for ( const prefix of [ "CH_M", "CH_W", "EU_M", "EU_W" ] ) {
		const [race, sex] = prefix.split( "_" ), dress = { ...roster.dress, defaultWearLanguage: 0 };
		const appearance = {
			items,
			roster: {
				models: [ {
					refObjId: 1,
					codename: `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_TEST`,
					glb: "/assets/1.glb",
					clips: [ "stand" ]
				} ],
				dress
			}
		};
		const f = fixture(
			{},
			2,
			false,
			false,
			false,
			false,
			false,
			false,
			false,
			false,
			undefined,
			undefined,
			undefined,
			{},
			{ appearance }
		);
		const shoulder = Number(
			Object.entries( dress.equipment ).find( ( [id, r] ) =>
				r.bodies[prefix] && r.slot === 2 && r.armorClass === 3
			)?.[0]
		);
		assert.ok( shoulder );
		let time = 0;
		const step = ( name, equipment = [] ) => {
			for ( let i = 0; i < 30; i++ ) {
				f.presentation.step(
					[ entity( 1, { kind: "player", equipment } ) ],
					null,
					time += .01,
					undefined,
					undefined,
					undefined,
					undefined,
					false,
					2,
					false,
					name
				);
			}
			assert.equal( f.presentation.error(), null );
			return f.actors.find( a => a.gid === 1 );
		};
		const defaults = () =>
			f.actors[0].model.startsWith( "assembly:" ) ?
				f.assemblies.at( -1 ).parts.filter( p => /\/ch_[mw]_(clothes|light)_(ba|la)\.glb$/.test( p.model ) )
					.map( p => p.model ).sort() :
				[];
		step( "Normal" );
		assert.deepEqual( defaults(), [], "frozen cold owner cannot invent default handles" );
		step( "Server#$T" );
		const committed = defaults();
		assert.equal( committed.length, race === "CH" ? 2 : 0 );
		for ( let cycle = 0; cycle < 3; cycle++ ) {
			step( "Normal", [ { slot: 2, refObjId: shoulder } ] );
			assert.deepEqual( defaults(), committed );
			assert.ok(
				f.assemblies.at( -1 ).parts.some( p => p.model === dress.equipment[shoulder].bodies[prefix].glb ),
				"freeze does not suppress real equipment"
			);
			step( "Normal" );
			assert.deepEqual( defaults(), committed );
		}
		step( "Server#$T", [ { slot: 2, refObjId: shoulder } ] );
		assert.equal( defaults().every( p => p.includes( "_light_" ) ), true );
		f.presentation.reset();
		step( "Normal" );
		assert.deepEqual( defaults(), [], "reset retires committed frozen handles" );
		f.dispose();
	}
});

test("move exit emits two real toe decals exactly once and clears them at reset", () => {
	const f = fixture(
		{},
		2,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		undefined,
		undefined,
		undefined,
		{},
		{ surface: () => "SNOW" }
	);
	const calls = [];
	f.renderer.characterSocket = ( actors, gid, bone ) => {
		calls.push( bone );
		return { regionId: 1, x: bone.includes( " L " ) ? 20 : 22, y: 20, z: 30, yaw: 0 };
	};
	for ( let i = 0; i < 30; i++ ) {
		f.presentation.step( [ entity( 1, { kind: "player", moving: true, movementRevision: 1 } ) ], null, i / 100 );
	}
	f.presentation.step( [ entity( 1, { kind: "player", moving: false, movementRevision: 1 } ) ], null, 1 );
	assert.equal( f.presentation.error(), null );
	assert.deepEqual( calls, [ "Bip01 L Toe0", "Bip01 R Toe0" ] );
	assert.equal( f.footprints.at( -1 ).length, 2 );
	f.presentation.step( [ entity( 1, { kind: "player", moving: false, movementRevision: 1 } ) ], null, 2 );
	assert.equal( calls.length, 2 );
	assert.deepEqual( f.footprints.at( -1 ).map( p => [ p.right, p.surface, p.pose.x ] ), [ [ false, "SNOW", 20 ], [
		true,
		"SNOW",
		22
	] ] );
	f.presentation.reset();
	assert.deepEqual( f.footprints.at( -1 ), [] );
	f.dispose();
});

test("movement animation toe keys respect terrain, missing sockets, sitting and expiry", () => {
	const states = {
		run: {
			durationMs: 1000,
			loop: true,
			trackEvents: [ { cursorMs: 100, eventCode: 2, param0: 0, param1: 0 }, {
				cursorMs: 200,
				eventCode: 2,
				param0: 1,
				param1: 0
			} ]
		}
	};
	let surface = "SNOW";
	const f = fixture(
		{},
		2,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		undefined,
		{ states, rules: [] },
		undefined,
		{},
		{ surface: () => surface }
	);
	const queried = [];
	f.renderer.characterSocket = ( actors, gid, bone ) => {
		queried.push( bone );
		return bone.includes( " L " ) ? { regionId: 1, x: 20, y: 0, z: 20, yaw: 0 } : null;
	};
	for ( let i = 0; i < 70; i++ ) {
		f.presentation.step(
			[ entity( 1, { kind: "player", moving: true, movementMode: 3, movementRevision: 1 } ) ],
			null,
			i / 100
		);
	}
	assert.equal( f.presentation.error(), null );
	assert.ok(
		queried.includes( "Bip01 L Toe0" ),
		JSON.stringify( { queried, actors: f.actors.map( a => ({ clip: a.clip, time: a.time }) ), feet: f.footprints } )
	);
	assert.ok( queried.includes( "Bip01 R Toe0" ) );
	assert.ok( f.footprints.at( -1 ).length > 0 );
	assert.ok( f.footprints.at( -1 ).every( p => !p.right ), "missing right toe never falls back to origin" );
	const count = queried.length;
	surface = "GRASS";
	for ( let i = 70; i < 180; i++ ) {
		f.presentation.step(
			[ entity( 1, { kind: "player", moving: true, movementMode: 3, movementRevision: 1 } ) ],
			null,
			i / 100
		);
	}
	assert.equal( queried.length, count );
	surface = "SNOW";
	f.presentation.step(
		[ entity( 1, { kind: "player", moving: false, movementMode: 4, movementRevision: 2 } ) ],
		null,
		2
	);
	assert.equal( queried.length, count, "sitting move-exit cannot stamp feet" );
	f.presentation.step( [ entity( 1, { kind: "player", moving: false, movementMode: 4 } ) ], null, 23 );
	assert.deepEqual( f.footprints.at( -1 ), [] );
	f.dispose();
});

/*
================
Roster admission regression
A ready auxiliary actor must never satisfy a blocked character assembly.
================
*/
test("dock admission waits for equipment even when its gecko is already rendered", () => {
	const blockedPaths = new Set( [ "http://localhost/assets/cold-sword.glb" ] );
	const roster = {
		models: [ {
			refObjId: 1,
			codename: "Fixture",
			glb: "/assets/1.glb",
			clips: [ "stand" ],
			previewGlb: "/assets/preview.glb",
			previewClips: [ "stand" ]
		} ],
		dress: {
			equipment: {
				3644: {
					code: "ITEM_CH_SWORD_01_A_DEF",
					slot: 6,
					armorClass: 0,
					thiefSuit: false,
					visualMask: 0,
					visualPriority: 90,
					model: "res/item/china/weapon/sword_01.bsr",
					source: 3644,
					bodies: { "": { glb: "/assets/cold-sword.glb", parts: [ "EQ0" ] } }
				}
			}
		}
	};
	const f = fixture(
		{},
		2,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		undefined,
		undefined,
		undefined,
		{},
		{ blockedPaths, appearance: { roster, items: {} } }
	);
	const rows = [ {
		id: 77,
		name: "Fixture",
		bodyShapeByte: 34,
		volumeIndex: 2,
		deletePending: false,
		visualLoadout: {
			modelCodename: "Fixture",
			items: [ { refObjId: 3644, plus: 0 } ],
			avatars: [],
			animationSetName: "punch",
			heightScale: 1,
			volumeScale: 1
		}
	} ];
	const step = () => f.presentation.step( [], null, 1, undefined, 0, rows, null, true );
	try {
		for ( let i = 0; i < 10; i++ ) f.presentation.step( [], null, 0, undefined, 0, [], null, true );
		for ( let i = 0; i < 30; i++ ) step();
		assert.ok(
			f.actors.some( actor => actor.gid === -1 ),
			JSON.stringify( { error: f.presentation.error(), actors: f.actors, requests: f.requests } )
		);
		assert.equal( f.presentation.dockReady(), false );
		blockedPaths.clear();
		for ( let i = 0; i < 30 && !f.presentation.dockReady(); i++ ) step();
		assert.equal( f.presentation.error(), null );
		assert.equal( f.presentation.dockReady(), true );
		assert.ok( f.actors.some( actor => actor.gid === 77 ) );
		f.presentation.reset();
		assert.equal( f.presentation.dockReady(), false );
	} finally {
		f.dispose();
	}
});

/*
================
worldEntryFixture

Keep world-entry catalog faults on the same real presentation fixture as
resource admission; only the asset response and its availability change.
================
*/
function worldEntryFixture( metadataAdmission ) {
	return fixture(
		{},
		2,
		true,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		undefined,
		undefined,
		undefined,
		{},
		metadataAdmission
	);
}

for ( const missing of [ "ing", "small", "normal", "large" ] ) {
	test(`world entry rejects missing gold catalog model ${missing}`, () => {
		const dropModels = goldDropModels();
		delete dropModels[`item/etc/drop_ch_money_${missing}.bsr`];
		const f = worldEntryFixture( { dropModels } );
		const player = entity( 1, { kind: "player" } );
		const state = {
			localGid: 1,
			inventory: [],
			casts: [],
			attachedEffects: [],
			skills: [],
			vitals: [],
			pose: { ...player, angle: player.heading }
		};
		try {
			for ( let i = 0; i < 80; i++ ) f.presentation.step( [ player ], state, i / 60 );
			assert.equal( f.presentation.ready( 1 ), true );
			assert.equal( f.presentation.entryReady(), false );
			assert.match( f.presentation.error() ?? "", new RegExp( `drop_ch_money_${missing}\\.bsr` ) );
			assert.ok( f.actors.every( actor => !actor.groundItem ) );
		} finally {
			f.dispose();
		}
	});
}

test("world entry waits for cold gold fanfare without presenting a ground entity", () => {
	const blockedPaths = new Set( [ "http://localhost/assets/itemdrop/fanfare.glb" ] );
	const f = worldEntryFixture( { blockedPaths } );
	const player = entity( 1, { kind: "player" } );
	const state = {
		localGid: 1,
		inventory: [],
		casts: [],
		attachedEffects: [],
		skills: [],
		vitals: [],
		pose: { ...player, angle: player.heading }
	};
	try {
		for ( let i = 0; i < 40; i++ ) f.presentation.step( [ player ], state, i / 60 );
		assert.equal(
			f.presentation.ready( 1 ),
			true,
			JSON.stringify( { error: f.presentation.error(), actors: f.actors, requests: f.requests } )
		);
		assert.equal( f.presentation.entryReady(), false );
		assert.ok( f.requests.some( request => request.url.endsWith( "/itemdrop/fanfare.glb" ) ) );
		assert.ok( f.actors.every( actor => !actor.groundItem ) );
		blockedPaths.clear();
		for ( let i = 40; i < 80 && !f.presentation.entryReady(); i++ ) {
			f.presentation.step( [ player ], state, i / 60 );
		}
		assert.equal( f.presentation.error(), null );
		assert.equal( f.presentation.entryReady(), true );
		f.presentation.reset();
		assert.equal( f.presentation.entryReady(), false );
	} finally {
		f.dispose();
	}
});

test("party portraits follow roster models before visibility, after despawn and until leaving", async () => {
	const { partyPortraitGid } = await load( "src/engine/foundation/ui/party-overlay.ts" );
	const f = fixture();
	try {
		f.warm();
		const local = entity( 1, { kind: "local-player", name: "me" } );
		const peer = entity( 2, { kind: "player", name: "peer" } );
		const gameplay = {
			localGid: 1,
			pose: { regionId: 1, x: 10, y: 20, z: 30, angle: 0 },
			inventory: [],
			vitals: [],
			casts: [],
			social: {
				localName: "me",
				leader: 1,
				members: [ { id: 1, name: "me", model: 1 }, { id: 2, name: "peer", model: 2 } ]
			}
		};
		const gid = partyPortraitGid( 2 );
		for ( let i = 0; i < 20; i++ ) f.step( [ local ], 1 + i / 10, gameplay );
		const initial = f.portraitSource( gid );
		assert.ok( initial, "a party member never seen in the world still has a portrait" );
		assert.equal( f.actors.some( actor => actor.gid === gid ), false, "portraits never enter world draws" );
		for ( let i = 0; i < 5; i++ ) f.step( [ local, peer ], 4 + i / 10, gameplay );
		f.presentation.receiveLifecycle( [ { kind: "despawn", gid: 2 } ] );
		for ( let i = 0; i < 30; i++ ) f.step( [ local ], 5 + i / 10, gameplay );
		assert.equal( f.portraitSource( 2 ), null, "the world actor has retired" );
		assert.equal( f.portraitSource( gid )?.model, initial.model, "roster preview survives visibility retirement" );
		f.step( [ local ], 9, {
			...gameplay,
			social: { ...gameplay.social, members: [ gameplay.social.members[0] ] }
		} );
		assert.equal( f.portraitSource( gid ), null, "leaving retires the roster preview" );
	} finally {
		f.dispose();
	}
});

/*
================
Berserk hair publication

Exercise local and remote actors with both Chinese skeletons. Appearance owns
admission and lifetime; the renderer owns the compound attachment transform.
================
*/
test("berserk hair publishes compound attachments only after the resource is ready", () => {
	const roster = JSON.parse( readFileSync( path.join( publicRoot, "assets/char/roster.json" ), "utf8" ) );
	for ( const prefix of [ "CH_M", "CH_W" ] ) {
		const hair = roster.dress.hwan[prefix];
		const blockedPaths = new Set( [ "http://localhost" + hair.glb ] );
		const appearance = {
			items: {},
			roster: {
				models: [ {
					refObjId: 1,
					codename: `CHAR_CH_${prefix === "CH_M" ? "MAN" : "WOMAN"}_TEST`,
					glb: "/assets/1.glb",
					clips: [ "stand" ]
				} ],
				dress: roster.dress
			}
		};
		const f = fixture(
			{},
			2,
			false,
			false,
			false,
			false,
			false,
			false,
			false,
			false,
			undefined,
			undefined,
			undefined,
			{},
			{ appearance, blockedPaths }
		);
		let now = 0;
		/*
		================
		step
		================
		*/
		function step( active ) {
			for ( let i = 0; i < 30; i++ ) {
				f.presentation.step(
					[
						entity( 1, {
							kind: "local-player",
							equipment: [],
							appearanceState: [ 1, 0, Number( active ) ]
						} ),
						entity( 2, {
							kind: "player",
							refObjId: 1,
							equipment: [],
							appearanceState: [ 1, 0, Number( active ) ]
						} )
					],
					null,
					now += .01
				);
			}
		}
		/*
		================
		children
		================
		*/
		function children() {
			return f.actors.filter( a => a.model === hair.glb );
		}
		step( false );
		step( true );
		assert.equal( children().length, 0, "cold hair must not publish an unattached actor" );
		blockedPaths.clear();
		step( true );
		assert.equal( f.presentation.error(), null );
		assert.equal( children().length, 2, prefix );
		for ( const child of children() ) {
			assert.deepEqual( child.attachment, {
				gid: child.attachment.gid,
				bone: hair.bone,
				offset: [ 0, 0, 0 ],
				basis: "compound"
			} );
		}
		step( false );
		assert.equal( children().length, 0, "expiry removes both hair actors" );
		step( true );
		assert.equal( children().length, 2 );
		f.presentation.step( [], null, now + 1 );
		assert.equal( children().length, 0, "despawn retires private skeletons" );
		f.dispose();
	}
});

test("a characterInfo ride that loads after its rider fades in on its own clock", () => {
	const ride = {
		kind: "ride",
		codename: "res/mob/ride.bsr",
		glb: "/assets/npc/mob/ride.glb",
		clips: [ "stand", "walk" ],
		requiredBy: [ "NPC_1" ]
	};
	const admission = { blockedPaths: new Set( [ "http://localhost" + ride.glb ] ) };
	const f = fixture(
		{},
		1,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		undefined,
		undefined,
		undefined,
		{},
		{ ...admission, rows: [ { codename: "NPC_1", soundProfileName: "MOB_TEST", riderTransformMode: 1 } ] },
		{ rides: [ ride ] }
	);
	try {
		f.warm();
		const rider = [ entity( 1, { kind: "monster" } ) ];
		for ( let i = 0; i < 50; i++ ) f.step( rider, i / 10 );
		assert.equal( f.actors.find( actor => actor.gid === 1 )?.opacity ?? 1, 1, "the rider finished its ramp" );
		admission.blockedPaths.clear();
		let first;
		for ( let i = 0; i < 20 && !first; i++ ) {
			f.step( rider, 5 + i / 10 );
			first = f.actors.find( actor => actor.model === ride.glb );
		}
		assert.ok( first, "the ride eventually draws" );
		// 861EE2: the ride's own CIDecoAppear starts with the ride.
		assert.equal( first.opacity, 0 );
	} finally {
		f.dispose();
	}
});

test("a characterInfo ride joins its rider, follows its motions and links by the native ride mode", () => {
	const ride = {
		kind: "ride",
		codename: "res/mob/ride.bsr",
		glb: "/assets/npc/mob/ride.glb",
		clips: [ "stand", "walk" ],
		requiredBy: [ "NPC_1", "NPC_2", "NPC_3" ]
	};
	// NPC_1 rides on the saddle (none), NPC_2 is RT_FIXED, NPC_3 is RT_DUMMY.
	const rows = [ 0, 1, 2 ].map( mode => ({
		codename: "NPC_" + (mode + 1),
		soundProfileName: "MOB_TEST",
		riderTransformMode: mode
	}) );
	const f = fixture(
		{},
		3,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		undefined,
		undefined,
		undefined,
		{},
		{
			rows
		},
		{ rides: [ ride ] }
	);
	try {
		f.warm();
		const riders = [ 1, 2, 3 ].map( gid => entity( gid, { kind: "monster" } ) );
		for ( let i = 0; i < 40; i++ ) f.step( riders, i / 10 );
		const rides = f.actors.filter( actor => actor.model === ride.glb );
		assert.equal( rides.length, 3, "every rider owns one ride" );
		const rideOf = gid => rides.find( actor => actor.pickOwner === gid );
		const actorOf = gid => f.actors.find( actor => actor.gid === gid );
		assert.equal( actorOf( 1 )?.mountedOn, rideOf( 1 )?.gid, "mode 0 seats the rider on the saddle" );
		assert.equal( actorOf( 2 )?.mountedOn, undefined );
		assert.equal( rideOf( 2 )?.attachment, undefined, "RT_FIXED links neither way" );
		assert.equal( actorOf( 3 )?.mountedOn, undefined );
		assert.deepEqual( rideOf( 3 )?.attachment, { gid: 3, bone: "", root: true, offset: [ 0, 0, 0 ] } );
		for ( const gid of [ 1, 2, 3 ] ) {
			assert.equal( rideOf( gid )?.clip, actorOf( gid )?.clip, "the ride plays the rider's motion" );
			assert.equal( rideOf( gid )?.scale, 1 );
		}
		f.step( riders.slice( 1 ), 4 );
		assert.equal( f.actors.some( actor => actor.pickOwner === 1 ), false, "the ride leaves with its rider" );
	} finally {
		f.dispose();
	}
});
