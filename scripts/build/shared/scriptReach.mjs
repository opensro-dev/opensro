/*
===========================================================================

scriptReach.mjs - the part of each Node module a step can reach

A variant step's code key holds every module in its static closure
(codeStamp.mjs moduleClosures): the Node scripts its Python named and
everything they import. Keyed whole, one shared module edited for
somebody else re-ran every step behind it: #4596 added an exported
constant to world/constants.mjs and two helpers to world/io.mjs for the
outdoor reuse key, and r204n re-ran 17 variant steps (about 50 s each)
whose scripts never call them (#4337).

This module keys a script by what a step can execute of it:

  - an entry (a script the step's Python runs) is reached whole;
  - a module that is loaded contributes its top-level statements that are
    not declarations (they run at load: an expression, a loop, a bare
    await);
  - a top-level declaration (function, class, variable statement,
    export default) is reached only when reached code names it, and a
    name imported from another module reaches that module's export, through
    re-exports;
  - a namespace import, a dynamic import or an eval reaches the target or
    the module whole, since names there are not static.

A declaration's initializer is taken as evaluated for its value: code that
depends on a module-level side effect must be a statement, not a
declaration. A module that contributes nothing - only declarations no
reached code names - is not part of the key at all.

Name lookup is by identifier text, so a local variable that shadows a
top-level name keeps that declaration too: the key may hold more than
runs, never less.

===========================================================================
*/
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import path from "node:path";
import ts from "typescript";

// A namespace import's imported name: every export of the target.
const NAMESPACE = "*";
// What a script's source must not hold to be reached by name: names there
// are computed, so the module is reached whole.
// The environment flags a reached text names (variant_code.py keys their
// values): the same pattern the Python side scans.
const FLAG_LITERAL = /SRO_[A-Z0-9_]+/g;

/*
================
exported

Whether a statement carries the export keyword, and whether it is the
default export.
================
*/
function exported( statement ) {
	const modifiers = ts.canHaveModifiers( statement ) ? ts.getModifiers( statement ) ?? [] : [];
	return {
		exported: modifiers.some( m => m.kind === ts.SyntaxKind.ExportKeyword ),
		isDefault: modifiers.some( m => m.kind === ts.SyntaxKind.DefaultKeyword )
	};
}

/*
================
bindingNames

Every name a variable declaration's binding pattern declares.
================
*/
function bindingNames( name, names = [] ) {
	if ( ts.isIdentifier( name ) ) {
		names.push( name.text );
		return names;
	}
	for ( const element of name.elements ) {
		if ( !ts.isOmittedExpression( element ) ) bindingNames( element.name, names );
	}
	return names;
}

/*
================
dynamicCode

What the module's code (not its comments or strings) runs by name at run
time, as { dynamic, lazy }. An import( ) of a relative string literal is
lazy: { target, names }, names being what a destructuring
`const { a, b } = await import( "./x.mjs" )` takes (followed like a
static import), or null for any other use (the target is reached whole).
An import( ) of anything else, or an eval( ), can run anything: dynamic,
the module itself reached whole. A text match took fsUtils.mjs's JSDoc
type import("node:fs").Dirent for code, and keying resolveCharRoster.mjs
whole for its one import( "../world/paths.mjs" ) moved every lane that
reads the roster on any edit to either.
================
*/
function dynamicCode( source, target ) {
	/** @type {{ dynamic: boolean, lazy: { target: string, names: string[] | null }[] }} */
	const found = { dynamic: false, lazy: [] };
	const importCall = node =>
		ts.isCallExpression( node ) && node.expression.kind === ts.SyntaxKind.ImportKeyword ? node : null;
	const literalTarget = call => {
		const [specifier] = call.arguments;
		return specifier && ts.isStringLiteralLike( specifier ) ? { from: target( specifier.text ) } : null;
	};
	const destructured = node => {
		if ( !ts.isVariableDeclaration( node ) || !ts.isObjectBindingPattern( node.name ) ) return null;
		const awaited = node.initializer && ts.isAwaitExpression( node.initializer ) ?
			node.initializer.expression :
			null;
		const call = awaited ? importCall( awaited ) : null;
		const literal = call ? literalTarget( call ) : null;
		if ( !call || !literal?.from ) return null;
		const names = [];
		for ( const element of node.name.elements ) {
			const name = element.propertyName ?? element.name;
			if ( element.dotDotDotToken || !ts.isIdentifier( name ) ) return null;
			names.push( name.text );
		}
		return { target: literal.from, names, call };
	};
	const visit = node => {
		const named = destructured( node );
		if ( named ) {
			found.lazy.push( { target: named.target, names: named.names } );
			ts.forEachChild( named.call, visit );
			return;
		}
		const call = importCall( node );
		if ( call ) {
			const literal = literalTarget( call );
			if ( literal?.from ) found.lazy.push( { target: literal.from, names: null } );
			else if ( !literal ) found.dynamic = true;
		} else if (
			ts.isCallExpression( node ) && ts.isIdentifier( node.expression ) && node.expression.text === "eval"
		) {
			found.dynamic = true;
		}
		ts.forEachChild( node, visit );
	};
	visit( source );
	return found;
}

/*
================
parseModule

One script's top level: its statements, the names each declares, its
imports and exports. target is the absolute file a relative specifier
names, null for a package (pinned by the lockfile, not followed).
================
*/
function parseModule( file, text ) {
	const source = ts.createSourceFile( file, text, ts.ScriptTarget.Latest, true, ts.ScriptKind.JS );
	const directory = path.dirname( file );
	/** @type {( specifier: string ) => string | null} */
	const target = specifier => specifier.startsWith( "." ) ? path.resolve( directory, specifier ) : null;
	// A relative string-literal specifier's file; a package, or anything not
	// a literal (never in a static import), is not followed.
	/** @type {( node: ts.Expression | undefined ) => string | null} */
	const literalFile = node => node && ts.isStringLiteral( node ) ? target( node.text ) : null;
	const module = {
		/** @type {ts.Statement[]} */
		statements: [],
		// indexes of the statements that run whenever the module loads
		/** @type {Set<number>} */
		effects: new Set(),
		// local name -> statement index
		declared: new Map(),
		// local name -> { statement, target, imported }
		imports: new Map(),
		// exported name -> { statement, local } | { statement, target, imported }
		exports: new Map(),
		// star re-export targets
		/** @type {string[]} */
		stars: [],
		// every module this one loads
		/** @type {string[]} */
		loads: [],
		...dynamicCode( source, target )
	};
	for ( const statement of source.statements ) {
		const index = module.statements.length;
		module.statements.push( statement );
		if ( ts.isImportDeclaration( statement ) ) {
			const from = literalFile( statement.moduleSpecifier );
			if ( from ) module.loads.push( from );
			const clause = statement.importClause;
			if ( !clause ) continue;
			if ( clause.name ) {
				module.imports.set( clause.name.text, { statement: index, target: from, imported: "default" } );
			}
			const bindings = clause.namedBindings;
			if ( bindings && ts.isNamespaceImport( bindings ) ) {
				module.imports.set( bindings.name.text, { statement: index, target: from, imported: NAMESPACE } );
			} else if ( bindings ) {
				for ( const element of bindings.elements ) {
					module.imports.set( element.name.text, {
						statement: index,
						target: from,
						imported: (element.propertyName ?? element.name).text
					} );
				}
			}
			continue;
		}
		if ( ts.isExportDeclaration( statement ) ) {
			const from = literalFile( statement.moduleSpecifier );
			if ( from ) module.loads.push( from );
			const clause = statement.exportClause;
			if ( !clause ) {
				if ( from ) module.stars.push( from );
			} else if ( ts.isNamespaceExport( clause ) ) {
				module.exports.set( clause.name.text, { statement: index, target: from, imported: NAMESPACE } );
			} else {
				for ( const element of clause.elements ) {
					const inner = (element.propertyName ?? element.name).text;
					module.exports.set(
						element.name.text,
						statement.moduleSpecifier ?
							{ statement: index, target: from, imported: inner } :
							{ statement: index, local: inner }
					);
				}
			}
			continue;
		}
		if ( ts.isExportAssignment( statement ) ) {
			module.exports.set( "default", { statement: index, local: null } );
			continue;
		}
		const names = [];
		if ( ts.isFunctionDeclaration( statement ) || ts.isClassDeclaration( statement ) ) {
			if ( statement.name ) names.push( statement.name.text );
		} else if ( ts.isVariableStatement( statement ) ) {
			for ( const declaration of statement.declarationList.declarations ) bindingNames( declaration.name, names );
		} else {
			// Not a declaration: it runs whenever the module loads.
			module.effects.add( index );
			continue;
		}
		for ( const name of names ) module.declared.set( name, index );
		const { exported: isExported, isDefault } = exported( statement );
		if ( isDefault ) module.exports.set( "default", { statement: index, local: null } );
		else if ( isExported ) {
			for ( const name of names ) module.exports.set( name, { statement: index, local: name } );
		}
	}
	return module;
}

/*
================
referencedNames

The identifiers a statement names, other than the property names of a
member access and the keys of an object literal: what it may read.
================
*/
function referencedNames( statement ) {
	const names = new Set();
	const visit = node => {
		if ( ts.isIdentifier( node ) ) {
			const parent = node.parent;
			const member = parent && ts.isPropertyAccessExpression( parent ) && parent.name === node;
			const key = parent && ts.isPropertyAssignment( parent ) && parent.name === node;
			if ( !member && !key ) names.add( node.text );
		}
		ts.forEachChild( node, visit );
	};
	visit( statement );
	return names;
}

/*
================
reachScripts

For one step's closure: each script file's reached text, by file. files
is the closure (other kinds are skipped); entries are the scripts the
step runs. A loaded script that reaches nothing maps to "".
================
*/
export async function reachScripts( files, entries ) {
	const modules = new Map();
	for ( const file of files ) {
		if ( !file.endsWith( ".mjs" ) && !file.endsWith( ".js" ) ) continue;
		modules.set( file, parseModule( file, await readFile( file, "utf8" ) ) );
	}
	const kept = new Map( [ ...modules.keys() ].map( file => [ file, new Set() ] ) );
	const whole = new Set();
	const loaded = new Set();
	const pending = [];
	const keep = ( file, index ) => {
		const set = kept.get( file );
		if ( !set || set.has( index ) ) return;
		set.add( index );
		pending.push( [ file, index ] );
	};
	const reachAll = file => {
		const module = modules.get( file );
		if ( !module || whole.has( file ) ) return;
		whole.add( file );
		module.statements.forEach( ( _, index ) => keep( file, index ) );
	};
	const reachExport = ( file, name, seen = new Set() ) => {
		const module = modules.get( file );
		if ( !module || seen.has( file ) ) return;
		seen.add( file );
		if ( name === NAMESPACE ) {
			reachAll( file );
			return;
		}
		const entry = module.exports.get( name );
		if ( entry ) {
			keep( file, entry.statement );
			if ( entry.local ) {
				const declaration = module.declared.get( entry.local );
				if ( declaration !== undefined ) keep( file, declaration );
				const binding = module.imports.get( entry.local );
				if ( binding ) reachExport( binding.target, binding.imported );
			} else if ( entry.target ) {
				reachExport( entry.target, entry.imported );
			}
			return;
		}
		for ( const star of module.stars ) reachExport( star, name, seen );
	};
	const load = file => {
		const module = modules.get( file );
		if ( !module || loaded.has( file ) ) return;
		loaded.add( file );
		if ( module.dynamic ) reachAll( file );
		for ( const { target, names } of module.lazy ) {
			load( target );
			if ( names ) names.forEach( name => reachExport( target, name ) );
			else reachAll( target );
		}
		for ( const index of module.effects ) keep( file, index );
		for ( const target of module.loads ) load( target );
	};
	for ( const entry of entries ) {
		load( entry );
		reachAll( entry );
	}
	for ( const file of modules.keys() ) load( file );
	while ( pending.length > 0 ) {
		const [file, index] = pending.pop();
		const module = modules.get( file );
		for ( const name of referencedNames( module.statements[index] ) ) {
			const declaration = module.declared.get( name );
			if ( declaration !== undefined ) keep( file, declaration );
			const binding = module.imports.get( name );
			if ( binding ) {
				keep( file, binding.statement );
				reachExport( binding.target, binding.imported );
			}
		}
	}
	const reached = new Map();
	for ( const [file, module] of modules ) {
		const indexes = [ ...(kept.get( file ) ?? []) ].sort( ( a, b ) => a - b );
		reached.set( file, indexes.map( index => module.statements[index].getText() ).join( "\n" ) );
	}
	return reached;
}

/*
================
scriptKeys

reachScripts as the variant runner keys it: per script, the digest of its
reached text and the environment flags that text names, or null for a
script that reaches nothing (it leaves the key).
================
*/
export async function scriptKeys( files, entries ) {
	const keys = {};
	for ( const [file, text] of await reachScripts( files, entries ) ) {
		keys[file] = text === "" ? null : {
			digest: createHash( "sha256" ).update( text ).digest( "hex" ),
			flags: [ ...new Set( text.match( FLAG_LITERAL ) ?? [] ) ].sort()
		};
	}
	return keys;
}
