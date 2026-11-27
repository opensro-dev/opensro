/*
===========================================================================

reads.mjs - the files one build step read, discovered while it runs

Some steps cannot name their inputs before they run: a character model
reads its BSR, then the skeleton, meshes, animations, material sets and
textures that BSR lists. Such a step runs inside trackReads, and the
readers it goes through (jmxAssetIO.mjs) call noteRead with every file
they open or probe. The action store records those files with their
digests (store.mjs storeOutputs) and a later build reuses the outputs
only while every one of them still holds those bytes (readRecord), the
way a compiler's depfile works.

A whole lane of the resource build has no single reader to go through,
so trackReads( step, { complete: true } ) also sees what the step asks
the fs module for: files opened, read or probed, and directories listed
(LISTED, a listing is an input of its own). A step that starts a child
process or a worker reads what nothing here can see, so such a scope is
opaque: its reads are no complete account, and its caller keys it the
coarse way (laneMemo.mjs).

Scopes nest. A read counts for its own scope and every enclosing one, so
a lane depends on what the actions inside it read too. Reads that only
the fs hooks saw go to the complete scopes alone: an action names what
it read through its readers, and refuses a read outside its roots.

The scope is an AsyncLocalStorage, so steps running at once each collect
their own reads. Outside a scope noteRead does nothing: a reader shared
with untracked steps costs one lookup.

A probe counts as a read. A step that chose a file because another was
missing (convertedTexturePath's two names) depends on that absence, and
the record keeps the missing file as the "missing" digest
(action.mjs MISSING_DIGEST), so its later appearance misses.

===========================================================================
*/
import { AsyncLocalStorage } from "node:async_hooks";
import childProcess from "node:child_process";
import fs from "node:fs";
import { syncBuiltinESMExports } from "node:module";
import path from "node:path";
import workerThreads from "node:worker_threads";

const scope = new AsyncLocalStorage();

// The prefix a directory listing carries in a complete scope's reads.
export const LISTED = "list:";

// The fs entry points that read or probe a path (first argument), and
// those that list a directory. open is a read only when its flags are.
const FILE_READERS = [
	"readFile",
	"readFileSync",
	"createReadStream",
	"stat",
	"statSync",
	"lstat",
	"lstatSync",
	"access",
	"accessSync",
	"exists",
	"existsSync"
];
const OPENERS = [ "open", "openSync" ];
const LISTERS = [ "readdir", "readdirSync", "opendir", "opendirSync" ];
const PROMISE_FILE_READERS = [ "readFile", "stat", "lstat", "access" ];
const PROMISE_LISTERS = [ "readdir", "opendir" ];
// What reads beyond the fs hooks' sight.
const SPAWNERS = [ "spawn", "spawnSync", "exec", "execSync", "execFile", "execFileSync", "fork" ];

let hooksInstalled = false;

/*
================
trackReads

Runs step() and returns { result, reads, opaque }: every file noted while
it ran (and, with complete, every path the fs hooks saw, listings as
LISTED + directory), as absolute paths. opaque names what made a
complete scope's account partial (a child process, a worker), else is
undefined.
================
*/
export async function trackReads( step, { complete = false } = {} ) {
	if ( complete ) installReadHooks();
	const frame = { reads: new Set(), complete, opaque: undefined, parent: scope.getStore() };
	const result = await scope.run( frame, step );
	return { result, reads: frame.reads, opaque: frame.opaque };
}

/*
================
noteRead

Called by a reader with a file it opened or probed: a read of every
enclosing scope.
================
*/
export function noteRead( file ) {
	const resolved = path.resolve( file );
	for ( let frame = scope.getStore(); frame; frame = frame.parent ) frame.reads.add( resolved );
}

/*
================
noteHooked

A path the fs hooks saw: a read of every enclosing complete scope.
================
*/
function noteHooked( entry ) {
	for ( let frame = scope.getStore(); frame; frame = frame.parent ) {
		if ( frame.complete ) frame.reads.add( entry );
	}
}

/*
================
noteOpaque
================
*/
function noteOpaque( reason ) {
	for ( let frame = scope.getStore(); frame; frame = frame.parent ) {
		if ( frame.complete ) frame.opaque ??= reason;
	}
}

/*
================
pathArgument

The absolute path an fs call names, or undefined for a descriptor, a
URL that is not a file, or anything else.
================
*/
function pathArgument( target ) {
	if ( typeof target === "string" ) return path.resolve( target );
	if ( Buffer.isBuffer( target ) ) return path.resolve( target.toString() );
	if ( target instanceof URL && target.protocol === "file:" ) {
		return path.resolve( decodeURIComponent( target.pathname.replace( /^\/(?=[A-Za-z]:)/, "" ) ) );
	}
	return undefined;
}

/*
================
readsFlags

Whether open's flags only read: no flags, "r", "rs" or the O_RDONLY bits.
================
*/
function readsFlags( flags ) {
	if ( flags === undefined || flags === null ) return true;
	if ( typeof flags === "number" ) return (flags & (fs.constants.O_WRONLY | fs.constants.O_RDWR)) === 0;
	return /^r[s]?$/.test( String( flags ) );
}

/*
================
wrap

Replaces owner[name] with a function that notes its call, then calls the
original. A hook never changes what the call does or returns.
================
*/
function wrap( owner, name, note ) {
	const original = owner[name];
	if ( typeof original !== "function" ) return;
	owner[name] = function( ...args ) {
		if ( scope.getStore() ) note( args );
		return original.apply( this, args );
	};
}

/*
================
installReadHooks

Once per process: the fs, fs.promises, child_process and worker_threads
hooks complete scopes see through. Outside a scope each costs one lookup.
================
*/
function installReadHooks() {
	if ( hooksInstalled ) return;
	hooksInstalled = true;
	const file = ( [target] ) => {
		const resolved = pathArgument( target );
		if ( resolved ) noteHooked( resolved );
	};
	const opened = ( [target, flags] ) => {
		if ( readsFlags( flags ) ) file( [ target ] );
	};
	const listed = ( [target] ) => {
		const resolved = pathArgument( target );
		if ( resolved ) noteHooked( LISTED + resolved );
	};
	for ( const name of FILE_READERS ) wrap( fs, name, file );
	for ( const name of OPENERS ) wrap( fs, name, opened );
	for ( const name of LISTERS ) wrap( fs, name, listed );
	for ( const name of PROMISE_FILE_READERS ) wrap( fs.promises, name, file );
	wrap( fs.promises, "open", opened );
	for ( const name of PROMISE_LISTERS ) wrap( fs.promises, name, listed );
	for ( const name of SPAWNERS ) wrap( childProcess, name, () => noteOpaque( `child process (${name})` ) );
	const Worker = workerThreads.Worker;
	workerThreads.Worker = class extends Worker {
		constructor( filename, options ) {
			noteOpaque( "worker thread" );
			super( filename, options );
		}
	};
	// ESM named imports of the built-ins see the hooks too.
	syncBuiltinESMExports();
}
