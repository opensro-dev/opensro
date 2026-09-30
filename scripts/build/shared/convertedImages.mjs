/*
===========================================================================

convertedImages.mjs - publish images converted from the client extraction

scripts/convert_images.py writes each extracted DDJ/TGA as a PNG under
.generated/intermediate/images/<Extracted>/ (imageSourceRoot). A publisher
copies the ones it needs into .generated/client-public/assets/images/.
When a DDJ shares its stem with a sibling TGA, the converter names the DDJ's
PNG <stem>.ddj.png; it is published under the plain .png name the client
requests.

===========================================================================
*/
import { copyFile, mkdir, stat } from "node:fs/promises";
import path from "node:path";
import { imagePublicRoot, imageSourceRoot } from "../world/paths.mjs";

const IMAGE_PUBLIC_PREFIX = "/assets/images/";

/*
================
exists
================
*/
async function exists( file ) {
	return stat( file ).then( () => true, () => false );
}

/*
================
publishConvertedImage

Copy one converted image to its public path, for example
"/assets/images/Media_extracted/icon/buf_effect.png". A required image the
conversion did not produce is an error: publishing without it would leave
the client requesting a missing file.
================
*/
export async function publishConvertedImage( publicPath ) {
	if ( !publicPath.startsWith( IMAGE_PUBLIC_PREFIX ) || !publicPath.endsWith( ".png" ) ) {
		throw new Error( `Not a converted image path: ${publicPath}` );
	}
	const relative = publicPath.slice( IMAGE_PUBLIC_PREFIX.length );
	const plain = path.join( imageSourceRoot, relative );
	const collision = path.join( imageSourceRoot, relative.replace( /\.png$/, ".ddj.png" ) );
	const source = await exists( plain ) ? plain : await exists( collision ) ? collision : null;
	if ( !source ) {
		throw new Error( `Converted image missing: ${plain} (run the asset build first)` );
	}
	const target = path.join( imagePublicRoot, relative );
	await mkdir( path.dirname( target ), { recursive: true } );
	await copyFile( source, target );
	return publicPath;
}

/*
================
convertedImageFolder

The converted-image folder behind a public image folder, for publishers
that publish every image in it.
================
*/
export function convertedImageFolder( publicFolder ) {
	if ( !publicFolder.startsWith( IMAGE_PUBLIC_PREFIX ) ) throw new Error( `Not an image folder: ${publicFolder}` );
	return path.join( imageSourceRoot, publicFolder.slice( IMAGE_PUBLIC_PREFIX.length ) );
}
