/*
===========================================================================

presentation-fixture.mjs - the Node character presentation behind the tests

Builds the shipping character presentation (runtime/characters) over the
real CPU side of the characters renderer, with the renderer and GPU ports
it talks to as plain objects. presentationRig owns that wiring; fixture()
adds the stub asset server the presentation tests describe their catalogs
with. tools/perf/presentation/harness.mjs builds the same rig over the
published asset tree, so a test and a replay present through one wiring.

Every import resolves from this file, not from the working directory: the
replay tool loads a chosen tree's copy of this helper by path.

===========================================================================
*/
import "./native-source-loader.mjs";
import { goldDropModels } from "./gold-drop-models.mjs";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { publicRoot } from "../../../../scripts/build/world/paths.mjs";
/*
================
load

One client source module, resolved against this tree's src.
================
*/
async function load( entry ) {
	return import( new URL( "../../" + entry, import.meta.url ).href );
}
const { createCharacterPresentation } = await load( "src/engine/runtime/characters/characters.ts" );
const { createCharacters } = await load( "src/engine/runtime/renderer/characters/characters.ts" );
const { createPresentationRandom } = await load( "src/engine/runtime/random/random.ts" );
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
/*
================
model

A minimal decoded character source; positionFloats sizes its geometry so a
test can charge the residency byte budget. It carries Bip01 Neck and
Bip01 Neck1, the wearer sockets shoulder garments' private branches link
under.
================
*/
export function model( positionFloats = 9 ) {
	return {
		nodes: [
			{ name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] },
			{ name: "Bip01 Neck", parent: 0, translation: [ 0, 140, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] },
			{ name: "Bip01 Neck1", parent: 1, translation: [ 0, 10, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] }
		],
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
export const entity = ( gid, extra = {} ) => ({
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
const appearanceBuffers = new WeakMap();
/*
================
barePayload

A catalog payload served as itself: the roster answer.
================
*/
const barePayload = value => value;
/*
================
itemsPayload

The item rows served under their itemsByRefObjId envelope.
================
*/
const itemsPayload = items => ({ itemsByRefObjId: items });
/*
================
appearanceBuffer

Encode one admitted catalog payload once per (source, wrap) pair: a test
that builds a fixture per family or race would otherwise stringify and
encode the same multi-megabyte roster and item payloads for every
fixture (#2924). The key carries the wrap because one source under two
envelopes is two payloads; the wraps are the module constants above, so
their identity is stable across calls. The presentation decodes the
bytes read-only, so one buffer can serve every fixture that admits the
same payload.
================
*/
function appearanceBuffer( source, wrap = barePayload ) {
	if ( !source || typeof source !== "object" ) {
		return new TextEncoder().encode( JSON.stringify( wrap( source ) ) ).buffer;
	}
	let byWrap = appearanceBuffers.get( source );
	if ( !byWrap ) {
		byWrap = new Map();
		appearanceBuffers.set( source, byWrap );
	}
	let buffer = byWrap.get( wrap );
	if ( !buffer ) {
		buffer = new TextEncoder().encode( JSON.stringify( wrap( source ) ) ).buffer;
		byWrap.set( wrap, buffer );
	}
	return buffer;
}
/*
================
presentationRig

The presentation and the renderer's CPU side wired as runtime.ts wires
them. spec.assets is the asset owner; spec.port(characters) adds or
replaces renderer port members (the defaults answer no socket, no
listener and night); spec.wrap sees the finished port before the
presentation does, so a recorder can stand in front of every call.
spec.random is the shared presentation RNG; spec.rendererRandom, when
given, is the one the renderer's cloth draws from (the client passes the
same object, the tests none). spec.due, when given, replaces the rules
that decide when an entity's appearance is resolved and its motion
decided again (presentation-due.ts). prepare() runs the renderer's CPU
frame.
================
*/
export function presentationRig( spec ) {
	const characters = createCharacters( spec.rendererRandom ), assemblies = [], footprints = [];
	let actors = [], instances = [], released = 0;
	const port = {
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
		// No listener: rule sounds are not distance-culled in these tests.
		audioListener: () => null,
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
		},
		...spec.port?.( characters )
	};
	const renderer = spec.wrap ? spec.wrap( port ) : port;
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
		spec.assets,
		renderer,
		"http://localhost",
		spec.play,
		spec.random,
		spec.surface,
		spec.health,
		spec.soundEnabled,
		spec.due
	);
	return {
		characters,
		presentation,
		renderer,
		assemblies,
		footprints,
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
		prepare
		================
		*/
		prepare() {
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
		dispose
		================
		*/
		dispose() {
			presentation.dispose();
			characters.dispose( gpu, null );
		}
	};
}
/*
================
fixture

The presentation tests' rig: a stub asset server that answers each
catalog and model from the flags below, over presentationRig.
================
*/
/**
 * @param {Record<string, unknown>} [effectCatalog]
 * @param {number} [modelCount]
 * @param {boolean} [drops]
 * @param {boolean} [eventRain]
 * @param {boolean} [postures]
 * @param {boolean} [idles]
 * @param {boolean} [gear]
 * @param {boolean} [audio]
 * @param {boolean} [host]
 * @param {boolean} [reference]
 * @param {any[]} [modifiers] Particle modifier rows, as the manifests publish them.
 * @param {{ states?: Record<string, unknown>; rules?: unknown[] }} [animationAudio]
 * @param {unknown} [animationBindings]
 * @param {Record<string, unknown>} [stageModels]
 * @param {Record<string, any>} [metadataAdmission] Catalog overrides and admission faults.
 * @param {Record<string, any>} [options] Rig options a test may change between steps.
 */
export function fixture(
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
	const pending = new Map(), requests = [], played = [], failures = new Set();
	let next = 0;
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
				if ( job.url.endsWith( "/char/roster.json" ) ) {
					return { kind: "bytes", buffer: appearanceBuffer( metadataAdmission.appearance.roster ) };
				}
				if ( job.url.endsWith( "/data/missionPresentation.json" ) ) {
					return {
						kind: "bytes",
						buffer: appearanceBuffer( metadataAdmission.appearance.items, itemsPayload )
					};
				}
			}
			if ( failures.has( job.url ) ) return { kind: "error", error: "Unavailable " + job.url };
			if ( options.boothModels && job.url.endsWith( "/npc/manifest.json" ) ) {
				return {
					kind: "bytes",
					buffer:
						new TextEncoder().encode( JSON.stringify( { models: {}, boothModels: options.boothModels } ) )
							.buffer
				};
			}
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
				for ( const name of options.extraClips ?? [] ) value.clips.push( { name, duration: 1, channels: [] } );
				for ( const name of metadataAdmission.appearance?.extraClips ?? [] ) {
					value.clips.push( { name, duration: 1, channels: [] } );
				}
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
	const rig = presentationRig( {
		assets,
		play: event => played.push( event ),
		random: createPresentationRandom( 1 ),
		surface: metadataAdmission.surface ?? (() => undefined)
	} );
	const presentation = rig.presentation;
	return {
		footprints: rig.footprints,
		portraitSource: rig.characters.portraitSource,
		presentation,
		renderer: rig.renderer,
		requests,
		assemblies: rig.assemblies,
		played,
		failures,
		pending,
		get actors() {
			return rig.actors;
		},
		get instances() {
			return rig.instances;
		},
		get released() {
			return rig.released;
		},
		/*
		================
		step
		================
		*/
		/** @param {Record<string, unknown> | null} [gameplay] Partial gameplay fixture for this owner. */
		step( entities, time, gameplay = null ) {
			presentation.step( entities, gameplay, time, undefined, options.cameraPitch );
			return rig.prepare();
		},
		/*
		================
		warm
		================
		*/
		warm() {
			for ( let t = 0; t < 30 && rig.actors.length === 0; t++ ) this.step( [ entity( 1 ) ], t / 100 );
			assert.equal( rig.actors.length, 1 );
			assert.equal( presentation.error(), null );
		},
		dispose: rig.dispose
	};
}
