/*
===========================================================================

docLinks.test.mjs - planted links for the doc-links gate

===========================================================================
*/

import assert from "node:assert/strict";
import { test } from "node:test";
import { linkProblems, markdownLinks, REPO_BLOB } from "../../checks/check_doc_links.mjs";

const TREE = new Set( [ "docs/native/RIZIN.md", "docs/native", "tools/re/revtool/README.md" ] );
const exists = ( relative ) => TREE.has( relative );

test("a relative link lands or is reported; anchors, schemes and fenced code are not links to check", () => {
	const text = [
		"[ok](RIZIN.md#1-install)",
		"[gone](MISSING.md)",
		"[up](../../tools/re/revtool/README.md)",
		"[web](https://example.org/x.md) [here](#top)",
		"```",
		"[example](NOT_A_FILE.md)",
		"```"
	].join( "\n" );
	assert.deepEqual( markdownLinks( text ).map( ( link ) => link.target ), [
		"RIZIN.md#1-install",
		"MISSING.md",
		"../../tools/re/revtool/README.md",
		"https://example.org/x.md",
		"#top"
	] );
	const problems = linkProblems( "docs/native/REVERSE_ENGINEERING.md", text, exists );
	assert.equal( problems.length, 1 );
	assert.match( problems[0], /MISSING\.md does not exist/ );
});

test("a home-page community file refuses a relative link even when the target exists", () => {
	const relative = linkProblems( ".github/CONTRIBUTING.md", "[Rizin](../docs/native/RIZIN.md)", exists );
	assert.equal( relative.length, 1 );
	assert.match( relative[0], /home page/ );
	const absolute = linkProblems( ".github/CONTRIBUTING.md", `[Rizin](${REPO_BLOB}docs/native/RIZIN.md)`, exists );
	assert.deepEqual( absolute, [] );
});

test("a link that climbs out of the repository is broken", () => {
	assert.equal( linkProblems( "README.md", "[out](../research/x.md)", () => true ).length, 1 );
});
