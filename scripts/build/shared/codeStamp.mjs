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
modules those helpers import. A cache whose stamp is
missing or different is rebuilt in full and stamped only after that run
succeeds. Stamps live in the generated tree they describe
(generatedPath( "build-stamps" )), so every worktree sharing it agrees.

===========================================================================
*/
import { createHash } from "node:crypto";
import { access, mkdir, readFile, rename, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { generatedPath } from "../../lib/generatedRoot.mjs";
import { rebuildRoot } from "../world/paths.mjs";

// Static and dynamic imports with a relative specifier.
const RELATIVE_IMPORT = /(?:\bfrom\s*|\bimport\s*\(\s*|\bimport\s+)["'](\.{1,2}\/[^"']+)["']/g;
// Python helpers named relative to the module (new URL( "x.py", import.meta.url ))
// or to the repository (path.join( rebuildRoot, "scripts/.../x.py" )).
const MODULE_PYTHON = /new URL\(\s*["']([^"']+\.py)["']\s*,\s*import\.meta\.url/g;
const REPOSITORY_PYTHON = /["'](scripts\/[^"']+\.py)["']/g;
// Block comments and whole-line comments, removed before scanning so an
// example in prose is never taken for a dependency.
const COMMENTS = /\/\*[\s\S]*?\*\/|^\s*\/\/.*$/gm;
// A Python import of a top-level name; it is followed when a module of that
// name sits beside the importer (scripts/sro_paths.py), else it is a package.
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
transitively, the Python helpers they name, and the sibling modules a
Python file imports. Package imports are pinned by the lockfiles and are
not followed.
================
*/
export async function codeClosure( entryFile ) {
	const seen = new Set();
	const pending = [ path.resolve( entryFile ) ];
	for ( let file = pending.pop(); file !== undefined; file = pending.pop() ) {
		if ( seen.has( file ) ) continue;
		seen.add( file );
		const directory = path.dirname( file );
		if ( file.endsWith( ".py" ) ) {
			pending.push( ...(await pythonSiblings( file, directory )) );
			continue;
		}
		if ( !file.endsWith( ".mjs" ) && !file.endsWith( ".js" ) ) continue;
		const source = (await readFile( file, "utf8" )).replace( COMMENTS, "" );
		for ( const match of source.matchAll( RELATIVE_IMPORT ) ) pending.push( path.resolve( directory, match[1] ) );
		for ( const match of source.matchAll( MODULE_PYTHON ) ) pending.push( path.resolve( directory, match[1] ) );
		for ( const match of source.matchAll( REPOSITORY_PYTHON ) ) {
			pending.push( path.resolve( rebuildRoot, match[1] ) );
		}
	}
	return [ ...seen ].sort();
}

/*
================
pythonSiblings

The modules beside a Python file that it imports by name.
================
*/
async function pythonSiblings( file, directory ) {
	const siblings = [];
	for ( const match of (await readFile( file, "utf8" )).matchAll( PYTHON_IMPORT ) ) {
		const sibling = path.join( directory, `${match[1] ?? match[2]}.py` );
		try {
			await access( sibling );
			siblings.push( sibling );
		} catch {
			// a package or the standard library
		}
	}
	return siblings;
}

/*
================
codeHash

The sha256 over the entry's closure (a file URL or path), keyed by
repository-relative path: any byte change in it changes the hash, a
checkout that only touches mtimes does not.
================
*/
export async function codeHash( entry ) {
	const hash = createHash( "sha256" );
	const entryFile = String( entry ).startsWith( "file:" ) ? fileURLToPath( entry ) : entry;
	for ( const file of await codeClosure( entryFile ) ) {
		const relative = path.relative( rebuildRoot, file ).split( path.sep ).join( "/" );
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
writeStamp

Records that a complete run of the named cache used code with this hash.
================
*/
export async function writeStamp( name, hash ) {
	const target = stampPath( name );
	await mkdir( path.dirname( target ), { recursive: true } );
	const temporary = `${target}.${process.pid}.tmp`;
	await writeFile( temporary, `${JSON.stringify( { name, codeHash: hash } )}\n` );
	await rename( temporary, target );
}
