/*
===========================================================================

runner.mjs - task dependency traversal and command lifetime

Resolves registered tasks, rejects dependency cycles and propagates command
failures. Children inherit the task console; background invocation must not
create a new Windows console window.

===========================================================================
*/

import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { getTask } from "./registry.mjs";
import { resolveProcessCommand } from "./processCommand.mjs";
import { runSuite } from "../test/runSuite.mjs";
import { assertRealNodeModules } from "./workspaceGuard.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", ".." );
const pipelineRunner = path.join( rebuildRoot, "scripts", "checks", "run_check_pipeline.mjs" );

/*
================
runTask
================
*/
export async function runTask( name, forwardedArgs = [], stack = [] ) {
	// Once, before the outermost task: never let pnpm work through a link.
	if ( stack.length === 0 ) assertRealNodeModules();
	const task = getTask( name );
	if ( !task ) {
		throw new Error( `Unknown task "${name}"` );
	}
	if ( stack.includes( task.name ) ) {
		throw new Error( `Task dependency cycle: ${[ ...stack, task.name ].join( " -> " )}` );
	}

	const nextStack = [ ...stack, task.name ];
	switch ( task.run.type ) {
		case "command":
			await runCommand( task.run.command, [ ...task.run.args, ...normalizeForwardedArgs( forwardedArgs ) ] );
			return;
		case "suite":
			await runSuite( task.run.suite, forwardedArgs );
			return;
		case "series":
			if ( forwardedArgs.length > 0 ) {
				throw new Error( `Task ${task.name} is a series and does not accept forwarded arguments` );
			}
			for ( const dependency of task.run.tasks ) {
				await runTask( dependency, [], nextStack );
			}
			return;
		case "pipeline":
			if ( forwardedArgs.length > 0 ) {
				throw new Error( `Task ${task.name} is a pipeline and does not accept forwarded arguments` );
			}
			await runCommand( "node", [ pipelineRunner, task.run.pipeline ] );
			return;
		default:
			throw new Error( `Task ${task.name} has an unsupported execution definition` );
	}
}

/*
================
normalizeForwardedArgs
================
*/
function normalizeForwardedArgs( args ) {
	return args[0] === "--" ? args.slice( 1 ) : args;
}

/*
================
runCommand
================
*/
function runCommand( command, args ) {
	const resolved = resolveProcessCommand( command, args );
	return new Promise( ( resolve, reject ) => {
		const child = spawn( resolved.executable, resolved.args, {
			windowsHide: true,
			cwd: rebuildRoot,
			env: process.env,
			stdio: "inherit",
			shell: false
		} );
		child.on( "error", reject );
		child.on( "close", ( code, signal ) => {
			if ( code === 0 ) {
				resolve( undefined );
				return;
			}
			reject( new Error( `${command} failed${signal ? ` with signal ${signal}` : ` with exit ${code}`}` ) );
		} );
	} );
}
