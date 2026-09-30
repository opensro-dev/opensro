/*
===========================================================================

character-assets.mjs - browser fixtures load through the production asset owner

Tests borrow the owner's request slots and receive the same model/image
contract as the runtime. They do not duplicate PNG or native mip decoding.

===========================================================================
*/
import { assetRequestBudget } from "../../../src/engine/foundation/assets/asset-budget.ts";

const LOAD_DEADLINE_MS = 15000;

/*
================
loadCharacter

Return ownership of one decoded result. A timed-out request is cancelled
before the fixture returns so it cannot consume another test's capacity.
================
*/
/** @param {import("../../../src/engine/contracts/assets").AssetOwner} assets */
export async function loadCharacter( assets, path ) {
	const id = assets.request( new URL( path, location.origin ).href, assetRequestBudget( "character" ), "character" );
	const deadline = performance.now() + LOAD_DEADLINE_MS;
	try {
		while ( performance.now() < deadline ) {
			const result = assets.take( id );
			if ( result?.kind === "character" ) return result;
			if ( result?.kind === "error" ) throw Error( result.error );
			if ( result ) throw Error( "Unexpected character fixture result" );
			await new Promise( requestAnimationFrame );
		}
		throw Error( "Character fixture deadline: " + path );
	} finally {
		assets.cancel( id );
	}
}
