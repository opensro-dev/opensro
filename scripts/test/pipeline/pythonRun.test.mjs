/*
===========================================================================

pythonRun.test.mjs - interpreter candidates per platform

SRO_PYTHON always comes first; Windows keeps the py launcher, other hosts
(macOS, Linux) have none and try python3. `python` is the last resort.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { pythonAttempts, pythonExecutable } from "../../build/shared/pythonRun.mjs";

/*
================
withEnvironment

Runs body with a temporary platform and SRO_PYTHON, restoring both.
================
*/
function withEnvironment( platform, sroPython, body ) {
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

const labels = ( attempts ) => attempts.map( ( attempt ) => attempt.label );

test("Windows tries the py launcher, then python", () => {
	const attempts = withEnvironment( "win32", undefined, () => pythonAttempts( [ "x.py" ] ) );
	assert.deepEqual( labels( attempts ), [ "py -3", "python" ] );
	assert.deepEqual( attempts[0].args, [ "-3", "x.py" ] );
});

test("macOS and Linux try python3, then python", () => {
	for ( const platform of [ "darwin", "linux" ] ) {
		const attempts = withEnvironment( platform, undefined, () => pythonAttempts( [ "x.py" ] ) );
		assert.deepEqual( labels( attempts ), [ "python3", "python" ] );
		assert.deepEqual( attempts[0], { label: "python3", command: "python3", args: [ "x.py" ] } );
	}
});

test("SRO_PYTHON comes first on every platform", () => {
	for ( const platform of [ "win32", "darwin" ] ) {
		const attempts = withEnvironment( platform, "/venv/bin/python", () => pythonAttempts( [ "x.py" ] ) );
		assert.deepEqual( attempts[0], { label: "SRO_PYTHON", command: "/venv/bin/python", args: [ "x.py" ] } );
	}
});

test("pythonExecutable is the first pythonAttempts entry on every platform", () => {
	for ( const platform of [ "win32", "darwin", "linux" ] ) {
		for ( const sroPython of [ undefined, "/venv/bin/python" ] ) {
			const executable = withEnvironment( platform, sroPython, () => pythonExecutable() );
			const first = withEnvironment( platform, sroPython, () => pythonAttempts( [] )[0] );
			assert.deepEqual( executable, { command: first.command, args: first.args } );
		}
	}
});

test("a plain python task command resolves per platform", async () => {
	const { resolveProcessCommand } = await import( "../../tasks/processCommand.mjs" );
	assert.deepEqual(
		withEnvironment( "darwin", undefined, () => resolveProcessCommand( "python", [ "x.py" ] ) ),
		{ executable: "python3", args: [ "x.py" ] }
	);
	assert.deepEqual(
		withEnvironment( "win32", undefined, () => resolveProcessCommand( "python", [ "x.py" ] ) ),
		{ executable: "py", args: [ "-3", "x.py" ] }
	);
	assert.deepEqual(
		withEnvironment( "darwin", "/venv/bin/python", () => resolveProcessCommand( "python", [ "x.py" ] ) ),
		{ executable: "/venv/bin/python", args: [ "x.py" ] }
	);
});
