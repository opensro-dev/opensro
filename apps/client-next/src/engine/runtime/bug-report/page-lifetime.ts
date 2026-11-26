/*
===========================================================================

page-lifetime.ts - the previous page's death certificate (#4545)

A page the OS kills (an iPhone jetsam, a crashed tab) runs no pagehide
and files no report: the tab simply comes back and everything the page
knew is gone (BR-261126-1443-4077: two players, a reload every three
or four minutes, no evidence). This module is the evidence. While the
page is in the world it keeps a sessionStorage marker - when the page
entered the world, when it last beat, how many entities it presented,
its session phase and its standing GPU allocation totals - refreshed
on a 30 s heartbeat. A clean end
removes the marker (bug-report's dispose, reached from the platform's
one pagehide listener). A marker still there at the next boot means
the previous page died without one, and takePreviousPage turns it into
the previousPage environment field: how long that page lived, how
stale its last heartbeat is, and the world it died in.

The heartbeat rides the reporter's own frame (watchPageLifetime in
bug-report.ts calls beatPageLifetime): timers belong to the clock
owners in this client, so the module owns no timer. A beat lands on
the first frame at or after its 30 s boundary; a backgrounded tab
beats late or not at all, so secondsSinceLastBeat is read as "at most
this fresh", never as a precise death time.

sessionStorage, not localStorage: the marker belongs to the tab, so a
fresh tab never inherits a stranger's death, while the tab's own
reload (a kill, a crash, the update prompt) keeps it.

Every storage access sits in a try/catch: a private window throws on
access, and a diagnostics marker must never take the page down.

===========================================================================
*/

import type { GpuAllocationTotals } from "@/engine/foundation/rendering/gpu-allocations";

// The tab's page-lifetime marker (sessionStorage).
const PAGE_LIFETIME_KEY = "sro:page-lifetime:1";

// How often the living page rewrites the marker.
export const PAGE_LIFETIME_BEAT_MS = 30_000;

/*
================
PageLifetimeSample

The living page at one heartbeat: how many entities it presents and
its session phase, as bug-report's state read names them.
================
*/
export interface PageLifetimeSample {
	readonly entities: number;
	readonly phase: string;
	/** The page's standing GPU allocation totals at this beat, or null. */
	readonly gpu?: GpuAllocationTotals | null;
}

/*
================
PreviousPage

The dead page as the next boot's environment.json carries it. died is
always true: a page that ended cleanly leaves no marker, so a marker's
existence is the death. Times are whole seconds.
================
*/
export interface PreviousPage {
	readonly died: true;
	readonly uptimeS: number;
	readonly lastEntities: number;
	readonly lastPhase: string;
	/** The dead page's standing GPU allocation totals at its last beat. */
	readonly lastGpu: GpuAllocationTotals | null;
	readonly secondsSinceLastBeat: number;
}

/*
================
PageLifetimeHooks

The marker's storage and clock, injectable so tests drive the module
with fakes. Production passes nothing: sessionStorage and Date.now.
The storage is the DOM Storage the page already owns; every call on
it may throw (private mode), and every call site here wraps it.
================
*/
export interface PageLifetimeHooks {
	readonly storage?: Storage | null;
	readonly now?: () => number;
}

// The one marker's living state: the sampler each beat reads, the
// storage and clock the start was given, the world-entry stamp every
// beat keeps and the last beat's stamp the 30 s boundary measures from.
interface PageLifetimeState {
	sample: (() => PageLifetimeSample) | null;
	storage: Storage | null;
	now: () => number;
	startedAtMs: number;
	lastBeatMs: number;
}

const pageLifetime: PageLifetimeState = {
	sample: null,
	storage: null,
	now: () => Date.now(),
	startedAtMs: 0,
	lastBeatMs: 0
};

/*
================
sessionStorageOrNull

The page's session storage, or null where there is none (a test
process) or access itself throws (a private window).
================
*/
function sessionStorageOrNull(): Storage | null {
	try {
		return typeof sessionStorage === "undefined" ? null : sessionStorage;
	} catch {
		return null;
	}
}

/*
================
writePageLifetimeMarker

One beat's write: the original world-entry stamp, this beat's time
and the sampler's current read. A storage that refuses (full,
blocked) loses the beat, never the page.
================
*/
function writePageLifetimeMarker( nowMs: number ): void {
	const sample = pageLifetime.sample, storage = pageLifetime.storage;
	if ( !sample || !storage ) return;
	const current = sample();
	pageLifetime.lastBeatMs = nowMs;
	try {
		storage.setItem(
			PAGE_LIFETIME_KEY,
			JSON.stringify( {
				startedAtMs: pageLifetime.startedAtMs,
				lastBeatMs: nowMs,
				entities: Number.isFinite( current.entities ) ? current.entities : 0,
				phase: typeof current.phase === "string" ? current.phase : "unknown",
				gpu: current.gpu ?? null
			} )
		);
	} catch {
		// A diagnostics write must never surface as a page failure.
	}
}

/*
================
startPageLifetime

World entry: stamp the marker; the reporter's frame beats it from
there (beatPageLifetime). A page that enters the world again (a relog
within the page) starts a fresh segment: the boot read already took
the old marker's measure, and the page that matters for the next
death certificate is the current one.
================
*/
export function startPageLifetime( sample: () => PageLifetimeSample, hooks: PageLifetimeHooks = {} ): void {
	pageLifetime.sample = sample;
	pageLifetime.storage = hooks.storage !== undefined ? hooks.storage : sessionStorageOrNull();
	pageLifetime.now = hooks.now ?? (() => Date.now());
	pageLifetime.startedAtMs = pageLifetime.now();
	writePageLifetimeMarker( pageLifetime.startedAtMs );
}

/*
================
beatPageLifetime

The heartbeat, offered every frame by the reporter: it writes only
when the 30 s boundary has passed since the last beat, so the frame
rate never reaches storage. nowMs is the caller's wall clock; a test
passes its own.
================
*/
export function beatPageLifetime( nowMs?: number ): void {
	if ( !pageLifetime.sample ) return;
	const now = nowMs ?? pageLifetime.now();
	if ( now - pageLifetime.lastBeatMs < PAGE_LIFETIME_BEAT_MS ) return;
	writePageLifetimeMarker( now );
}

/*
================
endPageLifetime

A clean end (the pagehide the platform already listens for, through
the runtime's dispose): take the marker away, so the next boot
reports no death.
================
*/
export function endPageLifetime(): void {
	pageLifetime.sample = null;
	const storage = pageLifetime.storage ?? sessionStorageOrNull();
	pageLifetime.storage = null;
	if ( !storage ) return;
	try {
		storage.removeItem( PAGE_LIFETIME_KEY );
	} catch {
		// The next boot's read clears a marker it cannot use anyway.
	}
}

/*
================
PageLifetimeMarker

The marker as written, before validation: every field unknown-typed,
because storage holds whatever an older or corrupt page left there.
================
*/
interface PageLifetimeMarker {
	startedAtMs?: unknown;
	lastBeatMs?: unknown;
	entities?: unknown;
	phase?: unknown;
	gpu?: unknown;
}

/*
================
takePreviousPage

Boot, read once: a surviving marker becomes the dead page's measure
and is cleared, so a second read (or a second report) never repeats
it. A marker that does not parse, or whose stamps are not finite
numbers, is cleared and reads as no death: yesterday's corrupt marker
must not accuse every future page.
================
*/
export function takePreviousPage( nowMs: number, hooks: PageLifetimeHooks = {} ): PreviousPage | null {
	const storage = hooks.storage !== undefined ? hooks.storage : sessionStorageOrNull();
	if ( !storage ) return null;
	let raw: string | null = null;
	try {
		raw = storage.getItem( PAGE_LIFETIME_KEY );
	} catch {
		return null;
	}
	if ( raw === null ) return null;
	try {
		storage.removeItem( PAGE_LIFETIME_KEY );
	} catch {
		// Read once in memory even when the clear is refused.
	}
	let marker: PageLifetimeMarker | null = null;
	try {
		marker = JSON.parse( raw ) as PageLifetimeMarker;
	} catch {
		return null;
	}
	const startedAtMs = marker?.startedAtMs, lastBeatMs = marker?.lastBeatMs;
	if (
		typeof startedAtMs !== "number" || !Number.isFinite( startedAtMs ) ||
		typeof lastBeatMs !== "number" || !Number.isFinite( lastBeatMs )
	) return null;
	return {
		died: true,
		uptimeS: Math.max( 0, Math.round( (lastBeatMs - startedAtMs) / 1000 ) ),
		lastEntities: typeof marker?.entities === "number" && Number.isFinite( marker.entities ) ? marker.entities : 0,
		lastPhase: typeof marker?.phase === "string" ? marker.phase : "unknown",
		lastGpu: gpuTotalsOrNull( marker?.gpu ),
		secondsSinceLastBeat: Math.max( 0, Math.round( (nowMs - lastBeatMs) / 1000 ) )
	};
}

/*
================
gpuTotalsOrNull

A marker's GPU totals, or null: an older marker has none, and a
corrupt one must not invent them. All four figures must be finite
numbers or the whole read is none - a half-total would mislead the
very question (did the dead page hold too much?) the field answers.
================
*/
function gpuTotalsOrNull( value: unknown ): GpuAllocationTotals | null {
	if ( typeof value !== "object" || value === null ) return null;
	const totals = value as Partial<GpuAllocationTotals>;
	for ( const field of [ "buffers", "bufferBytes", "textures", "textureBytes" ] as const ) {
		const figure = totals[field];
		if ( typeof figure !== "number" || !Number.isFinite( figure ) || figure < 0 ) return null;
	}
	return {
		buffers: totals.buffers!,
		bufferBytes: totals.bufferBytes!,
		textures: totals.textures!,
		textureBytes: totals.textureBytes!
	};
}
