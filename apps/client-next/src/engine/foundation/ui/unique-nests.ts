/*
===========================================================================

unique-nests.ts - the world map's unique nest signs (port-only)

Port-only, not native: the v1.150 world map draws no unique nest. The
build projects the server population's unique anchors
(scripts/build/data/buildUniqueNestsAsset.mjs) and the M window draws one
sign per anchor, so a unique with several candidate nests shows them all.
A sign is a place, never a live unique: no hover, no alive or dead state,
no timer.

===========================================================================
*/
import type { MapMarker } from "./world-map";

export const UNIQUE_NESTS_PATH = "/assets/data/uniqueNests.json";
// The shipped table is about 5 KiB; the cap (256 KiB) only bounds a
// corrupt answer.
export const UNIQUE_NESTS_BYTES_LIMIT = 262144;
// The minimap's native unique sign (+0x3b0, drawn for rarity 3).
export const UNIQUE_NEST_SIGN = "/assets/images/Media_extracted/interface/minimap/mm_sign_unique.png";
const UNIQUE_NESTS_FORMAT = "sro-unique-nests";
const UNIQUE_NESTS_VERSION = 1;
const MAX_UNIQUE_NESTS = 4096;
const MAX_REGION_ID = 0xffff;

/*
================
decodeUniqueNests

The published nests as world map markers, in table order. Any malformed
row rejects the whole table: a partial table would hide nests silently.
================
*/
export function decodeUniqueNests( raw: unknown ): readonly MapMarker[] {
	const data = raw as { format?: unknown; version?: unknown; nests?: unknown; } | null;
	if (
		!data || data.format !== UNIQUE_NESTS_FORMAT || data.version !== UNIQUE_NESTS_VERSION ||
		!Array.isArray( data.nests ) || data.nests.length > MAX_UNIQUE_NESTS
	) {
		throw Error( "Invalid unique nest table" );
	}
	return data.nests.map( ( row: unknown ) => {
		const nest = row as { regionId?: unknown; x?: unknown; z?: unknown; } | null;
		const regionId = nest?.regionId, x = nest?.x, z = nest?.z;
		if (
			typeof regionId !== "number" || !Number.isInteger( regionId ) || regionId < 0 ||
			regionId > MAX_REGION_ID || typeof x !== "number" || !Number.isFinite( x ) ||
			typeof z !== "number" || !Number.isFinite( z )
		) {
			throw Error( "Invalid unique nest" );
		}
		return { regionId, x, z, rotation: 0, path: UNIQUE_NEST_SIGN };
	} );
}
