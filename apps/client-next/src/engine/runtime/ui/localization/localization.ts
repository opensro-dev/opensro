/*
===========================================================================

localization.ts - localized display names and bounded retries

Owns one text catalogue request and its decoded entries. Visibility gates
new requests and retries, while each frame collects outstanding results.
Text loading never modifies skill eligibility.

===========================================================================
*/

import type { AssetOwner } from "@/engine/contracts/assets";

const MAX_ATTEMPTS = 6;
const RETRY_BASE_MS = 1000;
const RETRY_MAX_MS = 30000;
const CATALOGUE_MAX_BYTES = 8 << 20;

/*
================
createLocalization
================
*/
export function createLocalization(
	assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">,
	base: string
) {
	let request: number | null = null,
		entries: Record<string, string> | null = null,
		retryAt = 0,
		failures = 0,
		disposed = false;
	/*
	================
	retryAfterFailure

	Decode and submission failures share the same bounded retry schedule.
	The frame pump decides whether demand permits the next attempt.
	================
	*/
	function retryAfterFailure( now: number ) {
		failures++;
		retryAt = now + Math.min( RETRY_MAX_MS, RETRY_BASE_MS * 2 ** failures );
	}

	return {
		/*
		================
		step

		Demand gates admission only. Always collect previously admitted work.
		================
		*/
		step( now: number, needed = true ) {
			if ( disposed ) return false;
			if ( request !== null ) {
				const result = assets.take( request );
				if ( result ) {
					request = null;
					try {
						if ( result.kind !== "bytes" ) throw Error( "Text resource unavailable" );
						const value = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) );
						if (
							!value.entries || typeof value.entries !== "object" || Array.isArray( value.entries ) ||
							Object.values( value.entries ).some( v => typeof v !== "string" )
						) throw Error( "Invalid text catalogue" );
						entries = value.entries;
						return true;
					} catch {
						retryAfterFailure( now );
					}
				}
			}
			if (
				needed && !entries && request === null && failures < MAX_ATTEMPTS && now >= retryAt &&
				assets.available() > 0
			) {
				try {
					request = assets.request(
						new URL( "/assets/text/textdataname.en.json", base ).href,
						CATALOGUE_MAX_BYTES
					);
				} catch {
					retryAfterFailure( now );
				}
			}
			return false;
		},
		/*
		================
		text
		================
		*/
		text( symbol: string | undefined, fallback: string ) {
			return symbol && entries && Object.hasOwn( entries, symbol ) ? entries[symbol]! : fallback;
		},
		/*
		================
		dispose

		Release owned children and pending work before discarding local state.
		================
		*/
		dispose() {
			disposed = true;
			if ( request !== null ) assets.cancel( request );
			request = null;
			entries = null;
		}
	};
}
