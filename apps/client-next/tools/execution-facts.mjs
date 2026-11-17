/*
===========================================================================

execution-facts.mjs - what the type checker says about each source file, cached per file

computeExecutionFlow (execution-flow.mjs) asks the checker one kind of
question: per-file facts of the value flow - which symbol an identifier
names, which declaration a call resolves to. Measured 2026-11-18 on the
client: 41 of the analysis' 45 s were those checker queries, 1.2 s the
global fixed point. And the graph was cached only whole, keyed on every
source file, so every edit paid the 45 s (103 s under a release's load).

So the facts are extracted per file and cached in the shared warm store
(warm-cache.mjs), keyed by the file and every file it transitively
imports: its type closure, the only sources the checker reads to answer
for it. An edit re-extracts only the files whose closure holds it, split
over worker threads when there are many; the fixed point always runs over
every file's facts, so the graph is the same graph a cold run computes.

Every key in the facts is a string, so facts cross threads and runs:

  N:<file>:<pos>:<end>:<kind>      a syntax node
  S:<first declaration>:<name>     a symbol; S#<name>:<flags> without one
  O:<node key>                     the object an object literal or Map makes
  F:<function id>                  a function value
  R:<function id>                  what a function returns
  P:<node key>#<name>              a property slot of an object

Distinct checker symbols with one first declaration share a key. The
analysis is a conservative union, so that can only add flow, never drop it.

===========================================================================
*/
import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { isMainThread, parentPort, Worker, workerData } from "node:worker_threads";
import { ts } from "./project.mjs";
import { sharedWarmCacheRoot } from "./warm-cache.mjs";

const FACTS_VERSION = 3;
const THIS_FILE = fileURLToPath( import.meta.url );
// Below this many misses one in-process program is faster than starting
// workers, each of which builds its own program first (about 2 s).
const WORKER_THRESHOLD = 48;
// Each worker holds a whole program (~0.5 GB); more than this fights the
// rest of a release build for memory and cores.
const MAX_WORKERS = 6;
// The argument that makes this module the extraction child (extractInChild).
const CHILD_FLAG = "--execution-facts-child";
// Store entries no run has used for this long are pruned.
const STALE_DAYS = 7;
// A module that augments a global or another module (factKeys).
const AUGMENTATION = /^\s*declare\s+(global\b|module\s+["'])/m;

/*
================
readProject

The client's tsconfig and its sources: the src/ files, sorted by file name
as computeExecutionFlow has always ordered them.
================
*/
export function readProject( base ) {
	const config = ts.readConfigFile( path.join( base, "tsconfig.json" ), ts.sys.readFile );
	if ( config.error ) throw new Error( ts.flattenDiagnosticMessageText( config.error.messageText, "\n" ) );
	const parsed = ts.parseJsonConfigFileContent( config.config, ts.sys, base );
	const prefix = base.replaceAll( "\\", "/" ) + "/src/";
	const sources = parsed.fileNames
		.filter( file => !file.endsWith( ".d.ts" ) && file.replaceAll( "\\", "/" ).startsWith( prefix ) )
		.map( file => file.replaceAll( "\\", "/" ) )
		.sort( ( a, b ) => a.localeCompare( b ) );
	return { parsed, sources };
}

// ============================================================================
// Extraction (the checker side)
// ============================================================================

/*
================
extractFacts

One program over the whole project, and the facts of the named sources
in their order. The rules are the analysis' own, moved here unchanged
from computeExecutionFlow's scan and build passes.
================
*/
export function extractFacts( base, files ) {
	const { parsed } = readProject( base );
	const program = ts.createProgram( parsed.fileNames, parsed.options );
	const checker = program.getTypeChecker();
	const relative = fileName => path.relative( base, fileName ).replaceAll( "\\", "/" );
	const nodeKey = n => `N:${relative( n.getSourceFile().fileName )}:${n.pos}:${n.end}:${n.kind}`;
	const symbolKey = s => {
		const declaration = s.declarations?.[0] ?? s.valueDeclaration;
		return declaration ?
			`S:${nodeKey( declaration ).slice( 2 )}:${s.escapedName}` :
			`S#${s.escapedName}:${s.flags}`;
	};
	const symbol = n => {
		let s = checker.getSymbolAtLocation( n );
		if ( s?.flags & ts.SymbolFlags.Alias ) s = checker.getAliasedSymbol( s );
		return s ? symbolKey( s ) : nodeKey( n );
	};
	const printer = ts.createPrinter( { removeComments: true } );
	const syntax = n => printer.printNode( ts.EmitHint.Unspecified, n, n.getSourceFile() );
	const lineOf = n => n.getSourceFile().getLineAndCharacterOfPosition( n.getStart() ).line + 1;
	const functionName = n => {
		if ( n.name ) return syntax( n.name );
		const parent = n.parent;
		if ( ts.isVariableDeclaration( parent ) || ts.isPropertyAssignment( parent ) ) return syntax( parent.name );
		if ( ts.isCallExpression( parent ) || ts.isNewExpression( parent ) ) {
			const callee = parent.expression;
			return `callback:${
				ts.isPropertyAccessExpression( callee ) ?
					callee.name.text :
					ts.isIdentifier( callee ) ?
					callee.text :
					"call"
			}:${parent.arguments.indexOf( n )}`;
		}
		if ( ts.isBinaryExpression( parent ) && parent.right === n ) return `callback:${syntax( parent.left )}`;
		return "callback";
	};
	const propertyKey = ( objectKey, name ) => `P:${objectKey}#${name}`;
	// The declarations a call can reach. A callee whose type is a union of
	// two or more callable members reaches every member's signatures: the
	// checker's resolved signature names only the first member by its
	// internal type order, which depends on what it checked first, so it
	// differed between one program and a worker's (the UI's
	// `for ( const owner of [ ... ] ) owner.dispose()`). A union with one
	// callable member (an optional chain's `f | undefined`) keeps the
	// signature the checker resolved, overloads and all. Sorted by key.
	const callDeclarations = ( n, resolved ) => {
		const callee = checker.getTypeAtLocation( n.expression );
		const kind = ts.isNewExpression( n ) ? ts.SignatureKind.Construct : ts.SignatureKind.Call;
		const callable = callee.isUnion() ?
			callee.types.filter( member => checker.getSignaturesOfType( member, kind ).length > 0 ) :
			[];
		if ( callable.length < 2 ) return resolved ? [ resolved ] : [];
		const found = new Map();
		for ( const member of callable ) {
			for ( const signature of checker.getSignaturesOfType( member, kind ) ) {
				if ( signature.declaration ) found.set( nodeKey( signature.declaration ), signature.declaration );
			}
		}
		if ( found.size === 0 && resolved ) found.set( nodeKey( resolved ), resolved );
		return [ ...found.entries() ].sort( ( a, b ) => a[0].localeCompare( b[0] ) ).map( entry => entry[1] );
	};
	// A call is a declared boundary when it can reach no src function body
	// and a declaration file declares it: for one resolved signature, exactly
	// "resolved in a .d.ts", the rule before unions were expanded.
	const inDeclarationFile = d => d.getSourceFile().isDeclarationFile;
	const hasBody = d => ts.isFunctionLike( d ) && !!d.body;

	const fileFacts = source => {
		const file = relative( source.fileName );
		const facts = { file, functions: [], objects: [], maps: [], edges: [], puts: [], properties: [], calls: [] };
		const ids = new Map(), parents = new Map(), identities = new Map();
		const edge = ( from, to ) => facts.edges.push( [ from, to ] );
		const put = ( key, value ) => facts.puts.push( [ key, value ] );
		const scan = ( n, owner ) => {
			if ( ts.isFunctionLike( n ) && n.body ) {
				const name = functionName( n );
				const key = `${owner ?? file + "#module"}/${name}`;
				const occurrence = (identities.get( key ) ?? 0) + 1;
				identities.set( key, occurrence );
				const id = occurrence === 1 ? key : `${key}~${occurrence}`;
				ids.set( n, id );
				facts.functions.push( {
					node: nodeKey( n ),
					id,
					name,
					line: lineOf( n ),
					async: !!n.modifiers?.some( m => m.kind === ts.SyntaxKind.AsyncKeyword ),
					params: n.parameters.map( p => symbol( p.name ) )
				} );
				put( nodeKey( n ), `F:${id}` );
				if ( n.name && ts.isFunctionDeclaration( n ) ) edge( nodeKey( n ), symbol( n.name ) );
				owner = id;
			}
			parents.set( n, owner );
			if ( ts.isObjectLiteralExpression( n ) ) {
				facts.objects.push( nodeKey( n ) );
				put( nodeKey( n ), `O:${nodeKey( n )}` );
			}
			ts.forEachChild( n, child => scan( child, owner ) );
		};
		const build = n => {
			const key = nodeKey( n );
			if ( ts.isIdentifier( n ) ) edge( symbol( n ), key );
			if ( (ts.isVariableDeclaration( n ) || ts.isParameter( n )) && n.initializer ) {
				edge( nodeKey( n.initializer ), symbol( n.name ) );
			}
			if (
				ts.isParenthesizedExpression( n ) || ts.isNonNullExpression( n ) || ts.isAsExpression( n ) ||
				ts.isTypeAssertionExpression( n ) || ts.isSatisfiesExpression( n )
			) edge( nodeKey( n.expression ), key );
			if ( ts.isConditionalExpression( n ) ) {
				edge( nodeKey( n.whenTrue ), key );
				edge( nodeKey( n.whenFalse ), key );
			}
			if ( ts.isBinaryExpression( n ) ) {
				if ( n.operatorToken.kind === ts.SyntaxKind.EqualsToken ) {
					if ( ts.isIdentifier( n.left ) ) edge( nodeKey( n.right ), symbol( n.left ) );
					else if ( ts.isPropertyAccessExpression( n.left ) ) {
						facts.properties.push( {
							object: nodeKey( n.left.expression ),
							name: n.left.name.text,
							value: nodeKey( n.right ),
							write: true
						} );
					}
					edge( nodeKey( n.right ), key );
				} else {
					edge( nodeKey( n.left ), key );
					edge( nodeKey( n.right ), key );
				}
			}
			if ( ts.isObjectLiteralExpression( n ) ) {
				for ( const p of n.properties ) {
					if ( ts.isPropertyAssignment( p ) ) {
						edge(
							nodeKey( p.initializer ),
							propertyKey( key, p.name.getText().replace( /^['"]|['"]$/g, "" ) )
						);
					} else if ( ts.isShorthandPropertyAssignment( p ) ) {
						const value = checker.getShorthandAssignmentValueSymbol( p );
						edge( value ? symbolKey( value ) : symbol( p.name ), propertyKey( key, p.name.text ) );
					} else if ( ts.isMethodDeclaration( p ) ) {
						edge( nodeKey( p ), propertyKey( key, p.name.getText() ) );
					}
				}
			}
			if ( ts.isPropertyAccessExpression( n ) ) {
				facts.properties.push( {
					object: nodeKey( n.expression ),
					name: n.name.text,
					value: key,
					write: false
				} );
			}
			if ( ts.isElementAccessExpression( n ) && ts.isStringLiteralLike( n.argumentExpression ) ) {
				facts.properties.push( {
					object: nodeKey( n.expression ),
					name: n.argumentExpression.text,
					value: key,
					write: false
				} );
			}
			if ( ts.isReturnStatement( n ) && n.expression && parents.get( n ) ) {
				edge( nodeKey( n.expression ), `R:${parents.get( n )}` );
			}
			if ( ts.isArrowFunction( n ) && !ts.isBlock( n.body ) ) edge( nodeKey( n.body ), `R:${ids.get( n )}` );
			if ( ts.isCallExpression( n ) || ts.isNewExpression( n ) ) {
				const declaration = checker.getResolvedSignature( n )?.declaration;
				const candidates = callDeclarations( n, declaration );
				const declared = candidates.filter( inDeclarationFile );
				const external = declared.length > 0 &&
					!candidates.some( d => !inDeclarationFile( d ) && hasBody( d ) );
				const label = external ? declared[0] : null;
				const call = {
					node: key,
					expr: nodeKey( n.expression ),
					owner: parents.get( n ) ?? null,
					args: (n.arguments ?? []).map( nodeKey ),
					line: lineOf( n ),
					expression: syntax( n.expression ),
					text: n.expression.getText(),
					external: label ?
						`${path.basename( label.getSourceFile().fileName )}:${label.name?.getText() ?? "signature"}` :
						null,
					// The src declarations: the functions they are, when they are
					// ones, are known only once every file's facts are in.
					declarations: candidates.filter( d => !inDeclarationFile( d ) ).map( nodeKey )
				};
				if ( external && ts.isPropertyAccessExpression( n.expression ) ) {
					call.receiver = nodeKey( n.expression.expression );
					call.member = n.expression.name.text;
				}
				facts.calls.push( call );
				// Track native Map values conservatively (keys are unioned). This
				// keeps capabilities stored in owned job tables visible to the gate.
				if ( ts.isNewExpression( n ) && external && declaration.parent?.name?.getText() === "MapConstructor" ) {
					facts.objects.push( key );
					facts.maps.push( key );
					put( key, `O:${key}` );
				}
				// Object.freeze preserves object identity; it does not hide capability values.
				if ( n.expression.getText() === "Object.freeze" && n.arguments?.[0] ) {
					edge( nodeKey( n.arguments[0] ), key );
				}
			}
			ts.forEachChild( n, build );
		};
		scan( source, null );
		build( source );
		return facts;
	};
	return files.map( file => {
		const source = program.getSourceFile( file );
		if ( !source ) throw new Error( `execution facts: ${file} is not in the program` );
		return fileFacts( source );
	} );
}

// ============================================================================
// Keys (the type closure of each file)
// ============================================================================

/*
================
sha256
================
*/
function sha256( ...parts ) {
	const hash = createHash( "sha256" );
	for ( const part of parts ) {
		hash.update( part );
		hash.update( "\0" );
	}
	return hash.digest( "hex" );
}

/*
================
factKeys

{ source file -> key }. A file's key hashes, in order: what every file
shares (this tool, the TypeScript version, the tsconfig, the lockfile
that pins the declaration packages, every .d.ts and global script the
program includes, and every module that augments a global or another
module), then each file in its import closure with its bytes. A file
outside every closure that changed leaves the key as it was.

An augmentation (declare global, declare module "x") changes the types of
files that never import it: telemetry.ts adds window.sroDebug for every
file. Its bytes are therefore shared, as a global script's are.
================
*/
export function factKeys( base, project ) {
	const { parsed, sources } = project;
	const relative = file => path.relative( base, file ).replaceAll( "\\", "/" );
	const bytes = new Map();
	const read = file => {
		if ( !bytes.has( file ) ) bytes.set( file, fs.readFileSync( file ) );
		return bytes.get( file );
	};
	const shared = [ String( FACTS_VERSION ), ts.version, fs.readFileSync( THIS_FILE ) ];
	shared.push( fs.readFileSync( path.join( base, "tsconfig.json" ) ) );
	const lockfile = path.resolve( base, "..", "..", "pnpm-lock.yaml" );
	if ( fs.existsSync( lockfile ) ) shared.push( fs.readFileSync( lockfile ) );
	for ( const file of [ ...parsed.fileNames ].sort() ) {
		const text = read( file ), source = text.toString( "utf8" );
		const globalScript = !/^\s*(import|export)\b/m.test( source );
		const augments = AUGMENTATION.test( source );
		if ( file.endsWith( ".d.ts" ) || globalScript || augments ) shared.push( relative( file ), text );
	}
	const sharedKey = sha256( ...shared );

	// The import graph of every non-package file the sources reach.
	const cache = ts.createModuleResolutionCache( base, name => name, parsed.options );
	const imports = new Map();
	const importsOf = file => {
		if ( imports.has( file ) ) return imports.get( file );
		const found = [];
		imports.set( file, found );
		const info = ts.preProcessFile( read( file ).toString( "utf8" ), true, true );
		for ( const reference of info.referencedFiles ) {
			const target = path.resolve( path.dirname( file ), reference.fileName ).replaceAll( "\\", "/" );
			if ( fs.existsSync( target ) ) found.push( target );
		}
		for ( const imported of info.importedFiles ) {
			const resolved =
				ts.resolveModuleName( imported.fileName, file, parsed.options, ts.sys, cache ).resolvedModule;
			if ( resolved && !resolved.isExternalLibraryImport ) {
				found.push( resolved.resolvedFileName.replaceAll( "\\", "/" ) );
			}
		}
		return found;
	};
	const keys = new Map();
	for ( const source of sources ) {
		const closure = new Set( [ source ] ), stack = [ source ];
		while ( stack.length ) {
			for ( const next of importsOf( stack.pop() ) ) {
				if ( !closure.has( next ) ) {
					closure.add( next );
					stack.push( next );
				}
			}
		}
		const parts = [ sharedKey, relative( source ) ];
		for ( const file of [ ...closure ].sort() ) parts.push( relative( file ), read( file ) );
		keys.set( source, sha256( ...parts ) );
	}
	return keys;
}

// ============================================================================
// Store and workers
// ============================================================================

/*
================
factsStore
================
*/
function factsStore() {
	return path.join( sharedWarmCacheRoot(), "execution-facts" );
}

/*
================
readStored

The stored facts for a key, or null on a miss or an unreadable entry.
================
*/
function readStored( store, key, file ) {
	try {
		const facts = JSON.parse( fs.readFileSync( path.join( store, `${key}.json` ), "utf8" ) );
		return facts?.file === file ? facts : null;
	} catch {
		return null;
	}
}

/*
================
writeStored

Write then rename, as warm-cache.mjs does: concurrent checks may store the
same key at once, and a reader must never see a half-written entry.
================
*/
function writeStored( store, key, facts ) {
	try {
		fs.mkdirSync( store, { recursive: true } );
		const target = path.join( store, `${key}.json` );
		const temporary = `${target}.${process.pid}.tmp`;
		fs.writeFileSync( temporary, JSON.stringify( facts ) );
		fs.renameSync( temporary, target );
	} catch ( error ) {
		console.error( `execution facts: store skipped (${error.message})` );
	}
}

/*
================
pruneStore

Drops entries this run did not use and nothing has touched for
STALE_DAYS. A cache, never an input: failures only warn.
================
*/
function pruneStore( store, used ) {
	try {
		const cutoff = Date.now() - STALE_DAYS * 86400000;
		for ( const name of fs.readdirSync( store ) ) {
			if ( !name.endsWith( ".json" ) || used.has( name.replace( /\.json$/, "" ) ) ) continue;
			const file = path.join( store, name );
			if ( fs.statSync( file ).mtimeMs < cutoff ) fs.rmSync( file, { force: true } );
		}
	} catch ( error ) {
		console.error( `execution facts: prune skipped (${error.message})` );
	}
}

/*
================
extractInChild

The missing files' facts from a child process that runs a pool of worker
threads. executionFlow() is synchronous, and its callers (the gate, the map,
the architecture tests) use it so; a worker cannot start while its parent
thread is blocked, so the blocking wait is a spawnSync on a process whose
own event loop is free. Any worker failure fails the child, and the child's
exit code fails this call: a failed extraction never hangs or half-answers.
================
*/
function extractInChild( base, files, store ) {
	fs.mkdirSync( store, { recursive: true } );
	const request = path.join( store, `request-${process.pid}.tmp` ),
		reply = path.join( store, `reply-${process.pid}.tmp` );
	fs.writeFileSync( request, JSON.stringify( { base, files } ) );
	try {
		const child = spawnSync( process.execPath, [ THIS_FILE, CHILD_FLAG, request, reply ], {
			stdio: [ "ignore", "inherit", "inherit" ],
			windowsHide: true
		} );
		if ( child.status !== 0 ) {
			throw new Error( `execution facts: extraction failed (${child.error?.message ?? `exit ${child.status}`})` );
		}
		return JSON.parse( fs.readFileSync( reply, "utf8" ) );
	} finally {
		fs.rmSync( request, { force: true } );
		fs.rmSync( reply, { force: true } );
	}
}

/*
================
runPool

The child's job: split the files into contiguous runs of the sorted list
(neighbouring files share most of their closures), one worker thread each,
and write their facts in order. Rejects on any worker error or bad exit.
================
*/
async function runPool( request, reply ) {
	const { base, files } = JSON.parse( fs.readFileSync( request, "utf8" ) );
	const count = Math.max(
		1,
		Math.min( MAX_WORKERS, os.availableParallelism() - 1, Math.ceil( files.length / WORKER_THRESHOLD ) )
	);
	const size = Math.ceil( files.length / count );
	const parts = [];
	for ( let i = 0; i < files.length; i += size ) parts.push( files.slice( i, i + size ) );
	const results = await Promise.all( parts.map( part =>
		new Promise( ( resolve, reject ) => {
			let facts = null;
			const worker = new Worker( THIS_FILE, { workerData: { executionFacts: true, base, files: part } } );
			worker.once( "message", message => facts = message );
			worker.once( "error", reject );
			worker.once( "exit", code => {
				if ( code === 0 && facts ) resolve( facts );
				else reject( new Error( `execution facts worker exited with ${code}` ) );
			} );
		} )
	) );
	fs.writeFileSync( reply, JSON.stringify( results.flat() ) );
}

/*
================
loadFacts

Every source's facts, in source order: stored ones as they are, the rest
extracted (in a child's worker pool when there are many) and stored. useStore is false
for a scratch tree (the tests' fixtures), which extracts in process and
leaves the shared store alone.
================
*/
export function loadFacts( base, useStore = true ) {
	const project = readProject( base );
	if ( !useStore ) return extractFacts( base, project.sources );
	const store = factsStore();
	const keys = factKeys( base, project );
	const relative = file => path.relative( base, file ).replaceAll( "\\", "/" );
	const found = new Map(), missing = [];
	for ( const source of project.sources ) {
		const facts = readStored( store, keys.get( source ), relative( source ) );
		if ( facts ) found.set( source, facts );
		else missing.push( source );
	}
	if ( missing.length > 0 ) {
		const extracted = missing.length < WORKER_THRESHOLD ?
			extractFacts( base, missing ) :
			extractInChild( base, missing, store );
		missing.forEach( ( source, i ) => {
			found.set( source, extracted[i] );
			writeStored( store, keys.get( source ), extracted[i] );
		} );
		console.error(
			`execution facts: ${project.sources.length - missing.length} stored, ${missing.length} extracted`
		);
	}
	pruneStore( store, new Set( keys.values() ) );
	return project.sources.map( source => found.get( source ) );
}

if ( !isMainThread && workerData?.executionFacts ) {
	parentPort.postMessage( extractFacts( workerData.base, workerData.files ) );
} else if ( isMainThread && process.argv[2] === CHILD_FLAG ) {
	await runPool( process.argv[3], process.argv[4] );
}
