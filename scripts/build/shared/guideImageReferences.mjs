// CIFPML image dependencies live in texthelp, outside resinfo DDJ fields.
// Discover every authored language/branch, rather than maintaining an allowlist.
export function collectGuideImageReferences( source ) {
	const references = new Set();
	for ( const match of source.matchAll( /<img\b[^>]*\bsrc\s*=\s*["']([^"']+\.ddj)["']/gi ) ) {
		const reference = match[1].replaceAll( "\\", "/" ).toLowerCase();
		if ( !/^(interface|icon)\/[a-z0-9_./-]+\.ddj$/.test( reference ) || reference.split( "/" ).includes( ".." ) ) {
			throw new Error( `Invalid guide image reference: ${reference}` );
		}
		references.add( reference );
	}
	return [ ...references ].sort();
}
