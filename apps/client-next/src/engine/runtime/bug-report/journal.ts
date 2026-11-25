/*
===========================================================================

journal.ts - what happened during the replay, for the report's .zip

A bounded rolling record of the last two minutes, kept beside the video so a
developer can line up the picture with what the player pressed, what the
game said and how the page was doing:

	key        a key went down or up (never while typing in a text field)
	click      a press on the game world (normalized position, button)
	ui         a press on a game window control (its data-ui-id; the shared
	           report keeps only uiControl's prefix and slot number)
	refusal    the game refused a command the player gave (refusalReason)
	chat       a chat line the player saw or sent
	error      a JavaScript or runtime failure (message and sanitised stack)
	long-frame the main thread was blocked (Long Animation Frames or
	           long tasks, with the scripts the browser blames)
	sample     once a second: phase, place, vitals, target, heap
	movement   commands, receipts, corrections and display hitches

Keys typed into a text field (chat, login, the report itself) are not
recorded at all: they are messages and passwords, not game input.

Uncaught errors are the one kind that outlives the window. The first
throw of each distinct failure is kept for the page's life with a repeat
count, so a report of a storm that began minutes earlier still carries
the stack that names it (#3425: a once-a-frame RangeError that started
before the window left no stack at all).

===========================================================================
*/

import { movementErrorCodes } from "@/engine/contracts/movement-diagnostic";

export const MOVEMENT_ERROR_CODES = movementErrorCodes();
export const JOURNAL_WINDOW_MS = 120000;
const MAX_EVENTS = 6000;
const MAX_SCRIPTS = 3;
const FRAME_GROUP_MS = 1000;
const MAX_ERROR_MESSAGE = 500;
const MAX_ERROR_STACK = 1600;
// The location in a V8 frame ("    at fn (url:1:2)", "    at url:1:2") or a
// SpiderMonkey/JavaScriptCore one ("fn@url:1:2"): the URL, line and column.
const STACK_FRAME_LOCATION = /^\s+at\s(?:.*\()?(\S+):(\d{1,7}):(\d{1,7})\)?$|^[^\s@]*@(\S+):(\d{1,7}):(\d{1,7})$/;
// A path the page loaded code from: a built chunk or a dev-server module.
const SCRIPT_PATH = /\.(?:[cm]?js|jsx|tsx?)$/;
// The page's script paths kept for frame checks; a page loads far fewer.
const MAX_SCRIPT_PATHS = 4096;
// The verified frames an error event keeps, the throw site first.
export const MAX_ERROR_FRAMES = 12;
// The frames V8 records per error (default 10). A stack overflow inside code
// the page did not load (an extension's, an eval's) fills ten frames with that
// code and cuts the page's own caller off; 64 keeps it.
const STACK_TRACE_LIMIT = 64;
// Where a frame that names no page script ran, the only part of it an
// incident carries (foreignFrames): no function name and no URL.
const EXTENSION_SCHEME = /^(?:chrome|moz|safari(?:-web)?|ms-browser)-extension:/;
// The distinct failures kept for the page's life, each its first throw plus
// a repeat count. Past this many, new failures fall back to the ring.
const MAX_KEPT_ERRORS = 8;
// An error storm in the ring must not evict the input history the report
// exists for: at most this many ring error events per rolling window, and
// never the same failure twice in a row.
const ERROR_RATE_LIMIT = 10;
const ERROR_RATE_WINDOW_MS = 60000;
// A failure that missed the kept slots can still prove itself a storm
// (#3684): it files once it has thrown this many times. Late filings are
// capped at MAX_KEPT_ERRORS a page, like the kept slots themselves, so a
// loop of ever-new failures cannot flood the Agent either.
const STORM_FILE_REPEATS = 4;
// Repeat counts of failures that missed the kept slots; a storm needs
// one entry, so the table stays small and drops its oldest when full.
const MAX_OVERFLOW_SIGNATURES = 256;

/*
================
JournalEvent

`atMs` is performance.now(), the clock the replay's timestamps use.
================
*/
export interface JournalEvent {
	readonly atMs: number;
	readonly kind: "key" | "click" | "ui" | "chat" | "error" | "long-frame" | "sample" | "movement" | "refusal";
	readonly [field: string]: unknown;
}

// UI_CONTROL_PREFIX is a control id's own scheme word ("slot", "shop-repair",
// "storage-slot"); UI_CONTROL_INDEX is a slot or row number. Neither can carry
// a player's text: a character name has no hyphen, and the prefix comes from
// the client's id scheme, never from data.
export const UI_CONTROL_PREFIX = /^[a-z][a-z0-9]*(-[a-z0-9]+)*$/;
const UI_CONTROL_INDEX = /^[0-9]{1,6}$/;

/*
================
uiControl

The part of a control id the shared report may carry: the prefix before its
first ':' (or the whole id when it is a hyphenated scheme word) and a purely
numeric suffix. "slot:13" -> { control: "slot", index: 13 }; an id that
could hold a name ("friend:Name") keeps only "friend".
================
*/
export function uiControl( id: string ): { control?: string; index?: number; } {
	const colon = id.indexOf( ":" ),
		prefix = colon < 0 ? id : id.slice( 0, colon ),
		suffix = colon < 0 ? "" : id.slice( colon + 1 );
	if ( !UI_CONTROL_PREFIX.test( prefix ) || colon < 0 && !prefix.includes( "-" ) ) return {};
	return UI_CONTROL_INDEX.test( suffix ) ? { control: prefix, index: Number( suffix ) } : { control: prefix };
}

// REFUSAL_REASONS name the refusals the game's command owners raise, by a
// fragment of their message; anything else is "other". The message itself
// never leaves the page.
export const REFUSAL_REASONS: readonly (readonly [string, string])[] = [
	[ "Inventory is busy", "inventory-busy" ],
	[ "Inventory command unavailable", "inventory-busy" ],
	[ "Inventory process unavailable", "inventory-busy" ],
	[ "Inventory transaction timed out", "inventory-timeout" ],
	[ "Merchant selection changed", "merchant-changed" ],
	[ "Select a merchant first", "merchant-unselected" ],
	[ "COS shop unavailable", "pet-shop-unavailable" ],
	[ "Unknown shop offer", "shop-offer-unknown" ],
	[ "Local player is not initialized", "player-not-ready" ],
	...MOVEMENT_ERROR_CODES.map( ( code ): readonly [string, string] => [
		code,
		code === "Movement rejected" ? "movement-rejected" : code
	] )
];
export const REFUSAL_OTHER = "other";

/*
================
refusalReason
================
*/
export function refusalReason( message: string ): string {
	return REFUSAL_REASONS.find( ( [fragment] ) => message.includes( fragment ) )?.[1] ?? REFUSAL_OTHER;
}

/*
================
errorEventFields

The journal fields for one uncaught failure: its message and, when the
thrown value carries one, its stack. The stack is the only part that can
name the failure (a RangeError's message alone never says where), so it
travels with the report; the page's origin is stripped first, the same
trim the long-frame script names get, and both fields are sliced. frames
are the stack's verified frames (stackFrames), the only part of the stack
a shared report carries.
================
*/
function errorEventFields(
	error: unknown,
	fallback: string,
	scripts: ReadonlySet<string>
): { message: string; stack?: string; frames?: string[]; foreign?: string[]; } {
	const thrown = error instanceof Error ? error : null,
		message = (thrown?.message || fallback || String( error )).slice( 0, MAX_ERROR_MESSAGE ),
		rawStack = thrown?.stack ?? (error as { stack?: unknown; } | null)?.stack,
		origin = typeof location !== "undefined" ? location.origin : "",
		stack = typeof rawStack === "string" && rawStack ?
			(origin ? rawStack.replaceAll( origin, "" ) : rawStack).slice( 0, MAX_ERROR_STACK ) :
			undefined;
	if ( !stack ) return { message };
	const frames = stackFrames( String( rawStack ), scripts );
	return frames.length ?
		{ message, stack, frames } :
		{ message, stack, foreign: foreignFrames( String( rawStack ), scripts ) };
}

/*
================
recorded

An error's journal fields without the incident-only foreign classes: the
journal and its shared copy keep the reviewed fields alone.
================
*/
function recorded( fields: ReturnType<typeof errorEventFields> ) {
	const { foreign: _foreign, ...kept } = fields;
	return kept;
}

/*
================
foreignFrames

The location class of every frame in rawStack that names no page script,
for an incident whose stack has none of the page's own (2026-11-25: seven
stack overflows, each filed with no frame at all, so nothing could say
whose code recursed). Each is "    at <class>", with consecutive repeats
folded into one "    at <class> x<count>"; a class is one of extension,
anonymous (eval and new Function code), blob, file, other-origin or
native (a frame with no location, such as Array.map's). A class is the
whole of what it keeps: never the function name, the URL or the line.
================
*/
function foreignFrames( rawStack: string, scripts: ReadonlySet<string> ): string[] {
	const classes: string[] = [];
	for ( const line of rawStack.split( "\n" ) ) {
		const match = STACK_FRAME_LOCATION.exec( line );
		let kind: string;
		if ( match ) {
			const url = match[1] ?? match[4] ?? "", path = pagePath( url );
			if ( path !== undefined && scripts.has( path ) ) continue;
			kind = EXTENSION_SCHEME.test( url ) ?
				"extension" :
				url.startsWith( "blob:" ) ?
				"blob" :
				url.startsWith( "file:" ) ?
				"file" :
				/^[a-z][a-z0-9+.-]*:\/\//i.test( url ) ?
				"other-origin" :
				"anonymous";
		} else if ( /^\s+at\s/.test( line ) ) kind = "native";
		else continue;
		classes.push( kind );
	}
	const folded: string[] = [];
	for ( let i = 0; i < classes.length && folded.length < MAX_ERROR_FRAMES; ) {
		let run = 1;
		while ( classes[i + run] === classes[i] ) run++;
		folded.push( run > 1 ? `    at <${classes[i]}> x${run}` : `    at <${classes[i]}>` );
		i += run;
	}
	return folded;
}

/*
================
stackFrames

The frames of rawStack that name one of the page's own scripts, each
rebuilt as "    at <path>:<line>:<column>". A stack's text is not the
engine's alone: its header holds the message, which code may have built
from what a player typed and may have changed after the throw, so no line
count or line shape tells a frame from a message line. A location the
page loaded code from does: a line is kept only when its URL has the
page's origin and a path in scripts, and only that path and its two
numbers are kept, never the function name or the rest of the line.
================
*/
function stackFrames( rawStack: string, scripts: ReadonlySet<string> ): string[] {
	const frames: string[] = [];
	for ( const line of rawStack.split( "\n" ) ) {
		const match = STACK_FRAME_LOCATION.exec( line );
		if ( !match ) continue;
		const path = pagePath( match[1] ?? match[4] ?? "" );
		if ( path === undefined || !scripts.has( path ) ) continue;
		frames.push( `    at ${path}:${match[2] ?? match[5]}:${match[3] ?? match[6]}` );
		if ( frames.length === MAX_ERROR_FRAMES ) break;
	}
	return frames;
}

/*
================
pagePath

The path of url when it is on the page's own origin; undefined for any
other origin, an opaque one (a file: page) or a string that is no URL.
================
*/
function pagePath( url: string ): string | undefined {
	if ( typeof location === "undefined" || !location.origin || location.origin === "null" ) return undefined;
	try {
		const parsed = new URL( url );
		return parsed.origin === location.origin ? parsed.pathname : undefined;
	} catch {
		return undefined;
	}
}

/*
================
pageScripts

The paths the page has loaded code from, for stackFrames: every
same-origin resource with a script's extension since the page began
(Resource Timing, replayed from its buffer and then observed, so a chunk
loaded after the buffer fills still counts) plus the page's script
elements. The observer stops with signal.
================
*/
function pageScripts( signal: AbortSignal ): () => ReadonlySet<string> {
	const paths = new Set<string>();
	function add( url: string ) {
		if ( paths.size >= MAX_SCRIPT_PATHS ) return;
		const path = pagePath( url );
		if ( path !== undefined && SCRIPT_PATH.test( path ) ) paths.add( path );
	}
	const supported = typeof PerformanceObserver === "function" ? PerformanceObserver.supportedEntryTypes : [];
	if ( supported.includes( "resource" ) ) {
		const observer = new PerformanceObserver( list => {
			for ( const entry of list.getEntries() ) add( entry.name );
		} );
		observer.observe( { type: "resource", buffered: true } );
		signal.addEventListener( "abort", () => observer.disconnect() );
	}
	return () => {
		if ( typeof document !== "undefined" ) {
			for ( const script of Array.from( document.scripts ) ) if ( script.src ) add( script.src );
		}
		return paths;
	};
}

/*
================
Journal
================
*/
export interface Journal {
	record( kind: JournalEvent["kind"], fields: Readonly<Record<string, unknown>> ): void;
	/** The events from `fromMs` on, oldest first. */
	since( fromMs: number ): JournalEvent[];
	dispose(): void;
}

/*
================
JournalOptions

scriptPaths names the paths the page loaded code from (pageScripts by
default); tests pass their own.
================
*/
export interface JournalOptions {
	readonly scriptPaths?: () => ReadonlySet<string>;
	/** The first throw of each distinct uncaught failure, redacted for an
	 * automatic incident (uncaughtIncident); at most MAX_KEPT_ERRORS per
	 * page, plus one late filing per proven storm (recordError). */
	readonly firstError?: ( failure: UncaughtFailure ) => void;
}

/*
================
UncaughtFailure

An uncaught error or rejection as an automatic incident may carry it.
A bug report is sent by the player, with the message and stack it
kept; an incident is filed with no one asking, so it carries no text a
player could have put into an error: the class name, the message only
when the engine itself wrote it (ENGINE_ERRORS), and only the stack's
own frames (stackFrames: script path, line and column).
================
*/
export interface UncaughtFailure {
	readonly message: string;
	readonly frames: readonly string[];
}

// The engine's own error classes, whose messages the engine writes
// ("Maximum call stack size exceeded", "Cannot read properties of
// undefined"). An Error our code throws may interpolate game data.
const ENGINE_ERRORS: ReadonlyMap<string, ErrorConstructor> = new Map( [
	[ "RangeError", RangeError ],
	[ "TypeError", TypeError ],
	[ "ReferenceError", ReferenceError ],
	[ "SyntaxError", SyntaxError ],
	[ "URIError", URIError ],
	[ "EvalError", EvalError ]
] );

/*
================
uncaughtFailure
================
*/
function uncaughtFailure(
	error: unknown,
	frames: readonly string[] | undefined,
	foreign: readonly string[] | undefined
): UncaughtFailure {
	// The name is text too (code may rename an error), so it is trusted
	// only when the object is exactly an engine class's instance.
	const name = error instanceof Error ? error.name : "";
	const engine = error instanceof Error && Object.getPrototypeOf( error ) === ENGINE_ERRORS.get( name )?.prototype;
	return {
		message: engine ?
			`${name}: ${(error as Error).message}`.slice( 0, MAX_ERROR_MESSAGE ) :
			error instanceof Error ?
			"Error" :
			"Rejection",
		// The page's own frames name the failure; with none, the classes of
		// the frames that ran say whose code it was (foreignFrames).
		frames: frames?.length ? frames : foreign ?? []
	};
}

/*
================
createJournal
================
*/
export function createJournal( canvas: HTMLCanvasElement, options: JournalOptions = {} ): Journal {
	const lifetime = new AbortController();
	const signal = lifetime.signal;
	const scriptPaths = options.scriptPaths ?? pageScripts( signal );
	// V8 only: record STACK_TRACE_LIMIT frames per error while the journal
	// listens, so an overflow's page caller survives below the recursion.
	const traces = Error as { stackTraceLimit?: number; };
	const previousTraceLimit = traces.stackTraceLimit;
	if ( typeof previousTraceLimit === "number" && previousTraceLimit < STACK_TRACE_LIMIT ) {
		traces.stackTraceLimit = STACK_TRACE_LIMIT;
	}
	let events: JournalEvent[] = [];
	let next = 0, count = 0, frameSlot = -1;
	let errorTimes: number[] = [], lastErrorSignature = "";
	// signature -> the failure's first throw, its count and its last time.
	let keptErrors = new Map<string, JournalEvent>();
	let overflowRepeats = new Map<string, number>(), overflowFiled = new Set<string>();

	/*
	================
	record
	================
	*/
	function record( kind: JournalEvent["kind"], fields: Readonly<Record<string, unknown>> ) {
		const atMs = performance.now();
		if ( kind === "movement" && fields.event === "frame-spike" ) {
			const previous = events[frameSlot];
			if ( previous?.kind === kind && previous.event === fields.event && atMs - previous.atMs < FRAME_GROUP_MS ) {
				// Keep the worst frame's pose and duration, plus the count and interval.
				// A 30 Hz display must not replace input/error history every frame.
				const worst = Number( fields.durationMs ) > Number( previous.durationMs ) ? fields : previous;
				events[frameSlot] = {
					...worst,
					atMs: previous.atMs,
					kind,
					count: Number( previous.count ) + 1,
					lastAtMs: atMs
				};
				return;
			}
			frameSlot = next;
			append( { ...fields, atMs, kind, count: 1, lastAtMs: atMs } );
			return;
		}
		append( { ...fields, atMs, kind } );
	}

	/*
	================
	append

	Both event producers share a fixed-capacity ring. Writes never scan/copy
	the history; since() filters by replay time when a report is requested.
	================
	*/
	function append( event: JournalEvent ) {
		events[next] = event;
		next = (next + 1) % MAX_EVENTS;
		count = Math.min( count + 1, MAX_EVENTS );
	}

	/*
	================
	recordError

	One uncaught failure for the timeline. A failure (message and top
	frame) is kept on its first throw, outside the ring and for the page's
	life; its repeats only raise that event's count and lastAtMs, the
	fields a grouped frame spike carries. Only once MAX_KEPT_ERRORS
	distinct failures are kept do new ones go to the ring, rate-limited:
	repeats in a row and storms past ERROR_RATE_LIMIT in the window are
	dropped, so a throwing loop cannot evict the player's input history.
	A ring failure is still counted, and one that reaches
	STORM_FILE_REPEATS files its incident late: a storm that begins in a
	session already carrying eight failures must not read as silence
	(#3684).
	================
	*/
	function recordError( error: unknown, fallback: string ) {
		const fields = errorEventFields( error, fallback, scriptPaths() ),
			signature = fields.message + "\n" + (fields.stack?.split( "\n" )[1] ?? ""),
			atMs = performance.now(),
			kept = keptErrors.get( signature );
		if ( kept ) {
			keptErrors.set( signature, { ...kept, count: Number( kept.count ) + 1, lastAtMs: atMs } );
			return;
		}
		if ( keptErrors.size < MAX_KEPT_ERRORS ) {
			keptErrors.set( signature, { ...recorded( fields ), atMs, kind: "error", count: 1, lastAtMs: atMs } );
			options.firstError?.( uncaughtFailure( error, fields.frames, fields.foreign ) );
			return;
		}
		// A storm that began after the kept slots filled still files its
		// incident, once it has proven itself by repetition (#3684).
		// Counted before the ring's own drops below, which discard
		// consecutive repeats by design and would starve the count.
		const repeats = (overflowRepeats.get( signature ) ?? 0) + 1;
		if ( !overflowRepeats.has( signature ) && overflowRepeats.size >= MAX_OVERFLOW_SIGNATURES ) {
			for ( const oldest of overflowRepeats.keys() ) {
				overflowRepeats.delete( oldest );
				break;
			}
		}
		overflowRepeats.set( signature, repeats );
		if (
			repeats >= STORM_FILE_REPEATS && overflowFiled.size < MAX_KEPT_ERRORS &&
			!overflowFiled.has( signature )
		) {
			overflowFiled.add( signature );
			options.firstError?.( uncaughtFailure( error, fields.frames, fields.foreign ) );
		}
		errorTimes = errorTimes.filter( time => atMs - time < ERROR_RATE_WINDOW_MS );
		if ( signature === lastErrorSignature || errorTimes.length >= ERROR_RATE_LIMIT ) return;
		lastErrorSignature = signature;
		errorTimes.push( atMs );
		record( "error", recorded( fields ) );
	}
	window.addEventListener( "error", event => recordError( event.error, event.message ), { signal } );
	window.addEventListener(
		"unhandledrejection",
		event => recordError( event.reason, "Unhandled rejection: " + String( event.reason ) ),
		{ signal }
	);

	/*
	================
	typing

	A text field has focus: its keys are words, not input.
	================
	*/
	const typing = () => {
		const active = document.activeElement;
		return active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement ||
			(active instanceof HTMLElement && active.isContentEditable);
	};
	for ( const kind of [ "keydown", "keyup" ] as const ) {
		window.addEventListener( kind, event => {
			if ( event.repeat || typing() ) return;
			record( "key", {
				code: event.code,
				down: kind === "keydown",
				...(event.shiftKey ? { shift: true } : {}),
				...(event.ctrlKey ? { ctrl: true } : {}),
				...(event.altKey ? { alt: true } : {})
			} );
		}, { signal, capture: true, passive: true } );
	}
	window.addEventListener( "pointerdown", event => {
		const target = event.target instanceof Element ? event.target : null;
		const control = target?.closest( "[data-ui-id]" )?.getAttribute( "data-ui-id" );
		if ( control ) {
			record( "ui", { id: control, ...uiControl( control ), button: event.button } );
			return;
		}
		if ( target !== canvas ) return;
		const rect = canvas.getBoundingClientRect();
		record( "click", {
			button: event.button,
			x: +((event.clientX - rect.left) / Math.max( 1, rect.width )).toFixed( 3 ),
			y: +((event.clientY - rect.top) / Math.max( 1, rect.height )).toFixed( 3 )
		} );
	}, { signal, capture: true, passive: true } );

	// Long Animation Frames name the scripts behind a hitch; long tasks are
	// the older, coarser signal for browsers without them.
	const supported = typeof PerformanceObserver === "function" ? PerformanceObserver.supportedEntryTypes : [];
	const type = supported.includes( "long-animation-frame" ) ?
		"long-animation-frame" :
		supported.includes( "longtask" ) ?
		"longtask" :
		null;
	const observer = type ?
		new PerformanceObserver( list => {
			for ( const entry of list.getEntries() ) {
				const frame = entry as PerformanceEntry & {
					blockingDuration?: number;
					scripts?: readonly {
						duration: number;
						sourceURL?: string;
						sourceFunctionName?: string;
						invoker?: string;
					}[];
				};
				append( {
					atMs: entry.startTime,
					kind: "long-frame",
					durationMs: Math.round( entry.duration ),
					...(frame.blockingDuration !== undefined ?
						{ blockingMs: Math.round( frame.blockingDuration ) } :
						{}),
					...(frame.scripts?.length ?
						{
							scripts: [ ...frame.scripts ].sort( ( a, b ) => b.duration - a.duration ).slice(
								0,
								MAX_SCRIPTS
							)
								.map( script => ({
									durationMs: Math.round( script.duration ),
									source: script.sourceURL?.replace( location.origin, "" ) ?? "",
									function: script.sourceFunctionName ?? "",
									invoker: script.invoker ?? ""
								}) )
						} :
						{})
				} );
			}
		} ) :
		null;
	observer?.observe( { type: type!, buffered: false } );

	return {
		record,
		/*
		================
		since

		The ring's events from fromMs on, plus every kept failure however
		old its first throw: a storm that began before the window still
		names its stack.
		================
		*/
		since( fromMs ) {
			const cutoff = Math.max( fromMs, performance.now() - JOURNAL_WINDOW_MS ), result: JournalEvent[] = [];
			for ( let i = 0; i < count; i++ ) {
				const event = events[(next - count + i + MAX_EVENTS) % MAX_EVENTS]!;
				if ( event.atMs >= cutoff ) result.push( event );
			}
			result.push( ...keptErrors.values() );
			// Long-frame observers report start time after later input has arrived.
			return result.sort( ( a, b ) => a.atMs - b.atMs );
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			lifetime.abort();
			if ( traces.stackTraceLimit === STACK_TRACE_LIMIT ) traces.stackTraceLimit = previousTraceLimit;
			observer?.disconnect();
			events = [];
			next = count = 0;
			frameSlot = -1;
			errorTimes = [];
			lastErrorSignature = "";
			keptErrors = new Map();
		}
	};
}

/*
================
timelineEvents

The journal's events on timeline.json's clock: atMs becomes t and a grouped
event's lastAtMs becomes lastT, both seconds from `zeroMs` (the replay's
first frame). Raw performance.now() milliseconds left beside t cannot be
placed on its axis without the page's clock origin, and the last throw of
an error storm is what ties it to what the player was doing (#3425).
================
*/
export function timelineEvents( events: readonly JournalEvent[], zeroMs: number ): Record<string, unknown>[] {
	return events.map( ( { atMs, lastAtMs, ...event } ) => {
		const out: Record<string, unknown> = { t: timelineSeconds( atMs, zeroMs ), ...event };
		if ( typeof lastAtMs === "number" ) out.lastT = timelineSeconds( lastAtMs, zeroMs );
		return out;
	} );
}

/*
================
timelineSeconds
================
*/
function timelineSeconds( atMs: number, zeroMs: number ): number {
	return +((atMs - zeroMs) / 1000).toFixed( 3 );
}
