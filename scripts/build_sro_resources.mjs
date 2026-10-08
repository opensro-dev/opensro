/*
===========================================================================

build_sro_resources.mjs - `pnpm assets build`: the full browser asset build

The command-line entry. It takes the generated-assets lock, checks the
client inputs, skips the whole build when the fingerprint over every input
and output matches the last successful build, checks the Python modules the
build needs, runs buildSroResources (build/resourceBuild.mjs) and prints its
summary, then records the new fingerprint.

===========================================================================
*/

import { assertClientInputs, PYTHON_BUILD_MODULES, PYTHON_INSTALL_HINT } from "./build/shared/clientInputs.mjs";
import { runPython } from "./build/shared/pythonRun.mjs";
import {
	computeResourceBuildFingerprint,
	readRecordedFingerprint,
	writeRecordedFingerprint
} from "./build/shared/resourceBuildFingerprint.mjs";
import { buildSroResources, formatResourceBuildSummary } from "./build/resourceBuild.mjs";
import { beginPublication, commitPublication } from "./build/shared/publicationLedger.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

// Fail fast when the Python side of the pipeline is missing. convert_images.py
// (Pillow) only soft-warns at its call sites, which yields silently untextured
// assets; the font steps (Pillow + fontTools) throw late, deep in the build.
// Runs only when the fingerprint gate has already decided a rebuild is needed.
// runPython owns the py -3 -> python fallback and picks the remediation hint
// from what actually failed (no interpreter vs missing module) instead of a
// fixed "install the deps" line the evidence may contradict.
/*
================
checkPythonBuildDeps

Fail before publication when required image or font dependencies are missing.
================
*/
async function checkPythonBuildDeps() {
	// Importing (not only finding) each module lets a missing one surface as
	// ModuleNotFoundError, which runPython classifies with its remediation.
	const modules = PYTHON_BUILD_MODULES.filter( row => row.build ).map( row => row.module );
	const probeScript = "import importlib; print(', '.join(m + ' ' + str(getattr(importlib.import_module(m), " +
		`'__version__', '?')) for m in ${JSON.stringify( modules )}))`;
	const probe = await runPython( [ "-c", probeScript ], {
		task: "[resource-preflight] Python build-dependency check",
		context: [
			`The resource build needs Python 3 with its modules installed (${PYTHON_INSTALL_HINT}): ` +
			"Pillow + fontTools (image conversion, font atlas and repair) and pefile (cursors from SRO_Client.exe)."
		]
	} );
	console.log( `[resource-preflight] ${probe.command}: ${probe.stdout.trim()}` );
}

await withGeneratedAssetsLock( "SRO resource build", async () => {
	assertClientInputs( "asset build" );
	// Skip the whole pipeline when nothing it reads or writes has changed since
	// the last successful build. The fingerprint is stat-level (path, size,
	// mtime) over inputs AND outputs, recorded only after a build completes, so
	// a skip can never hide a change - at worst a bare timestamp touch causes an
	// unnecessary (and internally incremental) rebuild.
	const force = process.env.SRO_FORCE_RESOURCE_BUILD === "1";
	const recorded = force ? null : await readRecordedFingerprint();

	if ( recorded !== null ) {
		const current = await computeResourceBuildFingerprint();
		if ( current.hash === recorded.hash ) {
			console.log(
				`SRO resources up to date: fingerprint ${current.hash.slice( 0, 12 )} over ` +
					`${current.fileCount} files matches the last successful build ` +
					`(verified in ${(current.elapsedMs / 1000).toFixed( 1 )}s); skipping. ` +
					`Set SRO_FORCE_RESOURCE_BUILD=1 to rebuild anyway.`
			);
			return;
		}
		console.log(
			`SRO resource inputs/outputs changed since the last build ` +
				`(fingerprint miss over ${current.fileCount} files, ${
					(current.elapsedMs / 1000).toFixed( 1 )
				}s); building.`
		);
	}

	await checkPythonBuildDeps();
	// Everything this run writes or keeps is claimed for the resource build;
	// a failed run throws before the commit and leaves the old record.
	beginPublication( "resource-build" );
	const results = await buildSroResources();
	await commitPublication();
	for ( const line of formatResourceBuildSummary( results ) ) console.log( line );

	const after = await computeResourceBuildFingerprint();
	await writeRecordedFingerprint( after );
} );
