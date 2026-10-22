#!/usr/bin/env node
// Author English completions for untranslated retail text rows.
//
//   node scripts/tools/localization/englishGaps.mjs status
//   node scripts/tools/localization/englishGaps.mjs list <file.txt> [--offset N] [--limit N] [--out batch.json]
//   node scripts/tools/localization/englishGaps.mjs merge <translations.json>
//
// `list` emits pending rows as [{file,key,korean,chinese}], deduplicated by
// Korean so a string repeated across keys is translated once. `merge` takes
// { "<file.txt>": { "<key>": "<english>" } }. A Korean string translated for
// one key is applied to every pending key with the same Korean in that file.
// It validates markup/placeholders against the Korean before writing
// <source-data>/english-completions/<file>.json (local, never committed). See
// build/shared/englishCompletions.mjs for the policy.
import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import {
	ENGLISH_COMPLETION_FILES,
	loadEnglishCompletions,
	markupSignature,
	needsEnglish
} from "../../build/shared/englishCompletions.mjs";
import { ITEM_TEXT_COMPLETIONS } from "../../build/shared/itemTextCompletions.mjs";
import { GUIDE_TITLE_COMPLETIONS, UI_TEXT_COMPLETIONS } from "../../build/shared/textResources.mjs";
import { readLocalizedTextDataRowsSync } from "../../build/shared/textDataIo.mjs";
import { textDataDir } from "../../build/shared/resourceIo.mjs";
import { sourceDataRoot } from "../../build/world/paths.mjs";

const catalogDir = path.join( sourceDataRoot, "english-completions" );
const [command, ...args] = process.argv.slice( 2 );
const option = ( name, fallback ) => {
	const index = args.indexOf( `--${name}` );
	return index >= 0 ? args[index + 1] : fallback;
};

// Completion layers that already own specific keys; they count as covered.
const otherLayers = new Set( [
	...Object.keys( ITEM_TEXT_COMPLETIONS ),
	...Object.keys( GUIDE_TITLE_COMPLETIONS ),
	...Object.keys( UI_TEXT_COMPLETIONS )
] );

function rowsOf( file ) {
	return readLocalizedTextDataRowsSync( path.join( textDataDir, file ) );
}

function pending( file ) {
	const catalog = loadEnglishCompletions( file );
	const seenKeys = new Set();
	const rows = [];
	for ( const columns of rowsOf( file ) ) {
		const key = columns[1]?.trim();
		if ( !key || seenKeys.has( key ) ) continue;
		seenKeys.add( key );
		if ( !needsEnglish( columns ) || Object.hasOwn( catalog, key ) || otherLayers.has( key ) ) continue;
		rows.push( { file, key, korean: columns[2].trim(), chinese: (columns[5] ?? "").trim() } );
	}
	return rows;
}

function uniqueByKorean( rows ) {
	const seen = new Set();
	return rows.filter( ( row ) => (seen.has( row.korean ) ? false : (seen.add( row.korean ), true)) );
}

if ( command === "status" ) {
	let total = 0;
	for ( const file of ENGLISH_COMPLETION_FILES ) {
		const rows = pending( file );
		total += rows.length;
		console.log(
			`${file.padEnd( 18 )} authored ${
				String( Object.keys( loadEnglishCompletions( file ) ).length ).padStart( 5 )
			}  pending ${String( rows.length ).padStart( 5 )} (${uniqueByKorean( rows ).length} unique Korean)`
		);
	}
	console.log( `pending total ${total}` );
} else if ( command === "list" ) {
	const file = args[0];
	const offset = Number( option( "offset", 0 ) );
	const limit = Number( option( "limit", 100 ) );
	const batch = uniqueByKorean( pending( file ) ).slice( offset, offset + limit );
	const json = JSON.stringify( batch, null, 1 );
	const out = option( "out" );
	if ( out ) writeFileSync( out, json );
	else console.log( json );
} else if ( command === "merge" ) {
	const input = JSON.parse( readFileSync( args[0], "utf8" ) );
	for ( const [file, translations] of Object.entries( input ) ) {
		const catalogPath = path.join( catalogDir, file.replace( /\.txt$/, ".json" ) );
		const catalog = { ...loadEnglishCompletions( file ) };
		const rows = pending( file );
		const byKey = new Map( rows.map( ( row ) => [ row.key, row ] ) );
		const byKorean = new Map();
		const problems = [];
		for ( const [key, english] of Object.entries( translations ) ) {
			// An authored key is a correction: revalidated against its recorded source.
			const row = byKey.get( key ) ??
				(Object.hasOwn( catalog, key ) ? { file, key, korean: catalog[key].source } : undefined);
			if ( !row ) {
				problems.push( `${key}: not pending in ${file}` );
				continue;
			}
			if ( !byKey.has( key ) ) {
				byKey.set( key, row );
				rows.push( row );
			}
			if ( markupSignature( row.korean ).join( "\0" ) !== markupSignature( english ).join( "\0" ) ) {
				problems.push(
					`${key}: markup/placeholders differ\n  ko: ${markupSignature( row.korean ).join( " " )}\n  en: ${
						markupSignature( english ).join( " " )
					}`
				);
				continue;
			}
			if ( /[ᄀ-ᇿ㄰-㆏가-힯]/.test( english ) ) {
				problems.push( `${key}: English still contains Hangul` );
				continue;
			}
			byKorean.set( row.korean, english );
		}
		if ( problems.length ) {
			console.error( `${file}: ${problems.length} problem(s)\n${problems.join( "\n" )}` );
			process.exitCode = 1;
		}
		let added = 0;
		for ( const row of rows ) {
			const english = byKorean.get( row.korean );
			if ( english === undefined ) continue;
			catalog[row.key] = { english, basis: "translated-retail-korean", source: row.korean };
			added += 1;
		}
		const sorted = Object.fromEntries( Object.entries( catalog ).sort( ( [a], [b] ) => a.localeCompare( b ) ) );
		writeFileSync( catalogPath, `${JSON.stringify( sorted, null, 1 )}\n` );
		console.log( `${file}: +${added} (total ${Object.keys( sorted ).length})` );
	}
} else {
	console.error(
		"usage: englishGaps.mjs status | list <file.txt> [--offset N] [--limit N] [--out f] | merge <translations.json>"
	);
	process.exitCode = 2;
}
