/*
===========================================================================

music.ts - the background music owner

Plays the current region's or scene's track through one HTMLAudioElement
(fetched through the asset owner), with the native fades and the player's
BGM volume. The element changes with every track; element() exposes it so
the audio owner can tap the music into the bug reporter's replay.

===========================================================================
*/
import { audioAmplitude, initialAudioOptions } from "@/engine/foundation/audio/options";
import {
	createMusicSelection,
	musicFade,
	musicPath,
	musicSampleRate,
	type MusicRequest
} from "@/engine/foundation/audio/music";
import type { AssetOwner } from "@/engine/contracts/assets";
const NATIVE_MINIMUM_AMPLITUDE = audioAmplitude( 1, false );
// One compressed stream for title and mission BGM. No decoded long-track cache.
/*
================
createMusic
================
*/
export function createMusic( assets: AssetOwner, origin: string ) {
	let volume = audioAmplitude( initialAudioOptions().bgm, false ), volumeDb = -1400;
	let element: HTMLAudioElement | null = null,
		url: string | null = null,
		job: number | null = null,
		optionJob: number | null = null;
	let title: string | null = null,
		current: MusicRequest | null = null,
		retained: MusicRequest | null = null,
		world = false,
		enabled = false,
		disposed = false,
		error: string | null = null;
	let mode: "priming" | "audible" | "stopped" = "priming",
		revision = 0,
		attempting: HTMLAudioElement | null = null,
		blocked = false;
	let fading = false, factor = 1, fadeAt = 0, fadeDb = volumeDb, notificationSeconds = .25;
	const select = createMusicSelection();
	/*
================
release
================
	*/
	function release() {
		revision++;
		blocked = false;
		attempting = null;
		fading = false;
		factor = 1;
		if ( job !== null ) assets.cancel( job );
		job = null;
		if ( element ) {
			element.pause();
			element.onerror = null;
			element.removeAttribute( "src" );
			element.load();
			element = null;
		}
		if ( url ) URL.revokeObjectURL( url );
		url = null;
	}
	/*
================
playing
================
	*/
	function playing() {
		return !!element && !element.paused && !element.ended;
	}
	/*
================
play
================
	*/
	function play() {
		if (
			disposed || error || mode === "stopped" || !element || attempting === element || blocked ||
			!element.paused || element.ended && !element.loop
		) return;
		const el = element, at = revision;
		attempting = el;
		void el.play().then( () => {
			if ( disposed || mode === "stopped" || el !== element ) el.pause();
		}, reason => {
			if ( disposed || el !== element ) return;
			if ( reason?.name === "NotAllowedError" ) blocked = at === revision;
			else if ( reason?.name !== "AbortError" ) error = String( reason );
		} ).finally( () => {
			if ( attempting === el ) attempting = null;
		} );
	}
	/*
================
request
================
	*/
	function request( next: MusicRequest ) {
		if ( next.path ) musicPath( next.path );
		// 8F0C50: a different name first requests a fade. It is reconsidered by
		// the caller on a later tick, not queued as a stale destination here.
		if ( playing() ) {
			if ( current?.path === next.path ) return;
			if ( !fading ) {
				fading = true;
				factor = 1;
				fadeDb = volumeDb;
				fadeAt = Math.floor( element!.currentTime / notificationSeconds ) * notificationSeconds;
			}
			return;
		}
		if ( volume === 0 ) {
			release();
			current = null;
			return;
		}
		if ( !error && current?.path === next.path && (job !== null || element && !element.ended) ) return;
		release();
		current = next;
		retained = next;
		error = null;
	}
	return {
		element: () => element,
		/*
================
volume
================
		*/
		volume( value: number ) {
			if ( !Number.isFinite( value ) || value < 0 || value > 1 ) throw Error( "Invalid music volume" );
			if ( value === volume ) return;
			const wasMuted = volume === 0;
			volume = value;
			// Port-only, not native: quiet streams fade against level 1's reference
			// so their lower starting gain cannot trip the native -50 dB stop early.
			volumeDb = value ? Math.round( 2000 * Math.log10( Math.max( value, NATIVE_MINIMUM_AMPLITUDE ) ) ) : -10000;
			fadeDb = volumeDb;
			if ( element ) element.volume = mode === "audible" ? value : 0;
			// 8F05D0: unmute reissues the retained name with repeat=1.
			if ( wasMuted && retained && world ) {
				const next = { ...retained, loop: true };
				current = null;
				request( next );
			}
		},
		/*
================
active
================
		*/
		active( value: boolean, inWorld = false ) {
			if ( disposed ) return;
			enabled = true;
			if ( world !== inWorld ) {
				world = inWorld;
				request( { path: "", loop: true } );
				error = null;
				mode = "audible";
				if ( world && optionJob !== null ) {
					assets.cancel( optionJob );
					optionJob = null;
				}
			}
			if ( world ) return;
			const next = value || fading ? "audible" : mode === "priming" ? "priming" : "stopped";
			if ( next !== mode ) {
				const primed = mode === "priming";
				mode = next;
				revision++;
				blocked = false;
				if ( element ) {
					if ( primed && value ) element.currentTime = 0;
					element.volume = value ? volume : 0;
					if ( next === "stopped" ) element.pause();
				}
			}
			if ( value && title ) request( { path: title, loop: true } );
			else if ( !current && title && mode === "priming" ) current = { path: title, loop: true };
			play();
		},
		/*
================
regional
================
		*/
		regional( path: string, mode: number ) {
			if ( disposed || !world ) return;
			const pending = !error && (job !== null || !!element && !element.ended);
			const next = select( mode, path, playing() || pending, current?.path.split( "/" ).at( -1 ) ?? "" );
			if ( next ) request( next );
		},
		/*
================
unlock
================
		*/
		unlock() {
			revision++;
			blocked = false;
			play();
		},
		/*
================
step
================
		*/
		step() {
			if ( disposed || !enabled ) return;
			if ( optionJob !== null ) {
				const result = assets.take( optionJob );
				if ( result ) {
					optionJob = null;
					try {
						if ( result.kind !== "bytes" ) {
							throw Error( result.kind === "error" ? result.error : "Invalid music option response" );
						}
						title = musicPath(
							JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) )
								.introBgmPublicPath
						);
					} catch ( reason ) {
						error = String( reason );
					}
				}
			}
			if ( !world && !title && !error && optionJob === null && assets.available() > 0 ) {
				optionJob = assets.request( new URL( "/assets/config/option.json", origin ).href, 1 << 20 );
			}
			if ( !world && title && !current && mode !== "stopped" ) current = { path: title, loop: true };
			if ( job !== null ) {
				const result = assets.take( job );
				if ( result ) {
					job = null;
					try {
						if ( result.kind !== "bytes" ) {
							throw Error( result.kind === "error" ? result.error : "Invalid music asset response" );
						}
						notificationSeconds = 11025 / musicSampleRate( new Uint8Array( result.buffer ) );
						const el = new Audio();
						element = el;
						el.loop = current!.loop;
						el.preload = "auto";
						el.volume = mode === "audible" ? volume : 0;
						url = URL.createObjectURL( new Blob( [ result.buffer ], { type: "audio/mpeg" } ) );
						el.src = url;
						el.onerror = () => {
							if ( !disposed && element === el ) error = "Music could not be decoded: " + current?.path;
						};
					} catch ( reason ) {
						error = String( reason );
					}
				}
			}
			if ( !error && current?.path && !element && job === null && assets.available() > 0 && volume > 0 ) {
				job = assets.request( new URL( current.path, origin ).href, 16 << 20 );
			}
			if ( fading && element && playing() ) {
				// A23C00/A23DD0: 44100-frame buffer, four notifications. Follow the
				// source sample clock, not the rendering or AudioContext sample rate.
				let elapsed = element.currentTime - fadeAt;
				if ( elapsed < 0 ) {
					fadeAt = element.currentTime;
					elapsed = 0;
				}
				while ( elapsed >= notificationSeconds && fading ) {
					fadeAt += notificationSeconds;
					elapsed -= notificationSeconds;
					const next = musicFade( factor, fadeDb );
					factor = next.factor;
					fadeDb = next.db;
					if ( next.stop ) {
						release();
						current = null;
					} else if ( element ) {
						const amplitude = Math.pow( 10, fadeDb / 2000 );
						element.volume = mode === "audible" && volume > 0 ?
							(volume < NATIVE_MINIMUM_AMPLITUDE ?
								amplitude * (volume / NATIVE_MINIMUM_AMPLITUDE) :
								amplitude) :
							0;
					}
				}
			}
			play();
		},
		/*
================
status
================
		*/
		status() {
			if ( error ) return "failed";
			if ( mode === "stopped" ) return "stopped";
			if ( blocked ) return "waiting-for-gesture";
			if ( playing() ) return mode === "priming" ? "primed" : fading ? "fading" : "playing";
			if ( mode !== "audible" ) return "stopped";
			// Audible but not sounding: name the reason, so "loading" means a
			// request in flight and "queued" a track waiting for an asset slot.
			// A world region without music asks for the empty track (#527).
			if ( !current?.path ) return "silent";
			if ( volume === 0 ) return "muted";
			if ( job !== null ) return "loading";
			if ( !element ) return "queued";
			return element.ended ? "ended" : "starting";
		},
		snapshot: () => ({ world, path: current?.path ?? null, loop: current?.loop ?? false, fading, volume, error }),
		error: () => error,
		// 728C70 requests the empty track through the same fade owner. Hard
		// disposal remains separate; asynchronous loads cannot outlive this scene.
		/*
================
sceneReset
================
		*/
		sceneReset() {
			request( { path: "", loop: true } );
			if ( !playing() ) {
				release();
				current = null;
			}
			world = false;
			mode = fading ? "audible" : "stopped";
			error = null;
		},
		/*
================
reset
================
		*/
		reset() {
			release();
			current = null;
			retained = null;
			world = false;
			mode = "stopped";
			error = null;
		},
		/*
================
dispose
================
		*/
		dispose() {
			if ( disposed ) return;
			disposed = true;
			release();
			if ( optionJob !== null ) assets.cancel( optionJob );
			optionJob = null;
			current = null;
			mode = "stopped";
		}
	};
}
