/*
===========================================================================

nativeMipGenerator.mjs - which native mip generator this host runs

The lens and character-texture builds fill absent mip levels with the
original client's D3DX (32-bit Windows PowerShell), which reproduces the
native output byte for byte. D3DX needs a Direct3D 9 hardware device: a
Remote Desktop session or a cloud VM without a GPU refuses CreateDevice
(HRESULT 0x8876086C), and other systems have no D3DX at all. Those hosts
run the portable native_texture_mips.py, which writes the same container.

The choice is made once per process by the helper's -Probe batch, which
creates the device and exits. The two generators keep separate cache identities,
so a cache never mixes their outputs under one key.

===========================================================================
*/
import fs from "node:fs";
import path from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { createHash } from "node:crypto";
import { rebuildRoot } from "../world/paths.mjs";
import { runPython } from "./pythonRun.mjs";

const execute = promisify( execFile );
export const D3DX = "d3dx";
export const PORTABLE = "portable";
const GENERATOR_FILES = {
	[D3DX]: [ "scripts/build/NativeLensResources.cs", "scripts/build/native_lens_resources.ps1" ],
	[PORTABLE]: [ "scripts/build/native_texture_mips.py" ]
};
let selection;

/*
================
generatorHash

The generator's own source bytes, folded into every cache key it writes.
================
*/
export function generatorHash( kind ) {
	return GENERATOR_FILES[kind].reduce(
		( hash, file ) => hash.update( fs.readFileSync( path.join( rebuildRoot, file ) ) ),
		createHash( "sha256" )
	).digest( "hex" );
}

/*
================
candidateGenerators

The generators whose cached output a reader may accept, preferred first.
================
*/
export function candidateGenerators() {
	return process.platform === "win32" ? [ D3DX, PORTABLE ] : [ PORTABLE ];
}

/*
================
runD3dx

One 32-bit PowerShell session over a batch manifest or a source/output pair.
================
*/
async function runD3dx( args ) {
	const powershell = path.join(
		process.env.SystemRoot ?? "C:\\Windows",
		"SysWOW64",
		"WindowsPowerShell",
		"v1.0",
		"powershell.exe"
	);
	await execute( powershell, [
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-File",
		path.join( rebuildRoot, "scripts", "build", "native_lens_resources.ps1" ),
		...args
	], { windowsHide: true } );
}

/*
================
probeD3dx

The helper's -Probe batch: NativeLensResources.BuildFiles creates the
device, has no texture to convert, and releases it. A refusal names the
reason (a missing d3dx9_39.dll, or CreateDevice failing).
================
*/
async function probeD3dx() {
	try {
		await runD3dx( [ "-Probe" ] );
		return null;
	} catch ( error ) {
		return String( error.stderr || error.message ).trim().split( /\r?\n/ )[0];
	}
}

/*
================
selectMipGenerator

D3DX when this host can create its device, else the portable generator,
announced once so a release build that falls back says so.
================
*/
export function selectMipGenerator() {
	selection ??= chooseMipGenerator( { platform: process.platform, probe: probeD3dx, warn: console.warn } );
	return selection;
}

/*
================
chooseMipGenerator

The decision itself, with the host injected (tests run it on any system).
================
*/
export async function chooseMipGenerator( host ) {
	if ( host.platform !== "win32" ) return PORTABLE;
	const refusal = await host.probe();
	if ( refusal === null ) return D3DX;
	host.warn(
		"[native-mips] D3DX unavailable (" + refusal + "); using the portable generator. " +
			"Its mips are equivalent but not byte-identical to the native client's."
	);
	return PORTABLE;
}

/*
================
runMipGenerator

Run the selected generator over a batch manifest ({ manifest }) or one
source/output folder pair ({ sourceRoot, outputRoot }).
================
*/
export async function runMipGenerator( kind, job, task ) {
	const args = job.manifest ?
		[ "-Manifest", job.manifest ] :
		[ "-SourceRoot", job.sourceRoot, "-OutputRoot", job.outputRoot ];
	if ( kind === PORTABLE ) {
		await runPython( [ path.join( rebuildRoot, "scripts", "build", "native_texture_mips.py" ), ...args ], {
			task
		} );
		return;
	}
	try {
		await runD3dx( args );
	} catch ( error ) {
		throw new Error( `${task} failed: ${error.stderr || error.message}`, { cause: error } );
	}
}
