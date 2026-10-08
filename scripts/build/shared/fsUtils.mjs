import { readdir, stat } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";

/** True when an ES module is the process entrypoint. */
export function isMainScript( importMetaUrl, argvPath = process.argv[1] ) {
	if ( !argvPath || argvPath === "-" ) return false;
	return importMetaUrl === pathToFileURL( path.resolve( argvPath ) ).href;
}

/**
 * Recursively list files below `root`.
 *
 * `extensions` is a case-insensitive suffix allowlist. `filter` receives the
 * absolute file path and its Dirent. Callers opt into missing-root tolerance
 * explicitly; required asset trees still fail loudly by default.
 *
 * @param {string} root
 * @param {{
 *   extensions?: string[],
 *   filter?: (filePath: string, entry: import("node:fs").Dirent) => boolean,
 *   missing?: "throw" | "empty",
 *   sort?: boolean
 * }} [options]
 * @returns {Promise<string[]>}
 */
export async function listFiles(
	root,
	{ extensions, filter, missing = "throw", sort = false } = {}
) {
	const suffixes = extensions?.map( ( extension ) => String( extension ).toLowerCase() );
	const files = [];

	async function walk( directory ) {
		let entries;
		try {
			entries = await readdir( directory, { withFileTypes: true } );
		} catch ( error ) {
			if ( error?.code === "ENOENT" && missing === "empty" ) return;
			throw error;
		}

		for ( const entry of entries ) {
			const fullPath = path.join( directory, entry.name );
			if ( entry.isDirectory() ) {
				await walk( fullPath );
			} else if (
				entry.isFile() &&
				(!suffixes || suffixes.some( ( suffix ) => entry.name.toLowerCase().endsWith( suffix ) )) &&
				(!filter || filter( fullPath, entry ))
			) {
				files.push( fullPath );
			}
		}
	}

	await walk( root );
	return sort ? files.sort( ( left, right ) => left.localeCompare( right ) ) : files;
}

/** Return false only for a missing path; surface permission and I/O failures. */
export async function pathExists( targetPath ) {
	try {
		await stat( targetPath );
		return true;
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) {
			return false;
		}
		throw error;
	}
}
