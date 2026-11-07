/*
===========================================================================

encoders.mjs - the pinned texture encoders of the asset build

Port-only. The texture variant pass (buildTextureVariants.mjs) re-encodes
the retail BC textures for devices without BC (client
foundation/assets/texture-variants.ts). Its encoders are third-party
tools fetched from their official GitHub releases at the exact versions
and SHA-256 digests below, kept in the hub's .tools/ (shared by every
worktree, never shipped to players) and used only at build time.

- astcenc 5.7.0, Arm Limited, Apache-2.0
  (https://github.com/ARM-software/astc-encoder). The digest is the one
  Arm publishes in the release's release-sha256.txt. The release archive
  carries no licence file; licences/astcenc-5.7.0-LICENSE.txt is the
  LICENSE.txt of the 5.7.0 tag.
- texconv may2026, Microsoft Corporation, MIT (DirectXTex): the BC7
  encoder of the HD pass (#2061). The CPU codec (-nogpu) is forced by the
  caller: the DirectCompute codec is not reproducible across machines.
  The release ships a bare executable, no archive; licences/
  texconv-may2026-LICENSE.txt is the LICENSE of the may2026 tag.
- realesrgan-ncnn-vulkan 20220424 (Real-ESRGAN v0.2.5.0), Xintao Wang,
  BSD-3-Clause: the x2 upscaler of the HD pass (#2061), with the
  realesrgan-x4plus model its archive carries in models/. Its digest was
  measured from the official release archive (the release publishes no
  digest file); licences/realesrgan-0.2.5.0-LICENSE.txt is the LICENSE
  of the v0.2.5.0 tag.

===========================================================================
*/
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, renameSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { MAIN_CHECKOUT_ROOT } from "../../lib/generatedRoot.mjs";

/*
================
ENCODERS

Each pinned tool: the release archive (or, with `archive: false`, the bare
executable), its digest, and the executable inside it.
================
*/
export const ENCODERS = Object.freeze( {
	astcenc: Object.freeze( {
		version: "5.7.0",
		licence: "Apache-2.0",
		url: "https://github.com/ARM-software/astc-encoder/releases/download/5.7.0/astcenc-5.7.0-windows-x64.zip",
		sha256: "cbde7c78adcc90d0d90ecf10f32d03f3340fbe1720cb6d6c892147d625e74d2d",
		executable: "bin/astcenc-avx2.exe"
	} ),
	texconv: Object.freeze( {
		version: "may2026",
		licence: "MIT",
		url: "https://github.com/microsoft/DirectXTex/releases/download/may2026/texconv.exe",
		sha256: "dcfdec10244e02cf5037fba089c55fb7e1326b1c8181742d77d15fa5cb5eef06",
		executable: "texconv.exe",
		archive: false
	} ),
	realesrgan: Object.freeze( {
		version: "20220424",
		licence: "BSD-3-Clause",
		url: "https://github.com/xinntao/Real-ESRGAN/releases/download/v0.2.5.0/realesrgan-ncnn-vulkan-20220424-windows.zip",
		sha256: "abc02804e17982a3be33675e4d471e91ea374e65b70167abc09e31acb412802d",
		executable: "realesrgan-ncnn-vulkan.exe"
	} )
} );

/*
================
toolsRoot
================
*/
export function toolsRoot( mainCheckout = MAIN_CHECKOUT_ROOT ) {
	return path.join( mainCheckout, ".tools" );
}

/*
================
encoderPath

The installed executable of a pinned encoder, fetching and verifying its
release archive first when it is not installed yet.
================
*/
export async function encoderPath( name, root = toolsRoot() ) {
	const pin = ENCODERS[name];
	if ( !pin ) throw new Error( `Unknown encoder ${name}` );
	if ( process.platform !== "win32" ) throw new Error( `${name}: only the windows-x64 release is pinned` );
	const home = path.join( root, `${name}-${pin.version}` ), executable = path.join( home, pin.executable );
	if ( existsSync( executable ) ) return executable;
	const response = await fetch( pin.url );
	if ( !response.ok ) throw new Error( `${name}: download failed (${response.status})` );
	const download = Buffer.from( await response.arrayBuffer() );
	const digest = createHash( "sha256" ).update( download ).digest( "hex" );
	if ( digest !== pin.sha256 ) throw new Error( `${name}: SHA-256 ${digest} is not the pinned ${pin.sha256}` );
	// Unpack beside the final folder and rename it into place, so a broken
	// unpack never leaves a half-installed tool behind. A pin whose release
	// is a bare executable skips the archive step.
	const staging = home + ".partial", zip = home + ".zip";
	rmSync( staging, { recursive: true, force: true } );
	mkdirSync( staging, { recursive: true } );
	if ( pin.archive === false ) {
		writeFileSync( path.join( staging, pin.executable ), download );
	} else {
		writeFileSync( zip, download );
		// Windows' own tar (bsdtar) reads zip archives; a Git or MSYS tar first
		// on PATH does not.
		execFileSync( path.join( process.env.SystemRoot ?? "C:\\Windows", "System32", "tar.exe" ), [
			"-xf",
			zip,
			"-C",
			staging
		] );
		rmSync( zip, { force: true } );
	}
	writeFileSync(
		path.join( staging, "PINNED.json" ),
		JSON.stringify( { name, ...pin, installedAt: new Date().toISOString() }, null, 2 ) + "\n"
	);
	if ( !existsSync( path.join( staging, pin.executable ) ) ) {
		throw new Error( `${name}: ${pin.executable} is not in the release archive` );
	}
	renameSync( staging, home );
	return executable;
}
