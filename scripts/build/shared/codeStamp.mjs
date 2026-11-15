/*
===========================================================================

codeStamp.mjs - a reuse cache is only as fresh as the code that filled it

The outdoor build keeps a region bundle, and the shared object and render
indexes, whenever the file already exists. It never asked whether the code
that wrote them had changed since, so a builder fix never reached a tree
that was already built: the shared tree kept 4,889 outdoor meshes and 2,126
region bundles an older builder wrote, while two fresh clones agreed with
each other (clean-room comparison, 2026-10-08).

A stamp records the content hash of the code behind one reuse cache: the
entry module and every module it imports, followed through relative
imports, plus the Python helpers those modules name and the sibling
modules those helpers import, and the Node scripts those helpers run
(cap110_item_import.py runs scripts/analysis/cap110_asset_dependencies.mjs).
A cache whose stamp is missing or different is rebuilt in full and stamped
only after that run succeeds; the old stamp is removed before the first write under other
code, so a partial or failed run never leaves a stamp beside outputs it
does not describe. Stamps live in the generated tree they describe
(generatedPath( "build-stamps" )), so every worktree sharing it agrees.

===========================================================================
*/
import { createHash } from "node:crypto";
import { existsSync } from "node:fs";
import { readdir, readFile, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { generatedPath } from "../../lib/generatedRoot.mjs";
import { writeStateFile } from "./stateFile.mjs";
import { rebuildRoot } from "../world/paths.mjs";

// Static and dynamic imports with a relative specifier.
const RELATIVE_IMPORT = /(?:\bfrom\s*|\bimport\s*\(\s*|\bimport\s+)["'](\.{1,2}\/[^"']+)["']/g;
// Any string literal naming a Python file, however the path around it is
// assembled (new URL, path.join pieces, a repository-relative string).
const PYTHON_LITERAL = /["'`]([\w./-]*\w\.py)["'`]/g;
// A Python file's string literal naming a Node script it runs.
const SCRIPT_LITERAL = /["'`]([\w./-]*\w\.mjs)["'`]/g;
// A Python file's string literal naming a JSON file beside it
// (Path(__file__).with_name( "vsro_archive_pins.json" )): the pins and
// tables a module reads are part of what it does.
const DATA_LITERAL = /["']([\w.-]+\.json)["']/g;
// A Python whole-line comment, removed before those scans.
const PYTHON_COMMENT = /^\s*#.*$/gm;
// Where the repository's Python and tool scripts live; a literal resolves
// against these.
const SOURCE_ROOTS = [ "scripts", path.join( "apps", "client-next", "tools" ) ];
// Block comments and whole-line comments, removed before scanning so an
// example in prose is never taken for a dependency.
const COMMENTS = /\/\*[\s\S]*?\*\/|^\s*\/\/.*$/gm;
// A Python import of a top-level name; it is followed when the repository
// holds a module of that name (scripts/sro_paths.py), else it is a package.
const PYTHON_IMPORT = /^\s*(?:from\s+([A-Za-z_]\w*)[\w.]*\s+import|import\s+([A-Za-z_]\w*))/gm;

/*
================
stampPath
================
*/
function stampPath( name ) {
	return generatedPath( "build-stamps", `${name}.json` );
}

/*
================
codeClosure

Every source file the entry can run: relative JS imports followed
transitively, every Python file a module names, the repository modules a
Python file imports, the Node scripts it names and the JSON files it names
beside itself (data, not followed further). A Python or script
literal that names no repository file, or more than one, throws: a
dependency the stamp cannot see is the stale-reuse bug it exists to prevent. Package imports are pinned by the lockfiles and
are not followed.
================
*/
export async function codeClosure( entryFile, repositoryRoot = rebuildRoot ) {
	const python = await sourceIndex( repositoryRoot, ".py" );
	const scripts = await sourceIndex( repositoryRoot, ".mjs" );
	const seen = new Set();
	const pending = [ path.resolve( entryFile ) ];
	for ( let file = pending.pop(); file !== undefined; file = pending.pop() ) {
		if ( seen.has( file ) ) continue;
		seen.add( file );
		const directory = path.dirname( file );
		if ( file.endsWith( ".py" ) ) {
			const text = await readFile( file, "utf8" );
			for ( const match of text.matchAll( PYTHON_IMPORT ) ) {
				pending.push( ...(python.get( `${match[1] ?? match[2]}.py` ) ?? []) );
			}
			const code = text.replace( PYTHON_COMMENT, "" );
			for ( const match of code.matchAll( SCRIPT_LITERAL ) ) {
				pending.push( resolveNamed( match[1], { file, directory, repositoryRoot, index: scripts } ) );
			}
			for ( const match of code.matchAll( DATA_LITERAL ) ) {
				// Only beside the module: any other JSON a module names is
				// build input or output, which the caller fingerprints itself.
				const data = path.join( directory, match[1] );
				if ( existsSync( data ) ) seen.add( data );
			}
			continue;
		}
		if ( !file.endsWith( ".mjs" ) && !file.endsWith( ".js" ) ) continue;
		const source = (await readFile( file, "utf8" )).replace( COMMENTS, "" );
		for ( const match of source.matchAll( RELATIVE_IMPORT ) ) pending.push( path.resolve( directory, match[1] ) );
		for ( const match of source.matchAll( PYTHON_LITERAL ) ) {
			pending.push( resolveNamed( match[1], { file, directory, repositoryRoot, index: python } ) );
		}
	}
	return [ ...seen ].sort();
}

/*
================
sourceIndex

Every file with this extension under the repository's source roots, by
file name.
================
*/
async function sourceIndex( repositoryRoot, extension ) {
	const index = new Map();
	for ( const root of SOURCE_ROOTS ) {
		let names = [];
		try {
			names = await readdir( path.join( repositoryRoot, root ), { recursive: true } );
		} catch ( error ) {
			if ( error.code !== "ENOENT" ) throw error;
		}
		for ( const name of names ) {
			if ( !name.endsWith( extension ) || name.includes( "__pycache__" ) || name.includes( "node_modules" ) ) {
				continue;
			}
			const file = path.join( repositoryRoot, root, name );
			const list = index.get( path.basename( name ) ) ?? [];
			list.push( file );
			index.set( path.basename( name ), list );
		}
	}
	return index;
}

/*
================
resolveNamed

The one repository file a literal names: relative to the module, relative
to the repository, or by file name in the index of its kind.
================
*/
function resolveNamed( literal, { file, directory, repositoryRoot, index } ) {
	const byName = index.get( path.basename( literal ) ) ?? [];
	for ( const candidate of [ path.resolve( directory, literal ), path.resolve( repositoryRoot, literal ) ] ) {
		if ( byName.includes( candidate ) ) return candidate;
	}
	if ( byName.length === 1 ) return byName[0];
	throw new Error(
		`${path.relative( repositoryRoot, file )} names "${literal}", which matches ${byName.length} ` +
			"repository files; a code stamp must see every file a builder runs"
	);
}

/*
================
codeHash

The sha256 over the entry's closure (a file URL or path), keyed by
repository-relative path: any byte change in it changes the hash, a
checkout that only touches mtimes does not.
================
*/
export async function codeHash( entry, repositoryRoot = rebuildRoot ) {
	const hash = createHash( "sha256" );
	const entryFile = String( entry ).startsWith( "file:" ) ? fileURLToPath( entry ) : entry;
	for ( const file of await codeClosure( entryFile, repositoryRoot ) ) {
		const relative = path.relative( repositoryRoot, file ).split( path.sep ).join( "/" );
		hash.update( relative ).update( "\0" ).update( await readFile( file ) ).update( "\0" );
	}
	return hash.digest( "hex" );
}

/*
================
stampIsCurrent

Whether the named cache was last filled by code with this hash.
================
*/
export async function stampIsCurrent( name, hash ) {
	try {
		return JSON.parse( await readFile( stampPath( name ), "utf8" ) ).codeHash === hash;
	} catch ( error ) {
		if ( error.code === "ENOENT" ) return false;
		throw error;
	}
}

/*
================
invalidateStamp

Forgets the named cache's stamp before code it does not describe writes
into the cache.
================
*/
export async function invalidateStamp( name ) {
	await rm( stampPath( name ), { force: true } );
}

/*
================
writeStamp

Records that a complete run of the named cache used code with this hash.
================
*/
export async function writeStamp( name, hash ) {
	await writeStateFile( stampPath( name ), `${JSON.stringify( { name, codeHash: hash } )}\n` );
}
