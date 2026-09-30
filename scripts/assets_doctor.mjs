/*
===========================================================================

assets_doctor.mjs - check that this machine can build the game assets

`pnpm assets doctor` reads, never writes. It reports every input the asset
pipeline needs together, each with the next action, and exits non-zero when
any required one is missing:

	the game root and its client files (SRO_GAME_ROOT or the checkout parent)
	the prepared extraction, and whether it still matches the archives
	Python and the build's modules, ffmpeg, and the Windows-only runtimes

===========================================================================
*/
import { createHash } from "node:crypto";
import { createReadStream, existsSync } from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { extractedRoot, gameRoot, rebuildRoot } from "./build/world/paths.mjs";
import {
	BUILD_CLIENT_FILES,
	CLIENT_ARCHIVES,
	missingClientInputs,
	readPreparationManifest
} from "./build/shared/clientInputs.mjs";
import { runPython } from "./build/shared/pythonRun.mjs";

// Python modules the build and preparation import, with their pip names.
const PYTHON_MODULES = [
	[ "PIL", "Pillow" ],
	[ "fontTools", "fontTools" ],
	[ "pefile", "pefile" ],
	[ "Crypto", "pycryptodome" ]
];
const PIP_HINT = "py -3 -m pip install -r requirements-build.txt";
const D3DX_RUNTIME = "d3dx9_39.dll";

/*
================
sha256File
================
*/
function sha256File( file ) {
	return new Promise( ( resolve, reject ) => {
		const hash = createHash( "sha256" );
		createReadStream( file ).on( "data", chunk => hash.update( chunk ) ).on( "error", reject ).on(
			"end",
			() => resolve( hash.digest( "hex" ) )
		);
	} );
}

/*
================
checkPreparation

Compare the preparation record with the archives now in the game root. An
extraction made without `assets prepare` has no record; it is usable but
unverified.
================
*/
async function checkPreparation( report ) {
	const manifest = readPreparationManifest();
	if ( !manifest ) {
		report.warn( "extraction record", "no .opensro-preparation.json; run `pnpm assets prepare` to verify it" );
		return;
	}
	const recorded = { ...manifest.archives, ...(manifest.music ? { Music: manifest.music } : {}) };
	for ( const archive of CLIENT_ARCHIVES ) {
		const name = archive.replace( /\.pk2$/, "" ), file = path.join( gameRoot, archive );
		if ( !recorded[name] ) {
			report.fail( `extraction ${name}`, "not prepared (or interrupted); run `pnpm assets prepare`" );
		} else if ( existsSync( file ) && await sha256File( file ) !== recorded[name].sha256 ) {
			report.fail( `extraction ${name}`, `${archive} changed since preparation; run \`pnpm assets prepare\`` );
		} else {report.pass(
				`extraction ${name}`,
				recorded[name].files ?
					`${recorded[name].files} files` :
					`${Object.keys( recorded[name].tracks ?? {} ).length} tracks`
			);}
	}
}

/*
================
checkPython
================
*/
async function checkPython( report ) {
	const probe = "import importlib.util, json, sys; print(json.dumps({'version': sys.version.split()[0], 'missing': " +
		`[m for m in ${JSON.stringify( PYTHON_MODULES.map( ( [module] ) => module ) )} ` +
		"if importlib.util.find_spec(m) is None]}))";
	try {
		const result = await runPython( [ "-c", probe ], { task: "[doctor] Python probe" } );
		const { version, missing } = JSON.parse( result.stdout );
		if ( missing.length ) {
			const packages = PYTHON_MODULES.filter( ( [module] ) => missing.includes( module ) ).map( ( [, pip] ) =>
				pip
			);
			report.fail( "Python modules", `missing ${packages.join( ", " )}; ${PIP_HINT}` );
		} else report.pass( "Python modules", `${result.command} ${version}` );
	} catch ( error ) {
		report.fail( "Python", `no usable Python 3 interpreter (${String( error ).split( "\n" )[0]})` );
	}
}

/*
================
checkTools
================
*/
function checkTools( report ) {
	const ffmpeg = spawnSync( "ffmpeg", [ "-hide_banner", "-version" ], { encoding: "utf8" } );
	if ( ffmpeg.status === 0 ) report.pass( "ffmpeg", ffmpeg.stdout.split( "\n" )[0] );
	else report.fail( "ffmpeg", "not on PATH; `assets prepare` converts the music with it" );
	if ( process.platform !== "win32" ) {
		report.fail( "platform", "the full asset build needs Windows (32-bit D3DX lens mips, GDI font atlas)" );
		return;
	}
	const runtime = path.join( process.env.WINDIR ?? "C:\\Windows", "SysWOW64", D3DX_RUNTIME );
	if ( existsSync( runtime ) ) report.pass( "DirectX runtime", runtime );
	else report.fail( "DirectX runtime", `${D3DX_RUNTIME} is missing; install the DirectX End-User Runtime` );
}

/*
================
createReport
================
*/
function createReport() {
	const rows = [];
	const add = status => ( name, detail ) => rows.push( { status, name, detail } );
	return { rows, pass: add( "ok" ), warn: add( "warn" ), fail: add( "FAIL" ) };
}

/*
================
main
================
*/
async function main() {
	const report = createReport();
	report.pass( "repository", rebuildRoot );
	report.pass( "game root", `${gameRoot}${process.env.SRO_GAME_ROOT ? " (SRO_GAME_ROOT)" : " (checkout parent)"}` );
	for ( const archive of CLIENT_ARCHIVES.filter( name => !BUILD_CLIENT_FILES.includes( name ) ) ) {
		if ( !existsSync( path.join( gameRoot, archive ) ) ) {
			report.warn(
				archive,
				`not in the game root; \`assets prepare\` needs it unless extracted/ is already complete`
			);
		}
	}
	const missing = missingClientInputs();
	for ( const problem of missing ) report.fail( "build input", problem );
	if ( !missing.length ) report.pass( "build inputs", `${extractedRoot} is complete` );
	await checkPreparation( report );
	await checkPython( report );
	checkTools( report );
	for ( const row of report.rows ) {
		console.log( `${row.status.padEnd( 4 )}  ${row.name.padEnd( 22 )}  ${row.detail}` );
	}
	const failed = report.rows.filter( row => row.status === "FAIL" ).length;
	console.log(
		failed ?
			`\n${failed} problem(s) to fix before \`pnpm assets build full\`.` :
			"\nReady: `pnpm assets build full`."
	);
	process.exitCode = failed ? 1 : 0;
}

await main();
