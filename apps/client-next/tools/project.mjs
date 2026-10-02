/*
===========================================================================

project.mjs - the TypeScript source model shared by the architecture tools

Parses every TypeScript file under src/ with the compiler API and loads the
ownership manifest. verify-ownership, verify-capabilities and the execution
flow all read the client through this one model, so they agree on what a
file, an import and a resolved module are.

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";
import ts from "typescript";
export { ts };
export const root = path.resolve( import.meta.dirname, ".." );

// Parsed files by base-relative path, reused while the text is identical.
// The architecture tests verify many near-identical copies of src/ in one
// process; each copy then re-parses only the file it changed. Consumers
// only read the trees.
const parsed = new Map();

/*
================
parse
================
*/
function parse( key, text ) {
	const cached = parsed.get( key );
	if ( cached && cached.text === text ) return cached.file;
	const file = ts.createSourceFile( key, text, ts.ScriptTarget.Latest, true );
	parsed.set( key, { text, file } );
	return file;
}

/*
================
project

Parses every .ts/.tsx/.mts/.cts file under base/src, keyed by its
base-relative path, and loads src/engine/ownership.json.
================
*/
export function project( base = root ) {
	const files = new Map();
	function walk( dir ) {
		for ( const e of fs.readdirSync( dir, { withFileTypes: true } ) ) {
			const full = path.join( dir, e.name );
			if ( e.isDirectory() ) {
				walk( full );
			} else if ( /\.(?:ts|tsx|mts|cts)$/.test( e.name ) ) {
				const key = path.relative( base, full ).replaceAll( "\\", "/" );
				files.set( key, parse( key, fs.readFileSync( full, "utf8" ) ) );
			}
		}
	}
	walk( path.join( base, "src" ) );
	return {
		base,
		files,
		manifest: JSON.parse( fs.readFileSync( path.join( base, "src/engine/ownership.json" ), "utf8" ) )
	};
}
/*
================
visit

Depth-first walk that calls fn on node and every descendant.
================
*/
export function visit( node, fn ) {
	fn( node );
	ts.forEachChild( node, n => visit( n, fn ) );
}
/*
================
imports

Returns every module edge a file declares (static, type-only, re-export,
dynamic and worker) and an error for each one computed at run time: the
architecture checks can only reason about statically declared edges.
================
*/
export function imports( file, node ) {
	const edges = [], errors = [];
	const namedTypes = bindings =>
		bindings && ts.isNamedImports( bindings ) && bindings.elements.length > 0 &&
		bindings.elements.every( e => e.isTypeOnly );
	const add = ( arg, kind, typeOnly = false ) => {
		if ( !arg || !ts.isStringLiteralLike( arg ) ) {
			errors.push( `${file}: computed ${kind} is forbidden` );
			return;
		}
		edges.push( { specifier: arg.text, kind, typeOnly } );
	};
	visit( node, n => {
		if ( ts.isImportDeclaration( n ) ) {
			add(
				n.moduleSpecifier,
				"import",
				!!n.importClause &&
					(n.importClause.isTypeOnly || !n.importClause.name && !!namedTypes( n.importClause.namedBindings ))
			);
		} else if ( ts.isExportDeclaration( n ) && n.moduleSpecifier ) {
			add(
				n.moduleSpecifier,
				"export",
				n.isTypeOnly ||
					!!n.exportClause && ts.isNamedExports( n.exportClause ) && n.exportClause.elements.length > 0 &&
						n.exportClause.elements.every( e => e.isTypeOnly )
			);
		} else if ( ts.isImportTypeNode( n ) ) {
			add( ts.isLiteralTypeNode( n.argument ) ? n.argument.literal : null, "import type", true );
		} else if ( ts.isImportEqualsDeclaration( n ) && ts.isExternalModuleReference( n.moduleReference ) ) {
			add( n.moduleReference.expression, "require", n.isTypeOnly );
		} else if (
			ts.isCallExpression( n ) &&
			(n.expression.kind === ts.SyntaxKind.ImportKeyword ||
				ts.isIdentifier( n.expression ) && n.expression.text === "require")
		) {
			add( n.arguments[0], "dynamic import" );
		} else if ( ts.isNewExpression( n ) && ts.isIdentifier( n.expression ) && n.expression.text === "Worker" ) {
			const url = n.arguments?.[0];
			const base = url && ts.isNewExpression( url ) ? url.arguments?.[1] : undefined;
			if (
				!url || !ts.isNewExpression( url ) || url.expression.getText() !== "URL" ||
				!base || !ts.isPropertyAccessExpression( base ) || base.name.text !== "url" ||
				!ts.isMetaProperty( base.expression ) || base.expression.keywordToken !== ts.SyntaxKind.ImportKeyword ||
				base.expression.name.text !== "meta"
			) {
				errors.push( `${file}: worker URL must be statically declared` );
			} else {
				add( url.arguments?.[0], "worker" );
			}
		}
	} );
	for ( const ref of node.referencedFiles ) {
		edges.push( { specifier: ref.fileName, kind: "reference", typeOnly: true } );
	}
	return { edges, errors };
}
/*
================
resolve

Resolves a relative or @/ specifier to a file in the model, following the
bundler's .js -> .ts substitution. Package imports return null.
================
*/
export function resolve( model, file, specifier ) {
	const aliased = specifier.startsWith( "@/" );
	if ( !aliased && !specifier.startsWith( "./" ) && !specifier.startsWith( "../" ) ) {
		return null;
	}
	const candidate = path.posix.normalize(
		aliased ?
			path.posix.join( "src", specifier.slice( 2 ) ) :
			path.posix.join( path.posix.dirname( file ), specifier )
	);
	if ( aliased && !candidate.startsWith( "src/" ) ) return null;
	const substitution = /\.jsx?$/.test( candidate ) ?
		[ candidate.replace( /\.jsx?$/, ".ts" ), candidate.replace( /\.jsx?$/, ".tsx" ) ] :
		/\.mjs$/.test( candidate ) ?
		[ candidate.replace( /\.mjs$/, ".mts" ) ] :
		/\.cjs$/.test( candidate ) ?
		[ candidate.replace( /\.cjs$/, ".cts" ) ] :
		[];
	return [
		...substitution,
		candidate,
		`${candidate}.ts`,
		`${candidate}.tsx`,
		`${candidate}/index.ts`,
		`${candidate}/index.tsx`
	].find( p => model.files.has( p ) ) ?? null;
}
/*
================
main

Runs one checker and turns its issue list into output and an exit code.
================
*/
export function main( fn ) {
	try {
		const issues = fn();
		if ( issues.length ) {
			console.error( issues.join( "\n" ) );
			process.exitCode = 1;
		} else {
			console.log( "PASS" );
		}
	} catch ( e ) {
		console.error( e );
		process.exitCode = 1;
	}
}
