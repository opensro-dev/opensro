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
import { runPython } from "../build/shared/pythonRun.mjs";
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
		case "command": {
			const plan = commandPlan( task.run.command, [
				...task.run.args,
				...normalizeForwardedArgs( forwardedArgs )
			] );
			if ( plan.type === "python" ) {
				await runPythonCommand( task.name, plan.args );
				return;
			}
			await runCommand( plan.executable, plan.args, task.run.command );
			return;
		}
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
		case "pipeline": {
			if ( forwardedArgs.length > 0 ) {
				throw new Error( `Task ${task.name} is a pipeline and does not accept forwarded arguments` );
			}
			const plan = commandPlan( "node", [ pipelineRunner, task.run.pipeline ] );
			if ( plan.type !== "spawn" ) {
				throw new Error( "The check pipeline runner must spawn directly" );
			}
			await runCommand( plan.executable, plan.args, "node" );
			return;
		}
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
commandPlan

What running one command means. A "python" command never spawns the first
python on PATH: that interpreter may be an unprovisioned one (MSYS2's on
this workstation) whose missing modules say nothing about the tree under
test, so the task runs through pythonRun's candidates instead - SRO_PYTHON,
then py -3 / python3, then python - with fallback only when an interpreter
failed environmentally, never when the payload itself failed. Every other
command resolves to one direct spawn.
================
*/
export function commandPlan( command, args ) {
	if ( command === "python" ) {
		return { type: "python", args: [ ...args ] };
	}
	const resolved = resolveProcessCommand( command, args );
	return { type: "spawn", executable: resolved.executable, args: resolved.args };
}

/*
================
runPythonCommand

Run a python task through the house interpreter candidates (see
commandPlan) and replay its output: the streams are captured so a failed
attempt can be classified, then printed whole, so the operator still sees
everything the run produced.
================
*/
async function runPythonCommand( taskName, args ) {
	const result = await runPython( args, { task: taskName, cwd: rebuildRoot } );
	if ( result.stdout ) {
		process.stdout.write( result.stdout );
	}
	if ( result.stderr ) {
		process.stderr.write( result.stderr );
	}
}

/*
================
runCommand
================
*/
function runCommand( executable, args, label ) {
	return new Promise( ( resolve, reject ) => {
		const child = spawn( executable, args, {
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
			reject( new Error( `${label} failed${signal ? ` with signal ${signal}` : ` with exit ${code}`}` ) );
		} );
	} );
}
