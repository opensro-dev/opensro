/*
===========================================================================

credential-hints.test.mjs - which mirrored edits the browser may fill

Loads credential-hints.ts through the shared native loader. The login edits
keep their autocomplete tokens; every other edit (chat above all) carries
the unrecognized token that keeps Chrome's address Autofill from offering
the player's saved e-mail addresses, plus the password-manager opt-outs.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { configureCredentialHints, GAME_EDIT_AUTOCOMPLETE } = await import(
	"../../src/engine/runtime/platform/ui/credential-hints.ts"
);

/*
================
fakeInput

The attributes an input carries, read back the way the browser exposes
them: the autocomplete property and the attribute share one value.
================
*/
function fakeInput() {
	const attributes = new Map();
	return {
		attributes,
		set autocomplete( value ) {
			attributes.set( "autocomplete", value );
		},
		setAttribute( name, value ) {
			attributes.set( name, value );
		}
	};
}

test("the login edits keep the tokens a password manager fills", () => {
	for ( const [id, token] of [ [ "account", "username" ], [ "password", "current-password" ] ] ) {
		const input = fakeInput();
		configureCredentialHints( input, id );
		assert.equal( input.attributes.get( "autocomplete" ), token );
		assert.equal( input.attributes.has( "data-1p-ignore" ), false );
	}
});

test("chat and every other edit refuse Autofill and the password managers", () => {
	for ( const id of [ "chat-input", "stat-points", "stall-prompt-quantity" ] ) {
		const input = fakeInput();
		configureCredentialHints( input, id );
		// "off" is the value Chrome overrides for address Autofill.
		assert.equal( input.attributes.get( "autocomplete" ), GAME_EDIT_AUTOCOMPLETE );
		assert.notEqual( GAME_EDIT_AUTOCOMPLETE, "off" );
		assert.match( GAME_EDIT_AUTOCOMPLETE, /^[a-z-]+$/ );
		for ( const name of [ "data-1p-ignore", "data-lpignore", "data-bwignore" ] ) {
			assert.equal( input.attributes.get( name ), "true" );
		}
		assert.equal( input.attributes.get( "data-form-type" ), "other" );
	}
});
