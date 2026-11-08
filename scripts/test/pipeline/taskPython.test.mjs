/*
===========================================================================

taskPython.test.mjs - a python task runs through the interpreter candidates

A task declared with `command: "python"` must never spawn the first python
on PATH: on the dev workstation that is MSYS2's interpreter, which lacks
the build modules, so the check gates failed at import time without
testing anything (#2711). The runner plans such a task as a pythonRun run
(SRO_PYTHON, then py -3 / python3, then python, with fallback only on
environment-class failures); every other command plans as one direct spawn.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { pythonAttempts, runPython } from "../../build/shared/pythonRun.mjs";
import { commandPlan } from "../../tasks/runner.mjs";
import { listTasks } from "../../tasks/registry.mjs";

// The registry's python tasks: the four python checks and client extraction.
const PYTHON_TASKS = [
	"assets:prepare",
	"check:client-preparation",
	"check:compiled-quests",
	"check:loot-catalog",
	"check:pet-design"
];

/*
================
withPlatform

Runs body with a temporary platform and SRO_PYTHON, restoring both.
================
*/
function withPlatform( platform, sroPython, body ) {
	const original = Object.getOwnPropertyDescriptor( process, "platform" );
	const previous = process.env.SRO_PYTHON;
	Object.defineProperty( process, "platform", { value: platform } );
	if ( sroPython === undefined ) delete process.env.SRO_PYTHON;
	else process.env.SRO_PYTHON = sroPython;
	try {
		return body();
	} finally {
		Object.defineProperty( process, "platform", original );
		if ( previous === undefined ) delete process.env.SRO_PYTHON;
		else process.env.SRO_PYTHON = previous;
	}
}

test("every python task plans as a candidate run, never a bare PATH spawn", () => {
	const pythonTasks = listTasks( { includeInternal: true } ).filter( ( task ) =>
		task.run.type === "command" && task.run.command === "python"
	);
	assert.deepEqual( pythonTasks.map( ( task ) => task.name ).sort(), PYTHON_TASKS );
	for ( const task of pythonTasks ) {
		const plan = commandPlan( task.run.command, [ ...task.run.args ] );
		assert.equal( plan.type, "python", task.name );
		assert.deepEqual( plan.args, task.run.args, task.name );
	}
});

test("the bare python is the last candidate, an explicit SRO_PYTHON the first", () => {
	for ( const platform of [ "win32", "darwin", "linux" ] ) {
		const candidates = withPlatform( platform, undefined, () => pythonAttempts( [ "probe.py" ] ) );
		assert.equal( candidates[candidates.length - 1].command, "python", platform );
		const explicit = withPlatform( platform, "/venv/bin/python", () => pythonAttempts( [ "probe.py" ] ) );
		assert.equal( explicit[0].command, "/venv/bin/python", platform );
	}
});

test("node and other commands still resolve to one direct spawn", () => {
	const plan = commandPlan( "node", [ "scripts/task.mjs" ] );
	assert.equal( plan.type, "spawn" );
	assert.equal( plan.executable, process.execPath );
	assert.deepEqual( plan.args, [ "scripts/task.mjs" ] );
});

test("the winning candidate's output comes back (node as the stand-in interpreter)", async () => {
	const previous = process.env.SRO_PYTHON;
	process.env.SRO_PYTHON = process.execPath;
	try {
		const result = await runPython( [ "-e", "process.stdout.write( 'candidate ran' )" ], { task: "test probe" } );
		assert.equal( result.command, "SRO_PYTHON" );
		assert.equal( result.stdout, "candidate ran" );
	} finally {
		if ( previous === undefined ) delete process.env.SRO_PYTHON;
		else process.env.SRO_PYTHON = previous;
	}
});
