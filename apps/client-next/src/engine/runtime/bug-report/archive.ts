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
import { zipStore } from "@/engine/foundation/archive/zip";

const DATABASE = "sro-bug-reports";
const STORE = "reports";
const VERSION = 1;
const MAX_REPORTS = 10;
const MAX_BYTES = 600 * 1024 * 1024;
const README = `OpenSRO bug report

report.json       what the player wrote, the context sent to Discord, recent errors,
                  and "clip": the part of replay.mp4 that was sent (seconds).
replay.mp4        the last minute before the report, full quality, with sound.
timeline.json     what happened during that minute, on replay.mp4's clock (t, seconds):
                  key        keys pressed/released (never text typed into a field)
                  click      presses on the game world (x, y from 0 to 1 across the canvas)
                  ui         presses on game window controls (their ui id)
                  chat       chat lines seen or sent
                  error      JavaScript and runtime failures
                  long-frame main-thread hitches, with the scripts the browser blamed
                  sample     once a second: phase, region and position, HP/MP, target, heap
state.json        the game's state when the report was sent (large catalogs omitted).
environment.json  browser, screen, CPU, memory, network, the player's options, and
                  every resource loaded during the last minute (time, size, status).
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
	/** timeline.json, state.json and environment.json, by file name. */
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
				data: new TextEncoder().encode( JSON.stringify( details, null, "\t" ) + "\n" )
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
