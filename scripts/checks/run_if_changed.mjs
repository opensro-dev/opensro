/*
===========================================================================

run_if_changed.mjs - skip a gate whose inputs have not changed since it passed

	node scripts/checks/run_if_changed.mjs <name> <input>... -- <command> [args...]

Computes a fingerprint of everything the gate reads - the listed files and
directories (tracked and untracked, git-ignored files excluded), plus any
extra key such as a toolchain version - and compares it with the stamps of the
gate's recent passes under .state/check-stamps/. A match means the same
inputs already passed, so the gate reports "up to date" without running.
Otherwise the command runs, and a pass records the new stamp.

A stamp is only ever written after a pass, and any input change - a source
edit, a new file, a toolchain upgrade - changes the fingerprint, so a gate is
never skipped for inputs it has not checked. CI and fresh clones have no
stamps and run everything. SRO_CHECK_FORCE=1 ignores stamps.

Inputs prefixed with `@` are commands whose output joins the fingerprint
(for example `@go,version`: the command and its arguments, comma-separated). Inputs prefixed with `!` are ignored-by-git
files that still count (for example the generated asset manifest).

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
fingerprint

Tracked files contribute their index blob ids (`git ls-files -s`); files
modified in the working tree and untracked, non-ignored files contribute
their current content. That makes the fingerprint exact without re-hashing
the whole tree on every run.
================
*/
function fingerprint( inputs ) {
	const hash = createHash( "sha256" );
	const paths = inputs.filter( ( input ) => !input.startsWith( "@" ) && !input.startsWith( "!" ) );

	if ( paths.length > 0 ) {
		for ( const line of git( [ "ls-files", "-s", "--", ...paths ] ) ) {
			hash.update( `${line}\n` );
		}
		const changed = new Set( [
			...git( [ "diff", "--name-only", "--", ...paths ] ),
			...git( [ "ls-files", "-o", "--exclude-standard", "--", ...paths ] )
		] );
		for ( const file of [ ...changed ].sort() ) {
			const absolute = path.join( rebuildRoot, file );
			hash.update( `changed ${file}\n` );
			hash.update( existsSync( absolute ) ? readFileSync( absolute ) : "<deleted>" );
		}
	}
	for ( const input of inputs ) {
		if ( input.startsWith( "@" ) ) {
			const [command, ...args] = input.slice( 1 ).split( "," );
			const result = spawnSync( command, args, {
				windowsHide: true,
				cwd: rebuildRoot,
				encoding: "utf8",
				shell: false
			} );
			hash.update( `command ${input}\n${result.stdout ?? ""}${result.stderr ?? ""}` );
		} else if ( input.startsWith( "!" ) ) {
			const absolute = path.join( rebuildRoot, input.slice( 1 ) );
			hash.update( `extra ${input}\n` );
			hash.update( existsSync( absolute ) ? readFileSync( absolute ) : "<missing>" );
		}
	}
	return hash.digest( "hex" );
}

/*
================
main
================
*/
async function main() {
	const separator = process.argv.indexOf( "--" );
	if ( separator < 4 ) {
		throw new Error( "usage: run_if_changed.mjs <name> <input>... -- <command> [args...]" );
	}
	const name = process.argv[2];
	const inputs = process.argv.slice( 3, separator );
	const [command, ...args] = process.argv.slice( separator + 1 );
	const stampPath = path.join( stampDirectory, `${name}.json` );
	const started = performance.now();
	const key = fingerprint( inputs );

	/** @type {Array<{ fingerprint: string, passedAt: string }>} */
	const passes = existsSync( stampPath ) ? [ JSON.parse( readFileSync( stampPath, "utf8" ) ) ].flat() : [];
	if ( process.env.SRO_CHECK_FORCE !== "1" ) {
		const stamp = passes.find( ( pass ) => pass.fingerprint === key );
		if ( stamp ) {
			const seconds = ((performance.now() - started) / 1000).toFixed( 1 );
			process.stdout.write(
				`${name}: up to date (inputs unchanged since the pass at ${stamp.passedAt}; checked in ${seconds}s)\n`
			);
			return;
		}
	}

	const child = spawn( command, args, {
		windowsHide: true,
		cwd: rebuildRoot,
		stdio: "inherit",
		shell: process.platform === "win32"
	} );
	const code = await new Promise( ( resolve ) => child.on( "close", resolve ) );
	if ( code !== 0 ) {
		process.exit( code ?? 1 );
	}
	mkdirSync( stampDirectory, { recursive: true } );
	// Keep the most recent passes, so reverting an edit or switching back to a
	// branch that already passed is a skip too.
	const recent = [
		{ fingerprint: key, passedAt: new Date().toISOString() },
		...passes.filter( ( pass ) => pass.fingerprint !== key )
	].slice( 0, RECENT_PASSES );
	writeFileSync( stampPath, `${JSON.stringify( recent )}\n` );
}

await main();
