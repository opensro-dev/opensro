/*
===========================================================================

test-input-recorder.mjs - record every input a test process touches

Preloaded (NODE_OPTIONS --import) into every test process, its worker
threads and any Node child it starts, by tools/run-tests.mjs. It appends
one line per distinct input to the owning test file's log, so the runner
can reuse a pass for as long as none of those inputs change:

	R <path>    file contents read (fs reads, module loads)
	D <path>    directory listing
	E <path>    existence or metadata probe
	W <path>    file written by the test (never an input)
	X <reason>  something no file hash can capture: the test always runs

Inactive unless SRO_TEST_INPUT_LOG names the log directory.

===========================================================================
*/
import fs from "node:fs";
import childProcess from "node:child_process";
import { createHash } from "node:crypto";
import { createRequire, registerHooks, syncBuiltinESMExports } from "node:module";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const LOG_DIR = process.env.SRO_TEST_INPUT_LOG;
const TEMP_ROOT = path.resolve( os.tmpdir() ).toLowerCase();

/*
================
adopt

Gives a wrapper every own member of the function it replaces, including
non-enumerable ones such as util.promisify.custom and fs.realpathSync.native.
================
*/
function adopt( wrapper, original ) {
	for ( const key of Reflect.ownKeys( original ) ) {
		if ( [ "length", "name", "prototype", "arguments", "caller" ].includes( key ) ) continue;
		Object.defineProperty( wrapper, key, Object.getOwnPropertyDescriptor( original, key ) );
	}
	return wrapper;
}

/*
================
logPathFor

One log per test file, shared by its threads and children.
================
*/
export function logPathFor( logDir, testFile ) {
	const key = createHash( "sha1" ).update( path.resolve( testFile ).toLowerCase() ).digest( "hex" );
	return path.join( logDir, key + ".log" );
}

if ( LOG_DIR ) install();

/*
================
install
================
*/
function install() {
	// The first process names the owner; threads and children inherit it.
	process.env.SRO_TEST_INPUT_OWNER ??= path.resolve( process.argv[1] ?? "unknown" );
	const logPath = logPathFor( LOG_DIR, process.env.SRO_TEST_INPUT_OWNER );
	const appendFileSync = fs.appendFileSync, seen = new Set();

	/*
	================
	note
	================
	*/
	function note( kind, target ) {
		let file;
		try {
			if ( target === undefined || target === null || typeof target === "number" ) return;
			file = path.resolve( target instanceof URL ? fileURLToPath( target ) : String( target ) );
		} catch {
			return;
		}
		if ( file.toLowerCase().startsWith( TEMP_ROOT ) ) return;
		const line = kind + "\t" + file;
		if ( seen.has( line ) ) return;
		seen.add( line );
		appendFileSync( logPath, line + "\n" );
	}

	/*
	================
	uncacheable
	================
	*/
	function uncacheable( reason ) {
		const line = "X\t" + reason;
		if ( seen.has( line ) ) return;
		seen.add( line );
		appendFileSync( logPath, line + "\n" );
	}

	/*
	================
	wrap

	Replaces fs[name] with a recorder that notes its first argument.
	================
	*/
	function wrap( owner, name, kind ) {
		const original = owner[name];
		if ( typeof original !== "function" ) return;
		// Keep attached members such as fs.realpathSync.native.
		owner[name] = adopt( function( target, ...rest ) {
			note( kind, target );
			return original.call( this, target, ...rest );
		}, original );
	}

	for ( const name of [ "readFileSync", "readFile", "openSync", "open", "createReadStream" ] ) wrap( fs, name, "R" );
	for ( const name of [ "readFile", "open" ] ) wrap( fs.promises, name, "R" );
	for ( const name of [ "readdirSync", "readdir", "opendirSync", "opendir" ] ) wrap( fs, name, "D" );
	for ( const name of [ "readdir", "opendir" ] ) wrap( fs.promises, name, "D" );
	for (
		const name of [ "existsSync", "statSync", "lstatSync", "stat", "lstat", "accessSync", "access", "realpathSync" ]
	) {
		wrap( fs, name, "E" );
	}
	for ( const name of [ "stat", "lstat", "access", "realpath" ] ) wrap( fs.promises, name, "E" );
	for ( const name of [ "writeFileSync", "writeFile", "appendFileSync", "appendFile", "mkdirSync", "rmSync" ] ) {
		wrap( fs, name, "W" );
	}
	for ( const name of [ "writeFile", "appendFile", "mkdir", "rm" ] ) wrap( fs.promises, name, "W" );
	// Copies and renames write their second argument.
	for ( const name of [ "copyFileSync", "cpSync", "renameSync" ] ) {
		const original = fs[name];
		fs[name] = adopt( function( from, to, ...rest ) {
			note( "R", from );
			note( "W", to );
			return original.call( this, from, to, ...rest );
		}, original );
	}

	// A Node child keeps recording only if it inherits this environment; any
	// other program reads inputs no hash here can see.
	for ( const name of [ "spawn", "spawnSync", "execFile", "execFileSync", "fork", "exec", "execSync" ] ) {
		const original = childProcess[name];
		childProcess[name] = adopt( function( command, ...rest ) {
			const options = rest.find( value => value && typeof value === "object" && !Array.isArray( value ) );
			const args = rest.find( Array.isArray ) ?? [];
			const program = path.basename( String( command ) ).toLowerCase();
			const nodeChild = name === "fork" || command === process.execPath || /^node(\.exe)?$/.test( program );
			// esbuild's own service: its inputs arrive through the metafile.
			const esbuildService = /^esbuild(\.exe)?$/.test( program ) &&
				args.some( arg => String( arg ).startsWith( "--service=" ) );
			// A pure tool that only touches the test's own temporary files.
			const tempTool = program === "unzip" && args.length > 0 &&
				args.filter( arg => !String( arg ).startsWith( "-" ) )
					.every( arg =>
						path.resolve( String( options?.cwd ?? process.cwd() ), String( arg ) ).toLowerCase()
							.startsWith( TEMP_ROOT )
					);
			// Vite maps Windows network drives with `net use`; no file input.
			// exec() reaches spawn() with the whole command line as the program.
			const driveProbe = program === "net" && String( args[0] ?? "" ).toLowerCase() === "use" ||
				/^net use$/i.test( String( command ).trim() );
			if ( !nodeChild && !esbuildService && !tempTool && !driveProbe ) uncacheable( "spawns " + program );
			else if ( nodeChild && options?.env && !options.env.SRO_TEST_INPUT_LOG ) {
				uncacheable( "node child without the recorder" );
			}
			return original.call( this, command, ...rest );
		}, original );
	}
	syncBuiltinESMExports();

	let esbuildPatched = false;

	/*
	================
	recordEsbuild

	esbuild's native service reads its sources directly, out of sight of the
	fs hooks above. Its metafile names every input it read, so each build
	asks for one, records those files and hands the caller its own options'
	result. Patched before the first import so ESM named exports see it.
	================
	*/
	function recordEsbuild( url ) {
		const require = createRequire( url ), id = fileURLToPath( url );
		require( id );
		const cached = require.cache[id];
		// esbuild exports read-only getters: hand importers a patched copy.
		const esbuild = { ...cached.exports };
		const noteInputs = ( options, result ) => {
			const base = options?.absWorkingDir ?? process.cwd();
			for ( const input of Object.keys( result?.metafile?.inputs ?? {} ) ) {
				// Plugin namespaces ("ns:path") and stdin are not files.
				if ( /^[a-z][\w-]+:/i.test( input ) && !/^[a-z]:[\\/]/i.test( input ) ) continue;
				note( "R", path.resolve( base, input ) );
			}
			if ( result && !options?.metafile ) delete result.metafile;
			return result;
		};
		const build = esbuild.build, buildSync = esbuild.buildSync;
		esbuild.build = adopt(
			async options => noteInputs( options, await build( { ...options, metafile: true } ) ),
			build
		);
		esbuild.buildSync = adopt(
			options => noteInputs( options, buildSync( { ...options, metafile: true } ) ),
			buildSync
		);
		for ( const name of [ "context", "serve" ] ) {
			const original = esbuild[name];
			if ( typeof original === "function" ) {
				esbuild[name] = adopt( function( ...args ) {
					uncacheable( "esbuild " + name );
					return original.apply( this, args );
				}, original );
			}
		}
		cached.exports = esbuild;
	}

	// test-vite-recorder.mjs records what Rolldown reads natively.
	const viteRecorder = new URL( "./test-vite-recorder.mjs", import.meta.url ).href;
	globalThis[Symbol.for( "sro.testInputs" )] = { note, uncacheable };

	registerHooks( {
		resolve( specifier, context, nextResolve ) {
			if ( specifier === "vite" && context.parentURL !== viteRecorder ) {
				return { url: viteRecorder, format: "module", shortCircuit: true };
			}
			const resolved = nextResolve( specifier, context );
			if ( !esbuildPatched && specifier === "esbuild" && resolved.url.startsWith( "file:" ) ) {
				esbuildPatched = true;
				recordEsbuild( resolved.url );
			}
			return resolved;
		},
		load( url, context, nextLoad ) {
			if ( url.startsWith( "file:" ) ) note( "R", new URL( url ) );
			return nextLoad( url, context );
		}
	} );
}
