/*
===========================================================================
catalog-client.mjs - shared fresh catalogue and identity fetch for QA clients

The workbench and probes use the same endpoint and reject failed responses.
===========================================================================
*/
import { UI_QA_ORIGIN, UI_QA_CATALOG_PATH } from "./settings.mjs";

/*
================
isRecord
================
*/
/** @param {unknown} value @returns {value is Record<string, unknown>} */
function isRecord( value ) {
	return value !== null && typeof value === "object" && !Array.isArray( value );
}

/*
================
isCatalog

Validate the fields used by navigation and saved identity before trusting JSON.
================
*/
/** @param {unknown} value @returns {value is import("./catalog-types").Catalog} */
function isCatalog( value ) {
	if ( !isRecord( value ) || !isRecord( value.identity ) || !isRecord( value.roots ) ) return false;
	const identity = value.identity;
	if ( ![ "codeSha", "sourceHash", "assetHash", "fingerprint" ].every( key => typeof identity[key] === "string" ) ) {
		return false;
	}
	if ( ![ "defineHash", "fontHash" ].every( key => identity[key] === null || typeof identity[key] === "string" ) ) {
		return false;
	}
	if ( typeof identity.dirty !== "boolean" || typeof value.limitations !== "string" ) return false;
	if (
		!Object.values( value.roots ).every( root => typeof root === "string" ) || !Array.isArray( value.resources )
	) {
		return false;
	}
	return value.resources.every( resource => {
		if ( !isRecord( resource ) || typeof resource.name !== "string" ) return false;
		if (
			![ "nativeHash", "publishedHash", "url" ].every( key =>
				resource[key] === null || typeof resource[key] === "string"
			)
		) {
			return false;
		}
		if ( !Array.isArray( resource.references ) || !resource.references.every( row => typeof row === "string" ) ) {
			return false;
		}
		if ( !Array.isArray( resource.sections ) ) return false;
		return resource.sections.every( section => {
			if ( !isRecord( section ) || typeof section.name !== "string" || !Array.isArray( section.nodes ) ) {
				return false;
			}
			return section.nodes.every( node =>
				isRecord( node ) && typeof node.id === "number" &&
				typeof node.name === "string" && typeof node.type === "string" && typeof node.occurrence === "number" &&
				isRecord( node.rect ) && typeof node.rect.x === "number" && typeof node.rect.y === "number" &&
				typeof node.rect.width === "number" && typeof node.rect.height === "number"
			);
		} );
	} );
}

/*
================
fetchCatalog
================
*/
/** @param {typeof fetch} [request] @returns {Promise<import("./catalog-types").Catalog>} */
export async function fetchCatalog( request = fetch ) {
	const response = await request( UI_QA_ORIGIN + UI_QA_CATALOG_PATH, { cache: "no-store" } );
	if ( !response.ok ) throw Error( `UI QA catalogue request failed: ${response.status}` );
	/** @type {unknown} */
	const value = await response.json();
	if ( !isCatalog( value ) ) {
		throw Error( "Invalid UI QA catalogue or identity" );
	}
	return value;
}

/*
================
fetchCatalogIdentity
================
*/
/** @param {typeof fetch} [request] @returns {Promise<import("./catalog-types").Catalog["identity"]>} */
export async function fetchCatalogIdentity( request = fetch ) {
	return (await fetchCatalog( request )).identity;
}
