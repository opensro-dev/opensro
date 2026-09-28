/*
===========================================================================

runSuite.mjs - test-suite selection and child process ownership

Resolves registered suites and forwards selected tests to their runner. Child
output stays on the invoking console and exit status remains authoritative.

===========================================================================
*/

import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { getSuite, SUITES } from "./suites/index.mjs";
import { resolveProcessCommand } from "../tasks/processCommand.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", ".." );

/*
================
runSuite
================
*/
export async function runSuite( name, forwardedArgs = [] ) {
	const suite = getSuite( name );
	if ( !suite ) {
		throw new Error( `Unknown test suite "${name}". Available: ${Object.keys( SUITES ).join( ", " )}` );
	}

	const stages = suite.stages ?? [ suite ];
	for ( const stage of stages ) {
		await runStage( stage, forwardedArgs );
	}
}

/*
================
runStage
================
*/
function runStage( stage, forwardedArgs ) {
	const passThrough = forwardedArgs[0] === "--" ? forwardedArgs.slice( 1 ) : forwardedArgs;
	const isTsx = stage.runner === "tsx";
	if ( stage.serial && passThrough.some( ( argument ) => /^--test-concurrency(?:=|$)/.test( argument ) ) ) {
		throw new Error( "Serial test suites own --test-concurrency=1; callers cannot override it." );
	}
	if ( !isTsx && stage.tsconfig ) {
		throw new Error( "Test suite tsconfig is supported only by the tsx runner." );
	}
	const setupArgs = (stage.setupFiles ?? []).flatMap( ( file ) => [
		"--import",
		file.startsWith( "." ) || path.isAbsolute( file ) ? file : `./${file}`
	] );
	const executionPolicyArgs = stage.serial ? [ "--test-concurrency=1" ] : [];
	const runnerConfigArgs = stage.tsconfig ? [ "--tsconfig", `./${stage.tsconfig}` ] : [];
	const command = isTsx ? "pnpm" : "node";
	const args = isTsx ?
		[
			"exec",
			"tsx",
			...runnerConfigArgs,
			"--test",
			...setupArgs,
			...executionPolicyArgs,
			...passThrough,
			...stage.files
		] :
		[
			...(stage.stripTypes ? [ "--experimental-strip-types" ] : []),
			...setupArgs,
			"--test",
			...executionPolicyArgs,
			...passThrough,
			...stage.files
		];

	const resolved = resolveProcessCommand( command, args );
	return new Promise( ( resolve, reject ) => {
		const child = spawn( resolved.executable, resolved.args, {
			windowsHide: true,
			cwd: rebuildRoot,
			env: { ...process.env, ...(stage.env ?? {}) },
			stdio: "inherit",
			shell: false
		} );
		child.on( "error", reject );
		child.on( "close", ( code, signal ) => {
			if ( code === 0 ) {
				resolve( undefined );
				return;
			}
			reject(
				new Error(
					`Test suite stage ${stage.runner} failed${signal ? ` with signal ${signal}` : ` with exit ${code}`}`
				)
			);
		} );
	} );
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) {
	const [name, ...forwardedArgs] = process.argv.slice( 2 );
	if ( !name ) {
		console.error( `Usage: node scripts/test/runSuite.mjs <${Object.keys( SUITES ).join( "|" )}> [test options]` );
		process.exitCode = 2;
	} else {
		runSuite( name, forwardedArgs ).catch( ( error ) => {
			console.error( error instanceof Error ? error.message : error );
			process.exitCode = 1;
		} );
	}
}
