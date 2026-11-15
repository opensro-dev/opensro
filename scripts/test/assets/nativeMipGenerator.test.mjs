/*
===========================================================================

nativeMipGenerator.test.mjs - which mip generator a host runs

A Windows host whose Direct3D device creation fails (a Remote Desktop
session or a GPU-less VM) stops with the probe's reason, unless the build
opted into the portable generator, which it then uses and announces; a host
that creates the device keeps D3DX; other systems never probe.

===========================================================================
*/
import test from "node:test";
import assert from "node:assert/strict";
import {
	chooseMipGenerator,
	D3DX,
	generatorCandidates,
	PORTABLE,
	PORTABLE_OPT_IN
} from "../../build/shared/nativeMipGenerator.mjs";

/*
================
host
================
*/
function host( platform, refusal, allowPortable = false ) {
	const calls = { probes: 0, warnings: [] };
	return {
		calls,
		platform,
		allowPortable,
		probe: async () => {
			calls.probes++;
			return refusal;
		},
		warn: message => calls.warnings.push( message )
	};
}

test("a Windows host whose device creation fails stops with the reason unless it opted in", async () => {
	const windows = host( "win32", "CreateDevice failed: HRESULT 0x8876086C" );
	await assert.rejects( chooseMipGenerator( windows ), error => {
		assert.match( error.message, /0x8876086C/ );
		assert.ok( error.message.includes( PORTABLE_OPT_IN ) );
		return true;
	} );
	assert.deepEqual( windows.calls.warnings, [] );
});

test("an opted-in Windows host whose device creation fails uses the portable generator and warns", async () => {
	const windows = host( "win32", "CreateDevice failed: HRESULT 0x8876086C", true );
	assert.equal( await chooseMipGenerator( windows ), PORTABLE );
	assert.equal( windows.calls.warnings.length, 1 );
	assert.match( windows.calls.warnings[0], /0x8876086C/ );
});

test("a Windows host that creates the device keeps D3DX silently", async () => {
	const windows = host( "win32", null );
	assert.equal( await chooseMipGenerator( windows ), D3DX );
	assert.deepEqual( windows.calls.warnings, [] );
});

test("other systems use the portable generator without probing", async () => {
	const linux = host( "linux", "unused" );
	assert.equal( await chooseMipGenerator( linux ), PORTABLE );
	assert.equal( linux.calls.probes, 0 );
});

test("Windows reads portable cache entries only when the build opted in", () => {
	assert.deepEqual( generatorCandidates( { platform: "win32", allowPortable: false } ), [ D3DX ] );
	assert.deepEqual( generatorCandidates( { platform: "win32", allowPortable: true } ), [ D3DX, PORTABLE ] );
	assert.deepEqual( generatorCandidates( { platform: "linux", allowPortable: false } ), [ PORTABLE ] );
});
