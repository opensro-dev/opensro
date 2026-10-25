/*
===========================================================================

rebuildLockCli.mjs - the generated-assets lock's command-line front end

Runs one command under the lock (node scripts/rebuildLock.mjs --mode read
-- <command>) and prints the --status view: the writer, the reader set and
any waiting writers. The lock protocol itself - the state directory, the
transition mutex, stale reaping - lives in rebuildLock.mjs; this file only
parses arguments, spawns the command and formats the status. It is loaded
through rebuildLock.mjs's main guard, so a copied lock tree keeps a working
CLI.

===========================================================================
*/

import { execFile, spawn } from "node:child_process";
import {
	DEFAULT_STALE_MS,
	GENERATED_ASSETS_LOCK_LABEL,
	GENERATED_ASSETS_LOCK_NAME,
	formatOwner,
	inspectState,
	normalizeLockName,
	normalizeMode,
	numberFromEnv,
	rebuildLockDirectory,
	truncate,
	withRebuildLock
} from "./rebuildLock.mjs";

/*
================
runCommandUnderLock
================
*/
export async function runCommandUnderLock( args ) {
	const options = parseCliArgs( args );
	if ( options.status ) {
		const state = await readLockState( options.name );
		if ( state.writer ) {
			console.log( `${options.name} is locked by ${formatOwner( state.writer )}.` );
		} else if ( state.readers.length > 0 ) {
			const readers = state.readers.map( ( reader ) => formatOwner( reader ) ).join( "; " );
			console.log( `${options.name} is read-locked by ${state.readers.length} reader(s): ${readers}.` );
		} else {
			console.log( `${options.name} is not locked.` );
		}
		if ( state.waiting.length > 0 ) {
			const waiting = state.waiting.map( ( writer ) => formatOwner( writer ) ).join( "; " );
			console.log( `Writer(s) waiting: ${waiting}.` );
		}
		const relatedProcesses = await findRelatedGeneratedAssetProcesses();
		if ( relatedProcesses.length > 0 ) {
			console.log( "Related generated-asset process(es) are currently running:" );
			for ( const processInfo of relatedProcesses ) {
				console.log( `- pid ${processInfo.pid}: ${truncate( processInfo.commandLine, 220 )}` );
			}
		}
		return 0;
	}

	if ( options.command.length === 0 ) {
		console.error(
			'Usage: node scripts/rebuildLock.mjs [--name generated-assets] [--label "resource build"] [--mode read|write] -- <command> [args...]'
		);
		return 2;
	}

	return withRebuildLock(
		{ name: options.name, label: options.label, mode: options.mode, timeoutMs: options.timeoutMs },
		async () => spawnCommand( options.command )
	);
}

/*
================
readLockState

The status view: a read-only inspection (no mutex, no reaping), stale
claims filtered so a crashed holder never reports as live.
================
*/
async function readLockState( name ) {
	const safeName = normalizeLockName( name );
	const lockDir = await rebuildLockDirectory( safeName );
	const staleMs = Math.max(
		60000,
		numberFromEnv( "SRO_REBUILD_LOCK_STALE_MS", DEFAULT_STALE_MS )
	);
	try {
		return await inspectState( lockDir, staleMs, { reap: false } );
	} catch {
		return { writer: undefined, readers: [], waiting: [] };
	}
}

/*
================
parseCliArgs
================
*/
function parseCliArgs( args ) {
	/** @type {{ name: string, label: string, mode: string, timeoutMs: number | undefined, status: boolean, command: string[] }} */
	const options = {
		name: GENERATED_ASSETS_LOCK_NAME,
		label: GENERATED_ASSETS_LOCK_LABEL,
		mode: "write",
		timeoutMs: undefined,
		status: false,
		command: []
	};

	for ( let index = 0; index < args.length; index += 1 ) {
		const arg = args[index];
		if ( arg === "--" ) {
			options.command = args.slice( index + 1 );
			break;
		}
		if ( arg === "--status" ) {
			options.status = true;
			continue;
		}
		if ( arg === "--name" ) {
			options.name = args[index + 1] ?? options.name;
			index += 1;
			continue;
		}
		if ( arg === "--label" ) {
			options.label = args[index + 1] ?? options.label;
			index += 1;
			continue;
		}
		if ( arg === "--mode" ) {
			options.mode = args[index + 1] ?? options.mode;
			index += 1;
			continue;
		}
		if ( arg === "--timeout-ms" ) {
			options.timeoutMs = Number( args[index + 1] );
			index += 1;
			continue;
		}
		options.command = args.slice( index );
		break;
	}

	options.name = normalizeLockName( options.name );
	options.mode = normalizeMode( options.mode );
	return options;
}

/*
================
spawnCommand
================
*/
function spawnCommand( command ) {
	return new Promise( ( resolve, reject ) => {
		const child = spawn( command[0], command.slice( 1 ), {
			env: process.env,
			stdio: "inherit",
			windowsHide: true
		} );
		child.once( "error", reject );
		child.once( "exit", ( code, signal ) => {
			if ( signal ) {
				console.error( `[rebuild-lock] child exited from signal ${signal}.` );
				resolve( 1 );
				return;
			}
			resolve( code ?? 0 );
		} );
	} );
}

/*
================
findRelatedGeneratedAssetProcesses
================
*/
async function findRelatedGeneratedAssetProcesses() {
	if ( process.platform !== "win32" ) {
		return [];
	}

	const command = [
		"$matches = Get-CimInstance Win32_Process |",
		"Where-Object { $_.CommandLine -match 'build_sro_resources|rebuild_asset_packs_from_public|jsonAssetCompression|convert_images\\.py' } |",
		"Select-Object ProcessId,ParentProcessId,CommandLine;",
		"$matches | ConvertTo-Json -Compress"
	].join( " " );

	try {
		const { stdout } = await execFileCapture( "powershell", [ "-NoProfile", "-Command", command ], {
			timeoutMs: 3000
		} );
		const parsed = stdout.trim() ? JSON.parse( stdout ) : [];
		const rows = Array.isArray( parsed ) ? parsed : [ parsed ];
		return rows
			.map( ( row ) => ({
				pid: Number( row.ProcessId ),
				parentPid: Number( row.ParentProcessId ),
				commandLine: String( row.CommandLine ?? "" )
			}) )
			.filter( ( row ) => Number.isInteger( row.pid ) && row.pid !== process.pid && row.commandLine )
			.filter( ( row ) => !/Get-CimInstance Win32_Process|rebuildLock\.mjs --status/i.test( row.commandLine ) );
	} catch {
		return [];
	}
}

/*
================
execFileCapture
================
*/
function execFileCapture( file, args, { timeoutMs } ) {
	return new Promise( ( resolve, reject ) => {
		const child = execFile( file, args, { windowsHide: true, timeout: timeoutMs }, ( error, stdout, stderr ) => {
			if ( error ) {
				error.stderr = stderr;
				reject( error );
				return;
			}
			resolve( { stdout, stderr } );
		} );
		child.stdin?.end();
	} );
}
