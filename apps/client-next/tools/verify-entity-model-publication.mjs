import { CLIENT_PUBLIC_ROOT } from "../../../scripts/lib/generatedRoot.mjs";
import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";
import { readPackedAssetBytesSync, readPublishedAssetBytesSync } from "../../../scripts/lib/publishedAsset.mjs";
import { npcManifestModels } from "../../../scripts/build/shared/npcManifest.mjs";
const root = path.resolve( import.meta.dirname, ".." ), publicRoot = CLIENT_PUBLIC_ROOT;
const output = path.join( root, "temp/artifacts/bsr-parity" );
fs.mkdirSync( output, { recursive: true } );
const packed = process.argv.includes( "--packed" );
const read = url => {
	const bytes = (packed ? readPackedAssetBytesSync : readPublishedAssetBytesSync)( url, publicRoot );
	const loose = path.join( publicRoot, url );
	if ( packed && fs.existsSync( loose ) && !bytes.equals( fs.readFileSync( loose ) ) ) {
		throw Error( "Packed delivery differs from rebuilt asset: " + url );
	}
	return bytes;
};
const hash = bytes => createHash( "sha256" ).update( bytes ).digest( "hex" );
const outfile = path.join( output, "publication-decoder.mjs" );
await build( {
	entryPoints: [ path.join( root, "src/engine/runtime/assets/worker/model/model.ts" ) ],
	outfile,
	bundle: true,
	platform: "node",
	format: "esm"
} );
const { createModelDecoder } = await import( pathToFileURL( outfile ) ), decoder = createModelDecoder();
const models = new Map(), vats = new Set(), issues = [];
let bindings = 0;
const catalog = JSON.parse( read( "/assets/npc/animation-catalog.json" ) );
for ( const domain of [ "npc", "itemdrop" ] ) {
	const manifest = JSON.parse( read( `/assets/${domain}/manifest.json` ) );
	for ( const [key, row] of Object.entries( npcManifestModels( manifest ) ) ) {
		try {
			if ( domain === "npc" ) {
				if ( Number.isInteger( row.refObjId ) && catalog.models[key]?.refObjId !== row.refObjId ) {
					throw Error( "Animation catalog lost model identity" );
				}
				const resource = catalog.resources[row.bsr];
				if (
					!resource || resource.glb !== row.glb ||
					row.clips.some( name => !resource.animations.some( a => a.role === name ) )
				) throw Error( "Animation catalog lost resource clips" );
			}
			for ( const url of [ row.glb, ...Object.values( row.materialVariants ?? {} ) ] ) {
				if ( !models.has( url ) ) {
					const bytes = read( url ), model = decoder.character( decoder.decode( bytes ) );
					for ( const clip of model.clips ) {
						for ( const channel of clip.channels ) {
							for ( const data of [ channel.times, channel.values ] ) {
								if (
									data.some( n => !Number.isFinite( n ) )
								) throw Error( "Non-finite animation channel " + clip.name );
							}
						}
					}
					models.set( url, {
						sha256: hash( bytes ),
						bytes: bytes.length,
						clips: model.clips.map( c => c.name )
					} );
				}
				for ( const binding of row.animationBindings ?? [] ) {
					if ( binding.clip ) {
						bindings++;
						if ( !models.get( url ).clips.includes( binding.clip ) ) {
							throw Error( "Bound clip absent from delivered GLB: " + binding.clip );
						}
					}
				}
			}
			if ( row.vat && !vats.has( row.vat.manifest ) ) {
				const vat = JSON.parse( read( row.vat.manifest ) ),
					bin = read( row.vat.bin ),
					model = models.get( row.glb );
				if (
					vat.source?.glb !== row.glb || vat.source?.sha256 !== model.sha256 ||
					vat.source?.byteLength !== model.bytes
				) throw Error( "VAT source identity is stale" );
				if (
					vat.bin?.path !== row.vat.bin || vat.bin?.sha256 !== hash( bin ) ||
					vat.bin?.byteLength !== bin.length
				) throw Error( "VAT binary identity mismatch" );
				vats.add( row.vat.manifest );
			}
		} catch ( error ) {
			issues.push( { domain, key, error: String( error ) } );
		}
	}
}
const report = {
	delivery: packed ? "packed" : "loose-preferred",
	models: models.size,
	vats: vats.size,
	bindings,
	issues
};
fs.writeFileSync(
	path.join( output, `model-publication-${packed ? "packed" : "loose"}.json` ),
	JSON.stringify( report, null, 2 ) + "\n"
);
console.log( JSON.stringify( report, null, 2 ) );
if ( issues.length ) process.exitCode = 1;
