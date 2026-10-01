/*
===========================================================================

pythonRun.test.mjs - interpreter candidates per platform

SRO_PYTHON always comes first; Windows keeps the py launcher, other hosts
(macOS, Linux) have none and try python3. `python` is the last resort.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { pythonAttempts } from "../../build/shared/pythonRun.mjs";

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

test("a plain python task command resolves per platform", async () => {
	const { resolveProcessCommand } = await import( "../../tasks/processCommand.mjs" );
	assert.equal(
		withEnvironment( "darwin", undefined, () => resolveProcessCommand( "python", [] ).executable ),
		"python3"
	);
	assert.equal(
		withEnvironment( "win32", undefined, () => resolveProcessCommand( "python", [] ).executable ),
		"python"
	);
	assert.equal(
		withEnvironment( "darwin", "/venv/bin/python", () => resolveProcessCommand( "python", [] ).executable ),
		"/venv/bin/python"
	);
});
