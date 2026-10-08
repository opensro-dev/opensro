import { loadDataAsset } from "./jmxAssetIO.mjs";

/**
 * Read an authored asset when the native caller treats an archive miss as a
 * normal null result. Only a missing file is downgraded; permission, I/O and
 * parse failures remain fatal at their owning boundary.
 */
export async function loadOptionalDataAsset( gamePath ) {
	try {
		return await loadDataAsset( gamePath );
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) return null;
		throw error;
	}
}
