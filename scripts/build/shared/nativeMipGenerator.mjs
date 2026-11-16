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

On Windows the portable generator is never the default: its mips are not the
native bytes, so a build that cannot create the device stops with the probe's
reason, and its cached output is not read, unless SRO_PORTABLE_MIPS=1 opts that
build in (a local build over Remote Desktop). An unattended or release build
therefore never ships portable mips by accident, and a later build where D3DX
works regenerates whatever an opted-in build left in the shared cache.

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
// The opt-in that lets a Windows build use and read the portable generator.
export const PORTABLE_OPT_IN = "SRO_PORTABLE_MIPS";
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
	return generatorCandidates( { platform: process.platform, allowPortable: portableAllowed() } );
}

/*
================
generatorCandidates

candidateGenerators with the host injected: Windows reads D3DX output only,
unless the build opted into the portable generator.
================
*/
export function generatorCandidates( host ) {
	if ( host.platform !== "win32" ) return [ PORTABLE ];
	return host.allowPortable ? [ D3DX, PORTABLE ] : [ D3DX ];
}

/*
================
portableAllowed
================
*/
function portableAllowed() {
	return process.env[PORTABLE_OPT_IN] === "1";
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

D3DX when this host can create its device; else the portable generator,
announced once, where the host allows it (chooseMipGenerator).
================
*/
export function selectMipGenerator() {
	selection ??= chooseMipGenerator( {
		platform: process.platform,
		allowPortable: portableAllowed(),
		probe: probeD3dx,
		warn: console.warn
	} );
	return selection;
}

/*
================
chooseMipGenerator

The decision itself, with the host injected (tests run it on any system).
A Windows host that cannot create the device fails with the probe's reason
unless the build opted into the portable generator.
================
*/
export async function chooseMipGenerator( host ) {
	if ( host.platform !== "win32" ) return PORTABLE;
	const refusal = await host.probe();
	if ( refusal === null ) return D3DX;
	if ( !host.allowPortable ) {
		throw Error(
			"[native-mips] D3DX unavailable (" + refusal + "). Build from a desktop session that can create " +
				"a Direct3D 9 device, or set " + PORTABLE_OPT_IN + "=1 for a local build that accepts the " +
				"portable generator's non-native mips."
		);
	}
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
