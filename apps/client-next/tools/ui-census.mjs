/*
===========================================================================

ui-census.mjs - the ui.ts wave-0 census: routed ids and shared state

Wave 0 of the ui.ts split (#2513, decision record #2534) lands this census
before any window moves:

- ROUTES: every control-id guard in `activate()` and `event()` - the exact
 literals and the `startsWith` prefixes the ladders test, each with its
 anchor (function and line). A move that renames, drops or reorders a
 guard changes this list, so the diff is the routing review.
- STATE: every mutable closure field `createUi` declares, with the buckets
 that read and write it. A bucket is the control family of the nearest
 enclosing id guard (`stall`, `letter`, ...), or the closure function
 that touches the field unguarded. A field with more than one bucket is
 SHARED: two future window owners would both reach it. The rule for the
 epic is that a move PR may only shrink the shared set - a field leaves
 it when one owner takes the field with it, and no field may gain a
 bucket it did not have.

The census is regenerated, never edited: `--write` rewrites the checked-in
table, `--check` (the default) fails when the table is stale. Nothing in
ui.ts moves under this tool; it only reads.

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";

import { root, ts } from "./project.mjs";

const UI_PATH = path.join( root, "src/engine/runtime/ui/ui.ts" );
const CENSUS_PATH = path.join( root, "tests/runtime/ui-census.json" );

/*
================
lineOf

The 1-based line of a node's start in its source file.
================
*/
function lineOf( file, node ) {
	return file.getLineAndCharacterOfPosition( node.getStart( file ) ).line + 1;
}

/*
================
conditionOf

The tested expression of an if or a ternary (TypeScript names the if's
`expression`, the ternary's `condition`).
================
*/
function conditionOf( node ) {
	return ts.isIfStatement( node ) ? node.expression : node.condition;
}

/*
================
isIdish

Whether an expression denotes the routed control id: the `id` parameter
of `activate`, or `event.id` inside the dispatcher.
================
*/
function isIdish( node ) {
	if ( ts.isIdentifier( node ) ) return node.text === "id";
	return ts.isPropertyAccessExpression( node ) && node.name.text === "id" &&
		ts.isIdentifier( node.expression ) && node.expression.text === "event";
}

/*
================
familyOf

The control family of one literal or prefix: the text up to the first
separator, so "chat-send" is family "chat" and "slot:" is "slot".
================
*/
function familyOf( literal ) {
	const cut = literal.search( /[-:]/ );
	return cut > 0 ? literal.slice( 0, cut ) : literal;
}

/*
================
guardFamily

The family a condition tests, if it tests the routed id: an exact
`id === "lit"`, a prefix `id.startsWith( "pre" )`, or a panel gate
`panel === "Name"`. The whole condition is walked, so an `||` of exact
ids reports the first literal.
================
*/
function guardFamily( condition ) {
	let found = null;
	function visit( node ) {
		if ( !node || found !== null ) return;
		if (
			ts.isBinaryExpression( node ) &&
			(node.operatorToken.kind === ts.SyntaxKind.EqualsEqualsEqualsToken ||
				node.operatorToken.kind === ts.SyntaxKind.EqualsEqualsToken)
		) {
			for ( const [side, other] of [ [ node.left, node.right ], [ node.right, node.left ] ] ) {
				if ( isIdish( side ) && ts.isStringLiteral( other ) ) {
					found = familyOf( other.text );
					return;
				}
				if ( ts.isIdentifier( side ) && side.text === "panel" && ts.isStringLiteral( other ) ) {
					found = "panel:" + other.text;
					return;
				}
			}
		}
		if (
			ts.isCallExpression( node ) && ts.isPropertyAccessExpression( node.expression ) &&
			node.expression.name.text === "startsWith" && isIdish( node.expression.expression ) &&
			node.arguments.length === 1 && ts.isStringLiteral( node.arguments[0] )
		) {
			found = familyOf( node.arguments[0].text );
			return;
		}
		ts.forEachChild( node, visit );
	}
	visit( condition );
	return found;
}

/*
================
functionName

The census name of the function a node sits in: its declaration name, the
variable or property it is assigned to, or its anchor line.
================
*/
function functionName( file, node ) {
	if ( ts.isFunctionDeclaration( node ) && node.name ) return node.name.text;
	if ( (ts.isFunctionExpression( node ) || ts.isArrowFunction( node )) && node.parent ) {
		if ( ts.isVariableDeclaration( node.parent ) && ts.isIdentifier( node.parent.name ) ) {
			return node.parent.name.text;
		}
		if ( ts.isPropertyAssignment( node.parent ) ) return node.parent.name.getText( file );
		if ( ts.isMethodDeclaration( node.parent ) ) return node.parent.name.getText( file );
	}
	if ( ts.isMethodDeclaration( node ) && node.name ) return node.name.getText( file );
	return "anonymous@" + lineOf( file, node );
}

/*
================
isFunctionNode
================
*/
function isFunctionNode( node ) {
	return ts.isFunctionDeclaration( node ) || ts.isFunctionExpression( node ) ||
		ts.isArrowFunction( node ) || ts.isMethodDeclaration( node ) ||
		ts.isGetAccessorDeclaration( node ) || ts.isSetAccessorDeclaration( node );
}

/*
================
routePatterns

Every id pattern one condition tests, as census route rows.
================
*/
function routePatterns( file, fnName, condition, out ) {
	function visit( node ) {
		if ( !node ) return;
		if (
			ts.isBinaryExpression( node ) &&
			(node.operatorToken.kind === ts.SyntaxKind.EqualsEqualsEqualsToken ||
				node.operatorToken.kind === ts.SyntaxKind.EqualsEqualsToken)
		) {
			for ( const [side, other] of [ [ node.left, node.right ], [ node.right, node.left ] ] ) {
				if ( isIdish( side ) && ts.isStringLiteral( other ) ) {
					out.push( { fn: fnName, kind: "exact", pattern: other.text, line: lineOf( file, node ) } );
				}
			}
		}
		if (
			ts.isCallExpression( node ) && ts.isPropertyAccessExpression( node.expression ) &&
			node.expression.name.text === "startsWith" && isIdish( node.expression.expression ) &&
			node.arguments.length === 1 && ts.isStringLiteral( node.arguments[0] )
		) {
			out.push( { fn: fnName, kind: "prefix", pattern: node.arguments[0].text, line: lineOf( file, node ) } );
		}
		ts.forEachChild( node, visit );
	}
	visit( condition );
}

/*
================
census

Build the census from ui.ts source text. Pure: the same text always
builds the same table, which is what makes `--check` a gate.
================
*/
export function census( text ) {
	const file = ts.createSourceFile( "ui.ts", text, ts.ScriptTarget.Latest, true );
	let createUi = null;
	for ( const statement of file.statements ) {
		if ( ts.isFunctionDeclaration( statement ) && statement.name?.text === "createUi" ) createUi = statement;
	}
	if ( !createUi || !createUi.body ) throw new Error( "createUi not found in ui.ts" );

	// The mutable fields: `let` declarators declared directly in createUi.
	const fields = new Map();
	for ( const statement of createUi.body.statements ) {
		if ( !ts.isVariableStatement( statement ) ) continue;
		if ( !(statement.declarationList.flags & ts.NodeFlags.Let) ) continue;
		for ( const declaration of statement.declarationList.declarations ) {
			if ( ts.isIdentifier( declaration.name ) ) {
				fields.set( declaration.name.text, {
					name: declaration.name.text,
					line: lineOf( file, declaration ),
					buckets: {}
				} );
			}
		}
	}

	const routes = [];
	const parents = new Map();
	(function link( node ) {
		ts.forEachChild( node, child => {
			parents.set( child, node );
			link( child );
		} );
	})( createUi );

	/*
	================
	bucketOf

	The bucket one field reference belongs to: the family of the nearest
	enclosing id guard, else the enclosing function's name, else the
	createUi body itself. The walk stops at the first function boundary
	for the name, but a guard inside that function still wins.
	================
	*/
	function bucketOf( node ) {
		let current = node;
		let fn = null;
		while ( current && current !== createUi ) {
			if ( (ts.isIfStatement( current ) || ts.isConditionalExpression( current )) ) {
				const family = guardFamily( conditionOf( current ) );
				if ( family !== null ) return family;
			}
			if ( isFunctionNode( current ) && fn === null ) fn = functionName( file, current );
			current = parents.get( current );
		}
		return fn ?? "createUi";
	}

	function touch( name, bucket, write ) {
		const field = fields.get( name );
		if ( !field ) return;
		const entry = field.buckets[bucket] ?? (field.buckets[bucket] = { reads: 0, writes: 0 });
		if ( write ) entry.writes++;
		else entry.reads++;
	}

	// Names declared anywhere inside a nested function shadow the field
	// for that whole subtree (parameters, lets, catches). Conservative:
	// a shadowed reference is skipped, never misattributed.
	function shadowedNames( fnNode ) {
		const names = new Set();
		if ( fnNode.parameters ) {
			for ( const param of fnNode.parameters ) {
				if ( ts.isIdentifier( param.name ) ) names.add( param.name.text );
			}
		}
		(function scan( node ) {
			if ( node !== fnNode && isFunctionNode( node ) ) return;
			if ( ts.isVariableDeclaration( node ) && ts.isIdentifier( node.name ) ) names.add( node.name.text );
			if (
				ts.isCatchClause( node ) && node.variableDeclaration &&
				ts.isIdentifier( node.variableDeclaration.name )
			) {
				names.add( node.variableDeclaration.name.text );
			}
			ts.forEachChild( node, scan );
		})( fnNode.body ?? fnNode );
		return names;
	}

	function isWrite( node ) {
		const parent = node.parent;
		if ( !parent ) return false;
		if (
			ts.isBinaryExpression( parent ) && parent.left === node &&
			parent.operatorToken.kind >= ts.SyntaxKind.FirstAssignment &&
			parent.operatorToken.kind <= ts.SyntaxKind.LastAssignment
		) {
			return true;
		}
		return (ts.isPrefixUnaryExpression( parent ) || ts.isPostfixUnaryExpression( parent )) &&
			(parent.operator === ts.SyntaxKind.PlusPlusToken || parent.operator === ts.SyntaxKind.MinusMinusToken);
	}

	(function walk( node, shadow ) {
		if ( isFunctionNode( node ) && node !== createUi ) {
			const inner = shadowedNames( node );
			ts.forEachChild( node, child => walk( child, inner ) );
			return;
		}
		if ( ts.isIdentifier( node ) && fields.has( node.text ) && !shadow.has( node.text ) ) {
			// A declaration name or a property name is not a reference.
			const parent = node.parent;
			const isDecl = (ts.isVariableDeclaration( parent ) && parent.name === node) ||
				(ts.isPropertyAssignment( parent ) && parent.name === node) ||
				(ts.isPropertyAccessExpression( parent ) && parent.name === node) ||
				(ts.isFunctionDeclaration( parent ) && parent.name === node);
			if ( !isDecl ) touch( node.text, bucketOf( node ), isWrite( node ) );
		}
		if ( (ts.isIfStatement( node ) || ts.isConditionalExpression( node )) ) {
			// Routes are the ladders' vocabulary: guards inside activate/event.
			let current = node, fnName = null;
			while ( current && current !== createUi ) {
				if ( isFunctionNode( current ) ) {
					fnName = functionName( file, current );
					break;
				}
				current = parents.get( current );
			}
			if ( fnName === "activate" || fnName === "event" ) {
				routePatterns( file, fnName, conditionOf( node ), routes );
			}
		}
		ts.forEachChild( node, child => walk( child, shadow ) );
	})( createUi, new Set() );

	const fieldRows = [ ...fields.values() ]
		.sort( ( a, b ) => a.line - b.line )
		.map( field => {
			const buckets = {};
			for ( const name of Object.keys( field.buckets ).sort() ) buckets[name] = field.buckets[name];
			return { ...field, buckets, shared: Object.keys( buckets ).length > 1 };
		} );
	routes.sort( ( a, b ) => a.line - b.line );

	return {
		about: "Generated by tools/ui-census.mjs from src/engine/runtime/ui/ui.ts. " +
			"Do not edit: regenerate with --write. A field with more than one bucket is shared; " +
			"a move PR may only shrink the shared set (docs: #2515).",
		source: "src/engine/runtime/ui/ui.ts",
		summary: {
			fields: fieldRows.length,
			sharedFields: fieldRows.filter( field => field.shared ).length,
			routes: routes.length
		},
		fields: fieldRows,
		routes
	};
}

/*
================
canonical

The stable text form: sorted keys at every level, two-space indent, so
two builds of the same census are byte-identical.
================
*/
function canonical( value ) {
	if ( Array.isArray( value ) ) return value.map( canonical );
	if ( value && typeof value === "object" ) {
		const out = {};
		for ( const key of Object.keys( value ).sort() ) out[key] = canonical( value[key] );
		return out;
	}
	return value;
}

/*
================
main
================
*/
function main() {
	const write = process.argv.includes( "--write" );
	const table = canonical( census( fs.readFileSync( UI_PATH, "utf8" ) ) );
	const text = JSON.stringify( table, null, "\t" ) + "\n";
	if ( write ) {
		fs.writeFileSync( CENSUS_PATH, text );
		console.log(
			`ui-census: wrote ${path.relative( root, CENSUS_PATH )} ` +
				`(${table.summary.fields} fields, ${table.summary.sharedFields} shared, ${table.summary.routes} routes)`
		);
		return;
	}
	const current = fs.existsSync( CENSUS_PATH ) ? fs.readFileSync( CENSUS_PATH, "utf8" ) : "";
	if ( current === text ) {
		console.log(
			`ui-census: up to date (${table.summary.fields} fields, ` +
				`${table.summary.sharedFields} shared, ${table.summary.routes} routes)`
		);
		return;
	}
	console.error( "ui-census: tests/runtime/ui-census.json is stale; run node tools/ui-census.mjs --write" );
	const a = current.split( "\n" ), b = text.split( "\n" );
	for ( let i = 0; i < Math.max( a.length, b.length ); i++ ) {
		if ( a[i] !== b[i] ) {
			console.error(
				`first difference at line ${i + 1}:\n  census: ${a[i] ?? "(missing)"}\n  built:  ${b[i] ?? "(missing)"}`
			);
			break;
		}
	}
	process.exit( 1 );
}

if ( import.meta.filename === path.resolve( process.argv[1] ?? "" ) ) main();
