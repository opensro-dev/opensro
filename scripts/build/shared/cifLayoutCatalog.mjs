import { readdir } from "node:fs/promises";
import { compareText } from "./textOrder.mjs";

/**
 * Every shipped resinfo file is a runtime-addressable native resource. The
 * previous hand allowlist made the generated class catalog advertise 188
 * layouts that the public asset boundary returned as 404. Discover the source
 * directory instead, deterministically, so publication and catalog ownership
 * cannot drift apart.
 */
export async function discoverCifLayouts( resinfoDir ) {
	return (await readdir( resinfoDir ))
		.filter( ( fileName ) => fileName.toLowerCase().endsWith( ".txt" ) )
		.sort( ( left, right ) => compareText( left, right ) );
}
