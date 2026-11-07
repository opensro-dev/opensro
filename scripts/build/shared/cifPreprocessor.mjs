/*
===========================================================================

cifPreprocessor.mjs - the resinfo layout files' #ifdef preprocessor

The client's resinfo layout files carry feature branches (#ifdef KEY,
#ifndef KEY, #else, #endif) keyed by config\define.txt. This applies
them before the layout parser runs, keeping every line in place so
diagnostics still name the shipped file's line numbers.

The retail client reads these files without complaint from every client
build players own, so this reader is as lenient as the original's must
be: a directive is recognised by its keyword, whatever follows it ("#endif
// FEATURE", a tab after "#ifdef"), and a malformed structure (an #else or
#endif with nothing open, a second #else, a block left open at the end of
the file) is reported through warn and repaired, never thrown. A hand-made
2005 directive reader would compare the keyword prefix and drop whatever is
still open at the end of the file; that is the inference recorded here, as
no client address for the reader is cited yet. Throwing instead killed a
player's whole asset build on a later client's layout (2026-11-07,
"unterminated #ifdef CONTENT_CHARGING_SYSTEM").

===========================================================================
*/
import path from "node:path";

import { extractedRoot, readText } from "./resourceIo.mjs";

const cifDefinePath = path.join( extractedRoot, "Media_extracted", "config", "define.txt" );

const DIRECTIVE_PATTERN = /^#(ifdef|ifndef|else|endif)\b\s*([A-Za-z_][A-Za-z0-9_]*)?/;

/*
================
parseCifDefines

The native resinfo preprocessor gets its symbols from config\define.txt.
Keep that file as the only feature-branch authority: per-layout define
guesses make the catalog, generated JSON, and retail client describe three
different trees.
================
*/
export function parseCifDefines( raw ) {
	const defines = {};

	for ( const sourceLine of raw.replace( /^﻿/, "" ).split( /\r?\n/ ) ) {
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

/*
================
loadCifDefines
================
*/
export async function loadCifDefines() {
	return parseCifDefines( await readText( cifDefinePath ) );
}

/*
================
cifDirective

The directive a line opens with, or null for a content line: the keyword
and, for #ifdef and #ifndef, the key. Anything after them - a comment, a
repeated key on #endif - is ignored, as the original's prefix compare
would.
================
*/
export function cifDirective( line ) {
	const match = DIRECTIVE_PATTERN.exec( line.trim() );
	if ( !match ) {
		return null;
	}
	return { keyword: match[1], key: match[2] ?? "" };
}

/*
================
applyCifPreprocessor

Directive lines and inactive branches become empty lines rather than being
removed. The parser skips blanks, while diagnostics retain source line
numbers from the shipped resinfo file. warn receives one line per repaired
structure fault; the build logs it and goes on.
================
*/
export function applyCifPreprocessor( raw, defines, warn = message => console.warn( message ) ) {
	const output = [];
	const stack = [];
	const lines = raw.split( /\r?\n/ );
	const active = () => stack.every( entry => entry.active );

	for ( let lineIndex = 0; lineIndex < lines.length; lineIndex += 1 ) {
		const line = lines[lineIndex];
		const directive = cifDirective( line );

		if ( !directive ) {
			output.push( active() ? line : "" );
			continue;
		}

		output.push( "" );
		const { keyword, key } = directive;

		if ( keyword === "ifdef" || keyword === "ifndef" ) {
			const parentActive = active();
			// An #ifndef is an #ifdef whose branches are swapped.
			const wanted = keyword === "ifdef" ? Boolean( defines[key] ) : !defines[key];
			stack.push( {
				key,
				line: lineIndex + 1,
				parentActive,
				wanted,
				active: parentActive && wanted,
				hasElse: false
			} );
			continue;
		}

		const current = stack.at( -1 );
		if ( !current ) {
			warn( `CIF preprocessor: #${keyword} without #ifdef at line ${lineIndex + 1}, ignored` );
			continue;
		}

		if ( keyword === "else" ) {
			if ( current.hasElse ) {
				warn( `CIF preprocessor: second #else for #ifdef ${current.key} at line ${lineIndex + 1}, ignored` );
				continue;
			}
			current.active = current.parentActive && !current.wanted;
			current.hasElse = true;
			continue;
		}

		stack.pop();
	}

	for ( const open of stack ) {
		warn(
			`CIF preprocessor: #ifdef ${open.key} opened at line ${open.line} has no #endif, closed at the end of the file`
		);
	}

	return output.join( "\n" );
}

export { cifDefinePath };
