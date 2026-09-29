/*
===========================================================================

checkStamps.mjs - input fingerprints and pass stamps for skippable gates

A gate, or one step of a gate, is a function of the files and keys it
reads. fingerprint() digests those inputs; a pass records the digest under
.state/check-stamps/<name>.json; a later run with a recorded digest has
nothing new to check. run_if_changed.mjs applies this to whole gates and
check_go_server.mjs to each of its steps.

Inputs are git pathspecs, `@command,arg,...` whose output joins the digest,
`!path` for an ignored-by-git file that still counts, and `=text` for a
literal key. A gate with several steps takes one snapshot() and digests
each step's share of it with fingerprintOf().

A stamp is only written after a pass, and any input change - a source
edit, a new file, a toolchain upgrade - changes the fingerprint, so nothing
is skipped for inputs it has not checked. SRO_CHECK_FORCE=1 ignores stamps.

===========================================================================
*/
import { spawn, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", ".." );
const stampDirectory = path.join( rebuildRoot, ".state", "check-stamps" );
const RECENT_PASSES = 8;

/*
================
git

Runs a git command in the repository and returns its stdout lines.
================
*/
function git( args ) {
	const result = spawnSync( "git", args, {
		windowsHide: true,
		cwd: rebuildRoot,
		encoding: "utf8",
		maxBuffer: 256 * 1024 * 1024
	} );
	if ( result.status !== 0 ) {
		throw new Error( `git ${args.join( " " )} failed: ${result.stderr}` );
	}
	return result.stdout.split( /\r?\n/ ).filter( ( line ) => line.length > 0 );
}

/*
================
gitAsync

git without blocking the event loop, so independent calls overlap.
================
*/
function gitAsync( args ) {
	return new Promise( ( resolve, reject ) => {
		const child = spawn( "git", args, { windowsHide: true, cwd: rebuildRoot } );
		const chunks = [];
		let errors = "";
		child.stdout.on( "data", ( chunk ) => chunks.push( chunk ) );
		child.stderr.on( "data", ( chunk ) => {
			errors += chunk;
		} );
		child.on( "error", reject );
		child.on( "close", ( code ) => {
			if ( code !== 0 ) {
				reject( new Error( `git ${args.join( " " )} failed: ${errors}` ) );
				return;
			}
			resolve(
				Buffer.concat( chunks ).toString( "utf8" ).split( /\r?\n/ ).filter( ( line ) => line.length > 0 )
			);
		} );
	} );
}

/*
================
snapshotOf

Builds the sorted [path, state] array from the three git listings.
================
*/
function snapshotOf( tracked, modified, untracked ) {
	const state = new Map();
	for ( const line of tracked ) {
		const tab = line.indexOf( "\t" );
		state.set( line.slice( tab + 1 ), line.slice( 0, tab ) );
	}
	for ( const file of new Set( [ ...modified, ...untracked ] ) ) {
		const absolute = path.join( rebuildRoot, file );
		state.set(
			file,
			existsSync( absolute ) ?
				"changed " + createHash( "sha256" ).update( readFileSync( absolute ) ).digest( "hex" ) :
				"deleted"
		);
	}
	return [ ...state ].sort( ( left, right ) => (left[0] < right[0] ? -1 : left[0] > right[0] ? 1 : 0) );
}

/*
================
snapshot

The state of every file under the given pathspecs, in three git calls:
tracked files contribute their index blob ids (`git ls-files -s`); files
modified in the working tree and untracked, non-ignored files contribute
the digest of their current content. That makes fingerprints exact without
re-hashing the whole tree. Returns a sorted array of [path, state].
================
*/
export function snapshot( paths ) {
	if ( paths.length === 0 ) return [];
	return snapshotOf(
		git( [ "ls-files", "-s", "--", ...paths ] ),
		git( [ "diff", "--name-only", "--", ...paths ] ),
		git( [ "ls-files", "-o", "--exclude-standard", "--", ...paths ] )
	);
}

/*
================
snapshotAsync

snapshot() with the three git calls running concurrently.
================
*/
export async function snapshotAsync( paths ) {
	if ( paths.length === 0 ) return [];
	const [tracked, modified, untracked] = await Promise.all( [
		gitAsync( [ "ls-files", "-s", "--", ...paths ] ),
		gitAsync( [ "diff", "--name-only", "--", ...paths ] ),
		gitAsync( [ "ls-files", "-o", "--exclude-standard", "--", ...paths ] )
	] );
	return snapshotOf( tracked, modified, untracked );
}

/*
================
keyOf

The digest of one non-path input: `@command,arg,...` output, an `!path`
file's bytes, or an `=text` literal.
================
*/
function keyOf( input ) {
	if ( input.startsWith( "@" ) ) {
		const [command, ...args] = input.slice( 1 ).split( "," );
		const result = spawnSync( command, args, {
			windowsHide: true,
			cwd: rebuildRoot,
			encoding: "utf8",
			shell: false
		} );
		return `command ${input}\n${result.stdout ?? ""}${result.stderr ?? ""}`;
	}
	if ( input.startsWith( "!" ) ) {
		const absolute = path.join( rebuildRoot, input.slice( 1 ) );
		const bytes = existsSync( absolute ) ? readFileSync( absolute ) : "<missing>";
		return `extra ${input}\n` + createHash( "sha256" ).update( bytes ).digest( "hex" );
	}
	return `key ${input}`;
}

/*
================
fingerprintOf

Digests the snapshot entries a step reads (select(path) is true) together
with its non-path inputs.
================
*/
export function fingerprintOf( entries, select, keys ) {
	const hash = createHash( "sha256" );
	for ( const [file, state] of entries ) {
		if ( select( file ) ) hash.update( `${state}\t${file}\n` );
	}
	for ( const input of keys ) hash.update( `${keyOf( input )}\n` );
	return hash.digest( "hex" );
}

/*
================
fingerprint

One gate's inputs: pathspecs, plus `@`, `!` and `=` keys.
================
*/
export function fingerprint( inputs ) {
	const paths = inputs.filter( ( input ) => !/^[@!=]/.test( input ) );
	const keys = inputs.filter( ( input ) => /^[@!=]/.test( input ) );
	return fingerprintOf( snapshot( paths ), () => true, keys );
}

/*
================
readPasses
================
*/
function readPasses( name ) {
	const stampPath = path.join( stampDirectory, `${name}.json` );
	return existsSync( stampPath ) ? [ JSON.parse( readFileSync( stampPath, "utf8" ) ) ].flat() : [];
}

/*
================
findPass

The recorded pass for this fingerprint, or undefined. Always undefined
under SRO_CHECK_FORCE=1.
================
*/
export function findPass( name, key ) {
	if ( process.env.SRO_CHECK_FORCE === "1" ) return undefined;
	return readPasses( name ).find( ( pass ) => pass.fingerprint === key );
}

/*
================
recordPass

Keeps the most recent passes, so reverting an edit or switching back to a
branch that already passed is a skip too.
================
*/
export function recordPass( name, key ) {
	mkdirSync( stampDirectory, { recursive: true } );
	const recent = [
		{ fingerprint: key, passedAt: new Date().toISOString() },
		...readPasses( name ).filter( ( pass ) => pass.fingerprint !== key )
	].slice( 0, RECENT_PASSES );
	writeFileSync( path.join( stampDirectory, `${name}.json` ), `${JSON.stringify( recent )}\n` );
}
