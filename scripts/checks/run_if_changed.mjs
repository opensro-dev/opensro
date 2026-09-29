/*
===========================================================================

run_if_changed.mjs - skip a gate whose inputs have not changed since it passed

	node scripts/checks/run_if_changed.mjs <name> <input>... -- <command> [args...]

Computes a fingerprint of everything the gate reads - the listed files and
directories (tracked and untracked, git-ignored files excluded), plus any
extra key such as a toolchain version - and compares it with the stamps of the
gate's recent passes (checkStamps.mjs owns both). A match means the same
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

import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { findPass, fingerprint, recordPass } from "./checkStamps.mjs";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", ".." );

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
	const started = performance.now();
	const key = fingerprint( inputs );
	const stamp = findPass( name, key );
	if ( stamp ) {
		const seconds = ((performance.now() - started) / 1000).toFixed( 1 );
		process.stdout.write(
			`${name}: up to date (inputs unchanged since the pass at ${stamp.passedAt}; checked in ${seconds}s)\n`
		);
		return;
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
	recordPass( name, key );
}

await main();
