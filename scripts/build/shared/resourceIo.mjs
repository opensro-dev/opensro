// Split verbatim from resourcePipeline.mjs (2026-07-28): shared filesystem
// paths and generic infra helpers used by the per-domain resource modules.
import { readFile } from "node:fs/promises";
import path from "node:path";

import { publishFileFromTemp } from "./atomicPublish.mjs";
import { uniqueStrings } from "./collections.mjs";
import { listFiles, pathExists } from "./fsUtils.mjs";
import { writeJsonIfChanged } from "./jsonOut.mjs";
import { stripFullLineComment } from "./textDataIo.mjs";
import {
	extractedRoot,
	gameRoot,
	imagePublicRoot,
	imageSourceRoot,
	normalizeAssetPath,
	publicRoot,
	rebuildRoot,
	toGameRelative,
	toHex16
} from "../world/paths.mjs";

const mediaRoot = path.join( extractedRoot, "Media_extracted" );
const dataRoot = path.join( extractedRoot, "Data_extracted" );
const fontSourceRoot = path.join( extractedRoot, "Media_extracted", "fonts" );
const fontPublicRoot = path.join( publicRoot, "assets", "fonts" );
const musicSourceRoot = path.join( extractedRoot, "Music_mp3" );
const audioPublicRoot = path.join( publicRoot, "assets", "audio" );
const resinfoDir = path.join( extractedRoot, "Media_extracted", "resinfo" );
const textDataDir = path.join( extractedRoot, "Media_extracted", "server_dep", "silkroad", "textdata" );
const eventDataDir = path.join( extractedRoot, "Media_extracted", "server_dep", "silkroad", "event" );
async function readText( sourcePath ) {
	const bytes = await readFile( sourcePath );

	if ( bytes.length >= 2 && bytes[0] === 0xff && bytes[1] === 0xfe ) {
		return bytes.subarray( 2 ).toString( "utf16le" );
	}

	if ( bytes.length >= 2 && bytes[0] === 0xfe && bytes[1] === 0xff ) {
		throw new Error( `Unsupported UTF-16BE text file: ${sourcePath}` );
	}

	if ( bytes.length >= 3 && bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf ) {
		return bytes.subarray( 3 ).toString( "utf8" );
	}

	return new TextDecoder( "euc-kr" ).decode( bytes );
}
function cleanNumber( raw ) {
	return Number( String( raw ).replace( /f$/i, "" ) ) || 0;
}

/**
 * Tokenizer for HIERARCHICAL indented tab formats (textdata effectenvsnd.txt),
 * where leading tabs are indentation and an empty cell carries no meaning:
 * returns only the non-empty trimmed tokens, so `<1>`/`<2>`/`<3>` markers land
 * at index 0 whatever the nesting depth. NEVER use this for a column-indexed
 * table - dropping empty cells shifts every later column left; use
 * splitIndexedRowCells there instead.
 */
function splitHierarchyTokens( rawLine ) {
	return rawLine
		.split( "\t" )
		.map( ( cell ) => cell.trim() )
		.filter( ( cell ) => cell.length > 0 );
}

/**
 * Strict splitter for COLUMN-INDEXED tab tables (textdata regioninfo.txt):
 * trims each cell but PRESERVES empty cells so cells[n] always means column n.
 * A blank line yields [""] and a tabs-only line yields all-empty cells - the
 * caller decides row-ness (e.g. skip when every cell is empty). NEVER use this
 * for hierarchical indented formats - the indentation tabs would occupy the
 * leading indices; use splitHierarchyTokens there instead.
 */
function splitIndexedRowCells( rawLine ) {
	return rawLine.split( "\t" ).map( ( cell ) => cell.trim() );
}

const stripInlineComment = stripFullLineComment;

function stripQuotes( value ) {
	return value.replace( /^"+|"+$/g, "" ).trim();
}
const formatHexRegionId = toHex16;

const writeJson = writeJsonIfChanged;
const exists = pathExists;

async function assertExists( targetPath ) {
	if ( !(await exists( targetPath )) ) {
		throw new Error( `Expected asset does not exist: ${targetPath}` );
	}
}
export {
	rebuildRoot,
	gameRoot,
	extractedRoot,
	publicRoot,
	imageSourceRoot,
	imagePublicRoot,
	mediaRoot,
	dataRoot,
	fontSourceRoot,
	fontPublicRoot,
	musicSourceRoot,
	audioPublicRoot,
	resinfoDir,
	textDataDir,
	eventDataDir,
	readText,
	cleanNumber,
	splitHierarchyTokens,
	splitIndexedRowCells,
	stripInlineComment,
	stripQuotes,
	listFiles,
	normalizeAssetPath,
	uniqueStrings,
	toGameRelative,
	formatHexRegionId,
	writeJson,
	publishFileFromTemp,
	exists,
	assertExists
};
