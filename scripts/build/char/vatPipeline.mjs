import fs from "node:fs";
import { writeIntoPublicTree } from "../shared/publicWrite.mjs";
import { claimPublicFile } from "../shared/publicationLedger.mjs";
import path from "node:path";
import { readFile, unlink } from "node:fs/promises";

import { PRECOMPRESSED_ASSET_SUFFIXES } from "../shared/compressionUtils.mjs";
import { listFiles } from "../shared/fsUtils.mjs";
import { sha256Hex } from "../shared/hash.mjs";
import { normalizePublicPath } from "../shared/assetPaths.mjs";
import { readJsonOrNullSync, writeJsonIfChanged } from "../shared/jsonOut.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";

function dataBytes( data ) {
	return Buffer.from( data.buffer, data.byteOffset, data.byteLength );
}

/**
 * Shared high-level VAT artifact lifecycle. Callers retain ownership of model
 * admission, bake settings, manifest metadata, and the runtime-facing VAT row.
 */
export async function runVatPipeline( {
	manifestPath,
	missingResult,
	settings,
	settingsForModel,
	logTag,
	getModels,
	classifyModel,
	vatPublicPathsForGlb,
	publicPathToDisk,
	bakeVatFromGlb,
	isExistingVatFresh,
	createVatPrecisionCensus,
	reportVatPrecision,
	recordVatPrecision,
	createVatReference,
	updateManifest,
	shareByGlb = false,
	skipMissingGlb = false,
	skipEmptyBake = false,
	refreshSidecars = false,
	cleanupRoot = null
} ) {
	if ( !fs.existsSync( manifestPath ) ) {
		return missingResult;
	}

	const document = JSON.parse( await readFile( manifestPath, "utf8" ) );
	const models = getModels( document );
	const precision = createVatPrecisionCensus();
	const vatByGlb = new Map();
	const settingsKeyByGlb = new Map();
	let built = 0;
	let reused = 0;
	let shared = 0;
	let skipped = 0;
	let failed = 0;

	for ( const model of models ) {
		const admission = classifyModel( model );
		if ( admission === "ignore" ) continue;
		if ( admission === "skip" ) {
			skipped += 1;
			continue;
		}

		const glbPublicPath = normalizePublicPath( model.glb );
		const modelSettings = settingsForModel?.( model, settings ) ?? settings;
		const settingsKey = JSON.stringify( modelSettings );
		const priorVat = shareByGlb ? vatByGlb.get( glbPublicPath ) : null;
		if ( priorVat ) {
			if ( settingsKeyByGlb.get( glbPublicPath ) !== settingsKey ) {
				throw new Error( `${glbPublicPath}: shared GLB requested incompatible VAT settings.` );
			}
			model.vat = { ...priorVat };
			shared += 1;
			continue;
		}

		const glbPath = publicPathToDisk( glbPublicPath );
		if ( skipMissingGlb && !fs.existsSync( glbPath ) ) {
			skipped += 1;
			continue;
		}

		// Required crowd assets retain fail-fast semantics; optional NPC assets
		// are filtered by the existence branch above.
		const glbBytes = await readFile( glbPath );
		const glbSha256 = sha256Hex( glbBytes );
		const vatPublic = vatPublicPathsForGlb( glbPublicPath );
		const vatManifestPath = publicPathToDisk( vatPublic.manifest );
		const vatBinPath = publicPathToDisk( vatPublic.bin );
		const existing = readJsonOrNullSync( vatManifestPath );
		const label = model.codename ?? glbPublicPath;

		try {
			let vatManifest = existing;
			if ( isExistingVatFresh( existing, vatBinPath, { glbSha256 }, modelSettings ) ) {
				// A fresh VAT is kept, and still this build's output.
				claimPublicFile( vatManifestPath );
				claimPublicFile( vatBinPath );
				reused += 1;
			} else {
				const baked = await bakeVatFromGlb(
					{ glbBytes, glbPublicPath, glbSha256 },
					modelSettings
				);
				if ( !baked && skipEmptyBake ) {
					console.warn( `[${logTag}] ${label}: no whitelisted clip in the GLB; skipping` );
					skipped += 1;
					continue;
				}
				reportVatPrecision( logTag, label, baked );
				recordVatPrecision( precision, label, baked );
				const binBytes = dataBytes( baked.data );
				await writeIntoPublicTree( vatBinPath, binBytes );
				vatManifest = {
					...baked.manifestCore,
					bin: {
						path: vatPublic.bin,
						byteLength: binBytes.length,
						sha256: sha256Hex( binBytes )
					}
				};
				await writeJsonIfChanged( vatManifestPath, vatManifest );
				built += 1;
			}

			model.vat = createVatReference( vatManifest, vatPublic );
			if ( shareByGlb ) {
				vatByGlb.set( glbPublicPath, model.vat );
				settingsKeyByGlb.set( glbPublicPath, settingsKey );
			}
		} catch ( error ) {
			failed += 1;
			console.warn( `[${logTag}] ${label}: ${error?.message ?? error}` );
		}
	}

	updateManifest( document );
	await writeJsonIfChanged( manifestPath, document );

	if ( refreshSidecars ) {
		const vatManifestPaths = [
			...new Set(
				models.map( ( model ) => model?.vat?.manifest ).filter( Boolean )
			)
		].map( ( publicPath ) => publicPathToDisk( publicPath ) );
		await refreshPrecompressedSidecars( [ manifestPath, ...vatManifestPaths ], {
			onlyWhenStale: true
		} );
	}

	if ( cleanupRoot && failed === 0 ) {
		const retained = new Set();
		for ( const model of models ) {
			if ( !model?.vat ) continue;
			const vatManifestPath = publicPathToDisk( model.vat.manifest );
			retained.add( path.resolve( vatManifestPath ).toLowerCase() );
			retained.add( path.resolve( publicPathToDisk( model.vat.bin ) ).toLowerCase() );
			for ( const suffix of PRECOMPRESSED_ASSET_SUFFIXES ) {
				retained.add( path.resolve( `${vatManifestPath}${suffix}` ).toLowerCase() );
			}
		}
		for ( const filePath of await listFiles( cleanupRoot, { missing: "empty" } ) ) {
			if ( !retained.has( path.resolve( filePath ).toLowerCase() ) ) {
				await unlink( filePath );
			}
		}
	}

	return {
		built,
		reused,
		shared,
		skipped,
		failed,
		precision,
		assetCount: models.filter( ( model ) => model?.vat?.manifest && model?.vat?.bin ).length
	};
}
