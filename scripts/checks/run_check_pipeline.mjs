/*
===========================================================================

run_check_pipeline.mjs - bounded concurrent verification

Owns task scheduling, captured output, progress and failure cleanup. Each
child belongs to this pipeline; background checks do not open desktop
console windows.

===========================================================================
*/

import { spawn } from "node:child_process";
import { mkdirSync, openSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { getPipeline, getTask } from "../tasks/registry.mjs";

// The `pnpm check` gate, as a dependency graph instead of a && chain.
//
// The old chain ran fourteen independent gates strictly one after another, so
// its wall time was the SUM of every step. Almost nothing in it actually
// depends on anything else: only the suites that read generated public assets
// have to wait for assets:build:full, and the recursive package typecheck orders
// itself (pnpm -r is topological). Everything else runs concurrently here, so
// the wall time collapses to the critical path: assets:build:full plus the
// slowest asset-reading suite.
//
// Task commands and metadata live in scripts/tasks/. This scheduler only names
// WHICH registry tasks a pipeline runs and what they wait on.

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", ".." );
const progressLogPath = path.join( rebuildRoot, ".state", "check-pipeline.log" );
const taskCliPath = path.join( rebuildRoot, "scripts", "task.mjs" );

/*
Every task the gate runs. `task` names a first-party registry task and `after`
lists task ids that must PASS first.

assets:build:full dependents: everything that reads .generated/client-public/assets
(or must not read it mid-write). check:fidelity and the typecheck read only
sources and docs, so they overlap the build.

test-harness runs the re-harness parity suite (432 files at this revision):
bridge is the most-edited code in the tree and typecheck alone cannot
catch a behavioral parity regression, so the suite is gated here rather than
left to whoever remembers to run it. Its own registration guard keeps every
harness test file reachable from that package's `test` script.

test-client runs the client vitest component suite. It imports only source
modules (no public/assets), so it does not wait on assets:build:full.

test-probes runs the GRADUATED probes: the curated, by-name list in the
test:probes registry task. A probe graduates when it is deterministic,
fast, fully unattended (no dev server, no browser, no game session) and
asserts against live source other lanes churn - at which point leaving it
ungated means only a manual audit catches a regression, which is how two
probes were born carrying copied legacy debt on 2026-07-29. Scratchpad probes
stay ungated on purpose (a probe's first days want freedom); to graduate one,
append it to the test:probes chain. The three current graduates bundle live
source in-process with esbuild and read no public/assets, so the task does
not wait on assets:build:full.
*/

/**
 * One task as declared in a named check pipeline.
 * @typedef {object} CheckPipelineTaskDefinition
 * @property {string} id unique task id, referenced by `after` edges and reports
 * @property {string} task a canonical first-party registry task
 * @property {string[]} after ids of tasks that must pass before this one starts
 */

/**
 * A definition loaded into the scheduler, carrying its runtime state.
 * `startedAt` is written by startTask before anything reads it (the heartbeat
 * and the close handler only look at tasks that have started), so it is typed
 * as always-present rather than optional.
 * @typedef {CheckPipelineTaskDefinition & {
 *   commandLine: string,
 *   state: "pending" | "running" | "passed" | "failed" | "killed",
 *   output: string,
 *   elapsedMs: number,
 *   startedAt: number,
 *   child: import("node:child_process").ChildProcess | null,
 *   killRequested: boolean
 * }} PipelineTask
 */

/** @type {CheckPipelineTaskDefinition[]} */
export const CHECK_PIPELINE_TASKS = getPipeline( "full" );

// Heartbeat cadence: the tick fires often, but a line is only emitted after
// the pipeline has printed nothing at all for HEARTBEAT_QUIET_MS. Plain
// single lines, no ANSI - the output is routinely piped to files.
const HEARTBEAT_TICK_MS = 5000;
const HEARTBEAT_QUIET_MS = 15000;

// Tasks that must never be fail-fast killed. Killing assets:build:full mid-write
// can leave generated assets and the rebuild lock in a bad state, so on
// failure the pipeline waits for it instead.
const NEVER_KILL = new Set( [ "build-resources" ] );

/*
================
main
================
*/
function main( pipelineName = "full" ) {
	const startedAt = performance.now();
	const jobs = resolveJobLimit();
	// The full pipeline builds and checks against the licensed game data, so
	// a test that needs it must fail - not skip - when it is missing
	// (apps/server/internal/testsupport/licensed).
	if ( pipelineName === "full" ) process.env.SRO_REQUIRE_GAME_DATA = "1";
	const definitions = getPipeline( pipelineName );
	if ( !definitions ) {
		throw new Error( `Unknown check pipeline "${pipelineName}"` );
	}
	mkdirSync( path.dirname( progressLogPath ), { recursive: true } );
	const progressLogFd = openSync( progressLogPath, "w" );
	/** @type {Map<string, PipelineTask>} */
	const tasks = new Map();

	for ( const definition of definitions ) {
		// Cast: the literal omits startedAt, which startTask assigns before any read.
		tasks.set(
			definition.id,
			/** @type {PipelineTask} */ ({
				...definition,
				commandLine: `node scripts/task.mjs run ${definition.task}`,
				state: "pending",
				output: "",
				elapsedMs: 0,
				child: null,
				killRequested: false
			})
		);
	}

	validatePipeline( tasks );

	let running = 0;
	let failed = false;
	let lastOutputAt = performance.now();

	/** @param {string} text */
	/*
  ================
  emitTerminal
  ================
  */
	const emitTerminal = ( text ) => {
		lastOutputAt = performance.now();
		process.stdout.write( text );
	};

	/** @param {string} text */
	/*
  ================
  emit
  ================
  */
	const emit = ( text ) => {
		emitTerminal( text );
		writeFileSync( progressLogFd, text );
	};

	emit(
		`check pipeline ${pipelineName}: ${tasks.size} tasks, ${jobs} concurrent (SRO_CHECK_JOBS to change)\n` +
			`check pipeline: live output -> ${path.relative( rebuildRoot, progressLogPath )}\n`
	);

	const heartbeat = setInterval( () => {
		const now = performance.now();
		if ( now - lastOutputAt < HEARTBEAT_QUIET_MS ) {
			return;
		}
		const live = [ ...tasks.values() ].filter( ( task ) => task.state === "running" && !task.killRequested );
		if ( live.length === 0 ) {
			return;
		}
		if ( failed ) {
			emit( `[wait ] letting ${live.map( ( task ) => task.id ).join( ", " )} finish before exiting\n` );
			return;
		}
		const queued = [ ...tasks.values() ].filter( ( task ) => task.state === "pending" ).length;
		const ages = live
			.sort( ( left, right ) => left.startedAt - right.startedAt )
			.map( ( task ) => `${task.id} ${Math.round( (now - task.startedAt) / 1000 )}s` );
		emit( `[wait ] ${ages.join( ", " )} (${live.length} running, ${queued} queued)\n` );
	}, HEARTBEAT_TICK_MS );
	heartbeat.unref();

	/*
  ================
  schedule
  ================
  */
	const schedule = () => {
		if ( failed ) {
			return;
		}
		for ( const task of tasks.values() ) {
			if ( running >= jobs ) {
				return;
			}
			// Cast: validatePipeline proved every `after` id names a defined task.
			if (
				task.state !== "pending" ||
				!task.after.every( ( id ) => /** @type {PipelineTask} */ (tasks.get( id )).state === "passed" )
			) {
				continue;
			}
			running += 1;
			startTask( task );
		}
	};

	// Fail-fast teardown: kill the process TREE of every killable running task.
	// child.pid is the wrapper shell, so on Windows taskkill needs /T to reach
	// the real workers. Idempotent, and guarded against a child that exited
	// between the failure and the kill (its close handler settles it normally).
	/*
  ================
  killRunningTasks
  ================
  */
	const killRunningTasks = () => {
		for ( const task of tasks.values() ) {
			if ( task.state !== "running" || task.killRequested || NEVER_KILL.has( task.id ) ) {
				continue;
			}
			if ( !task.child || task.child.exitCode !== null ) {
				continue;
			}
			task.killRequested = true;
			try {
				if ( process.platform === "win32" ) {
					spawn( "taskkill", [ "/pid", String( task.child.pid ), "/T", "/F" ], {
						windowsHide: true,
						stdio: "ignore"
					} );
				} else {
					task.child.kill( "SIGKILL" );
				}
			} catch {
				// Already gone; the close event will still fire and settle the task.
			}
		}
	};

	/** @param {PipelineTask} task */
	/*
  ================
  startTask
  ================
  */
	const startTask = ( task ) => {
		task.state = "running";
		task.startedAt = performance.now();
		emit( `[start] ${task.id}  (${task.commandLine})\n` );

		const child = spawn( process.execPath, [ taskCliPath, "run", task.task ], {
			windowsHide: true,
			cwd: rebuildRoot,
			shell: false,
			env: process.env,
			stdio: [ "ignore", "pipe", "pipe" ]
		} );
		task.child = child;
		child.stdout.on( "data", ( chunk ) => {
			captureTaskOutput( task, chunk, progressLogFd );
		} );
		child.stderr.on( "data", ( chunk ) => {
			captureTaskOutput( task, chunk, progressLogFd );
		} );
		child.on( "close", ( code ) => {
			running -= 1;
			task.elapsedMs = performance.now() - task.startedAt;
			const seconds = (task.elapsedMs / 1000).toFixed( 1 );

			if ( code === 0 ) {
				task.state = "passed";
				emit( `[pass ] ${task.id} in ${seconds}s\n` );
				emitTerminal( indent( task.output ) );
			} else if ( task.killRequested ) {
				task.state = "killed";
				emit( `[kill ] ${task.id} in ${seconds}s (killed after another task failed)\n` );
				emitTerminal( indent( task.output ) );
			} else {
				task.state = "failed";
				failed = true;
				emit( `[FAIL ] ${task.id} in ${seconds}s (exit ${code})\n` );
				emitTerminal( indent( task.output ) );
				killRunningTasks();
			}

			if ( running === 0 && (failed || everySettled( tasks )) ) {
				clearInterval( heartbeat );
				finish( tasks, startedAt, emit );
				return;
			}
			schedule();
		} );
	};

	schedule();
}

/**
 * @param {Map<string, PipelineTask>} tasks
 * @param {number} startedAt performance.now() at pipeline start
 * @param {(text: string) => void} emit terminal + durable progress-log writer
 */
/*
================
finish
================
*/
function finish( tasks, startedAt, emit ) {
	const wallSeconds = ((performance.now() - startedAt) / 1000).toFixed( 1 );
	const summary = [ ...tasks.values() ]
		.filter( ( task ) => task.elapsedMs > 0 )
		.sort( ( left, right ) => right.elapsedMs - left.elapsedMs )
		.map( ( task ) =>
			`  ${(task.elapsedMs / 1000).toFixed( 1 ).padStart( 7 )}s  ${task.state.padEnd( 6 )}  ${task.id}`
		)
		.join( "\n" );

	emit( `\ncheck pipeline: task timings (slowest first)\n${summary}\n` );
	const outcome = summarizeCheckPipeline( tasks.values(), wallSeconds );
	emit( outcome.text );
	if ( !outcome.passed ) process.exitCode = 1;
}

/**
 * Render exactly one terminal pipeline verdict. Keeping this pure makes the
 * failure wording mutation-testable and prevents a failed run from printing a
 * second, contradictory PASS footer.
 *
 * @param {Iterable<{ id: string, state: PipelineTask["state"] }>} taskStates
 * @param {string} wallSeconds
 * @returns {{ passed: boolean, text: string }}
 */
/*
================
summarizeCheckPipeline
================
*/
export function summarizeCheckPipeline( taskStates, wallSeconds ) {
	const states = [ ...taskStates ];
	const failures = states.filter( ( task ) => task.state === "failed" );
	const killed = states.filter( ( task ) => task.state === "killed" );
	const blocked = states.filter( ( task ) => task.state === "pending" );

	if ( failures.length > 0 || killed.length > 0 ) {
		return {
			passed: false,
			text: `\ncheck pipeline: FAILED in ${wallSeconds}s - ` +
				`${failures.map( ( task ) => task.id ).join( ", " )} failed` +
				`${killed.length > 0 ? `; killed: ${killed.map( ( task ) => task.id ).join( ", " )}` : ""}` +
				`${blocked.length > 0 ? `; never started: ${blocked.map( ( task ) => task.id ).join( ", " )}` : ""}\n` +
				`Re-run one gate alone with: pnpm task run <task> (see [FAIL] output above)\n`
		};
	}
	return {
		passed: true,
		text: `\ncheck pipeline: PASSED, ${states.length} tasks in ${wallSeconds}s\n`
	};
}

/**
 * Keep the normal per-task capture used by the final report, while also
 * appending every child chunk to the durable live log. The explicit task
 * marker makes interleaved output from the concurrent scheduler attributable
 * without forcing noisy live output into the terminal.
 *
 * @param {PipelineTask} task
 * @param {Buffer|string} chunk
 * @param {number} progressLogFd
 */
/*
================
captureTaskOutput
================
*/
function captureTaskOutput( task, chunk, progressLogFd ) {
	const text = String( chunk );
	task.output += text;
	writeFileSync( progressLogFd, `[output:${task.id}]\n${text}` );
}

/*
Every `after` edge must name a defined task, every `task` must exist in the
registry, and the graph must be acyclic - a typo here would otherwise
deadlock the scheduler into a silent stall instead of failing.
*/
/** @param {Map<string, PipelineTask>} tasks */
/*
================
validatePipeline
================
*/
function validatePipeline( tasks ) {
	for ( const task of tasks.values() ) {
		if ( !getTask( task.task ) ) {
			throw new Error( `check pipeline: task ${task.id} names missing registry task "${task.task}"` );
		}
		for ( const dependency of task.after ) {
			if ( !tasks.has( dependency ) ) {
				throw new Error( `check pipeline: task ${task.id} waits on unknown task "${dependency}"` );
			}
		}
	}

	const settled = new Set();
	let progressed = true;
	while ( progressed ) {
		progressed = false;
		for ( const task of tasks.values() ) {
			if ( !settled.has( task.id ) && task.after.every( ( id ) => settled.has( id ) ) ) {
				settled.add( task.id );
				progressed = true;
			}
		}
	}
	if ( settled.size !== tasks.size ) {
		const stuck = [ ...tasks.keys() ].filter( ( id ) => !settled.has( id ) );
		throw new Error( `check pipeline: dependency cycle involving ${stuck.join( ", " )}` );
	}
}

/**
 * @param {Map<string, PipelineTask>} tasks
 * @returns {boolean}
 */
/*
================
everySettled
================
*/
function everySettled( tasks ) {
	return [ ...tasks.values() ].every(
		( task ) => task.state === "passed" || task.state === "failed" || task.state === "killed"
	);
}

/*
================
resolveJobLimit
================
*/
function resolveJobLimit() {
	const fromEnv = Number( process.env.SRO_CHECK_JOBS );
	if ( Number.isInteger( fromEnv ) && fromEnv > 0 ) {
		return fromEnv;
	}
	// Tasks fan out internally (node --test runs one process per file, tsgo is
	// multithreaded), so the outer limit stays modest to avoid oversubscription.
	return Math.max( 2, Math.min( 6, os.availableParallelism() - 2 ) );
}

/**
 * @param {string} output a task's captured stdout+stderr
 * @returns {string} the output indented four spaces, or "" when blank
 */
/*
================
indent
================
*/
function indent( output ) {
	const trimmed = output.replace( /\s+$/, "" );
	if ( trimmed === "" ) {
		return "";
	}
	return `${trimmed.split( "\n" ).map( ( line ) => `    ${line}` ).join( "\n" )}\n`;
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) {
	main( process.argv[2] ?? "full" );
}
