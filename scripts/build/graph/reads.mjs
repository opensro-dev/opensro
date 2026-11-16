/*
===========================================================================

reads.mjs - the files one build step read, discovered while it runs

Some steps cannot name their inputs before they run: a character model
reads its BSR, then the skeleton, meshes, animations, material sets and
textures that BSR lists. Such a step runs inside trackReads, and the
readers it goes through (jmxAssetIO.mjs) call noteRead with every file
they open or probe. The action store records those files with their
digests (store.mjs storeOutputs) and a later build reuses the outputs
only while every one of them still holds those bytes (readRecord), the
way a compiler's depfile works.

The scope is an AsyncLocalStorage, so steps running at once each collect
their own reads. Outside a scope noteRead does nothing: a reader shared
with untracked steps costs one lookup.

A probe counts as a read. A step that chose a file because another was
missing (convertedTexturePath's two names) depends on that absence, and
the record keeps the missing file as the "missing" digest
(action.mjs MISSING_DIGEST), so its later appearance misses.

===========================================================================
*/
import { AsyncLocalStorage } from "node:async_hooks";
import path from "node:path";

const scope = new AsyncLocalStorage();

/*
================
trackReads

Runs step() and returns { result, reads }: every file noted while it ran,
as absolute paths. Nested scopes each see their own reads only.
================
*/
export async function trackReads( step ) {
	const reads = new Set();
	const result = await scope.run( reads, step );
	return { result, reads };
}

/*
================
noteRead

Called by a reader with a file it opened or probed.
================
*/
export function noteRead( file ) {
	scope.getStore()?.add( path.resolve( file ) );
}
