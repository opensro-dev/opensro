/*
===========================================================================

nativeMipGenerator.test.mjs - which mip generator a host runs

A Windows host whose Direct3D device creation fails (a Remote Desktop
session or a GPU-less VM) builds with the portable generator and says so;
a host that creates the device keeps D3DX; other systems never probe.

===========================================================================
*/
import test from "node:test";
import assert from "node:assert/strict";
import { chooseMipGenerator, D3DX, PORTABLE } from "../../build/shared/nativeMipGenerator.mjs";

/*
================
host
================
*/
function host( platform, refusal ) {
	const calls = { probes: 0, warnings: [] };
	return {
		calls,
		platform,
		probe: async () => {
			calls.probes++;
			return refusal;
		},
		warn: message => calls.warnings.push( message )
	};
}

test("a Windows host whose device creation fails falls back to the portable generator and warns", async () => {
	const windows = host( "win32", "CreateDevice failed: HRESULT 0x8876086C" );
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
