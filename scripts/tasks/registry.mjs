import { ASSET_TASKS } from "./assets.mjs";
import { BUILD_TASKS } from "./build.mjs";
import { CHECK_PIPELINES, CHECK_TASKS } from "./checks.mjs";
import { MISC_TASKS } from "./misc.mjs";
import { TEST_TASKS } from "./tests.mjs";
import { SUITES } from "../test/suites/index.mjs";
import { compareText } from "../build/shared/textOrder.mjs";

/** @typedef {import("./define.mjs").Task} Task */
/** @typedef {{ id: string, task: string, after: string[] }} PipelineEntry */

export const TASKS = Object.freeze( [
	...BUILD_TASKS,
	...ASSET_TASKS,
	...MISC_TASKS,
	...TEST_TASKS,
	...CHECK_TASKS
] );

/** @type {Map<string, Readonly<Task>>} */
const tasksByName = new Map();
for ( const task of TASKS ) {
	if ( tasksByName.has( task.name ) ) {
		throw new Error( `Duplicate task name: ${task.name}` );
	}
	tasksByName.set( task.name, task );
}

validateRegistry();

/** @param {string} name @returns {Readonly<Task> | undefined} */
export function getTask( name ) {
	return tasksByName.get( name );
}

/** @param {string} name @returns {string | undefined} */
export function resolveTaskName( name ) {
	return getTask( name )?.name;
}

/** @param {{ kind?: string, includeInternal?: boolean }} [options] */
export function listTasks( { kind, includeInternal = false } = {} ) {
	return TASKS.filter( ( task ) => (includeInternal || task.kind !== "internal") && (!kind || task.kind === kind) )
		.sort( ( left, right ) => compareText( left.name, right.name ) );
}

/** @param {string} name @returns {PipelineEntry[]} */
export function getPipeline( name ) {
	const pipeline = /** @type {Record<string, PipelineEntry[]>} */ (CHECK_PIPELINES)[name];
	if ( !pipeline ) throw new Error( `Unknown task pipeline: ${name}` );
	return pipeline;
}

export function validateRegistry() {
	for ( const task of TASKS ) {
		if ( task.run.type === "series" ) {
			for ( const dependency of task.run.tasks ) {
				if ( !tasksByName.has( dependency ) ) {
					throw new Error( `Task ${task.name} references unknown series task ${dependency}` );
				}
			}
		} else if ( task.run.type === "suite" && !Object.hasOwn( SUITES, task.run.suite ) ) {
			throw new Error( `Task ${task.name} references unknown test suite ${task.run.suite}` );
		} else if ( task.run.type === "pipeline" && !Object.hasOwn( CHECK_PIPELINES, task.run.pipeline ) ) {
			throw new Error( `Task ${task.name} references unknown pipeline ${task.run.pipeline}` );
		}
	}

	for ( const [pipelineName, entries] of Object.entries( CHECK_PIPELINES ) ) {
		for ( const entry of entries ) {
			if ( !tasksByName.has( entry.task ) ) {
				throw new Error( `Pipeline ${pipelineName} references unknown task ${entry.task}` );
			}
		}
	}
}
