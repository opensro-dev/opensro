/*
===========================================================================

source-map.mjs - map trace call frames back to client source

A DevTools trace names frames by served script, line and column. Two
builds serve different scripts:

- the production bundle (index-<hash>.js): its source map sits in the
  beta package's private maps directory (tools/beta/build.mjs);
- the Vite dev server (http://localhost:5180/src/...ts): each module is
  the oxc transform of its source file. Re-running that transform on the
  current file reproduces the served lines when the tree is unchanged
  since the trace, and yields a map for them.

Decoding is local (VLQ), so the analyzer has no dependency.

===========================================================================
*/
import { readFileSync, existsSync, readdirSync } from "node:fs";
import path from "node:path";

/*
================
shortSource

A source path as the client names it: from src/ or tests/ when present,
else its last two segments. Dev maps carry absolute Windows paths.
================
*/
function shortSource( source ) {
	const parts = source.replaceAll( "\\", "/" ).split( "/" );
	const at = parts.lastIndexOf( "src" );
	if ( at < 0 ) return parts.slice( -2 ).join( "/" );
	const rest = parts.slice( at + 1 );
	return (rest[0] === "engine" ? rest.slice( 1 ) : rest).join( "/" );
}

const BASE64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
const DIGIT = new Map( [ ...BASE64 ].map( ( c, i ) => [ c, i ] ) );

/*
================
decodeMappings

The mappings of a version 3 source map as one sorted segment array per
generated line: [ column, source, line, column, name? ] with source lines
zero-based.
================
*/
function decodeMappings( mappings ) {
	const lines = [];
	let source = 0, sourceLine = 0, sourceColumn = 0, name = 0;
	for ( const text of mappings.split( ";" ) ) {
		const segments = [];
		let column = 0;
		for ( const part of text.split( "," ) ) {
			if ( !part ) continue;
			const values = [];
			let value = 0, shift = 0;
			for ( const c of part ) {
				const digit = DIGIT.get( c );
				value += (digit & 31) << shift;
				if ( digit & 32 ) {
					shift += 5;
					continue;
				}
				values.push( value & 1 ? -(value >>> 1) : value >>> 1 );
				value = 0;
				shift = 0;
			}
			column += values[0];
			if ( values.length < 4 ) continue;
			source += values[1];
			sourceLine += values[2];
			sourceColumn += values[3];
			if ( values.length > 4 ) name += values[4];
			segments.push( [ column, source, sourceLine, sourceColumn, values.length > 4 ? name : -1 ] );
		}
		lines.push( segments );
	}
	return lines;
}

/*
================
createSourceMap

Lookup of a generated (zero-based line, column) in one parsed map.
================
*/
export function createSourceMap( map ) {
	const lines = decodeMappings( map.mappings );
	return {
		/*
		================
		lookup

		The original { source, line (one-based), name } at or before column.
		================
		*/
		lookup( line, column ) {
			const segments = lines[line];
			if ( !segments?.length ) return null;
			let low = 0, high = segments.length - 1, found = -1;
			while ( low <= high ) {
				const middle = (low + high) >> 1;
				if ( segments[middle][0] <= column ) {
					found = middle;
					low = middle + 1;
				} else high = middle - 1;
			}
			const segment = segments[Math.max( 0, found )];
			return {
				source: map.sources[segment[1]] ?? "?",
				line: segment[2] + 1,
				name: segment[4] >= 0 ? map.names[segment[4]] : undefined
			};
		}
	};
}

/*
================
createSymbolizer

Resolves call frames. maps: a directory of *.js.map files (production).
root: the client directory a dev server served (apps/client-next); its
modules are re-transformed on demand. Frames neither covers keep their
served name and line.
================
*/
export async function createSymbolizer( { maps, root } = {} ) {
	const bundles = new Map();
	if ( maps && existsSync( maps ) ) {
		for ( const file of readdirSync( maps, { recursive: true } ) ) {
			if ( !String( file ).endsWith( ".js.map" ) ) continue;
			const name = path.basename( String( file ), ".map" );
			bundles.set( name, path.join( maps, String( file ) ) );
		}
	}
	const loaded = new Map();
	const transform = root ? (await import( "vite" )).transformWithOxc : null;

	/*
	================
	mapFor

	The source map of a served script URL, or null.
	================
	*/
	async function mapFor( url ) {
		if ( loaded.has( url ) ) return loaded.get( url );
		let map = null;
		try {
			const pathname = new URL( url ).pathname, file = path.basename( pathname );
			if ( bundles.has( file ) ) {
				map = createSourceMap( JSON.parse( readFileSync( bundles.get( file ), "utf8" ) ) );
			} else if ( transform && pathname.startsWith( "/src/" ) && /\.(ts|mts|tsx)$/.test( pathname ) ) {
				const source = path.join( root, pathname );
				if ( existsSync( source ) ) {
					const result = await transform( readFileSync( source, "utf8" ), source, {
						lang: "ts",
						sourcemap: true
					} );
					map = result.map ? createSourceMap( result.map ) : null;
				}
			}
		} catch {
			map = null;
		}
		loaded.set( url, map );
		return map;
	}

	return {
		/*
		================
		prepare

		Loads maps for every URL a profile names, before synchronous lookups.
		================
		*/
		async prepare( urls ) {
			for ( const url of urls ) if ( url ) await mapFor( url );
		},
		/*
		================
		frame

		{ name, file, line } of a call frame (lineNumber and columnNumber are
		zero-based as DevTools records them).
		================
		*/
		frame( callFrame ) {
			const fallback = {
				name: callFrame.functionName || "(anonymous)",
				file: callFrame.url ? path.basename( new URL( callFrame.url, "http://x" ).pathname ) : "",
				line: callFrame.lineNumber + 1
			};
			const map = callFrame.url ? loaded.get( callFrame.url ) : null;
			if ( !map || callFrame.lineNumber < 0 ) return fallback;
			const found = map.lookup( callFrame.lineNumber, callFrame.columnNumber );
			if ( !found ) return fallback;
			return {
				name: found.name ?? fallback.name,
				file: shortSource( found.source ),
				line: found.line
			};
		},
		/*
		================
		position

		The source { file, line } of a sampled position inside url (one-based
		line and column, as profile chunks record them).
		================
		*/
		position( url, line, column ) {
			const map = url ? loaded.get( url ) : null;
			if ( !map ) return null;
			const found = map.lookup( line - 1, Math.max( 0, column - 1 ) );
			return found ? { file: shortSource( found.source ), line: found.line } : null;
		}
	};
}
