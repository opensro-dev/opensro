import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { noteRead } from "../graph/reads.mjs";
import { pathExists } from "../shared/fsUtils.mjs";
import { writeJsonIfChanged } from "../shared/jsonOut.mjs";
import { claimPublicFile } from "../shared/publicationLedger.mjs";
import { publicRoot } from "./paths.mjs";

/**
 * Write minified JSON, skipping the write when the target already holds identical bytes.
 * Minified is the file's final form anyway (optimizePublicJsonAssets rewrites pretty JSON
 * in place), and the unchanged-skip keeps mtimes stable so the minify/sidecar/pack caches
 * hold across rebuilds - re-writing the multi-MB region JSONs every run forced minutes of
 * sidecar recompression for byte-identical content.
 */
export const writeJson = writeJsonIfChanged;

export async function writePublicFile( publicPath, bytes ) {
	const targetPath = path.join( publicRoot, publicPath.replace( /^\/+/, "" ) );
	await mkdir( path.dirname( targetPath ), { recursive: true } );
	await writeFile( targetPath, bytes );
	claimPublicFile( targetPath );
}

export const exists = pathExists;

/*
================
notedExists

pathExists that tells a tracked step (graph/reads.mjs) it probed the
file: a step that chose a source because another was missing depends on
that absence. Outside a tracked step it is pathExists.
================
*/
export async function notedExists( file ) {
	noteRead( file );
	return pathExists( file );
}

/*
================
notedReadFile

readFile that tells a tracked step it read the file.
================
*/
export async function notedReadFile( file, ...rest ) {
	noteRead( file );
	return readFile( file, ...rest );
}
