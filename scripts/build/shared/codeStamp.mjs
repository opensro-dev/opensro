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
import { scriptKeys } from "./scriptReach.mjs";

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
// The same import at module level only (column 0): what loading the module
// runs, without the imports its functions make when they are called.
const PYTHON_MODULE_IMPORT = /^(?:from\s+([A-Za-z_]\w*)[\w.]*\s+import|import\s+([A-Za-z_]\w*))/gm;

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
	return walkClosure( [ entryFile ], [], repositoryRoot, await sourceIndexes( repositoryRoot ), PYTHON_IMPORT, true );
}

/*
================
moduleClosure

The code of what a step ran (variant_steps.py, #4251): ranFiles are the
Python files whose code ran, importedFiles those the step only imported.
A Python file's module-level imports are followed, not the imports inside
functions the step never called. A file that ran also brings the Node
scripts it names (it may have run them); a file reached only by import
brings its own imports and the JSON beside it, never its scripts: a
module-level import of cap110_item_import put its asset-dependency script,
and the ~75 pipeline modules behind it, into the shop step's key.
================
*/
export async function moduleClosure( ranFiles, repositoryRoot = rebuildRoot, importedFiles = [] ) {
	const indexes = await sourceIndexes( repositoryRoot );
	return walkClosure( ranFiles, importedFiles, repositoryRoot, indexes, PYTHON_MODULE_IMPORT, false );
}

/*
================
moduleClosures

moduleClosure for many records at once, [ ranFiles, importedFiles ] each:
the variant runner keys every step of a build in one process, indexing the
source roots once, instead of one Node start per step (~0.5 s each).
================
*/
export async function moduleClosures( records, repositoryRoot = rebuildRoot ) {
	const indexes = await sourceIndexes( repositoryRoot );
	const closures = [];
	for ( const [ranFiles, importedFiles] of records ) {
		closures.push(
			await walkClosure( ranFiles, importedFiles, repositoryRoot, indexes, PYTHON_MODULE_IMPORT, false )
		);
	}
	return closures;
}

/*
================
moduleClosureKeys

moduleClosures with what each closure's Node scripts can reach
(scriptReach.mjs): per record, { files, scripts }, scripts mapping a
script to its reached digest and flags, or null when the step reaches
none of it. The scripts the step's Python names are its entries.
================
*/
export async function moduleClosureKeys( records, repositoryRoot = rebuildRoot ) {
	const indexes = await sourceIndexes( repositoryRoot );
	const keys = [];
	for ( const [ranFiles, importedFiles] of records ) {
		const entries = new Set();
		const files = await walkClosure(
			ranFiles,
			importedFiles,
			repositoryRoot,
			indexes,
			PYTHON_MODULE_IMPORT,
			false,
			entries
		);
		keys.push( { files, scripts: await scriptKeys( files, [ ...entries ] ) } );
	}
	return keys;
}

/*
================
sourceIndexes

The Python and script indexes one walk resolves names against.
================
*/
async function sourceIndexes( repositoryRoot ) {
	return { python: await sourceIndex( repositoryRoot, ".py" ), scripts: await sourceIndex( repositoryRoot, ".mjs" ) };
}

/*
================
walkClosure

The one closure walk behind codeClosure and moduleClosure. ranFiles are
walked in full; importedFiles, and with importsRun false the Python modules
any file imports, are walked without the scripts they name. pythonImport is
the pattern a Python file's followed imports match. A file reached first
by import and later as run code is walked again for its scripts.
entries, when given, collects the Node scripts a Python file that ran
names: the scripts the closure's code may run.
================
*/
async function walkClosure(
	ranFiles,
	importedFiles,
	repositoryRoot,
	{ python, scripts },
	pythonImport,
	importsRun,
	entries = new Set()
) {
	// file -> whether it was walked as code that ran.
	const seen = new Map();
	const pending = [
		...importedFiles.map( ( file ) => [ path.resolve( file ), false ] ),
		...ranFiles.map( ( file ) => [ path.resolve( file ), true ] )
	];
	for ( let next = pending.pop(); next !== undefined; next = pending.pop() ) {
		const [file, ran] = next;
		if ( seen.has( file ) && (seen.get( file ) || !ran) ) continue;
		seen.set( file, ran );
		const directory = path.dirname( file );
		if ( file.endsWith( ".py" ) ) {
			const text = await readFile( file, "utf8" );
			for ( const match of text.matchAll( pythonImport ) ) {
				for ( const module of python.get( `${match[1] ?? match[2]}.py` ) ?? [] ) {
					pending.push( [ module, importsRun ] );
				}
			}
			const code = text.replace( PYTHON_COMMENT, "" );
			if ( ran ) {
				for ( const match of code.matchAll( SCRIPT_LITERAL ) ) {
					const script = resolveNamed( match[1], { file, directory, repositoryRoot, index: scripts } );
					entries.add( script );
					pending.push( [ script, true ] );
				}
			}
			for ( const match of code.matchAll( DATA_LITERAL ) ) {
				// Only beside the module: any other JSON a module names is
				// build input or output, which the caller fingerprints itself.
				const data = path.join( directory, match[1] );
				if ( existsSync( data ) ) seen.set( data, true );
			}
			continue;
		}
		if ( !file.endsWith( ".mjs" ) && !file.endsWith( ".js" ) ) continue;
		// A script is walked in full: whatever names it ran it.
		const source = (await readFile( file, "utf8" )).replace( COMMENTS, "" );
		for ( const match of source.matchAll( RELATIVE_IMPORT ) ) {
			pending.push( [ path.resolve( directory, match[1] ), true ] );
		}
		for ( const match of source.matchAll( PYTHON_LITERAL ) ) {
			pending.push( [ resolveNamed( match[1], { file, directory, repositoryRoot, index: python } ), true ] );
		}
	}
	return [ ...seen.keys() ].sort();
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

The sha256 over what the entry (a file URL or path) can run, keyed by
repository-relative path. A Node module counts by what the entry and the
scripts its Python names can reach (scriptReach.mjs): a shared module
gaining a declaration nothing reached names leaves the hash standing, and
a module that reaches nothing leaves it. Other files (Python, the JSON
beside a module) count by their bytes. A checkout that only touches
mtimes changes nothing. Keyed on whole files, #4596's unused constant in
world/constants.mjs reran the npc, title and interface lanes of r204n's
no-op asset build (#4337).
================
*/
export async function codeHash( entry, repositoryRoot = rebuildRoot ) {
	const hash = createHash( "sha256" );
	const entryFile = path.resolve( String( entry ).startsWith( "file:" ) ? fileURLToPath( entry ) : entry );
	const entries = new Set( [ entryFile ] );
	const indexes = await sourceIndexes( repositoryRoot );
	const files = await walkClosure( [ entryFile ], [], repositoryRoot, indexes, PYTHON_IMPORT, true, entries );
	const reached = await scriptKeys( files, [ ...entries ] );
	for ( const file of files ) {
		const relative = path.relative( repositoryRoot, file ).split( path.sep ).join( "/" );
		if ( !Object.hasOwn( reached, file ) ) {
			hash.update( relative ).update( "\0" ).update( await readFile( file ) ).update( "\0" );
			continue;
		}
		const key = reached[file];
		if ( key === null ) continue;
		hash.update( relative ).update( "\0" ).update( key.digest ).update( key.flags.join( "," ) ).update( "\0" );
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
