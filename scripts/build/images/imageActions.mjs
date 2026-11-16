/*
===========================================================================

imageActions.mjs - the source image conversion as content-addressed actions

The asset build's first lane (plan slice B2, sourceImages: 94-167 s per
build) converted every image whose PNG was older than its source. A
variant rebuild rewrites its sources with fresh times, so release 55e
reconverted all 25,629 images for byte-identical sources. Here every
image is an action (graph/action.mjs) keyed on its source bytes, the
converter's code closure, its kind, its output name and the toolchain that
writes the PNG (Python, Pillow, ffmpeg): a touched source is a hit, a
changed byte or a new Pillow a miss.

	1. convert_images.py --plan: every image, its output name and kind
	2. key each one; a hit is laid down from the store (graph/store.mjs)
	3. convert_images.py --convert-list: the misses, in one process pool,
	   and the manifest for every image
	4. the converted PNGs go into the store

Discovery, naming (the .ddj.png collision rule) and conversion stay the
converter's; this module decides only which images need it. One process
handles every miss, not one per image: a Python start costs more than a
conversion.

===========================================================================
*/
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { defineAction } from "../graph/action.mjs";
import { keyOf, materialize, openStore, readRecord, storeOutputs } from "../graph/store.mjs";
import { mapWithConcurrency } from "../shared/asyncUtils.mjs";
import { extractedRoot, imageSourceRoot, rebuildRoot } from "../world/paths.mjs";

export const CONVERT_IMAGES_SCRIPT = path.join( rebuildRoot, "scripts", "convert_images.py" );
const PLAN_FORMAT = "sro-image-plan";
// Keys, store reads and stores are file work, not CPU: a wide fan-out.
const KEY_CONCURRENCY = 64;

/*
================
imageAction

One image as an action. Nothing runs it alone: its misses convert
together (convertSourceImages).
================
*/
export function imageAction( asset, toolchain, roots ) {
	return defineAction( {
		name: `image:${asset.output}`,
		inputRoot: roots.extracted,
		inputs: [ asset.source ],
		outputRoot: roots.images,
		outputs: [ asset.output ],
		code: [ roots.script ],
		params: { kind: asset.kind, source: asset.source, toolchain },
		// The converter replaces each PNG whole (convert_images.convert_asset)
		// and nothing else writes the image tree, so the store and the tree
		// can share the bytes: 1.5 GB a tree, on a drive near full.
		link: true,
		run: async () => {
			throw new Error( `image ${asset.output} converts in its batch, never alone` );
		}
	} );
}

/*
================
convertSourceImages

The lane. spawn( args ) runs convert_images.py with args and resolves
{ status } (convertImagesRunner's contract). Returns { status, images,
hits, converted, placed }. A failed spawn returns its status and stores
nothing: a listed image whose conversion failed may still hold an older
PNG, which must never be stored under the new key.
================
*/
export async function convertSourceImages( spawn, options = {} ) {
	const roots = {
		extracted: options.extracted ?? extractedRoot,
		images: options.images ?? imageSourceRoot,
		script: options.script ?? CONVERT_IMAGES_SCRIPT
	};
	const store = options.store ?? (await openStore());
	const scratch = await mkdtemp( path.join( os.tmpdir(), "sro-image-actions-" ) );
	try {
		const planFile = path.join( scratch, "plan.json" );
		const planned = await spawn( [ "--plan", planFile ] );
		if ( planned.status !== 0 ) return { status: planned.status };
		const plan = JSON.parse( await readFile( planFile, "utf8" ) );
		if ( plan.format !== PLAN_FORMAT || !Array.isArray( plan.assets ) ) {
			throw new Error( `convert_images.py --plan wrote no ${PLAN_FORMAT} document` );
		}
		const actions = plan.assets.map( asset => imageAction( asset, plan.toolchain, roots ) );
		const keys = await mapWithConcurrency( actions, KEY_CONCURRENCY, action => keyOf( store, action, {} ) );
		let placed = 0;
		const misses = [];
		await mapWithConcurrency( actions, KEY_CONCURRENCY, async ( action, index ) => {
			// SRO_FORCE_IMAGE_CONVERT=1 (options.force) reconverts everything.
			const record = options.force ? undefined : await readRecord( store, keys[index] );
			// Await first, then add: `placed += await ...` reads placed before the
			// await, so concurrent hits lost counts from the summary.
			if ( record ) {
				const laid = await materialize( store, action, record );
				placed += laid;
			} else misses.push( index );
		} );
		misses.sort( ( a, b ) => a - b );
		const listFile = path.join( scratch, "convert.json" );
		await writeFile( listFile, JSON.stringify( misses.map( index => actions[index].outputs[0] ) ) );
		// Always run: the manifest covers every image, hit or miss.
		const converted = await spawn( [ "--convert-list", listFile ] );
		if ( converted.status !== 0 ) return { status: converted.status };
		await mapWithConcurrency(
			misses,
			KEY_CONCURRENCY,
			index => storeOutputs( store, actions[index], keys[index] )
		);
		const summary = {
			status: 0,
			images: actions.length,
			hits: actions.length - misses.length,
			converted: misses.length,
			placed
		};
		console.log(
			`[image-actions] ${summary.images} images: ${summary.hits} from the store (${placed} laid down), ` +
				`${summary.converted} converted`
		);
		return summary;
	} finally {
		await rm( scratch, { recursive: true, force: true } );
		if ( !options.store ) await store.shutdown();
	}
}
