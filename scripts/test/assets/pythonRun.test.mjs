import assert from "node:assert/strict";
import { test } from "node:test";

import { classifyPythonFailure, describeHeldFile } from "../../build/shared/pythonRun.mjs";

// classifyPythonFailure is the load-bearing mechanism of the 2026-07-29 fix
// (see pythonRun.mjs's header): a handful of regexes plus Python-repr
// unescaping decide, from raw stderr, whether a failed interpreter attempt is
// "no-interpreter", "missing-module", "operation" or "unrecognized" - and
// THAT decision is what makes runPython throw the real cause immediately
// instead of burying it behind an interpreter fallback. Regex text
// classification rots silently, and its whole purpose is to be right on the
// rare bad day someone is already misdiagnosing, so every branch is pinned
// here against realistic captured stderr shapes.

/**
 * A finished, failed interpreter attempt in the shape runPythonAttempt
 * produces (see the PythonAttemptResult typedef in pythonRun.mjs).
 * @param {string} stderr
 * @param {{ exitCode?: number | null, spawnError?: Error | null, stdout?: string }} [overrides]
 * @returns {import("../../build/shared/pythonRun.mjs").PythonAttemptResult}
 */
function failedAttempt( stderr, { exitCode = 1, spawnError = null, stdout = "" } = {} ) {
	return {
		command: "py",
		args: [ "-3", "-c", "<payload>" ],
		spawnError,
		exitCode,
		stdout,
		stderr
	};
}

// The real incident's stderr shape: `py -3` with a WORKING fontTools dies at
// the final save because the Windows Font Cache Service holds the .ttf open.
// Paths in the exception line are Python reprs, so backslashes arrive
// doubled; String.raw keeps them doubled in the fixture exactly as captured.
const FONT_SAVE_PERMISSION_STDERR = [
	"Traceback (most recent call last):",
	String.raw`  File "<string>", line 42, in <module>`,
	String.raw`  File "C:\\Python313\\Lib\\site-packages\\fontTools\\ttLib\\ttFont.py", line 213, in save`,
	'    with open(file, "wb") as f:',
	String
		.raw`PermissionError: [Errno 13] Permission denied: 'H:\\sro\\rebuild\\apps\\client\\public\\assets\\fonts\\sro-default.ttf'`
].join( "\n" );

test("the incident shape - PermissionError under a <string> traceback - classifies operation with the held path unescaped", () => {
	const verdict = classifyPythonFailure( failedAttempt( FONT_SAVE_PERMISSION_STDERR ) );
	assert.equal( verdict.kind, "operation" );
	assert.match( verdict.summary, /PermissionError: \[Errno 13\] Permission denied/ );
	assert.ok( verdict.permission !== null, "a permission/lock death must carry permission evidence" );
	assert.equal(
		verdict.permission.path,
		String.raw`H:\sro\rebuild\apps\client\public\assets\fonts\sro-default.ttf`,
		"the Python-repr doubled backslashes must be unescaped into a usable Windows path"
	);
	assert.match( verdict.permission.line, /^PermissionError/ );
});

test("a [WinError 5] denial classifies operation and extracts the held path", () => {
	const stderr = [
		"Traceback (most recent call last):",
		String.raw`  File "<string>", line 7, in <module>`,
		String.raw`PermissionError: [WinError 5] Access is denied: 'H:\\sro\\rebuild\\temp\\glyph-atlas.bin'`
	].join( "\n" );
	const verdict = classifyPythonFailure( failedAttempt( stderr ) );
	assert.equal( verdict.kind, "operation" );
	assert.ok( verdict.permission !== null );
	assert.equal( verdict.permission.path, String.raw`H:\sro\rebuild\temp\glyph-atlas.bin` );
});

test("EACCES and EBUSY denial lines classify operation with permission evidence", () => {
	for (
		const lastLine of [
			String.raw`OSError: EACCES: permission denied, open 'H:\\sro\\rebuild\\temp\\pack.dat'`,
			String.raw`OSError: [Errno 16] EBUSY: resource busy or locked: 'H:\\sro\\rebuild\\temp\\pack.dat'`
		]
	) {
		const stderr = [
			"Traceback (most recent call last):",
			String.raw`  File "<string>", line 3, in <module>`,
			lastLine
		].join( "\n" );
		const verdict = classifyPythonFailure( failedAttempt( stderr ) );
		assert.equal( verdict.kind, "operation", lastLine );
		assert.ok( verdict.permission !== null, `${lastLine} must be recognized as a permission/lock line` );
		assert.equal( verdict.permission.path, String.raw`H:\sro\rebuild\temp\pack.dat` );
	}
});

test("ModuleNotFoundError as the last line classifies missing-module", () => {
	const stderr = [
		"Traceback (most recent call last):",
		String.raw`  File "<string>", line 1, in <module>`,
		"ModuleNotFoundError: No module named 'fontTools'"
	].join( "\n" );
	const verdict = classifyPythonFailure( failedAttempt( stderr ) );
	assert.equal( verdict.kind, "missing-module" );
	assert.match( verdict.summary, /No module named 'fontTools'/ );
	assert.equal( verdict.permission, null );
});

// THE PRECEDENCE CASE THAT FIXES THE ORIGINAL BUG. The incident's whole point:
// stderr that carries BOTH module-ish noise AND an operation-failure traceback
// is an operation failure. Python's chained-exception format produces exactly
// this shape (an optional import fails, the code recovers, then the real work
// dies), and classifying it "missing-module" would re-open the interpreter
// fallback that buried the file lock in the first place.
test("PRECEDENCE: an operation traceback with module noise above it classifies operation, never missing-module", () => {
	const stderr = [
		"Traceback (most recent call last):",
		String.raw`  File "<string>", line 3, in <module>`,
		"ModuleNotFoundError: No module named 'brotli'",
		"",
		"During handling of the above exception, another exception occurred:",
		"",
		"Traceback (most recent call last):",
		String.raw`  File "<string>", line 9, in <module>`,
		String.raw`  File "C:\\Python313\\Lib\\site-packages\\fontTools\\ttLib\\ttFont.py", line 213, in save`,
		String
			.raw`PermissionError: [Errno 13] Permission denied: 'H:\\sro\\rebuild\\apps\\client\\public\\assets\\fonts\\sro-default.ttf'`
	].join( "\n" );
	const verdict = classifyPythonFailure( failedAttempt( stderr ) );
	assert.notEqual( verdict.kind, "missing-module", "module noise above the real failure must not win" );
	assert.equal( verdict.kind, "operation" );
	assert.ok( verdict.permission !== null );
	assert.equal(
		verdict.permission.path,
		String.raw`H:\sro\rebuild\apps\client\public\assets\fonts\sro-default.ttf`
	);
});

// THE #2711 SHAPE. A unittest discovery run under an interpreter without
// the build modules prints one import traceback per test module and ends
// with unittest's own summary, so the missing module is never the last
// line - a last-line-only rule reads this as an operation failure and the
// fallback to a provisioned interpreter never happens (check.yml's
// check:client-preparation, 2026-11-07). Warnings the interpreter prints
// (ResourceWarning) are not exceptions and must not count as the payload
// having run.
test("a discovery run that only fails to import classifies missing-module", () => {
	const stderr = [
		"EEE",
		"======================================================================",
		"ERROR: test_pk2_reader (unittest.loader._FailedTest.test_pk2_reader)",
		"----------------------------------------------------------------------",
		"ImportError: Failed to import test module: test_pk2_reader",
		"Traceback (most recent call last):",
		String.raw`  File "D:\Tools\msys64\mingw64\lib\python3.12\unittest\loader.py", line 396, in _find_test_path`,
		String.raw`  File "H:\sro\rebuild\scripts\test\python\test_pk2_reader.py", line 11, in <module>`,
		"    from Crypto.Cipher import Blowfish",
		"ModuleNotFoundError: No module named 'Crypto'",
		"",
		"ResourceWarning: unclosed file <_io.TextIOWrapper name=5 encoding='cp1252'>",
		"----------------------------------------------------------------------",
		"Ran 115 tests in 79.590s",
		"",
		"FAILED (errors=23)"
	].join( "\n" );
	const verdict = classifyPythonFailure( failedAttempt( stderr ) );
	assert.equal( verdict.kind, "missing-module" );
	assert.match( verdict.summary, /No module named 'Crypto'/ );
	assert.equal( verdict.permission, null );
});

// The boundary: import failures AND a real test failure. The payload ran,
// so its failure is authoritative and no interpreter fallback may follow.
test("import failures beside a real test failure classify operation", () => {
	const stderr = [
		"ImportError: Failed to import test module: test_pk2_reader",
		"ModuleNotFoundError: No module named 'Crypto'",
		"======================================================================",
		"FAIL: test_round_trip (test_archive.TestArchive.test_round_trip)",
		"----------------------------------------------------------------------",
		"Traceback (most recent call last):",
		String.raw`  File "H:\sro\rebuild\scripts\test\python\test_archive.py", line 88, in test_round_trip`,
		"AssertionError: 4 != 5",
		"----------------------------------------------------------------------",
		"Ran 40 tests in 3.1s",
		"",
		"FAILED (failures=1, errors=1)"
	].join( "\n" );
	const verdict = classifyPythonFailure( failedAttempt( stderr ) );
	assert.equal( verdict.kind, "operation" );
	assert.equal( verdict.permission, null );
});

test("a spawn error classifies no-interpreter regardless of stderr", () => {
	const verdict = classifyPythonFailure(
		failedAttempt( "", { exitCode: null, spawnError: new Error( "spawn py ENOENT" ) } )
	);
	assert.equal( verdict.kind, "no-interpreter" );
	assert.match( verdict.summary, /spawn py ENOENT/ );
});

test("each Windows no-Python-behind-this-command stderr shape classifies no-interpreter", () => {
	const shapes = [
		{
			label: "Microsoft Store alias stub",
			stderr: "Python was not found; run without arguments to install from the Microsoft Store, " +
				"or disable this shortcut from Settings > Apps > Advanced app settings > App execution aliases.",
			exitCode: 9009
		},
		{
			label: "py launcher version miss",
			stderr: "Requested Python version (3) not installed, use -0 for available pythons",
			exitCode: 103
		},
		{
			label: "cmd not-recognized",
			stderr: "'python' is not recognized as an internal or external command,\noperable program or batch file.",
			exitCode: 1
		}
	];
	for ( const shape of shapes ) {
		const verdict = classifyPythonFailure( failedAttempt( shape.stderr, { exitCode: shape.exitCode } ) );
		assert.equal( verdict.kind, "no-interpreter", shape.label );
		assert.match( verdict.summary, /^interpreter did not run/, shape.label );
	}
});

test("an unclassifiable non-zero exit is an operation failure and cannot trigger interpreter fallback", () => {
	const stderr =
		"usage: buildfont.py [-h] --glyphs GLYPHS\nbuildfont.py: error: the following arguments are required: --glyphs";
	const verdict = classifyPythonFailure( failedAttempt( stderr, { exitCode: 2 } ) );
	assert.equal( verdict.kind, "operation" );
	assert.match( verdict.summary, /launched payload exited 2/ );
	assert.equal( verdict.permission, null );
});

test("a non-zero audit that reports on stdout classifies as an operation failure", () => {
	const verdict = classifyPythonFailure(
		failedAttempt( "", {
			stdout: "== proof audit ==\n[error] sub_123456: exact graph mismatch\n== gate: FAIL ==\n"
		} )
	);
	assert.equal( verdict.kind, "operation" );
	assert.match( verdict.summary, /proof audit/ );
	assert.equal( verdict.permission, null );
});

test("signal death after launch is an operation failure rather than an interpreter fallback", () => {
	const verdict = classifyPythonFailure( failedAttempt( "", { exitCode: null } ) );
	assert.equal( verdict.kind, "operation" );
	assert.match( verdict.summary, /launched payload died from a signal \(no diagnostic output\)/ );
});

test("describeHeldFile names the Font Cache Service for font files only", () => {
	for (
		const fontPath of [
			String.raw`H:\sro\assets\fonts\sro-default.ttf`,
			String.raw`H:\sro\assets\fonts\sro-default.TTC`,
			String.raw`H:\sro\assets\fonts\sro-default.otf`,
			String.raw`H:\sro\assets\fonts\sro-default.ttf.tmp`
		]
	) {
		assert.match( describeHeldFile( fontPath ), /Font Cache Service/, fontPath );
	}
	for (
		const otherPath of [
			String.raw`H:\sro\assets\atlas.png`,
			String.raw`H:\sro\assets\pack.json`,
			String.raw`H:\sro\assets\fonts.zip`
		]
	) {
		assert.doesNotMatch( describeHeldFile( otherPath ), /Font Cache Service/, otherPath );
	}
});
