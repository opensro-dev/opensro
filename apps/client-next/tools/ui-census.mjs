/*
===========================================================================

ui-census.mjs - the ui.ts wave-0 census: routed ids and shared state

Wave 0 of the ui.ts split (#2513, decision record #2534) lands this census
before any window moves:

- ROUTES: every control-id guard in `activate()` and `event()` - the exact
 literals and the `startsWith` prefixes the ladders test, keyed by the
 pattern they test. A move that renames or drops a guard changes this
 list, so the diff is the routing review.
- STATE: every mutable closure field `createUi` declares, keyed by name,
 with the buckets that read and write it. A bucket is the control family
 of the nearest enclosing id guard (`stall`, `letter`, ...), or the
 closure function that touches the field unguarded. A field with more
 than one bucket is SHARED: two future window owners would both reach
 it. The rule for the epic is that a move PR may only shrink the shared
 set - a field leaves it when one owner takes the field with it, and no
 field may gain a bucket it did not have.

Keys are stable identity, never source position (#4456): fields sort by
name, routes by the pattern they test, and an anonymous function is
named for its enclosing named function plus an ordinal inside it. A
pure move or insertion in ui.ts therefore leaves the table unchanged.
Source lines exist only as check-time diagnostics, computed on failure
and never committed.

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

The 1-based line of a node's start in its source file. Check-time
diagnostics only: a line may be printed when the gate fails, it is
never written into the census (#4456).
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
directName

The name a function carries by itself: its declaration name, or the
variable, property or method it is assigned to. An anonymous function
has none and returns null; census() names it for its enclosing named
function instead (#4456).
================
*/
function directName( file, node ) {
	if ( ts.isFunctionDeclaration( node ) && node.name ) return node.name.text;
	if ( (ts.isFunctionExpression( node ) || ts.isArrowFunction( node )) && node.parent ) {
		if ( ts.isVariableDeclaration( node.parent ) && ts.isIdentifier( node.parent.name ) ) {
			return node.parent.name.text;
		}
		if ( ts.isPropertyAssignment( node.parent ) ) return node.parent.name.getText( file );
		if ( ts.isMethodDeclaration( node.parent ) ) return node.parent.name.getText( file );
	}
	if ( ts.isMethodDeclaration( node ) && node.name ) return node.name.getText( file );
	return null;
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

Every id pattern one condition tests, as census route rows. The guard's
line rides in the parallel `lines` list for check-time diagnostics; it
is not part of the row (#4456).
================
*/
function routePatterns( file, fnName, condition, out, lines ) {
	function visit( node ) {
		if ( !node ) return;
		if (
			ts.isBinaryExpression( node ) &&
			(node.operatorToken.kind === ts.SyntaxKind.EqualsEqualsEqualsToken ||
				node.operatorToken.kind === ts.SyntaxKind.EqualsEqualsToken)
		) {
			for ( const [side, other] of [ [ node.left, node.right ], [ node.right, node.left ] ] ) {
				if ( isIdish( side ) && ts.isStringLiteral( other ) ) {
					out.push( { fn: fnName, kind: "exact", pattern: other.text } );
					lines.push( lineOf( file, node ) );
				}
			}
		}
		if (
			ts.isCallExpression( node ) && ts.isPropertyAccessExpression( node.expression ) &&
			node.expression.name.text === "startsWith" && isIdish( node.expression.expression ) &&
			node.arguments.length === 1 && ts.isStringLiteral( node.arguments[0] )
		) {
			out.push( { fn: fnName, kind: "prefix", pattern: node.arguments[0].text } );
			lines.push( lineOf( file, node ) );
		}
		ts.forEachChild( node, visit );
	}
	visit( condition );
}

/*
================
censusWithLines

Build the census from ui.ts source text, with the source lines of every
field and route kept beside it for check-time diagnostics. Pure: the
same text always builds the same table, which is what makes `--check`
a gate. The lines never enter the table itself (#4456).
================
*/
export function censusWithLines( text ) {
	const file = ts.createSourceFile( "ui.ts", text, ts.ScriptTarget.Latest, true );
	let createUi = null;
	for ( const statement of file.statements ) {
		if ( ts.isFunctionDeclaration( statement ) && statement.name?.text === "createUi" ) createUi = statement;
	}
	if ( !createUi || !createUi.body ) throw new Error( "createUi not found in ui.ts" );

	// The mutable fields: `let` declarators declared directly in createUi.
	// The declaration line is diagnostics only; the key is the name.
	const fields = new Map();
	const fieldLines = new Map();
	for ( const statement of createUi.body.statements ) {
		if ( !ts.isVariableStatement( statement ) ) continue;
		if ( !(statement.declarationList.flags & ts.NodeFlags.Let) ) continue;
		for ( const declaration of statement.declarationList.declarations ) {
			if ( ts.isIdentifier( declaration.name ) ) {
				fields.set( declaration.name.text, {
					name: declaration.name.text,
					buckets: {}
				} );
				fieldLines.set( declaration.name.text, lineOf( file, declaration ) );
			}
		}
	}

	const routes = [], routeLines = [];
	const parents = new Map();
	(function link( node ) {
		ts.forEachChild( node, child => {
			parents.set( child, node );
			link( child );
		} );
	})( createUi );

	// Every function in the closure, in source order, with the census
	// name it answers to (#4456): its own name, or - anonymous - its
	// enclosing named function's name plus an ordinal among that
	// function's anonymous functions. A move keeps the anchor and the
	// order, so it keeps the name; only gaining or losing an anonymous
	// function renumbers the later ones, which is a real change.
	const functionNodes = [];
	(function collect( node ) {
		if ( isFunctionNode( node ) ) functionNodes.push( node );
		ts.forEachChild( node, collect );
	})( createUi );
	const functionNames = new Map();
	{
		const ordinals = new Map();
		for ( const node of functionNodes ) {
			const own = directName( file, node );
			if ( own !== null ) {
				functionNames.set( node, own );
				continue;
			}
			let anchor = parents.get( node );
			while ( anchor && !(isFunctionNode( anchor ) && directName( file, anchor ) !== null) ) {
				anchor = parents.get( anchor );
			}
			// createUi encloses the whole walk and is named, so the climb
			// always lands; naming the root is the unreachable fallback.
			const base = anchor ? directName( file, anchor ) : "createUi";
			const ordinal = (ordinals.get( anchor ) ?? 0) + 1;
			ordinals.set( anchor, ordinal );
			functionNames.set( node, base + "#anon" + ordinal );
		}
	}
	const functionName = node => functionNames.get( node ) ?? "createUi";

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
			if ( isFunctionNode( current ) && fn === null ) fn = functionName( current );
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
					fnName = functionName( current );
					break;
				}
				current = parents.get( current );
			}
			if ( fnName === "activate" || fnName === "event" ) {
				routePatterns( file, fnName, conditionOf( node ), routes, routeLines );
			}
		}
		ts.forEachChild( node, child => walk( child, shadow ) );
	})( createUi, new Set() );

	const byText = ( a, b ) => a < b ? -1 : a > b ? 1 : 0;
	const fieldRows = [ ...fields.values() ]
		.sort( ( a, b ) => byText( a.name, b.name ) )
		.map( field => {
			const buckets = {};
			for ( const name of Object.keys( field.buckets ).sort() ) buckets[name] = field.buckets[name];
			return { ...field, buckets, shared: Object.keys( buckets ).length > 1 };
		} );
	// Every guard stays a row, duplicates included: guards that test the
	// same pattern are told apart by an ordinal among themselves, and
	// the committed order is the key order (function, kind, pattern,
	// ordinal), so moving a guard cannot rewrite the table (#4456).
	const routeLinesByKey = new Map();
	{
		const counts = new Map();
		routes.forEach( ( route, index ) => {
			const key = route.fn + "\0" + route.kind + "\0" + route.pattern;
			const ordinal = (counts.get( key ) ?? 0) + 1;
			counts.set( key, ordinal );
			route.ordinal = ordinal;
			routeLinesByKey.set( key + "\0" + ordinal, routeLines[index] );
		} );
	}
	routes.sort( ( a, b ) =>
		byText( a.fn, b.fn ) || byText( a.kind, b.kind ) || byText( a.pattern, b.pattern ) ||
		a.ordinal - b.ordinal
	);

	const table = {
		about: "Generated by tools/ui-census.mjs from src/engine/runtime/ui/ui.ts. " +
			"Do not edit: regenerate with --write. Entries are keyed by stable identity " +
			"(field name; route function, kind and pattern), never by source position, so " +
			"moving code in ui.ts does not rewrite this file (#4456). A field with more " +
			"than one bucket is shared; a move PR may only shrink the shared set (docs: #2515).",
		source: "src/engine/runtime/ui/ui.ts",
		summary: {
			fields: fieldRows.length,
			sharedFields: fieldRows.filter( field => field.shared ).length,
			routes: routes.length
		},
		fields: fieldRows,
		routes
	};
	return { table, fieldLines, routeLines: routeLinesByKey };
}

/*
================
census

The census table alone: what the gate compares and the tests read.
================
*/
export function census( text ) {
	return censusWithLines( text ).table;
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
	const built = censusWithLines( fs.readFileSync( UI_PATH, "utf8" ) );
	const table = canonical( built.table );
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
			// Point at the code, computed now and never committed (#4456):
			// the field or pattern the differing line names, at its line
			// in this build of ui.ts.
			for ( const line of [ b[i] ?? "", a[i] ?? "" ] ) {
				const named = line.match( /"(name|pattern)": "(.*)"/ );
				if ( !named ) continue;
				if ( named[1] === "name" && built.fieldLines.has( named[2] ) ) {
					console.error(
						`ui.ts:${built.fieldLines.get( named[2] )} declares field "${named[2]}" in this build`
					);
					break;
				}
				if ( named[1] === "pattern" ) {
					for ( const [key, routeLine] of built.routeLines ) {
						if ( key.split( "\0" )[2] === named[2] ) {
							console.error( `ui.ts:${routeLine} tests route "${named[2]}" in this build` );
							break;
						}
					}
					break;
				}
			}
			break;
		}
	}
	process.exit( 1 );
}

if ( import.meta.filename === path.resolve( process.argv[1] ?? "" ) ) main();
