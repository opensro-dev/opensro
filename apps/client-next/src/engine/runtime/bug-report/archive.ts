/*
===========================================================================

archive.ts - the reports kept on the player's device

Every report sent keeps its full-quality replay (the whole minute, before
any compression for Discord) in this browser's IndexedDB, under the report
ID that Discord shows. When the team needs more than the compressed clip,
the player exports that report as a .zip (zip.ts) from the report window.

The archive is bounded: at most MAX_REPORTS reports and MAX_BYTES of
media; saving a new report drops the oldest ones first.

===========================================================================
*/
import type { BugReportField } from "@/engine/contracts/bug-report";
import { zipStore, type ZipFile } from "@/engine/foundation/archive/zip";
import { MOVEMENT_ERROR_CODES, REFUSAL_OTHER, REFUSAL_REASONS, UI_CONTROL_PREFIX } from "./journal";
import {
	GPU_TOKEN_PATTERN,
	reportedDegradedPipelines,
	reportedGpuFeatures
} from "@/engine/foundation/rendering/gpu-facts";

const DATABASE = "sro-bug-reports";
const STORE = "reports";
const VERSION = 1;
const MAX_REPORTS = 10;
const MAX_BYTES = 600 * 1024 * 1024;
const MAX_DIAGNOSTIC_BYTES = 2 * 1024 * 1024;
// Agent bugreport.maxDiagnosticsUnpacked is part of the upload contract.
const MAX_DIAGNOSTIC_UNPACKED_BYTES = 8 * 1024 * 1024;
// assets.json's shared strings: enumerated words (phases, states, decode
// kinds) and request paths, nothing free-form.
const SHARED_ASSET_WORD = /^[a-z][a-z0-9-]{0,31}$/;
const SHARED_ASSET_PATH = /^\/[A-Za-z0-9_\-./%]{1,240}$/;
const MIN_DIAGNOSTIC_BYTES = 4096;
const MAX_USER_AGENT_LENGTH = 512;
const SHARED_GAMEPLAY_FIELDS = [
	"revision",
	"localGid",
	"pose",
	"authoritativePose",
	"moving",
	"poseAtMs",
	"movementPath",
	"movementRevision",
	"movementTransition",
	"movementDiagnostics",
	"pendingMoves",
	"acknowledgedMove",
	"target",
	"targetPending",
	"casts",
	"castPrediction",
	"vitals",
	"inventory",
	"inventoryPending",
	"inventoryJournal",
	"avatarInventory",
	"equipmentSlotCount",
	"inventorySlotCount",
	"itemCooldowns",
	"skillCooldowns",
	"skillQueue",
	"skillDenied",
	"skills",
	"navigationFloor",
	"navigationRequestId",
	"navigationRegion",
	"navigationBlock",
	"activeCos",
	"storage"
] as const;
const SHARED_ENVIRONMENT_FIELDS = [
	"userAgent",
	"cpuThreads",
	"deviceMemoryGB",
	"screen",
	"window",
	"canvas",
	"network",
	"replay",
	"gpu"
] as const;
const SHARED_MOVEMENT_FIELDS = [
	"version",
	"mainTimeOriginMs",
	"simulationOriginMs",
	"capturedAtMs",
	"localGid",
	"events"
] as const;
const TECHNICAL_FIELDS = new Set( [
	...SHARED_GAMEPLAY_FIELDS,
	...SHARED_MOVEMENT_FIELDS,
	...SHARED_ENVIRONMENT_FIELDS,
	"events",
	"kind",
	"features",
	"limits",
	"maxBufferSize",
	"maxStorageBufferBindingSize",
	"maxTextureDimension2D",
	"vendor",
	"architecture",
	"degradedPipelines",
	"control",
	"index",
	"refusal",
	"iconless",
	"capacity",
	"items",
	"event",
	"atMs",
	"t",
	"clip",
	"start",
	"end",
	"simulationAtMs",
	"serverTimeMs",
	"requestId",
	"simulationRoundTripMs",
	"accepted",
	"stale",
	"serverGoal",
	"serverFrom",
	"serverDepartAtMs",
	"serverArriveAtMs",
	"before",
	"after",
	"heading",
	"distance",
	"source",
	"reason",
	"phase",
	"region",
	"regionId",
	"x",
	"y",
	"z",
	"angle",
	"gid",
	"refObjId",
	"token",
	"caster",
	"skill",
	"count",
	"lastAtMs",
	"elapsedMs",
	"cpuMs",
	"durationMs",
	"total",
	"recent",
	"hp",
	"maxHp",
	"mp",
	"maxMp",
	"dead",
	"entities",
	"heapMB",
	"abnormal",
	"deathState",
	"targetKind",
	"key",
	"down",
	"repeat",
	"button",
	"buttons",
	"width",
	"height",
	"devicePixelRatio",
	"type",
	"rttMs",
	"downlinkMbps",
	"enabled",
	"droppedFrames",
	"relocation",
	"logicalDistance",
	"eligible",
	"pathEligible",
	"corridor",
	"from",
	"to",
	"previousPath",
	"turn",
	"incoming",
	"outgoing",
	"departAtMs",
	"arriveAtMs",
	"fixedTiming",
	"speed",
	"mode",
	"moving",
	"revision",
	"pose",
	"authoritativePose",
	"displayed",
	"camera",
	"deltaMs",
	"intervalMs",
	"remaining",
	"pending",
	"tail",
	"slot",
	"itemId",
	"quantity",
	"refItemId",
	"durability",
	"plus",
	"optLevel",
	"variance",
	"magicOptions",
	"id",
	"value",
	"level",
	"status",
	"cooldownMs",
	"endAtMs",
	"startAtMs",
	"shotAtMs",
	"stage",
	"results",
	"damage",
	"critical",
	"blocked",
	"dodge",
	"hits",
	"position",
	"duration",
	"startTime",
	"blockingDuration",
	"renderStart",
	"styleAndLayoutStart",
	"firstUIEventTimestamp",
	"scripts",
	"executionStart",
	"forcedStyleAndLayoutDuration",
	"pauseDuration",
	"sourceCharPosition",
	"windowAttribution",
	"frameAtMs",
	"workerAtMs",
	"workerDebtMs",
	"logical",
	"bodyYaw",
	"visibility",
	"transition",
	"acknowledged",
	"cos",
	"error",
	"stack",
	"gameplayError",
	"worldSequence",
	"walkSpeed",
	"runSpeed",
	"animationRate"
] );
const TECHNICAL_STRINGS: Readonly<Record<string, readonly string[]>> = {
	kind: [
		"movement",
		"movement-diagnostic",
		"key",
		"click",
		"ui",
		"error",
		"long-frame",
		"sample",
		"refusal",
		"local-player",
		"player",
		"monster",
		"npc"
	],
	event: [ "request", "receipt", "correction", "clock", "frame-spike", "world-reset" ],
	error: MOVEMENT_ERROR_CODES,
	gameplayError: MOVEMENT_ERROR_CODES,
	visibility: [ "visible", "hidden" ],
	reason: [ "spawn", "input", "correction", "receipt", "native", "cast", "displacement", "death", "clear" ],
	source: [ "native", "receipt", "server", "prediction", "cast", "pickup", "native-stop", "native-source" ],
	phase: [ "starting", "title", "connecting", "login", "character", "loading-world", "world", "disconnected" ],
	targetKind: [ "player", "monster", "npc", "item", "pet" ],
	type: [ "slow-2g", "2g", "3g", "4g" ],
	key: [
		"KeyW",
		"KeyA",
		"KeyS",
		"KeyD",
		"ArrowUp",
		"ArrowDown",
		"ArrowLeft",
		"ArrowRight",
		"Space",
		"Escape",
		"Digit0",
		"Digit1",
		"Digit2",
		"Digit3",
		"Digit4",
		"Digit5",
		"Digit6",
		"Digit7",
		"Digit8",
		"Digit9",
		"F1",
		"F2",
		"F3",
		"F4",
		"F5",
		"F6",
		"F7",
		"F8",
		"F9",
		"F10",
		"F11",
		"F12"
	],
	// The refusal reasons journal.ts refusalReason names.
	refusal: [ ...new Set( [ ...REFUSAL_REASONS.map( ( [, reason] ) => reason ), REFUSAL_OTHER ] ) ],
	// The adapter features and degraded compute pipelines gpu-facts.ts names.
	features: reportedGpuFeatures(),
	degradedPipelines: reportedDegradedPipelines()
};
// Strings a field may carry by shape rather than by list: a UI control's
// scheme prefix (journal.ts uiControl), never free text.
const TECHNICAL_PATTERNS: Readonly<Record<string, RegExp>> = {
	control: UI_CONTROL_PREFIX,
	vendor: new RegExp( GPU_TOKEN_PATTERN ),
	architecture: new RegExp( GPU_TOKEN_PATTERN ),
	// A journal stack is recorded with the page's origin already stripped
	// (journal.ts errorEventFields); anything that still carries a URL
	// scheme did not come from that recorder and stays behind.
	stack: /^(?![\s\S]*:\/\/)[\s\S]+$/
};
const README = `OpenSRO bug report

report.json       what the player wrote, the context sent to Discord, recent errors,
                  and "clip": the part of replay.mp4 that was sent (seconds).
replay.mp4        the last minute before the report, full quality, with sound.
timeline.json     what happened during that minute, on replay.mp4's clock (t, seconds):
                  key        keys pressed/released (never text typed into a field)
                  click      presses on the game world (x, y from 0 to 1 across the canvas)
                  ui         presses on game window controls (their ui id; the shared copy
                             keeps only the control's kind and slot number)
                  refusal    a command the game refused, by reason (inventory-busy, ...)
                  chat       chat lines seen or sent
                  error      JavaScript and runtime failures (message and stack)
                  long-frame main-thread hitches, with the scripts the browser blamed
                  sample     once a second: phase, region and position, HP/MP, target, heap
network.json      every transport frame of that window, on the timeline's clock:
                  [t, "in" | "out", opcode, bytes] (no payloads); a "gap" row
                  counts frames the journal dropped
movement.json     up to two minutes of movement commands, server receipts, corrections
                  and frame hitches; clock domains are explicit in the header/events.
state.json        the game's state when the report was sent (large catalogs omitted).
environment.json  browser, screen, CPU, memory, network, the WebGPU adapter's features,
                  limits, vendor and architecture, the player's options, and every
                  resource loaded during the last minute (time, size, status).
assets.json       the asset queue every 5 s for the last six minutes (#1975): free
                  request slots and each open load's path, state and age, with the
                  session phase; it says why a loading screen stalled.
screenshot.jpg    only when no replay was attached.
`;

/*
================
ArchivedReport
================
*/
export interface ArchivedReport {
	readonly id: string;
	readonly createdAt: string;
	readonly description: string;
	readonly context: readonly BugReportField[];
	readonly errors: readonly string[];
	/** The attached part, in seconds from the start of `replay`. */
	readonly clip: { readonly start: number; readonly end: number; } | null;
	readonly delivered: boolean;
	readonly replay: Blob | null;
	readonly screenshot: Blob | null;
	/** Timeline, movement, state and environment diagnostics, by file name. */
	readonly diagnostics?: Readonly<Record<string, string>>;
}

/*
================
ArchivedSummary
================
*/
export interface ArchivedSummary {
	readonly id: string;
	readonly createdAt: string;
	readonly description: string;
	readonly bytes: number;
	readonly delivered: boolean;
}

/*
================
ReportArchive
================
*/
export interface ReportArchive {
	save( report: ArchivedReport ): Promise<void>;
	list(): Promise<ArchivedSummary[]>;
	/** The report as a .zip: report.json, replay.mp4 and screenshot.jpg. */
	exportZip( id: string ): Promise<Blob | null>;
	remove( id: string ): Promise<void>;
}

/*
================
DiagnosticUploadOptions
================
*/
export interface DiagnosticUploadOptions {
	readonly id: string;
	readonly clip: { readonly start: number; readonly end: number; } | null;
	readonly maxBytes: number;
}

/*
================
diagnosticUploadBudget

An older Agent rejects unknown multipart parts. Only its explicit settings
capability admits a diagnostics attachment; absent or invalid means legacy.
================
*/
export function diagnosticUploadBudget( maxBytes: number, advertised: unknown ): number {
	if ( typeof advertised !== "number" || !Number.isFinite( advertised ) || !Number.isFinite( maxBytes ) ) return 0;
	const bytes = Math.floor( Math.min( advertised, maxBytes, MAX_DIAGNOSTIC_BYTES ) );
	return bytes >= MIN_DIAGNOSTIC_BYTES ? bytes : 0;
}

/*
================
diagnosticValue

The one JSON replacer for report data, shared with bug-report.ts: Maps and
Sets as plain data, binary as its size, bigints as decimal text.
JSON.stringify throws on a bigint, and report documents carry bigint values
(the codec's session id among them), so every stringify of report data in
this module goes through it instead of the bare form.
================
*/
export function diagnosticValue( _key: string, item: unknown ): unknown {
	if ( item instanceof Map ) return Object.fromEntries( item );
	if ( item instanceof Set ) return [ ...item ];
	if ( ArrayBuffer.isView( item ) ) return `[${item.constructor.name}: ${item.byteLength} bytes]`;
	if ( typeof item === "bigint" ) return item.toString();
	return item;
}

/*
================
sharedValue

The shared attachment excludes chat, identities and credentials. Numeric cast
tokens and entity IDs remain intact because replay needs their correlations.
================
*/
function sharedValue( value: unknown, field = "" ): unknown {
	if ( typeof value === "string" ) {
		return TECHNICAL_STRINGS[field]?.includes( value ) || TECHNICAL_PATTERNS[field]?.test( value ) ?
			value :
			undefined;
	}
	if ( Array.isArray( value ) ) {
		// An enumerated list (an adapter's features) drops what it does not
		// name; any other array keeps its positions.
		const items = value.map( item => sharedValue( item, field ) );
		return TECHNICAL_STRINGS[field] ? items.filter( item => item !== undefined ) : items;
	}
	if ( !value || typeof value !== "object" ) return value;
	const out: Record<string, unknown> = {};
	for ( const [key, item] of Object.entries( value ) ) {
		if ( !TECHNICAL_FIELDS.has( key ) ) continue;
		const shared = sharedValue( item, key );
		if ( shared !== undefined ) out[key] = shared;
	}
	return out;
}

/*
================
iconlessRows

A row drawn with no icon (it carries no icon path) is marked iconless: the
shared copy drops every path, and threads reported items "going invisible"
in the bag and in storage with nothing in the report to say which slot.
================
*/
function iconlessRows( rows: unknown ): unknown {
	if ( !Array.isArray( rows ) ) return rows;
	return rows.map( row =>
		row && typeof row === "object" && !(row as { icon?: unknown; }).icon ? { ...row, iconless: true } : row
	);
}

/*
================
markIconless

The gameplay snapshot with iconlessRows applied to the bag, the avatar
sockets and the open storage.
================
*/
function markIconless( value: unknown ): unknown {
	if ( !value || typeof value !== "object" ) return value;
	const gameplay = value as Record<string, unknown>;
	const storage = gameplay.storage && typeof gameplay.storage === "object" ?
		gameplay.storage as Record<string, unknown> :
		undefined;
	return {
		...gameplay,
		inventory: iconlessRows( gameplay.inventory ),
		avatarInventory: iconlessRows( gameplay.avatarInventory ),
		...(storage ? { storage: { capacity: storage.capacity, items: iconlessRows( storage.items ) } } : {})
	};
}

/*
================
selectedFields
================
*/
function selectedFields( value: unknown, fields: readonly string[] ): Record<string, unknown> {
	const source = value && typeof value === "object" ? value as Record<string, unknown> : {};
	const out: Record<string, unknown> = {};
	for ( const key of fields ) {
		if ( !(key in source) ) continue;
		const shared = sharedValue( source[key], key );
		if ( shared !== undefined ) out[key] = shared;
	}
	return out;
}

/*
================
diagnosticZip

Compression is optional for browsers without raw DEFLATE support. Stored ZIP
entries use the same budget and remain readable by the existing archive tools.
================
*/
async function diagnosticZip( documents: Record<string, unknown> ): Promise<{ blob: Blob; unpackedBytes: number; }> {
	const files: ZipFile[] = [];
	let unpackedBytes = 0;
	for ( const [name, value] of Object.entries( documents ) ) {
		const data = new TextEncoder().encode( JSON.stringify( value, diagnosticValue ) + "\n" );
		unpackedBytes += data.byteLength;
		let deflated: Uint8Array | undefined;
		try {
			const stream = new Blob( [ data ] ).stream().pipeThrough( new CompressionStream( "deflate-raw" ) );
			const packed = new Uint8Array( await new Response( stream ).arrayBuffer() );
			if ( packed.length < data.length ) deflated = packed;
		} catch {
			// Older browsers can still submit a bounded, ordinary stored ZIP.
		}
		files.push( { name, data, deflated } );
	}
	return {
		blob: new Blob( [ zipStore( files, new Date( 0 ) ) as BlobPart ], { type: "application/zip" } ),
		unpackedBytes
	};
}

/*
================
sharedAssets

assets.json rebuilt from typed fields alone (asset-history.ts): times, slot
counts, ages, enumerated states and request paths. Paths are public asset
locations (the asset owner already strips credentials and queries); one that
does not look like one is dropped, as is any free-form error text.
================
*/
function sharedAssets( value: Record<string, unknown> ): Record<string, unknown> {
	const word = ( text: unknown ) => typeof text === "string" && SHARED_ASSET_WORD.test( text ) ? text : null;
	const number = ( n: unknown ) => typeof n === "number" && Number.isFinite( n ) ? n : null;
	const queue = ( raw: unknown ) => {
		if ( !raw || typeof raw !== "object" ) return null;
		const q = raw as Record<string, unknown>;
		const jobs = Array.isArray( q.jobs ) ? q.jobs : [];
		return {
			phase: word( q.phase ),
			failed: typeof q.error === "string",
			available: number( q.available ),
			jobs: jobs.filter( ( job ): job is Record<string, unknown> => !!job && typeof job === "object" ).map(
				job => ({
					id: number( job.id ),
					path: typeof job.path === "string" && SHARED_ASSET_PATH.test( job.path ) ? job.path : null,
					decode: word( job.decode ),
					ageMs: number( job.ageMs ),
					state: word( job.state ),
					result: word( job.result )
				})
			),
			...(typeof q.omittedJobs === "number" ? { omittedJobs: q.omittedJobs } : {})
		};
	};
	const sample = ( raw: unknown ) => {
		const s = raw && typeof raw === "object" ? raw as Record<string, unknown> : {};
		return { t: number( s.t ), phase: word( s.phase ), queue: queue( s.queue ) };
	};
	const samples = Array.isArray( value.samples ) ? value.samples : [];
	return {
		note: typeof value.note === "string" ? value.note : "",
		...(value.current ? { current: sample( value.current ) } : {}),
		samples: samples.map( sample )
	};
}

/*
================
sharedNetwork

network.json's rows, rebuilt from their number, direction, opcode and size
fields alone, so nothing else can ride along into the shared archive.
================
*/
function sharedNetwork( value: Record<string, unknown> ): Record<string, unknown> {
	const rows = Array.isArray( value.events ) ? value.events : [];
	const events = rows.filter( ( row ): row is [number, string, string, number] =>
		Array.isArray( row ) && row.length === 4 && typeof row[0] === "number" &&
		(row[1] === "in" || row[1] === "out" || row[1] === "gap") && typeof row[2] === "string" &&
		/^0x[0-9A-F]{4}$/.test( row[2] ) &&
		typeof row[3] === "number"
	).map( ( [t, direction, opcode, bytes] ) => [ t, direction, opcode, bytes ] );
	return { note: typeof value.note === "string" ? value.note : "", events };
}

/*
================
createDiagnosticUpload

Capture all documents before transcoding the video. Prefer complete compressed
evidence; if it cannot fit, reduce the largest event history or state field. The
manifest makes every omission explicit; the full local archive is unchanged.
================
*/
export async function createDiagnosticUpload(
	files: Readonly<Record<string, string>>,
	options: DiagnosticUploadOptions
): Promise<Blob> {
	const limit = Math.min( MAX_DIAGNOSTIC_BYTES, Math.floor( options.maxBytes ) );
	if ( !Number.isFinite( options.maxBytes ) || limit < MIN_DIAGNOSTIC_BYTES ) {
		throw Error( "Report attachment budget is too small" );
	}
	const read = ( name: string ): Record<string, unknown> => JSON.parse( files[name] ?? "{}" );
	const timeline = read( "timeline.json" );
	const events = Array.isArray( timeline.events ) ? timeline.events : [];
	const sharedTimeline = sharedValue( {
		clip: timeline.clip,
		events: events.filter( event => event.kind !== "chat" )
	} ) as Record<string, unknown>;
	const state = read( "state.json" );
	const gameplay = selectedFields( markIconless( state.gameplay ), SHARED_GAMEPLAY_FIELDS );
	const sourceEnvironment = read( "environment.json" );
	const environment = selectedFields( sourceEnvironment, SHARED_ENVIRONMENT_FIELDS );
	// Browser identification is an explicit environmental field, not game text.
	if ( typeof sourceEnvironment.userAgent === "string" ) {
		environment.userAgent = sourceEnvironment.userAgent.slice( 0, MAX_USER_AGENT_LENGTH );
	}
	const omissions: string[] = [];
	const documents: Record<string, unknown> = {
		"manifest.json": {
			version: 1,
			reportId: options.id,
			clip: options.clip,
			videoTimelineOffsetSeconds: options.clip?.start ?? 0,
			note: "Add videoTimelineOffsetSeconds to the attached video time to obtain timeline t. " +
				"Only permitted technical fields and enumerated game strings are shared; " +
				"chat, names, account/session credentials and browser storage are excluded.",
			omissions
		},
		"movement.json": selectedFields( read( "movement.json" ), SHARED_MOVEMENT_FIELDS ),
		"timeline.json": sharedTimeline,
		// Opcode, direction, size and time only: technical by construction.
		"network.json": sharedNetwork( read( "network.json" ) ),
		"state.json": { session: selectedFields( state.session, [ "phase" ] ), entities: state.entities, gameplay },
		"environment.json": environment,
		"assets.json": sharedAssets( read( "assets.json" ) )
	};
	for ( ;; ) {
		const zip = await diagnosticZip( documents );
		if ( zip.blob.size <= limit && zip.unpackedBytes <= MAX_DIAGNOSTIC_UNPACKED_BYTES ) return zip.blob;
		// The trimmable histories: each document's rows, oldest first.
		const candidates = ([
			[ "timeline.json", "events" ],
			[ "movement.json", "events" ],
			[ "network.json", "events" ],
			[ "assets.json", "samples" ]
		] as const).map( ( [name, rows] ) => ({
			name,
			rows,
			document: documents[name] as Record<string, unknown>
		}) ).filter( entry =>
			Array.isArray( entry.document[entry.rows] ) && (entry.document[entry.rows] as unknown[]).length > 0
		)
			.sort( ( a, b ) =>
				JSON.stringify( b.document[b.rows], diagnosticValue ).length -
				JSON.stringify( a.document[a.rows], diagnosticValue ).length
			);
		const entry = candidates[0];
		const key = Object.keys( gameplay ).sort( ( a, b ) =>
			JSON.stringify( gameplay[b], diagnosticValue ).length -
			JSON.stringify( gameplay[a], diagnosticValue ).length
		)[0];
		// A large inventory snapshot must not evict a tiny, decisive receipt.
		if (
			key &&
			(!entry ||
				JSON.stringify( gameplay[key], diagnosticValue ).length >=
					JSON.stringify( entry.document[entry.rows], diagnosticValue ).length)
		) {
			delete gameplay[key];
			omissions.push( `state.json: omitted gameplay.${key} to fit upload limit` );
			continue;
		}
		if ( entry ) {
			const rows = entry.document[entry.rows] as unknown[];
			const dropped = Math.ceil( rows.length / 2 );
			entry.document[entry.rows] = rows.slice( dropped );
			omissions.push( `${entry.name}: dropped ${dropped} oldest ${entry.rows} to fit upload limit` );
			continue;
		}
		throw Error( "Technical diagnostics exceed the report attachment budget" );
	}
}

/*
================
createReportArchive
================
*/
export function createReportArchive(): ReportArchive {
	let database: Promise<IDBDatabase> | null = null;

	/*
	================
	open
	================
	*/
	function open(): Promise<IDBDatabase> {
		database ??= new Promise( ( resolve, reject ) => {
			const request = indexedDB.open( DATABASE, VERSION );
			request.onupgradeneeded = () => request.result.createObjectStore( STORE, { keyPath: "id" } );
			request.onsuccess = () => resolve( request.result );
			request.onerror = () => reject( request.error );
		} );
		return database;
	}

	/*
	================
	all

	Oldest first. Promise chains, not async: the report window lists the
	archive when it opens, which happens inside the frame.
	================
	*/
	function all(): Promise<ArchivedReport[]> {
		return open()
			.then( db => settle<ArchivedReport[]>( db.transaction( STORE ).objectStore( STORE ).getAll() ) )
			.then( rows => rows.sort( ( a, b ) => a.createdAt.localeCompare( b.createdAt ) ) );
	}

	/*
	================
	remove
	================
	*/
	async function remove( id: string ) {
		const db = await open();
		await settle( db.transaction( STORE, "readwrite" ).objectStore( STORE ).delete( id ) );
	}

	return {
		async save( report ) {
			const db = await open();
			await settle( db.transaction( STORE, "readwrite" ).objectStore( STORE ).put( report ) );
			const rows = await all();
			let bytes = rows.reduce( ( sum, row ) => sum + mediaBytes( row ), 0 );
			for ( const [index, row] of rows.entries() ) {
				if ( row.id === report.id ) continue;
				if ( rows.length - index <= MAX_REPORTS && bytes <= MAX_BYTES ) break;
				await remove( row.id );
				bytes -= mediaBytes( row );
			}
		},
		list() {
			return all().then( rows =>
				rows.reverse().map( row => ({
					id: row.id,
					createdAt: row.createdAt,
					description: row.description,
					bytes: mediaBytes( row ),
					delivered: row.delivered
				}) )
			);
		},
		async exportZip( id ) {
			const db = await open();
			const row = await settle<ArchivedReport | undefined>(
				db.transaction( STORE ).objectStore( STORE ).get( id )
			);
			if ( !row ) return null;
			const { replay, screenshot, diagnostics, ...details } = row;
			const files = [ {
				name: "report.json",
				data: new TextEncoder().encode( JSON.stringify( details, diagnosticValue, "\t" ) + "\n" )
			} ];
			if ( replay ) files.push( { name: "replay.mp4", data: new Uint8Array( await replay.arrayBuffer() ) } );
			if ( screenshot ) {
				files.push( { name: "screenshot.jpg", data: new Uint8Array( await screenshot.arrayBuffer() ) } );
			}
			for ( const [name, text] of Object.entries( diagnostics ?? {} ) ) {
				files.push( { name, data: new TextEncoder().encode( text + "\n" ) } );
			}
			files.push( { name: "README.txt", data: new TextEncoder().encode( README ) } );
			return new Blob( [ zipStore( files, new Date( row.createdAt ) ) as BlobPart ], {
				type: "application/zip"
			} );
		},
		remove
	};
}

/*
================
mediaBytes
================
*/
function mediaBytes( report: ArchivedReport ) {
	return (report.replay?.size ?? 0) + (report.screenshot?.size ?? 0);
}

/*
================
settle

An IndexedDB request as a promise.
================
*/
function settle<T>( request: IDBRequest ): Promise<T> {
	return new Promise( ( resolve, reject ) => {
		request.onsuccess = () => resolve( request.result as T );
		request.onerror = () => reject( request.error );
	} );
}
