/*
===========================================================================

bug-report.ts - the in-game bug reporter (issue #90)

Owns everything about reporting a bug from the game: the server's settings
(GET /title/bug-report), the player's Record/Stop recording (recorder.ts),
the report window and its controls (dialog.ts), recent client errors and
the upload (POST /title/bug-report).

The server decides whether reporting exists at all. Until it answers, and
whenever it says the feature is off, there is no button, no recording and
/bug declines. A settings read that fails (the Agent restarting during a
release, a network drop) is retried with backoff, and /bug asks again at
once: one failed read at page load used to switch reporting off for the
whole session.

Recording runs only between the player's Record and Stop (or the server's
replaySeconds cap); Stop opens the report window with the clip. An
always-on rolling replay used to run for every player and cost about
10 ms of main thread per second (measured 2026-10-07). The replayDefault
field older Agents still send is ignored.

===========================================================================
*/
import type { BugReportControl, BugReportField, ReplayState } from "@/engine/contracts/bug-report";
import { RELEASE_PROTOCOL, RELEASE_PROTOCOL_HEADER } from "@/engine/foundation/release/protocol";
import { muxMp4, type Mp4Track } from "@/engine/foundation/media/mp4";
import {
	replayLinkedReport,
	replayRecoveryLink,
	replayReportState,
	replayTrackBytes,
	reportIdIn
} from "@/engine/foundation/media/replay-window";
import { createReplayRecorder } from "./recorder";
import { createBugReportDialog, type OutgoingReport, type SendOutcome } from "./dialog";
import { createRecording } from "./recording";
import { createReportArchive, createDiagnosticUpload, diagnosticUploadBudget } from "./archive";
import { fitTrack } from "./transcode";
import { createJournal, JOURNAL_WINDOW_MS } from "./journal";

const ROUTE = "/title/bug-report";
const MAX_ERRORS = 50;
const MAX_ERROR_LENGTH = 500;
const MAX_CONTEXT_FIELDS = 16;
const MAX_FIELD_NAME = 40;
const MAX_FIELD_VALUE = 200;
const MEGABYTE = 1024 * 1024;
const WHISPER_CHANNEL = 2;
// Waits before asking for the settings again after a failed read.
const SETTINGS_RETRY_MS = [ 5000, 15000, 30000, 60000 ] as const;
const PENDING_OPEN_MS = 30000;
const SAMPLE_MS = 1000;
// Reading performance.memory makes Chrome total the heap: about 1 ms of a
// frame. A report needs the heap trend, so every tenth sample carries it.
const HEAP_SAMPLE_EVERY = 10;
// Per top-level field of the game state in state.json; larger ones are
// catalogs (skills, item mall, guide) the developer already has.
const MAX_STATE_FIELD_BYTES = 256 * 1024;
const MAX_RESOURCE_ENTRIES = 400;

/*
================
ServerSettings

The GET answer (bugreport.Settings on the server).
================
*/
export interface ServerSettings {
	readonly enabled: boolean;
	readonly maxBytes: number;
	readonly maxDiagnosticsBytes: number;
	readonly replaySeconds: number;
	readonly destinations: readonly string[];
}

/*
================
DiagnosticState
================
*/
export interface DiagnosticState {
	readonly session: import("@/engine/contracts/session").SessionState | null;
	readonly gameplay: import("@/engine/contracts/gameplay").GameplayState | null;
	readonly target: import("@/engine/contracts/world").EntityState | undefined;
	readonly entities: number;
}

/*
================
BugReportOptions
================
*/
export interface BugReportOptions {
	readonly canvas: HTMLCanvasElement;
	readonly apiBase: string;
	/** Game context at the moment the window opens (character, place…). */
	readonly context: () => readonly BugReportField[];
	/** The game's sound for the replay, or null until there is any. */
	readonly sound: () => MediaStream | null;
	/** The session and game state, for the timeline's samples and state.json. */
	readonly state: () => DiagnosticState;
}

/*
================
BugReportOwner
================
*/
export interface BugReportOwner extends BugReportControl {
	/** A runtime failure worth attaching to the next report. */
	note( message: string ): void;
	movement(
		event: import("@/engine/contracts/movement-diagnostic").MovementDiagnostic | Readonly<Record<string, unknown>>
	): void;
	movementClock( simulationOriginMs: number ): void;
	dumpMovement(): unknown;

	dispose(): void;
}

/*
================
createBugReport
================
*/
export function createBugReport( options: BugReportOptions ): BugReportOwner {
	const lifetime = new AbortController();
	const recorder = createReplayRecorder( options.canvas, options.sound );
	const archive = createReportArchive();
	const journal = createJournal( options.canvas );
	let movementOriginMs: number | undefined;
	/*
	================
	dumpMovement

	Keep clock domains explicit: journal atMs is main performance.now(); movement
	requests and receipts carry simulationAtMs, serverTimeMs is server wall time.
	================
	*/
	function dumpMovement() {
		return {
			version: 1,
			mainTimeOriginMs: performance.timeOrigin,
			simulationOriginMs: movementOriginMs,
			capturedAtMs: performance.now(),
			localGid: options.state().gameplay?.localGid,
			protocol: "predicted envelope 0x0009 / result 0x000A; IDs are not native 0x7738 fields",
			events: journal.since( performance.now() - JOURNAL_WINDOW_MS ).filter(
				event => event.kind === "movement" || event.kind === "long-frame"
			)
		};
	}
	let lastSampleMs = -Infinity, samples = 0;
	let settings: ServerSettings | null = null;
	// "on" and "off" are the server's answer; "unknown" until it gives one.
	let availability: "unknown" | "off" | "on" = "unknown";
	// A failed read is asked again from the frame (chat) once performance.now()
	// passes retryAtMs: timers belong to the clock owners.
	let settingsLoading = false, settingsRetry = 0, retryAtMs = Infinity;
	// The text of a /bug made while the settings were unknown: the window
	// opens with it when they arrive, so the player need not type it again.
	// It lapses after PENDING_OPEN_MS: a window popping up minutes later, in
	// the middle of a fight, would be worse than typing /bug again.
	let pendingOpen: string | null = null, pendingOpenAtMs = 0;
	const errors: string[] = [];
	// The newest chat sequence examined for a report request; lines at or
	// below it are history. null until the first frame primes it.
	let lastChatSequence: number | null = null;
	let disposed = false;

	const dialog = createBugReportDialog( {
		deliver,
		still: () => recorder.still(),
		saved: () => archive.list(),
		exportZip: id => archive.exportZip( id ),
		forget: id => archive.remove( id ),
		launch: () => void open( "" ),
		record: () => recording.record(),
		stopRecording: () => recording.stop( "" )
	} );
	const recording = createRecording( {
		recorder,
		now: () => performance.now(),
		maxSeconds: () => settings?.enabled ? settings.replaySeconds : 0,
		show: ( phase, seconds, maxSeconds ) => dialog.showRecording( phase, seconds, maxSeconds ),
		failed: reason => {
			note( reason );
			dialog.notice( recorder.unsupported() ? "This browser cannot record the game." : reason );
		},
		finished: ( track, text ) => {
			if ( !track ) note( recorder.lastError() ?? "The recording was empty" );
			open( text );
		}
	} );

	/*
	================
	note
	================
	*/
	function note( message: string ) {
		const line = `${new Date().toISOString().slice( 11, 19 )} ${message}`.slice( 0, MAX_ERROR_LENGTH );
		if ( errors[errors.length - 1] === line ) return;
		errors.push( line );
		journal.record( "error", { message: message.slice( 0, MAX_ERROR_LENGTH ) } );
		if ( errors.length > MAX_ERRORS ) errors.splice( 0, errors.length - MAX_ERRORS );
	}
	window.addEventListener( "error", event => note( event.message || String( event.error ) ), {
		signal: lifetime.signal
	} );
	window.addEventListener( "unhandledrejection", event => note( "Unhandled rejection: " + String( event.reason ) ), {
		signal: lifetime.signal
	} );

	/*
	================
	replayState
	================
	*/
	function replayState(): ReplayState {
		return recorder.unsupported() ? "unsupported" : "idle";
	}

	/*
	================
	loadSettings
	================
	*/
	function loadSettings() {
		if ( settingsLoading || availability !== "unknown" || disposed ) return;
		settingsLoading = true;
		retryAtMs = Infinity;
		// A promise chain, not an async function: /bug and the frame start it.
		fetch( options.apiBase + ROUTE, {
			headers: { [RELEASE_PROTOCOL_HEADER]: String( RELEASE_PROTOCOL ) },
			credentials: "omit",
			cache: "no-store",
			redirect: "error",
			signal: lifetime.signal
		} ).then( response =>
			response.json().then( ( body: { bugReports?: Partial<ServerSettings>; } ) => {
				if ( !response.ok || !body.bugReports ) throw Error( `HTTP ${response.status}` );
				adoptSettings( body.bugReports );
			} )
		).catch( failure => {
			// No reporter until the Agent can say, then ask again.
			if ( disposed ) return;
			note( "Bug report settings unavailable: " + String( failure ) );
			const wait = SETTINGS_RETRY_MS[Math.min( settingsRetry++, SETTINGS_RETRY_MS.length - 1 )]!;
			retryAtMs = performance.now() + wait;
		} ).finally( () => {
			settingsLoading = false;
		} );
	}
	/*
	================
	adoptSettings

	The server's answer: off for good, or on with its limits.
	================
	*/
	function adoptSettings( value: Partial<ServerSettings> ) {
		if ( disposed ) return;
		if ( !value.enabled ) {
			availability = "off";
			pendingOpen = null;
			return;
		}
		settings = readSettings( value );
		availability = "on";
		dialog.showLauncher( true );
		if ( pendingOpen !== null ) {
			const text = pendingOpen;
			pendingOpen = null;
			if ( performance.now() - pendingOpenAtMs < PENDING_OPEN_MS ) open( text );
		}
	}

	/*
	================
	open
	================
	*/
	function open( text: string ): "opened" | "off" | "unavailable" {
		if ( availability === "off" ) return "off";
		if ( !settings?.enabled ) {
			// Ask now rather than at the next backoff step, and open when the
			// answer comes (adoptSettings).
			pendingOpen = text;
			pendingOpenAtMs = performance.now();
			loadSettings();
			return "unavailable";
		}
		if ( dialog.isOpen() ) return "opened";
		// /bug during a recording ends it: the report is what it was for.
		const phase = recording.phase();
		if ( phase === "recording" || phase === "finishing" ) {
			recording.stop( text );
			return "opened";
		}
		dialog.open( {
			text,
			replay: recording.recorded(),
			maxBytes: settings.maxBytes,
			destinations: settings.destinations,
			replayState: replayState(),
			replayError: recorder.lastError()
		} );
		return "opened";
	}

	/*
	================
	reportContext
	================
	*/
	function reportContext( id: string, report: OutgoingReport, sent: Mp4Track | null ): BugReportField[] {
		const seconds = ( track: Mp4Track ) =>
			(track.samples[track.samples.length - 1]!.timestampUs - track.samples[0]!.timestampUs) / 1e6;
		const fields: BugReportField[] = [ { name: "Report ID", value: id }, ...options.context() ];
		fields.push(
			{ name: "Page", value: location.host + location.pathname },
			{ name: "Browser", value: navigator.userAgent },
			{
				name: "Screen",
				value: `${options.canvas.width}x${options.canvas.height} @${devicePixelRatio.toFixed( 2 )}x`
			}
		);
		if ( report.clip && sent ) {
			const clipSeconds = seconds( report.clip ), raw = replayTrackBytes( report.clip );
			fields.push( {
				name: "Replay",
				value: `${clipSeconds.toFixed( 1 )} s, ` +
					`${(report.clip.samples.length / Math.max( clipSeconds, 1e-3 )).toFixed( 0 )} fps, ` +
					`${report.clip.audio ? "with" : "no"} sound, ${recorder.dropped()} frames dropped` +
					(sent === report.clip ?
						"" :
						`; compressed ${(raw / MEGABYTE).toFixed( 1 )} → ${
							(replayTrackBytes( sent ) / MEGABYTE).toFixed( 1 )
						} MB`)
			} );
		} else {fields.push( {
				name: "Replay",
				value: replayReportState( report.replay !== null, recorder.lastError() )
			} );}
		if ( report.replay ) {
			fields.push( {
				name: "Full quality",
				value: `${seconds( report.replay ).toFixed( 0 )} s replay on the player's device. ` +
					`To get it, whisper the player ${id} in game ` +
					`(or send ${replayRecoveryLink( location.origin, location.pathname, id )})`
			} );
		}
		return fields.slice( 0, MAX_CONTEXT_FIELDS ).map( field => ({
			name: field.name.slice( 0, MAX_FIELD_NAME ),
			value: field.value.slice( 0, MAX_FIELD_VALUE )
		}) );
	}

	/*
	================
	deliver

	Compresses the clip when it is over the cap, posts the report, and keeps
	the full-quality replay on the device whether or not the post succeeded.
	================
	*/
	async function deliver( report: OutgoingReport, progress: ( text: string ) => void ): Promise<SendOutcome> {
		const id = reportId();
		const maxBytes = settings?.maxBytes ?? 0;
		let sent: Mp4Track | null = null, outcome: SendOutcome;
		let captured: Record<string, string> = {};
		try {
			captured = diagnostics( report );
			progress( "Preparing technical diagnostics…" );
			const diagnosticBudget = diagnosticUploadBudget( maxBytes, settings?.maxDiagnosticsBytes );
			const diagnosticZip = diagnosticBudget ?
				await createDiagnosticUpload( captured, { id, clip: report.range, maxBytes: diagnosticBudget } ) :
				null;
			const mediaBudget = maxBytes - (diagnosticZip?.size ?? 0);
			let media: Blob | null = report.screenshot;
			if ( report.clip ) {
				const original = muxMp4( report.clip );
				const containerBytes = original.byteLength - replayTrackBytes( report.clip );
				sent = await fitTrack( report.clip, mediaBudget - containerBytes, fraction => {
					progress( `Compressing the clip to fit the upload limit… ${Math.round( fraction * 100 )}%` );
				} );
				media = new Blob( [ (sent === report.clip ? original : muxMp4( sent )) as BlobPart ], {
					type: "video/mp4"
				} );
			}
			if ( media && media.size > mediaBudget ) {
				throw Error( "The clip and diagnostics are too large; choose a shorter clip" );
			}
			progress( "Sending the report…" );
			const form = new FormData();
			form.append( "description", report.description );
			form.append( "meta", JSON.stringify( { context: reportContext( id, report, sent ), errors } ) );
			if ( diagnosticZip ) form.append( "diagnostics", diagnosticZip, "diagnostics.zip" );
			if ( media ) {
				form.append( sent ? "clip" : "screenshot", media, sent ? "replay.mp4" : "screenshot.jpg" );
			}
			const response = await fetch( options.apiBase + ROUTE, {
				method: "POST",
				headers: { [RELEASE_PROTOCOL_HEADER]: String( RELEASE_PROTOCOL ) },
				credentials: "include",
				cache: "no-store",
				redirect: "error",
				body: form,
				signal: lifetime.signal
			} );
			const body = await response.json().catch( () => ({}) ) as { code?: string; retryAfter?: number; };
			// A sent recording is not offered again with the next report.
			if ( response.ok ) recording.sent( report.replay );
			outcome = response.ok ?
				{ ok: true, message: `Report ${id} sent. Thank you!` } :
				{ ok: false, message: refusalMessage( body.code, body.retryAfter ) };
		} catch ( failure ) {
			outcome = { ok: false, message: "The report could not be sent: " + String( failure ) };
		}
		try {
			await archive.save( {
				id,
				createdAt: new Date().toISOString(),
				description: report.description,
				context: reportContext( id, report, sent ),
				errors: [ ...errors ],
				clip: report.range,
				delivered: outcome.ok,
				replay: report.replay ?
					new Blob( [ muxMp4( report.replay ) as BlobPart ], { type: "video/mp4" } ) :
					null,
				screenshot: report.screenshot,
				diagnostics: captured
			} );
			if ( report.replay ) {
				outcome = {
					...outcome,
					message: outcome.message + " The full-quality replay is saved on this device (list below)."
				};
			}
		} catch ( failure ) {
			note( "Saving the report on this device failed: " + String( failure ) );
		}
		return outcome;
	}

	/*
	================
	sample

	One timeline row: where the player is and how they are doing.
	================
	*/
	function sample(): Record<string, unknown> {
		const { session, gameplay, target, entities } = options.state();
		const pose = gameplay?.pose;
		const vitals = gameplay?.vitals.find( row => row.gid === gameplay.localGid );
		const memory = samples++ % HEAP_SAMPLE_EVERY === 0 ?
			(performance as Performance & { memory?: { usedJSHeapSize: number; }; }).memory :
			undefined;
		return {
			phase: session?.phase ?? "starting",
			...(pose ?
				{
					region: "0x" + pose.regionId.toString( 16 ).toUpperCase(),
					x: +pose.x.toFixed( 1 ),
					y: +pose.y.toFixed( 1 ),
					z: +pose.z.toFixed( 1 )
				} :
				{}),
			...(gameplay ?
				{
					moving: gameplay.moving ?? false,
					pendingMoves: gameplay.pendingMoves,
					casts: gameplay.casts.length,
					target: gameplay.target || null,
					...(gameplay.error ? { gameplayError: gameplay.error } : {})
				} :
				{}),
			...(vitals ?
				{
					hp: vitals.hp,
					maxHp: vitals.maxHp,
					mp: vitals.mp,
					maxMp: vitals.maxMp,
					...(vitals.abnormal ? { abnormal: vitals.abnormal } : {}),
					...(vitals.deathState ? { dead: true } : {})
				} :
				{}),
			...(target ? { targetName: target.name, targetKind: target.kind } : {}),
			entities,
			...(memory ? { heapMB: Math.round( memory.usedJSHeapSize / 1048576 ) } : {})
		};
	}

	/*
	================
	diagnostics

	The .zip's JSON files, frozen when the report is sent. Times in the
	timeline are seconds on the replay's own clock (0 = its first frame).
	================
	*/
	function diagnostics( report: OutgoingReport ): Record<string, string> {
		const zeroMs = report.replay ? report.replay.samples[0]!.timestampUs / 1000 : performance.now() - 60000;
		// The whole journal window, not just the recording: the player presses
		// Record after noticing a bug, so what led up to it comes before t = 0.
		const fromMs = Math.min( zeroMs - 2000, performance.now() - JOURNAL_WINDOW_MS );
		const timeline = {
			note: "t is seconds from the first frame of replay.mp4 (negative: before the recording); " +
				"clip is the part sent with the report.",
			clip: report.range,
			events: journal.since( fromMs ).map( ( { atMs, ...event } ) => ({
				t: +((atMs - zeroMs) / 1000).toFixed( 3 ),
				...event
			}) )
		};
		const current = options.state();
		const gameplay: Record<string, unknown> = {};
		for ( const [key, value] of Object.entries( current.gameplay ?? {} ) ) {
			const text = json( value );
			gameplay[key] = text.length > MAX_STATE_FIELD_BYTES ?
				`[omitted: ${(text.length / 1024).toFixed( 0 )} KB]` :
				value;
		}
		const state = {
			session: current.session,
			target: current.target ?? null,
			entities: current.entities,
			gameplay
		};
		return {
			"timeline.json": json( timeline ),
			"movement.json": json( dumpMovement() ),
			"state.json": json( state ),
			"environment.json": json( environment() )
		};
	}

	/*
	================
	environment
	================
	*/
	function environment() {
		const connection = (navigator as Navigator & {
			connection?: { effectiveType?: string; rtt?: number; downlink?: number; };
		}).connection;
		const preferences: Record<string, unknown> = {};
		for ( let index = 0; index < localStorage.length; index++ ) {
			const key = localStorage.key( index );
			if ( !key?.startsWith( "sro:" ) ) continue;
			const raw = localStorage.getItem( key ) ?? "";
			try {
				preferences[key] = JSON.parse( raw );
			} catch {
				preferences[key] = raw;
			}
		}
		const since = performance.now() - 70000;
		const resources = performance.getEntriesByType( "resource" ).filter( entry => entry.startTime >= since )
			.slice( -MAX_RESOURCE_ENTRIES ).map( entry => {
				const resource = entry as PerformanceResourceTiming;
				return {
					url: resource.name.replace( location.origin, "" ).replace( /\?.*$/, "" ),
					type: resource.initiatorType,
					startMs: Math.round( resource.startTime ),
					durationMs: Math.round( resource.duration ),
					bytes: resource.transferSize,
					...(resource.responseStatus ? { status: resource.responseStatus } : {})
				};
			} );
		return {
			page: location.origin + location.pathname,
			userAgent: navigator.userAgent,
			languages: navigator.languages,
			timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
			cpuThreads: navigator.hardwareConcurrency,
			deviceMemoryGB: (navigator as Navigator & { deviceMemory?: number; }).deviceMemory ?? null,
			screen: { width: screen.width, height: screen.height, devicePixelRatio },
			window: { width: innerWidth, height: innerHeight },
			canvas: { width: options.canvas.width, height: options.canvas.height },
			network: connection ?
				{ type: connection.effectiveType, rttMs: connection.rtt, downlinkMbps: connection.downlink } :
				null,
			replay: { phase: recording.phase(), droppedFrames: recorder.dropped(), lastError: recorder.lastError() },
			preferences,
			resources
		};
	}

	/*
	================
	chat

	Called with the whole chat whenever the UI assembles the HUD, which is
	not every frame (the recording ticks from recordingFrame). Lines
	present at the first call are history and never trigger a request;
	after that each new incoming
	whisper is examined once, by its sequence (chat.ts numbers every line).
	A sequence that went backwards is a new session's chat: its lines start
	as history again. Nothing is built for lines already examined, where a
	key string per line per frame used to be. Being the reporter's frame, it
	also retries a failed settings read once its backoff has passed.
	================
	*/
	function chat( lines: readonly import("@/engine/contracts/gameplay").ChatLine[] ) {
		const newest = lines.length ? lines[lines.length - 1]!.sequence ?? 0 : 0;
		// Unprimed, or a sequence that went backwards (a new session's chat):
		// every line present now is history.
		const after = lastChatSequence !== null && newest >= lastChatSequence ? lastChatSequence : newest;
		lastChatSequence = newest;
		for ( const line of lines ) {
			if ( (line.sequence ?? 0) <= after ) continue;
			journal.record( "chat", {
				channel: line.channel,
				name: line.name,
				text: line.text,
				outgoing: line.outgoing
			} );
			if ( line.outgoing || line.channel !== WHISPER_CHANNEL ) continue;
			const id = reportIdIn( line.text );
			if ( id ) offer( id, line.name, false );
		}
		const now = performance.now();
		if ( now >= retryAtMs ) loadSettings();
		if ( now - lastSampleMs >= SAMPLE_MS ) {
			lastSampleMs = now;
			journal.record( "sample", sample() );
		}
	}

	/*
	================
	offer

	Offers the saved report's .zip. From a whisper, a report this device does
	not have is ignored (it was someone else's); from a link, the player is
	told it is not here.
	================
	*/
	function offer( id: string, from: string, explicit: boolean ) {
		archive.list().then( rows => {
			const row = rows.find( candidate => candidate.id === id );
			if ( row ) dialog.offer( row, from );
			else if ( explicit ) dialog.notice( `Report ${id} is not saved in this browser.` );
		}, failure => note( "Reading saved reports failed: " + String( failure ) ) );
	}

	/*
	================
	offerLinked

	A recovery link (#bug=<id>) offers its report. It is read at load and on
	every hash change: a player who already has the game open and pastes the
	link into the same tab gets a same-document navigation, not a reload.
	================
	*/
	function offerLinked() {
		const linked = replayLinkedReport( location.hash );
		if ( linked ) offer( linked, "", true );
	}

	loadSettings();
	offerLinked();
	addEventListener( "hashchange", offerLinked, { signal: lifetime.signal } );

	return {
		/*
		================
		movement
		================
		*/
		movement: event => journal.record( "movement", { ...event } ),
		/*
		================
		movementClock
		================
		*/
		movementClock( simulationOriginMs: number ) {
			if ( simulationOriginMs === movementOriginMs ) return;
			movementOriginMs = simulationOriginMs;
			journal.record( "movement", { event: "clock", simulationOriginMs } );
		},
		dumpMovement,
		reportsEnabled: () => availability === "on",
		recordingFrame: () => recording.frame(),
		chat,
		open,
		note,
		/*
		================
		dispose
		================
		*/
		dispose() {
			disposed = true;
			lifetime.abort();
			journal.dispose();
			recording.dispose();
			dialog.dispose();
		}
	};
}

/*
================
readSettings

An enabled GET answer with defaults for what an older Agent leaves out.
Older Agents also send replayDefault (the retired always-on replay); it is
not read: recording starts only at the player's Record.
================
*/
export function readSettings( value: Partial<ServerSettings> ): ServerSettings {
	const maxBytes = Number( value.maxBytes ) || 10 * 1024 * 1024;
	return {
		enabled: true,
		maxBytes,
		maxDiagnosticsBytes: diagnosticUploadBudget( maxBytes, value.maxDiagnosticsBytes ),
		replaySeconds: Number( value.replaySeconds ) || 60,
		destinations: Array.isArray( value.destinations ) ?
			value.destinations.filter( destination => destination === "discord" || destination === "directory" ) :
			[]
	};
}

/*
================
refusalMessage

The Agent's refusal codes, in words a player can act on.
================
*/
function refusalMessage( code: string | undefined, retryAfter: number | undefined ) {
	switch ( code ) {
		case "RATE_LIMITED":
			return `You can send another report in ${waitText( retryAfter )}.`;
		case "BUSY":
			return `Reports are busy right now; try again in ${waitText( retryAfter )}.`;
		case "REPORT_TOO_LARGE":
			return "The clip is too large. Choose a shorter part of the replay.";
		case "INVALID_REPORT":
			return "The report was not accepted. Check the description and try again.";
		case "UNAUTHORIZED":
			return "Log in to send a report.";
		case "BUG_REPORTS_DISABLED":
			return "Bug reports are disabled on this server.";
		default:
			return "The report could not be delivered. Please try again later.";
	}
}

/*
================
waitText
================
*/
function waitText( seconds: number | undefined ) {
	const value = Math.max( 1, Math.ceil( seconds ?? 60 ) );
	if ( value < 120 ) return `${value} seconds`;
	if ( value < 7200 ) return `${Math.ceil( value / 60 )} minutes`;
	return `${Math.ceil( value / 3600 )} hours`;
}

/*
================
reportId

Short and readable over chat: BR-YYMMDD-HHMM-XXXX (UTC, random suffix).
================
*/
function reportId() {
	const stamp = new Date().toISOString().replace( /[-:T]/g, "" ).slice( 2, 12 );
	const suffix = Array.from(
		crypto.getRandomValues( new Uint8Array( 2 ) ),
		byte => byte.toString( 16 ).padStart( 2, "0" )
	)
		.join( "" ).toUpperCase();
	return `BR-${stamp.slice( 0, 6 )}-${stamp.slice( 6 )}-${suffix}`;
}

/*
================
json

JSON for the .zip: Maps and Sets as plain data, binary as its size.
================
*/
function json( value: unknown ): string {
	return JSON.stringify( value, ( _key, item: unknown ) => {
		if ( item instanceof Map ) return Object.fromEntries( item );
		if ( item instanceof Set ) return [ ...item ];
		if ( ArrayBuffer.isView( item ) ) return `[${item.constructor.name}: ${item.byteLength} bytes]`;
		if ( typeof item === "bigint" ) return item.toString();
		return item;
	}, "\t" ) ?? "null";
}
