/*
===========================================================================

audio.ts - sound playback, decoded residency and scene readiness

Owns Web Audio voices and the bounded decoded buffer cache. Scene sound
preparation fills that same cache before world admission; cue timing remains
owned by character and UI presentation, and expired cues never replay late.

===========================================================================
*/
import { createSoundPreparation, type SoundScene, type SoundEntity } from "./preparation";
import {
	initialAudioOptions,
	audioOptions,
	audioAmplitude,
	type AudioOptions
} from "@/engine/foundation/audio/options";
import { audioLoopEnd } from "@/engine/foundation/audio/loop";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";
import {
	decodeAmbientProfiles,
	decodeAudioRegions,
	ambientProfileName,
	ambientPeriod,
	type AmbientProfile,
	type AudioRegion,
	type AmbientLayer
} from "@/engine/foundation/audio/environment";
import type { Pose, WorldClockSeed } from "@/engine/contracts/gameplay";
import { createUiSoundCatalog, type UiSoundHandle } from "@/engine/foundation/ui/sound-catalog";
import { createItemSoundCatalog, createItemDropSoundCatalog } from "@/engine/foundation/audio/item-sound-catalog";
import { itemSoundCategory } from "@/engine/foundation/audio/item-sounds";
import { createMusic } from "./music/music";
import type { AssetOwner } from "@/engine/contracts/assets";
import type { SoundEvent } from "@/engine/contracts/audio";
const MAX_DECODES = 2, SOUND_INPUT_BYTES = 4 << 20, SOUND_RESIDENT_BYTES = 32 << 20;
const RESIDENCY_RETRY_SECONDS = 2;
/*
================
createAudio
================
*/
export function createAudio( assets: AssetOwner, origin: string, random: PresentationRandom, timerStartMs = 0 ) {
	if ( !Number.isInteger( timerStartMs ) || timerStartMs < 0 || timerStartMs > 0xffffffff ) {
		throw Error( "Invalid audio timer registration clock" );
	}
	const uiSoundCatalog = createUiSoundCatalog();
	const itemSoundCatalog = createItemSoundCatalog();
	const itemDropSoundCatalog = createItemDropSoundCatalog();
	const music = createMusic( assets, origin );
	const preparation = createSoundPreparation( assets, origin );
	let admittedGid = 0, admittedSounds = false;
	let preferences = initialAudioOptions();
	const voiceGains = new Map<AudioBufferSourceNode, { gain: GainNode; level: number; ambient: boolean; }>();
	/*
================
mixGain
================
	*/
	function mixGain( level: number, ambient: boolean ) {
		return level *
			audioAmplitude(
				ambient ? preferences.environment : preferences.effects,
				ambient ? preferences.muteEnvironment : preferences.muteEffects
			);
	}
	let context: AudioContext | null = null,
		disposed = false,
		epoch = 0,
		failure: string | null = null,
		job: {
			id: number;
			path: string;
		} | null = null;
	// Native decodes cannot be cancelled. Their slots survive reset until settlement.
	const decoding = new Map<string, number>();
	const buffers = new Map<string, AudioBuffer>(),
		pending = new Map<string, SoundEvent>(),
		seen = new Set<string>(),
		voices = new Set<AudioBufferSourceNode>();
	const loops = new Map<string, AudioBufferSourceNode>();
	// Preparation is one pass per world admission. Eviction must not turn
	// speculative warmup into an endless decode/evict cycle during play.
	const preparedPaths = new Set<string>();
	let resident = 0;
	let ambientProfiles: readonly AmbientProfile[] | null = null,
		audioRegions: ReadonlyMap<number, AudioRegion> | null = null,
		ambientJob: number | null = null;
	let ambientEnabled = false, ambientLookup = true, ambientFailure: string | null = null;
	let requestedAmbient: AmbientProfile | null = null,
		requestedPeriod: "day" | "night" = "day",
		activeAmbient: AmbientProfile | null = null,
		activePeriod: "day" | "night" = "day";
	let ambientTimer = timerStartMs, ambientSequence = 0, ambientGeneration = 0, musicMode = 0;
	let ambientLayers: { layer: AmbientLayer; remaining: number; }[] = [];
	/*
================
clearAmbient
================
	*/
	function clearAmbient() {
		for ( const [id, event] of pending ) if ( id.startsWith( "ambient:" ) ) pending.delete( id );
		for ( const [id, voice] of loops ) {
			if ( id.startsWith( "ambient:" ) ) {
				loops.delete( id );
				voice.stop();
			}
		}
		activeAmbient = null;
		ambientLayers = [];
	}
	/*
================
ambientCue
================
	*/
	function ambientCue( layer: AmbientLayer, loop: boolean, seconds: number ) {
		const id = `ambient:${ambientGeneration}:${++ambientSequence}`;
		if ( layer.path && pending.size < 128 ) {
			pending.set( id, {
				id,
				path: layer.path,
				gain: 1,
				x: 0,
				y: 0,
				z: 0,
				expires: seconds + 2,
				spatial: false,
				loop
			} );
		}
	}
	/*
================
stepAmbient
================
	*/
	function stepAmbient( seconds: number ) {
		// Resource completion and playback readiness never consume this stream.
		if ( ambientJob !== null ) {
			const result = assets.take( ambientJob );
			if ( result ) {
				ambientJob = null;
				try {
					if ( result.kind !== "bytes" ) {
						throw Error( result.kind === "error" ? result.error : "Invalid ambient catalog response" );
					}
					const value: unknown = JSON.parse(
						new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer )
					);
					if ( !ambientProfiles ) ambientProfiles = decodeAmbientProfiles( value );
					else audioRegions = decodeAudioRegions( value );
				} catch ( error ) {
					ambientFailure = String( error );
				}
			}
		}
		if (
			ambientEnabled && !ambientFailure && (!ambientProfiles || !audioRegions) && ambientJob === null &&
			assets.available() > 0
		) {
			ambientJob = assets.request(
				new URL(
					!ambientProfiles ? "/assets/audio/effectenvsnd.json" : "/assets/audio/regioninfo.json",
					origin
				).href,
				1 << 20
			);
		}
		const now = Math.floor( seconds * 1000 ) >>> 0;
		if ( ((now - ambientTimer) >>> 0) < 2000 ) return;
		ambientTimer = now;
		// CGEffSoundBody::OnTimer(1), 8F7590: switch on the timer, not on the
		// profile setter; initialize each countdown to min without drawing RNG.
		if ( ambientLookup && (requestedAmbient !== activeAmbient || requestedPeriod !== activePeriod) ) {
			clearAmbient();
			activeAmbient = requestedAmbient;
			activePeriod = requestedPeriod;
			ambientGeneration++;
			ambientLayers = (activeAmbient?.[activePeriod] ?? []).map( layer => ({ layer, remaining: layer.min }) );
			for ( const { layer } of ambientLayers ) if ( layer.min === 0 ) ambientCue( layer, true, seconds );
		}
		// 8F7688..8F7965: null region or missing named profile skips music;
		// a valid profile reaches all four mode arms on this same timer.
		if ( ambientEnabled && ambientLookup && requestedAmbient ) music.regional( requestedAmbient.music, musicMode );
		for ( const state of ambientLayers ) {
			const { layer } = state;
			if ( layer.min === 0 ) continue;
			if ( state.remaining > 2 ) {
				state.remaining -= 2;
				continue;
			}
			ambientCue( layer, false, seconds );
			state.remaining = random.range( layer.min, layer.max );
		}
	}
	// Prewarm authored UI buffers through the same bounded queue as live cues.
	const uiFiles = {
		click: [ "uibutton_a.wav", "uibutton_b.wav" ],
		open: [ "uiwinopen.wav" ],
		close: [ "uiwinclose.wav" ],
		message: [ "error.wav" ],
		quest: [ "questopen.wav" ]
	} as const;
	const buffEndPath = "/assets/audio/sfx/prim/snd/ui/buf_disappear.wav";
	const uiPaths: readonly string[] = [
		buffEndPath,
		...new Set(
			[
				...Object.values( uiSoundCatalog ).flat(),
				...Object.values( itemSoundCatalog ).flat(),
				...Object.values( itemDropSoundCatalog ).flat()
			].map( row => row.path )
		)
	];
	const uiRetryAt = new Map<string, number>();
	let uiSequence = 0, clock = 0, uiActive = false;
	// 8FAA30 passes null position. A88C2A skips A89190's per-sound
	// weighting for that branch; A88F16 installed the master volume alone.
	// The table's 80 is not an extra linear 0.8 amplitude on UI playback.
	/*
================
nativeUi
================
	*/
	function nativeUi( handle: UiSoundHandle, at = clock, position?: readonly [number, number, number] ) {
		// 6B734E -> manager +30 (8FAA90) passes the local actor position.
		// Its float 1 is distance scale (A886A0), not UI volume.
		const alarm = handle === "SND_ALARM";
		if ( disposed || pending.size >= 128 || (alarm && !position) ) return;
		if ( alarm && !position!.every( Number.isFinite ) ) throw Error( "Invalid alarm position" );
		const rows = uiSoundCatalog[handle], row = rows[random.range( 0, rows.length )]!, id = "ui:" + ++uiSequence;
		const [x, y, z] = alarm ? position! : [ 0, 0, 0 ];
		pending.set( id, { id, path: row.path, gain: 1, x, y, z, expires: at + .5, spatial: alarm } );
	}
	/*
================
uiSound
================
	*/
	function uiSound( kind: keyof typeof uiFiles ) {
		nativeUi(
			({
				click: "SND_BUTTON_CLICK",
				open: "SND_WINDOW_OPEN",
				close: "SND_WINDOW_CLOSE",
				message: "SND_ERROR",
				quest: "SND_QUEST"
			} as const)[kind]
		);
	}
	/*
================
nativeItem
================
	*/
	function nativeItem( cue: import("@/engine/contracts/audio").ItemSoundRequest, at = clock ) {
		if ( disposed || pending.size >= 128 ) return;
		if (
			![ "SND_EQUIP", "SND_DROPITEM" ].includes( cue.handle ) || !Number.isInteger( cue.typeFlags ) ||
			cue.typeFlags < 0 || cue.typeFlags > 0xffff
		) throw Error( "Invalid ITEM sound request" );
		const rows =
			(cue.handle === "SND_EQUIP" ? itemSoundCatalog : itemDropSoundCatalog)[itemSoundCategory( cue.typeFlags )];
		if ( !rows?.length ) return; // Native exact key miss is silent, including avatar TIDs.
		const row = rows[random.range( 0, rows.length )]!, id = "item:" + ++uiSequence;
		// 8F9710 -> 8F9280(null position): master effects volume only.
		pending.set( id, { id, path: row.path, gain: 1, x: 0, y: 0, z: 0, expires: at + .5, spatial: false } );
	}
	return {
		/*
================
options
================
		*/
		options( value: AudioOptions ) {
			preferences = audioOptions( value );
			music.volume( audioAmplitude( preferences.bgm, preferences.muteBgm ) );
			for ( const voice of voiceGains.values() ) voice.gain.gain.value = mixGain( voice.level, voice.ambient );
		},
		/*
================
world
================
		*/
		world( pose: Pose | null, seed: WorldClockSeed | undefined, simulationMs: number, mode = 0 ) {
			if ( disposed ) return;
			musicMode = mode;
			ambientEnabled = pose !== null;
			const name = pose && audioRegions ? ambientProfileName( audioRegions, pose ) : null;
			ambientLookup = !pose || name !== null;
			// Native null region-name skips the profile lookup and preserves the
			// active layer set. A named but absent profile tears it down.
			if ( !pose ) requestedAmbient = null;
			else if ( name ) requestedAmbient = ambientProfiles?.find( p => p.name === name ) ?? null;
			if ( seed ) requestedPeriod = ambientPeriod( seed, simulationMs );
		},
		// CIFButton mouse-up sends SND_BUTTON_CLICK; effect sound rows 40/41
		// supply the two non-spatial variants at volume 80.
		/*
================
uiClick
================
		*/
		uiClick() {
			uiSound( "click" );
		},
		uiSound,
		nativeUi,
		nativeItem,
		/*
================
buffEnded
================
		*/
		buffEnded( at: number, position: readonly [number, number, number] ) {
			if ( disposed || pending.size >= 128 ) return;
			if ( !Number.isFinite( at ) || !position.every( Number.isFinite ) ) {
				throw Error( "Invalid buff sound position" );
			}
			const id = "buff:" + ++uiSequence;
			pending.set( id, {
				id,
				path: buffEndPath,
				gain: 1,
				x: position[0],
				y: position[1],
				z: position[2],
				expires: at + .5,
				spatial: true
			} );
		},
		/*
================
prepareCombat
================
		*/
		prepareCombat( gameplay: SoundScene | null, entities: readonly SoundEntity[] ) {
			const gid = gameplay?.localGid ?? 0;
			if ( gid !== admittedGid ) {
				admittedGid = gid;
				admittedSounds = false;
				preparedPaths.clear();
				for ( const path of buffers.keys() ) preparedPaths.add( path );
			}
			preparation.step( gameplay, entities );
		},
		/*
================
ready

World entry is a one-time admission barrier. Preparation proves that each sound decodes, not that an unbounded scene fits
in the resident cache. New nearby entities warm once without reopening entry.
================
		*/
		ready() {
			const complete = preparation.ready() && preparation.paths().every( path => preparedPaths.has( path ) ) &&
				(!uiActive || uiPaths.every( path => preparedPaths.has( path ) ));
			if ( admittedGid && complete ) admittedSounds = true;
			return admittedGid ? admittedSounds : complete;
		},
		/*
================
prepareUi
================
		*/
		prepareUi( active: boolean ) {
			if ( disposed ) return;
			uiActive = active;
			if ( active && !context ) context = new AudioContext();
		},
		music: music.active,
		musicStatus: music.status,
		musicSnapshot: music.snapshot,
		/*
================
unlock
================
		*/
		unlock() {
			if ( disposed ) {
				return;
			}
			music.unlock();
			if ( !context ) {
				context = new AudioContext();
			}
			context.resume().catch( error => {
				failure = String( error );
			} );
		},
		/*
================
enqueue
================
		*/
		enqueue( event: SoundEvent ) {
			if ( event.stop ) {
				pending.delete( event.id );
				seen.delete( event.id );
				const voice = loops.get( event.id );
				if ( voice ) {
					loops.delete( event.id );
					voice.stop();
				}
				return;
			}
			if ( disposed || seen.has( event.id ) ) {
				return;
			}
			if (
				!event.path.startsWith( "/assets/audio/" ) || event.path.includes( ".." ) ||
				![ event.gain, event.x, event.y, event.z, event.expires ].every( Number.isFinite )
			) {
				throw new Error( "Invalid spatial audio event" );
			}
			if ( pending.size >= 128 ) {
				return;
			}
			seen.add( event.id );
			if ( seen.size > 4096 ) {
				seen.delete( seen.values().next().value! );
			}
			pending.set( event.id, { ...event } );
		},
		/*
================
step
================
		*/
		step(
			seconds: number,
			listener: readonly [
				number,
				number,
				number
			],
			orientation?: Pick<import("@/engine/contracts/audio").SoundListener, "forward" | "up">
		) {
			if ( disposed ) {
				return;
			}
			stepAmbient( seconds );
			music.step();
			clock = seconds;
			for ( const [id, event] of pending ) {
				if ( !event.loop && event.expires < seconds ) {
					pending.delete( id );
				}
			}
			if ( !context ) {
				return;
			}
			context.listener.positionX.value = listener[0];
			context.listener.positionY.value = listener[1];
			context.listener.positionZ.value = listener[2];
			if ( orientation ) {
				context.listener.forwardX.value = orientation.forward[0];
				context.listener.forwardY.value = orientation.forward[1];
				context.listener.forwardZ.value = orientation.forward[2];
				context.listener.upX.value = orientation.up[0];
				context.listener.upY.value = orientation.up[1];
				context.listener.upZ.value = orientation.up[2];
			}
			// Cue expiry cancels playback, not a bounded resource load. A cold
			// footstep must be able to populate the cache for its next occurrence.
			if ( job ) {
				const result = assets.take( job.id );
				if ( result ) {
					const path = job.path;
					job = null;
					if ( result.kind === "bytes" ) {
						const generation = epoch;
						const reject = ( error: unknown ) => {
							decoding.delete( path );
							if ( generation === epoch && !disposed ) {
								if ( uiPaths.includes( path ) || preparation.paths().includes( path ) ) {
									uiRetryAt.set( path, clock + 2 );
								}
								failure = String( error );
								for ( const [id, event] of pending ) {
									if ( event.path === path ) {
										pending.delete( id );
									}
								}
							}
						};
						try {
							if ( result.buffer.byteLength > SOUND_INPUT_BYTES ) {
								throw new Error( "Encoded sound exceeds decode budget" );
							}
							decoding.set( path, generation );
							context.decodeAudioData( result.buffer ).then( buffer => {
								decoding.delete( path );
								if ( disposed || generation !== epoch ) {
									return;
								}
								const bytes = buffer.length * buffer.numberOfChannels * 4;
								if ( !Number.isSafeInteger( bytes ) || bytes < 0 || bytes > SOUND_RESIDENT_BYTES ) {
									reject( "Decoded sound exceeds residency budget" );
									return;
								}
								// Playing sources retain their buffers even if removed from the cache.
								// Keep them charged to residency until their voices have ended.
								for ( const [key, old] of buffers ) {
									if ( resident + bytes <= SOUND_RESIDENT_BYTES ) {
										break;
									}
									if (
										[ ...voices ].some( source => source.buffer === old )
									) {
										continue;
									}
									resident -= old.length * old.numberOfChannels * 4;
									buffers.delete( key );
								}
								if ( resident + bytes > SOUND_RESIDENT_BYTES ) {
									// Decode succeeded, but live voices own the available capacity.
									// Remember preparation and retry demand after those voices can end.
									preparedPaths.add( path );
									uiRetryAt.set( path, clock + RESIDENCY_RETRY_SECONDS );
									return;
								}
								buffers.set( path, buffer );
								preparedPaths.add( path );
								uiRetryAt.delete( path );
								resident += bytes;
							} ).catch( reject );
						} catch ( error ) {
							reject( error );
						}
					} else if ( result.kind === "error" ) {
						if ( uiPaths.includes( path ) || preparation.paths().includes( path ) ) {
							uiRetryAt.set( path, clock + 2 );
						}
						failure = result.error;
						for ( const [id, event] of pending ) {
							if ( event.path === path ) {
								pending.delete( id );
							}
						}
					}
				}
			}
			for ( const [id, event] of pending ) {
				// A89190 rejects inaudible positional voices before allocating playback.
				if (
					event.spatial !== false &&
					Math.hypot( event.x - listener[0], event.y - listener[1], event.z - listener[2] ) >= 300
				) {
					pending.delete( id );
					continue;
				}
				const buffer = buffers.get( event.path );
				if ( !buffer ) {
					if (
						!job && decoding.size < MAX_DECODES && !decoding.has( event.path ) &&
						(uiRetryAt.get( event.path ) ?? 0) <= clock && assets.available() > 0
					) {
						job = {
							id: assets.request( new URL( event.path, origin ).href, SOUND_INPUT_BYTES ),
							path: event.path
						};
					}
					continue;
				}
				if ( context.state !== "running" ) continue;
				buffers.delete( event.path );
				buffers.set( event.path, buffer );
				if ( voices.size >= 32 ) {
					if ( !event.loop ) pending.delete( id );
					continue;
				}
				pending.delete( id );
				const source = context.createBufferSource(),
					gain = context.createGain(),
					panner = event.spatial === false ? null : context.createPanner();
				source.buffer = buffer;
				source.loop = event.loop === true;
				if ( source.loop ) source.loopEnd = audioLoopEnd( buffer.length, buffer.sampleRate );
				if ( event.loop || id.startsWith( "ambient:" ) ) loops.set( id, source );
				const level = Math.max( 0, Math.min( 1, event.gain ) ), ambient = id.startsWith( "ambient:" );
				gain.gain.value = mixGain( level, ambient );
				voiceGains.set( source, { gain, level, ambient } );
				if ( panner ) {
					panner.panningModel = "equalpower";
					panner.distanceModel = "linear";
					panner.refDistance = 100;
					panner.maxDistance = 300;
					panner.rolloffFactor = 1;
					panner.positionX.value = event.x;
					panner.positionY.value = event.y;
					panner.positionZ.value = event.z;
				}
				source.connect( gain );
				if ( panner ) {
					gain.connect( panner );
					panner.connect( context.destination );
				} else gain.connect( context.destination );
				voices.add( source );
				source.onended = () => {
					voiceGains.delete( source );
					voices.delete( source );
					if ( loops.get( id ) === source ) loops.delete( id );
					source.disconnect();
					gain.disconnect();
					panner?.disconnect();
				};
				source.start();
			}
			if ( !job && decoding.size < MAX_DECODES && assets.available() > 0 ) {
				const candidates = preparation.paths().filter( path => !preparedPaths.has( path ) );
				const path = [
					...candidates,
					...(uiActive ? uiPaths.filter( path => !preparedPaths.has( path ) ) : [])
				].find( path =>
					!buffers.has( path ) && !decoding.has( path ) && (uiRetryAt.get( path ) ?? 0) <= clock
				);
				if ( path ) job = { id: assets.request( new URL( path, origin ).href, SOUND_INPUT_BYTES ), path };
			}
		},
		/*
================
snapshot
================
		*/
		snapshot(): import("@/engine/contracts/audio").AudioResidencySnapshot {
			const required = preparation.paths();
			return {
				sampleRate: context?.sampleRate ?? 0,
				limitBytes: SOUND_RESIDENT_BYTES,
				residentBytes: resident,
				admitted: admittedSounds,
				required: [ ...required ],
				decoding: [ ...decoding.keys() ],
				buffers: [ ...buffers ].map( ( [path, buffer] ) => ({
					path,
					bytes: buffer.length * buffer.numberOfChannels * 4,
					playing: [ ...voices ].some( source => source.buffer === buffer ),
					preparing: !admittedSounds && required.includes( path ),
					ui: (uiActive || admittedGid !== 0) && uiPaths.includes( path )
				}) )
			};
		},
		error: () => preparation.error() ?? failure ?? ambientFailure ?? music.error(),
		/*
================
reset
================
		*/
		reset() {
			music.sceneReset();
			clearAmbient();
			requestedAmbient = null;
			epoch++;
			if ( job ) {
				assets.cancel( job.id );
			}
			job = null;
			pending.clear();
			seen.clear();
			failure = null;
			for ( const source of voices ) {
				source.stop();
			}
			voices.clear();
			voiceGains.clear();
			loops.clear();
		},
		/*
================
dispose
================
		*/
		dispose() {
			if ( disposed ) {
				return;
			}
			disposed = true;
			preparation.dispose();
			music.dispose();
			if ( ambientJob !== null ) assets.cancel( ambientJob );
			ambientJob = null;
			clearAmbient();
			requestedAmbient = null;
			ambientProfiles = null;
			audioRegions = null;
			epoch++;
			if ( job ) {
				assets.cancel( job.id );
			}
			job = null;
			for ( const source of voices ) {
				source.stop();
			}
			voices.clear();
			voiceGains.clear();
			loops.clear();
			buffers.clear();
			preparedPaths.clear();
			pending.clear();
			seen.clear();
			resident = 0;
			context?.close().catch( () => {} );
			context = null;
		}
	};
}
