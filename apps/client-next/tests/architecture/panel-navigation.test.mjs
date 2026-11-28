/*
===========================================================================

panel-navigation.test.mjs - every UI navigation names its intent

A new panel must use the shared transition owner (setPanel / resetPanel),
and a new control must say whether it opens, selects or toggles. Runs in
the normal pnpm test and check pipeline.

Only src/engine/runtime/ui/ is audited, so only its files are read, and a
file is parsed once, and only when its text could hold an audited
construct (audited). The test used to parse all ~950 source files through
project(), then each UI file again and ui.ts a third time, which ran past
its 20 s budget on a busy CI runner (#2907).

===========================================================================
*/

import { test } from "node:test";
import assert from "node:assert/strict";
import { parse, sourceTexts, ts, visit } from "../../tools/project.mjs";

const UI_DIR = "src/engine/runtime/ui";
const PANEL_INTENTS = [ "open", "toggle", "select", "warm" ];
const PANEL_OWNERS = [ "setPanel", "resetPanel" ];

/*
================
audited

Whether a file's text can hold anything audit reports. Every rule needs
one of these spellings in the raw text: a string literal starting
"window:", the identifier panel, or the identifier setPanel. A spelling
made only of escape sequences (panel) is the one way past it, and no
source here writes one.
================
*/
function audited( text ) {
	return text.includes( "window:" ) || /\bpanel\b/.test( text ) || text.includes( "setPanel" );
}

/*
================
uiTrees

The parsed trees of the audited directory's source files that audited()
keeps, by root-relative path.
================
*/
function uiTrees() {
	const trees = new Map();
	for ( const [name, text] of sourceTexts( UI_DIR ) ) {
		if ( audited( text ) ) trees.set( name, parse( name, text ) );
	}
	return trees;
}

/*
================
audit

The navigation errors in one parsed file. "warm" is the unseen first
build of a window (window-warm.ts): no hooks or resets.
================
*/
function audit( file ) {
	const errors = [];
	visit( file, node => {
		if ( ts.isStringLiteralLike( node ) && node.text.startsWith( "window:" ) ) {
			errors.push( "ambiguous window action" );
		}
		if (
			ts.isBinaryExpression( node ) && ts.isIdentifier( node.left ) && node.left.text === "panel" &&
			node.operatorToken.kind >= ts.SyntaxKind.FirstAssignment &&
			node.operatorToken.kind <= ts.SyntaxKind.LastAssignment
		) {
			let owner = node.parent;
			while ( owner && !ts.isFunctionLike( owner ) ) owner = owner.parent;
			if ( !owner?.name || !PANEL_OWNERS.includes( owner.name.text ) ) {
				errors.push( "panel assignment bypasses transition owner" );
			}
		}
		if (
			ts.isCallExpression( node ) && ts.isIdentifier( node.expression ) && node.expression.text === "setPanel"
		) {
			const intent = node.arguments[1];
			if (
				node.arguments.length < 1 || node.arguments.length > 2 ||
				intent && (!ts.isStringLiteral( intent ) || !PANEL_INTENTS.includes( intent.text ))
			) errors.push( "invalid panel intent" );
		}
	} );
	return errors;
}

const trees = uiTrees();

test("all UI navigation uses explicit actions and one transition owner", () => {
	assert.ok( trees.has( UI_DIR + "/ui.ts" ), "ui.ts was not audited" );
	for ( const [name, tree] of trees ) assert.deepEqual( audit( tree ), [], name );
});

for (
	const source of [
		'function activate(){panel="Shop";}',
		'function sidebar(){return "window:Inventory";}',
		'function select(){setPanel("Inventory","maybe-toggle");}'
	]
) {
	test( "navigation guard rejects " + source, () => {
		// The prefilter keeps it, and the audit reports it.
		assert.ok( audited( source ), "audited() would skip it" );
		assert.ok( audit( parse( "ui.ts", source ) ).length );
	} );
}

test("open is the default and toggle remains an explicit opt-in", () => {
	let setter;
	visit( trees.get( UI_DIR + "/ui.ts" ), node => {
		if ( ts.isFunctionDeclaration( node ) && node.name?.text === "setPanel" ) setter = node;
	} );
	assert.equal( setter?.parameters[1]?.initializer?.text, "open" );
});
