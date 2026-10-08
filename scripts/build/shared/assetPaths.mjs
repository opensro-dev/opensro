import path from "node:path";

/**
 * Native asset-path case folding: ASCII A-Z only. The client's paths are
 * CP949 byte strings and its pk2 lookup folds only ASCII letters; Unicode
 * toLowerCase() would also fold Latin-1/Hangul-adjacent code points and, on a
 * CP949 name carried as Latin-1, rewrite bytes (0xC1 'Á' -> 0xE1 'á'). That
 * turned `중심벽.ddj` into the unresolvable `áß½éº®.png` (2026-09-23).
 */
export function foldAssetCase( value ) {
	return String( value ).replace( /[A-Z]+/g, ( letters ) => letters.toLowerCase() );
}

/** Canonical case-insensitive native asset identity below an extracted root. */
export function normalizeAssetPath( value ) {
	return foldAssetCase(
		String( value ?? "" )
			.trim()
			.replaceAll( "\\", "/" )
			.replace( /\/+/g, "/" )
			.replace( /^\/+/, "" )
	);
}

/** Canonical browser-public path: forward slashes and exactly one leading slash. */
export function normalizePublicPath( value ) {
	return `/${String( value ).replaceAll( "\\", "/" ).replace( /^\/+/, "" )}`.replace( /\/{2,}/g, "/" );
}

/** Asset-pack boundary: public paths outside /assets are never admissible. */
export function normalizePublicAssetPath( value ) {
	const normalized = normalizePublicPath( value );
	if ( !normalized.startsWith( "/assets/" ) ) {
		throw new Error( `Asset packs only accept public /assets paths, got ${value}` );
	}
	return normalized;
}

/** Convert a disk path below a public root to a browser-facing path. */
export function toPublicPath( filePath, publicRoot, options = {} ) {
	const relative = path.relative( publicRoot, filePath ).replaceAll( "\\", "/" );
	return options.leadingSlash === false ? relative : `/${relative}`;
}

/**
 * Refuse a target outside root (or root itself): every generated-tree write or
 * removal names the root it must stay below.
 */
export function assertInsideRoot( root, target, label ) {
	const relative = path.relative( path.resolve( root ), path.resolve( target ) );
	if ( !relative || relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
		throw new Error( `${label} must stay below ${root}, got ${target}` );
	}
}

/** Resolve a public /assets path to its file below publicRoot, refusing escapes. */
export function containedPublicFile( publicRoot, publicPath ) {
	const file = path.resolve( publicRoot, normalizePublicAssetPath( publicPath ).slice( 1 ) );
	assertInsideRoot( publicRoot, file, `public path ${publicPath}` );
	return file;
}

/** Resolve a browser-public path below a caller-selected public root. */
export function publicPathToFile( publicPath, publicRoot ) {
	return path.join( publicRoot, ...normalizePublicPath( publicPath ).slice( 1 ).split( "/" ) );
}

/**
 * Map an extracted DDJ-style image reference to its browser PNG path.
 * Relative extracted paths are case-folded like the game asset resolver while
 * the on-disk extracted-folder name keeps its published casing.
 */
export function toPublicImagePath(
	extractedSubfolder,
	relativePath,
	{ basename = false, replaceExtension = true } = {}
) {
	const folder = String( extractedSubfolder )
		.replaceAll( "\\", "/" )
		.replace( /^\/+|\/+$/g, "" );
	let imagePath = foldAssetCase(
		String( relativePath )
			.replaceAll( "\\", "/" )
			.replace( /\/+/g, "/" )
			.replace( /^\/+/, "" )
	);
	if ( basename ) imagePath = imagePath.split( "/" ).at( -1 ) ?? "";
	if ( replaceExtension ) imagePath = imagePath.replace( /\.[^/.]+$/, ".png" );
	return normalizePublicPath( `/assets/images/${folder}/${imagePath}` );
}
