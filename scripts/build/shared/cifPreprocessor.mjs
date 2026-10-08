import path from "node:path";

import { extractedRoot, readText } from "./resourceIo.mjs";

const cifDefinePath = path.join( extractedRoot, "Media_extracted", "config", "define.txt" );

/**
 * The native resinfo preprocessor gets its symbols from config\\define.txt.
 * Keep that file as the only feature-branch authority: per-layout define
 * guesses make the catalog, generated JSON, and retail client describe three
 * different trees.
 */
export function parseCifDefines( raw ) {
	const defines = {};

	for ( const sourceLine of raw.replace( /^\uFEFF/, "" ).split( /\r?\n/ ) ) {
		const line = sourceLine.split( "//", 1 )[0].trim();
		if ( !line ) {
			continue;
		}

		const key = /^([A-Za-z_][A-Za-z0-9_]*)/.exec( line )?.[1];
		if ( key ) {
			defines[key] = true;
		}
	}

	return defines;
}

export async function loadCifDefines() {
	return parseCifDefines( await readText( cifDefinePath ) );
}

// Directive lines and inactive branches become empty lines rather than being
// removed. The parser skips blanks, while diagnostics retain source line
// numbers from the shipped resinfo file.
export function applyCifPreprocessor( raw, defines ) {
	const output = [];
	const stack = [];
	const lines = raw.split( /\r?\n/ );

	for ( let lineIndex = 0; lineIndex < lines.length; lineIndex += 1 ) {
		const line = lines[lineIndex];
		const trimmed = line.trim();

		if ( trimmed.startsWith( "#ifdef " ) ) {
			const key = trimmed.slice( "#ifdef ".length ).trim();
			const parentActive = stack.every( ( entry ) => entry.active );
			stack.push( {
				key,
				line: lineIndex + 1,
				parentActive,
				active: parentActive && Boolean( defines[key] ),
				hasElse: false
			} );
			output.push( "" );
			continue;
		}

		if ( trimmed === "#else" ) {
			const current = stack.at( -1 );
			if ( !current ) {
				throw new Error( `CIF preprocessor #else without #ifdef at line ${lineIndex + 1}` );
			}
			if ( current.hasElse ) {
				throw new Error( `CIF preprocessor duplicate #else at line ${lineIndex + 1}` );
			}
			current.active = current.parentActive && !Boolean( defines[current.key] );
			current.hasElse = true;
			output.push( "" );
			continue;
		}

		if ( trimmed === "#endif" ) {
			if ( !stack.pop() ) {
				throw new Error( `CIF preprocessor #endif without #ifdef at line ${lineIndex + 1}` );
			}
			output.push( "" );
			continue;
		}

		output.push( stack.every( ( entry ) => entry.active ) ? line : "" );
	}

	const unterminated = stack.at( -1 );
	if ( unterminated ) {
		throw new Error(
			`CIF preprocessor unterminated #ifdef ${unterminated.key} opened at line ${unterminated.line}`
		);
	}

	return output.join( "\n" );
}

export { cifDefinePath };
