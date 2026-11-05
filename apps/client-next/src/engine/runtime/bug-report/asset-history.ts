/*
===========================================================================

asset-history.ts - the asset queue's recent history, for the report's .zip

A loading stall can hold the screen for minutes before the player files a
report (#1975: Restart to Character Select sat about four minutes with the
server idle), so the queue it left behind is sampled every ASSET_SAMPLE_MS
for MAX_ASSET_SAMPLES samples, further back than the journal's two minutes.
The samples settle which stall it was:

	no free slot, the departed scene's loads listed   slot retention
	the next scene's loads listed with growing ages   a stalled download
	no load listed at all                             the scene never asked

Each sample is a bounded copy of AssetOwner.snapshot (paths only; the asset
owner already strips credentials and queries).

===========================================================================
*/
import type { AssetJobSnapshot } from "@/engine/contracts/assets";

// 72 samples 5 s apart are six minutes.
export const ASSET_SAMPLE_MS = 5000;
export const MAX_ASSET_SAMPLES = 72;
// Jobs listed per sample; the queue holds a handful in practice.
export const MAX_ASSET_JOBS = 24;

/*
================
AssetHistoryOptions
================
*/
export interface AssetHistoryOptions {
	/** The asset owner's copied request state, or undefined before there is one. */
	readonly read: () => AssetJobSnapshot | undefined;
	/** The session phase at the sample ("world", "character-select", ...). */
	readonly phase: () => string | null;
}

/*
================
AssetQueue
================
*/
interface AssetQueue {
	readonly phase: AssetJobSnapshot["phase"];
	readonly error: string | null;
	readonly available: number;
	readonly jobs: AssetJobSnapshot["jobs"];
	readonly omittedJobs?: number;
}

/*
================
createAssetHistory
================
*/
export function createAssetHistory( options: AssetHistoryOptions ) {
	const samples: { atMs: number; phase: string | null; queue: AssetQueue | null; }[] = [];
	let lastSampleMs = -Infinity;

	/*
	================
	queue
	================
	*/
	function queue(): AssetQueue | null {
		const snapshot = options.read();
		if ( !snapshot ) return null;
		return {
			phase: snapshot.phase,
			error: snapshot.error,
			available: snapshot.available,
			jobs: snapshot.jobs.slice( 0, MAX_ASSET_JOBS ).map( job => ({ ...job, ageMs: Math.round( job.ageMs ) }) ),
			...(snapshot.jobs.length > MAX_ASSET_JOBS ? { omittedJobs: snapshot.jobs.length - MAX_ASSET_JOBS } : {})
		};
	}

	return {
		/*
		================
		sample

		Called every UI frame, the loading screen's included; keeps one copy
		per ASSET_SAMPLE_MS.
		================
		*/
		sample( nowMs: number ) {
			if ( nowMs - lastSampleMs < ASSET_SAMPLE_MS ) return;
			lastSampleMs = nowMs;
			samples.push( { atMs: nowMs, phase: options.phase(), queue: queue() } );
			if ( samples.length > MAX_ASSET_SAMPLES ) samples.shift();
		},
		/*
		================
		document

		assets.json: the queue now and its samples, in seconds on the
		timeline's clock (zeroMs is the replay's first frame).
		================
		*/
		document( zeroMs: number, nowMs: number ) {
			const at = ( ms: number ) => +((ms - zeroMs) / 1000).toFixed( 3 );
			return {
				note: "t is seconds from the first frame of replay.mp4 (negative: before the recording); " +
					"available is free request slots; ageMs is how long each listed load has been open.",
				current: { t: at( nowMs ), phase: options.phase(), queue: queue() },
				samples: samples.map( ( { atMs, ...sample } ) => ({ t: at( atMs ), ...sample }) )
			};
		}
	};
}
