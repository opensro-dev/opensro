/*
===========================================================================

creation-equipment.test.mjs - independent clothing and weapon selection

Checks the draft through its public actions. European protector indices are
relative to the weapon, so retaining an index is not retaining an armor type.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createCreation } = await import( "../../src/engine/runtime/frontend/creation/creation.ts" );
const { creationProtectors, creationRange } = await import( "../../src/engine/foundation/ui/character-create.ts" );

/*
================
createDraft
================
*/
function createDraft( race ) {
	/** @type {import("../../src/engine/contracts/assets.ts").AssetOwner} */
	const assets = {
		available: () => 0,
		request: () => 1,
		take: () => null,
		progress: () => null,
		health: () => ({ phase: "running" }),
		/*
================
cancel
================
		*/
		cancel() {},
		/*
================
install
================
		*/
		install() {},
		/*
================
dispose
================
		*/
		dispose() {}
	};
	const owner = createCreation( assets, "http://fixture.invalid", () => {} );
	owner.open( race );
	return owner;
}

test("changing weapons preserves every compatible clothing type for both races", () => {
	for ( const race of [ 0, 1 ] ) {
		const owner = createDraft( race );
		const maxWeapon = creationRange( defined( owner.snapshot() ).selection, "weapon" )[1];
		for ( let oldWeapon = 1; oldWeapon <= maxWeapon; oldWeapon++ ) {
			owner.action( "create:weapon:" + oldWeapon );
			const types = creationProtectors( defined( owner.snapshot() ).selection );
			for ( const [index, type] of types.entries() ) {
				for ( let nextWeapon = 1; nextWeapon <= maxWeapon; nextWeapon++ ) {
					owner.action( "create:weapon:" + oldWeapon );
					owner.action( "create:protector:" + (index + 1) );
					owner.action( "create:weapon:" + nextWeapon );
					const selection = defined( owner.snapshot() ).selection;
					const expected = creationProtectors( selection ).indexOf( type ) + 1;
					assert.equal(
						selection.protector,
						expected,
						race + ": " + oldWeapon + " -> " + nextWeapon + " " + type
					);
				}
			}
		}
		owner.dispose();
	}
});
