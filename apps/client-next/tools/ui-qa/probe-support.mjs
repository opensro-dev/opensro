/*
===========================================================================
probe-support.mjs - request isolation and repository-root probe artifacts

The guard prevents external HTTP requests from pages, frames and workers in
the owned browser context. Its blocked list records attempts, not requests sent.
===========================================================================
*/
import path from "node:path";
import { fileURLToPath } from "node:url";
import { UI_QA_ORIGIN } from "./settings.mjs";

export const UI_QA_REPOSITORY_ROOT = fileURLToPath( new URL( "../../../../", import.meta.url ) );

/*
================
artifactDirectory
================
*/
/** @param {string} [name] */
export function artifactDirectory( name = "" ) {
	const root = path.join( UI_QA_REPOSITORY_ROOT, "temp/artifacts/ui-qa" );
	const target = path.resolve( root, name );
	if ( target !== root && !target.startsWith( root + path.sep ) ) throw Error( "Artifact path leaves UI QA root" );
	return target;
}

/*
================
isLocalReviewRequest

Compare parsed origins: a prefix match could admit another port or host.
================
*/
/** @param {string} url */
export function isLocalReviewRequest( url ) {
	try {
		return new URL( url ).origin === UI_QA_ORIGIN;
	} catch {
		return false;
	}
}

/*
================
guardReviewRequests

Install before navigation. Routing disables the HTTP cache, so every probe
uses the same isolation policy and real workers still load local assets.
================
*/
/**
 * @param {{ route(pattern: string, handler: (route: {
 * request(): { url(): string }, continue(): Promise<void>, abort(reason: string): Promise<void>
 * }) => Promise<void>): Promise<void> }} context
 * @param {string[]} blocked
 */
export async function guardReviewRequests( context, blocked ) {
	await context.route( "**/*", async route => {
		const url = route.request().url();
		if ( isLocalReviewRequest( url ) ) return route.continue();
		blocked.push( url );
		await route.abort( "blockedbyclient" );
	} );
}
