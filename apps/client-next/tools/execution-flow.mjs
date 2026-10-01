import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
import path from "node:path";
import fs from "node:fs";
import { root, ts } from "./project.mjs";

/*
================
executionFlow

The resolved execution graph for a client tree. For the real client (the
common case: `verify:execution` and the architecture tests both ask for it)
the graph is cached under temp/cache/execution-flow, keyed by a hash of
everything it is computed from: the TypeScript version, tsconfig.json, the
ownership manifest, these analysis tools and every source file. Any change to
an input changes the key, so a stale graph can never be returned.
================
*/
export function executionFlow( base = root ) {
	if ( path.resolve( base ) !== path.resolve( root ) ) return computeExecutionFlow( base );
	const key = executionFlowKey( base );
	const cacheFile = path.join( base, "temp", "cache", "execution-flow", `${key}.json` );
	if ( fs.existsSync( cacheFile ) ) return JSON.parse( fs.readFileSync( cacheFile, "utf8" ) );
	const graph = computeExecutionFlow( base );
	const directory = path.dirname( cacheFile );
	fs.mkdirSync( directory, { recursive: true } );
	// Only the current tree's graph is worth keeping; drop superseded ones.
	for ( const stale of fs.readdirSync( directory ) ) {
		if ( !stale.startsWith( key ) ) fs.rmSync( path.join( directory, stale ), { force: true } );
	}
	// Write then rename: concurrent checks may compute the same graph at once,
	// and a reader must never see a half-written file.
	const temporary = `${cacheFile}.${process.pid}.tmp`;
	fs.writeFileSync( temporary, JSON.stringify( graph ) );
	fs.renameSync( temporary, cacheFile );
	return graph;
}

/*
================
executionFlowKey
================
*/
function executionFlowKey( base ) {
	const hash = createHash( "sha256" );
	hash.update( ts.version );
	const add = file => {
		hash.update( path.relative( base, file ).replaceAll( "\\", "/" ) );
		hash.update( "\0" );
		hash.update( fs.readFileSync( file ) );
		hash.update( "\0" );
	};
	for ( const file of [ "tsconfig.json", "src/engine/ownership.json" ] ) add( path.join( base, file ) );
	const tools = path.dirname( fileURLToPath( import.meta.url ) );
	for ( const file of [ "execution-flow.mjs", "project.mjs" ] ) add( path.join( tools, file ) );
	const walk = directory => {
		for (
			const entry of fs.readdirSync( directory, { withFileTypes: true } ).sort( ( a, b ) =>
				a.name.localeCompare( b.name )
			)
		) {
			const full = path.join( directory, entry.name );
			if ( entry.isDirectory() ) walk( full );
			else add( full );
		}
	};
	walk( path.join( base, "src" ) );
	return hash.digest( "hex" );
}

// Inclusion-based value flow. Function values, returned objects, aliases and
// parent-issued callbacks propagate to a fixed point. This is conservative:
// branches are unioned, never assumed unreachable to make a gate pass.
function computeExecutionFlow( base ) {
	const config = ts.readConfigFile( path.join( base, "tsconfig.json" ), ts.sys.readFile );
	if ( config.error ) throw new Error( ts.flattenDiagnosticMessageText( config.error.messageText, "\n" ) );
	const parsed = ts.parseJsonConfigFileContent( config.config, ts.sys, base );
	const program = ts.createProgram( parsed.fileNames, parsed.options );
	const checker = program.getTypeChecker();
	const local = source =>
		!source.isDeclarationFile &&
		source.fileName.replaceAll( "\\", "/" ).startsWith( base.replaceAll( "\\", "/" ) + "/src/" );
	const sources = program.getSourceFiles().filter( local ).sort( ( a, b ) => a.fileName.localeCompare( b.fileName ) );
	const relative = source => path.relative( base, source.fileName ).replaceAll( "\\", "/" );
	const values = new Map(),
		constraints = [],
		properties = [],
		calls = [],
		definitions = new Map(),
		parents = new Map();
	const functions = new Map(), objects = new Map(), returns = new Map(), maps = new Set();
	const slot = key => {
		if ( !values.has( key ) ) values.set( key, new Set() );
		return key;
	};
	const put = ( key, value ) => values.get( slot( key ) ).add( value );
	const edge = ( from, to ) => {
		slot( from );
		slot( to );
		constraints.push( [ from, to ] );
	};
	const symbol = node => {
		let s = checker.getSymbolAtLocation( node );
		if ( s?.flags & ts.SymbolFlags.Alias ) s = checker.getAliasedSymbol( s );
		return s ?? node;
	};
	const prop = ( object, name ) => {
		const map = objects.get( object );
		if ( !map.has( name ) ) map.set( name, {} );
		return slot( map.get( name ) );
	};
	const printer = ts.createPrinter( { removeComments: true } );
	const syntax = n => printer.printNode( ts.EmitHint.Unspecified, n, n.getSourceFile() );
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
	const identities = new Map();
	function scan( n, owner ) {
		if ( ts.isFunctionLike( n ) && n.body ) {
			const key = `${owner ?? relative( n.getSourceFile() ) + "#module"}/${functionName( n )}`;
			const occurrence = (identities.get( key ) ?? 0) + 1;
			identities.set( key, occurrence );
			const id = occurrence === 1 ? key : `${key}~${occurrence}`;
			const result = {};
			functions.set( n, id );
			definitions.set( id, {
				id,
				file: relative( n.getSourceFile() ),
				name: functionName( n ),
				line: n.getSourceFile().getLineAndCharacterOfPosition( n.getStart() ).line + 1,
				async: !!n.modifiers?.some( m => m.kind === ts.SyntaxKind.AsyncKeyword )
			} );
			returns.set( id, result );
			slot( result );
			put( n, id );
			if ( n.name && ts.isFunctionDeclaration( n ) ) edge( n, symbol( n.name ) );
			owner = id;
		}
		parents.set( n, owner );
		if ( ts.isObjectLiteralExpression( n ) ) {
			objects.set( n, new Map() );
			put( n, n );
		}
		ts.forEachChild( n, child => scan( child, owner ) );
	}
	for ( const source of sources ) scan( source, null );
	function build( n ) {
		slot( n );
		if ( ts.isIdentifier( n ) ) edge( symbol( n ), n );
		if ( (ts.isVariableDeclaration( n ) || ts.isParameter( n )) && n.initializer ) {
			edge( n.initializer, symbol( n.name ) );
		}
		if (
			ts.isParenthesizedExpression( n ) || ts.isNonNullExpression( n ) || ts.isAsExpression( n ) ||
			ts.isTypeAssertionExpression( n ) || ts.isSatisfiesExpression( n )
		) edge( n.expression, n );
		if ( ts.isConditionalExpression( n ) ) {
			edge( n.whenTrue, n );
			edge( n.whenFalse, n );
		}
		if ( ts.isBinaryExpression( n ) ) {
			if ( n.operatorToken.kind === ts.SyntaxKind.EqualsToken ) {
				if ( ts.isIdentifier( n.left ) ) edge( n.right, symbol( n.left ) );
				else if ( ts.isPropertyAccessExpression( n.left ) ) {
					properties.push( {
						object: n.left.expression,
						name: n.left.name.text,
						value: n.right,
						write: true
					} );
				}
				edge( n.right, n );
			} else {
				edge( n.left, n );
				edge( n.right, n );
			}
		}
		if ( ts.isObjectLiteralExpression( n ) ) {
			for ( const p of n.properties ) {
				if ( ts.isPropertyAssignment( p ) ) {
					edge( p.initializer, prop( n, p.name.getText().replace( /^['"]|['"]$/g, "" ) ) );
				} else if ( ts.isShorthandPropertyAssignment( p ) ) {
					edge( checker.getShorthandAssignmentValueSymbol( p ) ?? symbol( p.name ), prop( n, p.name.text ) );
				} else if ( ts.isMethodDeclaration( p ) ) {
					edge( p, prop( n, p.name.getText() ) );
				}
			}
		}
		if ( ts.isPropertyAccessExpression( n ) ) {
			properties.push( { object: n.expression, name: n.name.text, value: n, write: false } );
		}
		if ( ts.isElementAccessExpression( n ) && ts.isStringLiteralLike( n.argumentExpression ) ) {
			properties.push( { object: n.expression, name: n.argumentExpression.text, value: n, write: false } );
		}
		if ( ts.isReturnStatement( n ) && n.expression && parents.get( n ) ) {
			edge( n.expression, returns.get( parents.get( n ) ) );
		}
		if ( ts.isArrowFunction( n ) && !ts.isBlock( n.body ) ) edge( n.body, returns.get( functions.get( n ) ) );
		if ( ts.isCallExpression( n ) || ts.isNewExpression( n ) ) {
			const declaration = checker.getResolvedSignature( n )?.declaration;
			const external = !!declaration?.getSourceFile().isDeclarationFile;
			calls.push( { node: n, owner: parents.get( n ), external, declaration, targets: new Set() } );
			// Track native Map values conservatively (keys are unioned). This
			// keeps capabilities stored in owned job tables visible to the gate.
			if ( ts.isNewExpression( n ) && external && declaration.parent?.name?.getText() === "MapConstructor" ) {
				objects.set( n, new Map() );
				maps.add( n );
				put( n, n );
			}
			// Object.freeze preserves object identity; it does not hide capability values.
			if ( n.expression.getText() === "Object.freeze" && n.arguments?.[0] ) edge( n.arguments[0], n );
			if ( declaration && functions.has( declaration ) ) put( n.expression, functions.get( declaration ) );
		}
		ts.forEachChild( n, build );
	}
	for ( const source of sources ) build( source );
	// Worklist fixed point. Inclusion constraints, property reads and writes,
	// Map get/set and call-target binding are monotone set unions, so
	// propagating only keys whose values grew reaches the same least fixed point
	// as re-merging every constraint each pass (which this replaced: it spent
	// most of the analysis re-merging unchanged sets).
	function propagate() {
		const byIdLocal = new Map( [ ...functions ].map( ( [n, id] ) => [ id, n ] ) );
		const successors = new Map(), queue = [], queued = new Set(), handled = new Map();
		const parameterSymbols = new Map();
		const parameterSymbol = n => {
			if ( !parameterSymbols.has( n ) ) parameterSymbols.set( n, symbol( n ) );
			return parameterSymbols.get( n );
		};
		const index = ( table, key, row ) => {
			const rows = table.get( key );
			if ( rows ) rows.push( row );
			else table.set( key, [ row ] );
		};
		const enqueue = key => {
			if ( !queued.has( key ) ) {
				queued.add( key );
				queue.push( key );
			}
		};
		const flow = ( from, to ) => {
			slot( from );
			slot( to );
			let next = successors.get( from );
			if ( !next ) {
				next = new Set();
				successors.set( from, next );
			}
			if ( next.has( to ) ) return;
			next.add( to );
			const source = values.get( from ), target = values.get( to );
			let grew = false;
			for ( const v of source ) {
				if ( !target.has( v ) ) {
					target.add( v );
					grew = true;
				}
			}
			if ( grew ) enqueue( to );
		};
		const reads = new Map(), mapCalls = new Map(), callees = new Map();
		for ( const p of properties ) index( reads, p.object, p );
		for ( const c of calls ) {
			if ( c.external && ts.isPropertyAccessExpression( c.node.expression ) ) {
				index( mapCalls, c.node.expression.expression, c );
			}
			index( callees, c.node.expression, c );
		}
		for ( const [from, to] of constraints ) flow( from, to );
		for ( const [key, set] of values ) if ( set.size ) enqueue( key );
		for ( let head = 0; head < queue.length; head++ ) {
			const key = queue[head];
			queued.delete( key );
			const current = values.get( key );
			for ( const to of successors.get( key ) ?? [] ) {
				const target = values.get( to );
				let grew = false;
				for ( const v of current ) {
					if ( !target.has( v ) ) {
						target.add( v );
						grew = true;
					}
				}
				if ( grew ) enqueue( to );
			}
			let done = handled.get( key );
			if ( !done ) {
				done = new Set();
				handled.set( key, done );
			}
			for ( const v of current ) {
				if ( done.has( v ) ) continue;
				done.add( v );
				if ( objects.has( v ) ) {
					for ( const p of reads.get( key ) ?? [] ) {
						if ( p.write ) flow( p.value, prop( v, p.name ) );
						else flow( prop( v, p.name ), p.value );
					}
				}
				if ( maps.has( v ) ) {
					for ( const c of mapCalls.get( key ) ?? [] ) {
						const name = c.node.expression.name.text;
						if ( name === "set" && c.node.arguments?.[1] ) {
							flow( c.node.arguments[1], prop( v, "[[MapValues]]" ) );
							flow( key, c.node );
						}
						if ( name === "get" ) flow( prop( v, "[[MapValues]]" ), c.node );
					}
				}
				if ( byIdLocal.has( v ) ) {
					for ( const c of callees.get( key ) ?? [] ) {
						c.targets.add( v );
						flow( returns.get( v ), c.node );
						const params = byIdLocal.get( v ).parameters;
						c.node.arguments?.forEach( ( arg, i ) => {
							if ( params[i] ) flow( arg, parameterSymbol( params[i].name ) );
						} );
					}
				}
			}
			if ( head > 4096 && head * 2 > queue.length ) {
				queue.splice( 0, head + 1 );
				head = -1;
			}
		}
	}
	const byId = new Map( [ ...functions ].map( ( [n, id] ) => [ id, n ] ) );
	propagate();
	const issues = [], output = [], events = [];
	for ( const p of properties ) {
		if ( p.write ) {
			for ( const id of values.get( slot( p.value ) ) ) {
				if ( byId.has( id ) ) {
					events.push( { file: relative( p.object.getSourceFile() ), event: p.name, target: id } );
				}
			}
		}
	}
	for ( const c of calls ) {
		const source = c.node.getSourceFile(),
			line = source.getLineAndCharacterOfPosition( c.node.getStart() ).line + 1;
		const targets = [ ...c.targets ].sort();
		if ( !targets.length && !c.external ) {
			issues.push( `${relative( source )}:${line}: unresolved execution ${c.node.expression.getText()}` );
		}
		const callbacks = [];
		c.node.arguments?.forEach( ( arg, index ) => {
			for ( const id of values.get( slot( arg ) ) ) {
				if ( byId.has( id ) ) callbacks.push( { argument: index, target: id } );
			}
		} );
		output.push( {
			file: relative( source ),
			line,
			caller: c.owner,
			expression: syntax( c.node.expression ),
			targets,
			external: c.external ?
				`${path.basename( c.declaration.getSourceFile().fileName )}:${
					c.declaration.name?.getText() ?? "signature"
				}` :
				null,
			callbacks
		} );
	}
	const ownership = JSON.parse( fs.readFileSync( path.join( base, "src/engine/ownership.json" ), "utf8" ) );
	return {
		version: 2,
		analysis:
			"Conservative fixed-point function/object value flow; external APIs are declared boundaries, not JavaScript execution proofs.",
		entry: ownership.bootstrap,
		workers: Object.entries( ownership.workers ).map( ( [entry, parent] ) => ({ entry, parent }) ),
		functions: [ ...definitions.values() ],
		calls: output,
		events,
		issues
	};
}
