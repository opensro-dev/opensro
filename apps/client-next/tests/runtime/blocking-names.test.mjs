/*
===========================================================================

blocking-names.test.mjs - tests for character-create.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const { creationNameRules, creationNameError, checkedNameError, textAllowed, NAME_FILTER_CHAT } = await import(
	sourceFileUrl( "src/engine/foundation/ui/character-create.ts" ).href
);
test("the chat column (790C70) is its own list, not the character-name column", () => {
	// One word per column: column 1 refuses names, column 2 refuses chat
	// lines, and neither leaks into the other (545EF0's local refusal).
	const data = "#ALLOW_ID_TABLE\t0x0061\t0x007A\n#ALLOW_ID_TABLE\t0x0020\t0x0020\n" +
		"namebad\t2\t0\t0\t0\nchatbad\t0\t2\t0\t0\nchatword\t0\t1\t0\t0\n";
	const bytes = new TextEncoder().encode( data ).buffer;
	const names = creationNameRules( bytes ), chat = creationNameRules( bytes, NAME_FILTER_CHAT );
	assert.equal( textAllowed( "say namebad", chat ), true );
	assert.equal( textAllowed( "say namebad", names ), false );
	// Type 2 is a substring, type 1 a whole word, both ASCII-folded.
	assert.equal( textAllowed( "say chatbadx", chat ), false );
	assert.equal( textAllowed( "a chatword here", chat ), false );
	assert.equal( textAllowed( "chatwordx", chat ), true );
	assert.equal( textAllowed( "CHATBAD", chat ), false );
	assert.equal( textAllowed( "a clean line", chat ), true );
});

test("native type 1 is a whole word, type 2 a substring; whisper does not inherit creation length rules", () => {
	const data = "#ALLOW_ID_TABLE\t0x0061\t0x007A\n#ALLOW_ID_TABLE\t0x0020\t0x0020\nwhole\t1\npart\t2\n";
	const r = creationNameRules( new TextEncoder().encode( data ).buffer );
	assert.equal( checkedNameError( "A", r ), null );
	assert.equal( checkedNameError( "abcdefghijklm", r ), null );
	assert.ok( creationNameError( "A", r ) );
	assert.ok( creationNameError( "abcdefghijklm", r ) );
	assert.ok( checkedNameError( "WHOLE", r ) );
	assert.equal( checkedNameError( "wholegrain", r ), null );
	assert.ok( checkedNameError( "a WHOLE b", r ) );
	assert.ok( checkedNameError( "partial", r ) );
	assert.ok( checkedNameError( "[GM]", r ) );
});
