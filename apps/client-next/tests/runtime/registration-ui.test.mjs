/*
===========================================================================

registration-ui.test.mjs - signup through the production title controls

The real UI and retail assets prove button routing, username/password editors,
Enter submission, credential clearing and the return to login after success.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";

/*
================
loginFixture
================
*/
function loginFixture() {
	const commands = [], fixture = uiFixture( command => commands.push( command ) );
	const state = {
		...fixture.state,
		worldReady: false,
		frontend: { phase: "login", generation: 1, elapsed: 1000, alpha: 1, logoAlpha: 0, error: null },
		session: {
			phase: "signed-out",
			revision: 1,
			servers: [],
			registration:
				/** @type {import("../../src/engine/contracts/session").SessionState["registration"]} */ (undefined)
		}
	};
	let now = 1000, shown;
	/*
	================
	step
	================
	*/
	function step() {
		shown = fixture.ui.step( state, now += 100 ) ?? shown;
		assert.ok( shown );
		return shown;
	}
	/*
	================
	event
	================
	*/
	function event( value ) {
		fixture.ui.event( value );
		return step();
	}
	step();
	state.session.revision++;
	step();
	return { ...fixture, state, commands, step, event };
}

test("Register opens signup, Enter submits once and success returns to login", t => {
	const fixture = loginFixture();
	t.after( () => fixture.dispose() );
	let shown = fixture.step();
	assert.equal( shown.controls.find( control => control.id === "register-open" )?.label, "Register" );
	shown = fixture.event( { kind: "activate", id: "register-open" } );
	assert.ok( shown.controls.some( control => control.id === "registration-username" ) );
	assert.ok(
		shown.controls.some( control => control.id === "registration-confirm-password" && control.kind === "password" )
	);
	assert.ok(
		shown.controls.some( control => control.id === "registration-password" && control.kind === "password" )
	);
	assert.equal( shown.controls.some( control => control.id === "native:servers" ), false );
	fixture.event( {
		kind: "edit",
		id: "registration-username",
		value: "PlayerOne",
		start: 18,
		end: 18,
		composing: false
	} );
	fixture.event( {
		kind: "edit",
		id: "registration-password",
		value: "password123",
		start: 11,
		end: 11,
		composing: false
	} );
	fixture.event( {
		kind: "edit",
		id: "registration-confirm-password",
		value: "password123",
		start: 11,
		end: 11,
		composing: false
	} );
	shown = fixture.event( { kind: "activate", id: "submit" } );
	assert.deepEqual( fixture.commands.at( -1 ), {
		kind: "register",
		apiBase: "https://fixture.invalid/",
		username: "PlayerOne",
		password: "password123"
	} );
	assert.equal( shown.controls.find( control => control.id === "registration-password" )?.value, "" );
	assert.equal( shown.controls.find( control => control.id === "registration-confirm-password" )?.value, "" );
	fixture.event( { kind: "activate", id: "register-submit" } );
	assert.equal( fixture.commands.filter( command => command.kind === "register" ).length, 1 );
	fixture.state.session = {
		...fixture.state.session,
		revision: 3,
		registration: { status: "succeeded", message: "Account created. Log in with your username and password." }
	};
	shown = fixture.step();
	assert.equal( shown.controls.find( control => control.id === "account" )?.value, "PlayerOne" );
	assert.equal( shown.controls.find( control => control.id === "password" )?.value, "" );
	assert.equal( shown.controls.some( control => control.id === "register-submit" ), false );
	assert.ok( shown.controls.some( control => control.id === "login" ) );
});

test("Back to login clears the signup password without sending a request", t => {
	const fixture = loginFixture();
	t.after( () => fixture.dispose() );
	fixture.event( { kind: "activate", id: "register-open" } );
	fixture.event( {
		kind: "edit",
		id: "registration-password",
		value: "password123",
		start: 11,
		end: 11,
		composing: false
	} );
	const shown = fixture.event( { kind: "activate", id: "register-back" } );
	assert.equal( shown.controls.find( control => control.id === "password" )?.value, "" );
	assert.equal( fixture.commands.some( command => command.kind === "register" ), false );
});

test("the native preference hides registration on the login screen", t => {
	const fixture = loginFixture();
	t.after( () => fixture.dispose() );
	const shown = fixture.event( { kind: "experimental-preferences", value: { registration: false } } );
	assert.equal( shown.controls.some( control => control.id === "register-open" ), false );
	assert.ok( shown.controls.some( control => control.id === "login" ) );
});

test("signup requires matching passwords and shows the password requirements", t => {
	const fixture = loginFixture();
	t.after( () => fixture.dispose() );
	fixture.event( { kind: "activate", id: "register-open" } );
	assert.ok( fixture.hasText( "Use at least 8 characters (maximum 72 UTF-8 bytes)." ) );
	assert.ok( fixture.hasText( "Password and confirm password must match." ) );
	fixture.event( {
		kind: "edit",
		id: "registration-username",
		value: "PlayerOne",
		start: 9,
		end: 9,
		composing: false
	} );
	fixture.event( {
		kind: "edit",
		id: "registration-password",
		value: "password123",
		start: 11,
		end: 11,
		composing: false
	} );
	fixture.event( {
		kind: "edit",
		id: "registration-confirm-password",
		value: "different123",
		start: 12,
		end: 12,
		composing: false
	} );
	const mismatch = fixture.event( { kind: "activate", id: "submit" } );
	assert.equal( fixture.commands.some( command => command.kind === "register" ), false );
	assert.equal( mismatch.message, "Passwords do not match." );
	fixture.event( { kind: "edit", id: "registration-password", value: "short", start: 5, end: 5, composing: false } );
	fixture.event( {
		kind: "edit",
		id: "registration-confirm-password",
		value: "short",
		start: 5,
		end: 5,
		composing: false
	} );
	fixture.event( { kind: "activate", id: "submit" } );
	assert.equal( fixture.commands.some( command => command.kind === "register" ), false );
});
